// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

// Package ascbridge is the git bridge of a repository stored in asc (SPEC section 13, PLAN unit K13).
//
// At Stage 4 no bare git directory is the truth of a storage_backend=asc repository. Legacy git consumers
// (git clone over HTTP and SSH, actions/checkout, push mirrors, external submodules) still need one, so
// Forgejo keeps a git directory per repository, the bridge, at the path it always used (Path). Everything
// that hands a repository path to git goes through Path and Ensure, never through repo.RepoPath() directly.
//
// # Modes ([asc] BRIDGE)
//
//   - eager: asc's outbox keeps the bridge current (bridge_promote). Ensure only makes sure the directory exists.
//   - lazy: Forgejo builds the bridge on the first legacy request with `asc export --git <bridge>` and rebuilds
//     it when the refs in asc have moved. Ensure compares a fingerprint of asc's refs with the one the last
//     build recorded, and exports under a per-repository lock when they differ.
//   - off: git access to asc repositories through Forgejo is refused (ErrOff).
//
// # Lifecycle
//
// The asc Writer (modules/repostore/asc) calls Lifecycle after Init, Fork, Delete and Rename, so the bridge
// directory exists as soon as the repository does and goes away with it (see Init).
//
// # Limits
//
// A bridge is read by git; nothing here writes a ref in it. Pushes into the bridge by legacy clients are the
// Stage 2 flow (pre-receive imports them, services/ascsync) and mirrors pulled into it are imported by
// SyncFromBridge.
package ascbridge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/util"
)

// Named limits. Each bounds time, memory or message volume that one input can cost.
const (
	// MarkerName is the file in the bridge that records what the last build exported.
	MarkerName = "asc-bridge.json"
	// RefPage is the page size asked of ListRefs.
	RefPage = 1000
	// MaxRefs bounds the refs one fingerprint or sync reads.
	MaxRefs = 500_000
	// CallTimeout bounds one RPC.
	CallTimeout = 30 * time.Second
	// MapBatch is the number of ids per GitMap call (the server's MAX_LIST).
	MapBatch = 4096
	// MaxMarkerBytes bounds the marker file that is read back.
	MaxMarkerBytes = 4096
)

var (
	// ErrDisabled is returned for a storage_backend=asc repository while [asc] ENABLED is false. The
	// repository is not served from a stale directory instead.
	ErrDisabled = errors.New("ascbridge: the repository is stored in asc but [asc] is not enabled")
	// ErrOff is returned when [asc] BRIDGE = off: legacy git access to asc repositories is refused.
	ErrOff = errors.New("ascbridge: git access to this repository is off ([asc] BRIDGE = off)")
	// ErrBadAnswer marks an asc answer that failed a check. Nothing from it is used.
	ErrBadAnswer = errors.New("ascbridge: the asc server sent an answer that failed verification")
)

// Mode returns [asc] BRIDGE.
func Mode() string {
	if setting.Asc.Bridge == "" {
		return setting.AscBridgeEager
	}
	return setting.Asc.Bridge
}

// IsAsc reports whether repo is stored in asc. An unknown storage_backend is an error: such a repository is
// never treated as plain git.
func IsAsc(repo *repo_model.Repository) (bool, error) {
	if repo == nil {
		return false, errors.New("ascbridge: no repository")
	}
	sb, err := gitrepo.StorageBackendOf(repo)
	if err != nil {
		return false, err
	}
	return sb == gitrepo.StorageBackendAsc, nil
}

// Path is bridgePath(repo): the git directory legacy consumers read the repository from. It is the
// directory the repository always had, so a repository that moves between git and asc needs no path change.
func Path(repo *repo_model.Repository) string { return repo.RepoPath() }

// WikiPath is the bridge of the wiki, which is a repository of its own in asc.
func WikiPath(repo *repo_model.Repository) string { return repo.WikiPath() }

// ---------------------------------------------------------------------------------- client

var (
	apiMu  sync.Mutex
	apiVal ascv1.RepoClient

	// newAPI builds the Repo client. Tests replace it through UseClient.
	newAPI = func() (ascv1.RepoClient, error) {
		if setting.InternalToken == "" {
			return nil, errors.New("ascbridge: the internal token is not set")
		}
		c, err := ascclient.New(ascclient.Config{Target: setting.Asc.ServerURL, InternalToken: setting.InternalToken, CallTimeout: CallTimeout})
		if err != nil {
			return nil, err
		}
		return c.Repo(), nil
	}
)

// UseClient replaces the Repo client until the returned function is called. It is for tests.
func UseClient(api ascv1.RepoClient) (restore func()) {
	apiMu.Lock()
	defer apiMu.Unlock()
	prev := apiVal
	apiVal = api
	return func() {
		apiMu.Lock()
		defer apiMu.Unlock()
		apiVal = prev
	}
}

func repoAPI() (ascv1.RepoClient, error) {
	apiMu.Lock()
	defer apiMu.Unlock()
	if apiVal == nil {
		api, err := newAPI()
		if err != nil {
			return nil, fmt.Errorf("ascbridge: asc client: %w", err)
		}
		apiVal = api
	}
	return apiVal, nil
}

// ---------------------------------------------------------------------------------- build

// Exporter writes the history of an asc repository into a git directory (`asc export --git`).
type Exporter interface {
	Export(ctx context.Context, repoName, bridgeDir string) error
}

var (
	exporterMu  sync.Mutex
	exporterVal Exporter
)

// UseExporter replaces the Exporter until the returned function is called. It is for tests.
func UseExporter(e Exporter) (restore func()) {
	exporterMu.Lock()
	defer exporterMu.Unlock()
	prev := exporterVal
	exporterVal = e
	return func() {
		exporterMu.Lock()
		defer exporterMu.Unlock()
		exporterVal = prev
	}
}

func exporter() Exporter {
	exporterMu.Lock()
	defer exporterMu.Unlock()
	if exporterVal == nil {
		exporterVal = NewCLIExporter()
	}
	return exporterVal
}

// locks serialises builds of one bridge inside this process: a request-scoped lock, so that concurrent first
// requests wait for one export instead of starting several.
var locks = newKeyedMutex()

type keyedMutex struct {
	mu sync.Mutex
	m  map[string]*entry
}

type entry struct {
	mu   sync.Mutex
	refs int
}

func newKeyedMutex() *keyedMutex { return &keyedMutex{m: map[string]*entry{}} }

func (k *keyedMutex) lock(key string) (unlock func()) {
	k.mu.Lock()
	e := k.m[key]
	if e == nil {
		e = &entry{}
		k.m[key] = e
	}
	e.refs++
	k.mu.Unlock()
	e.mu.Lock()
	return func() {
		e.mu.Unlock()
		k.mu.Lock()
		if e.refs--; e.refs == 0 {
			delete(k.m, key)
		}
		k.mu.Unlock()
	}
}

// Ensure makes the bridge of repo (or of its wiki) readable by git and returns its path. For a repository
// that is not stored in asc it returns the repository path and does nothing else.
func Ensure(ctx context.Context, repo *repo_model.Repository, wiki bool) (string, error) {
	path := Path(repo)
	if wiki {
		path = WikiPath(repo)
	}
	isAsc, err := IsAsc(repo)
	if err != nil {
		return "", err
	}
	if !isAsc {
		return path, nil
	}
	if !setting.Asc.Enabled {
		return "", ErrDisabled
	}
	switch Mode() {
	case setting.AscBridgeOff:
		return "", ErrOff
	case setting.AscBridgeEager:
		return path, ensureDir(ctx, path, repo.DefaultBranch)
	}

	name, err := ascName(path)
	if err != nil {
		return "", err
	}
	defer locks.lock(path)()
	api, err := repoAPI()
	if err != nil {
		return "", err
	}
	fp, err := fingerprint(ctx, api, name)
	if err != nil {
		return "", err
	}
	if readMarker(path) == fp {
		return path, nil
	}
	if err := ensureDir(ctx, path, repo.DefaultBranch); err != nil {
		return "", err
	}
	if err := exporter().Export(ctx, name, path); err != nil {
		return "", err
	}
	if err := writeMarker(path, fp); err != nil {
		return "", err
	}
	return path, nil
}

// ascName derives the asc repository name (owner/name, lower case as on disk) from a bridge path, wikis included.
func ascName(path string) (string, error) {
	rel, err := filepath.Rel(filepath.Clean(setting.RepoRootPath), filepath.Clean(path))
	if err != nil {
		return "", errors.New("ascbridge: the bridge is not below the repository root")
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) != 2 || parts[0] == "" || parts[0] == ".." || !strings.HasSuffix(parts[1], ".git") || parts[1] == ".git" {
		return "", errors.New("ascbridge: the bridge path is not owner/name.git below the repository root")
	}
	return parts[0] + "/" + strings.TrimSuffix(parts[1], ".git"), nil
}

// fingerprint hashes the refs of asc that a bridge carries (branches, tags and pull request heads).
func fingerprint(ctx context.Context, api ascv1.RepoClient, name string) (string, error) {
	var lines []string
	after := ""
	for {
		cctx, cancel := context.WithTimeout(ascclient.WithSystem(ctx), CallTimeout)
		resp, err := api.ListRefs(cctx, &ascv1.ListRefsRequest{
			Repo: &ascv1.RepoRef{Name: name, IdMode: ascv1.IdMode_ID_MODE_ASC}, Prefix: "refs/", After: after, Limit: RefPage,
		})
		cancel()
		if err != nil {
			return "", fmt.Errorf("ascbridge: list refs: %w", err)
		}
		for _, r := range resp.GetRefs() {
			if !carried(r.GetName()) {
				continue
			}
			if lines = append(lines, fmt.Sprintf("%s %x%x", r.GetName(), r.GetTarget().GetChangeset(), r.GetTarget().GetTag())); len(lines) > MaxRefs {
				return "", fmt.Errorf("ascbridge: more than %d refs", MaxRefs)
			}
		}
		if after = resp.GetNext(); after == "" {
			break
		}
	}
	slices.Sort(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:]), nil
}

// carried reports whether a bridge holds the ref: branches, tags and pull request heads.
func carried(name string) bool {
	return strings.HasPrefix(name, "refs/heads/") || strings.HasPrefix(name, "refs/tags/") || strings.HasPrefix(name, "refs/pull/")
}

type marker struct {
	Fingerprint string `json:"fingerprint"`
	BuiltUnix   int64  `json:"built"`
}

func readMarker(dir string) string {
	f, err := os.Open(filepath.Join(dir, MarkerName))
	if err != nil {
		return ""
	}
	defer f.Close()
	var m marker
	if err := json.NewDecoder(io.LimitReader(f, MaxMarkerBytes)).Decode(&m); err != nil {
		return ""
	}
	return m.Fingerprint
}

func writeMarker(dir, fp string) error {
	data, err := json.Marshal(marker{Fingerprint: fp, BuiltUnix: time.Now().Unix()})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, MarkerName+".*")
	if err != nil {
		return fmt.Errorf("ascbridge: write marker: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("ascbridge: write marker: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("ascbridge: write marker: %w", err)
	}
	return util.Rename(tmp.Name(), filepath.Join(dir, MarkerName))
}
