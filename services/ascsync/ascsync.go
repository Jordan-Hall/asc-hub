// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package ascsync is the Forgejo side of Stage 2 of the asc storage swap (SPEC section 13, PLAN unit K09).
//
// At Stage 2 a repository with storage_backend=asc has asc as its source of truth and its bare git
// directory as the git bridge. Two things follow, and this package carries both:
//
//   - A git push (and the temp-repo pushes Forgejo makes for merges) still lands in the bridge through
//     git's own receive-pack. The pre-receive hook, after every policy check, hands the push's
//     quarantine to asc (ImportPush, the ImportGitPush RPC). An asc error rejects the push. Post-receive
//     does nothing for such a call, because asc's outbox is the single fan-out (see PushTriggerAsc).
//   - The refs under refs/pull/ that Forgejo itself writes (PR head sync and AGit proc-receive) are
//     moved in asc first (SetPullHead, UpdateRefs with HOOK_MODE_SKIP_PRE). The bridge is never written
//     directly; it follows through asc's bridge_promote effect, and SetPullHead waits for that.
//
// Everything here is behind [asc] ENABLED and storage_backend=asc. It fails closed: a repository whose
// storage_backend is asc is never written through the git path because asc is unreachable or disabled.
//
// # Credentials
//
// Every call is a system call made with the internal token (ascclient.WithSystem). Forgejo has already
// decided that the pusher may write before it asks asc, and ImportGitPush and HOOK_MODE_SKIP_PRE accept
// only the internal token (SPEC section 15.1).
//
// # Errors
//
// A refusal that is the pusher's to see (a rejected ref, a path policy, a stale old value) is a
// *RejectError whose message is asc's own, stripped of control characters and cut to a named limit.
// Every other failure is an ordinary error for the log, and callers show the pusher a fixed text.
// Neither carries a token, a header or a file system path.
package ascsync

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git/ascbackend"
	"forgejo.org/modules/gitrepo"
	repo_module "forgejo.org/modules/repository"
	"forgejo.org/modules/setting"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// PushTriggerAsc is the PushTrigger of every hook call that asc makes (asc's HookOptions::for_push). Such a
// call is the fan-out of a push asc already holds; every other call to a storage_backend=asc repository is
// git-origin.
const PushTriggerAsc repo_module.PushTrigger = "asc"

// Named limits. Each bounds the size, count or time of something an untrusted party influences.
const (
	// MaxRefUpdates is the most ref commands one ImportGitPush carries (asc's MAX_REF_UPDATES).
	MaxRefUpdates = 1024
	// MaxAlternates is the most alternate object directories one push may name (asc's MAX_ALTERNATES).
	MaxAlternates = 16
	// MaxDirPathBytes is the longest directory path sent to asc (asc's MAX_DIR_PATH_BYTES).
	MaxDirPathBytes = 4096
	// MaxRefNameBytes is the longest ref name sent to asc.
	MaxRefNameBytes = 1024
	// MaxRejectBytes is how much of an asc refusal the pusher sees.
	MaxRejectBytes = 512
	// MaxLogMessageBytes is how much of an asc answer an internal error repeats.
	MaxLogMessageBytes = 200
	// MaxRepoNameBytes bounds the owner/name sent to asc.
	MaxRepoNameBytes = 255

	// ImportTimeout bounds one push import. It sits under the 60 s the git hook waits for pre-receive
	// (modules/private/hook.go), so the hook never gives up on a push asc then accepts.
	ImportTimeout = 55 * time.Second
	// BulkImportTimeout bounds one import that carries a whole history (a migration, WithBulkImport). It
	// is far longer than ImportTimeout because nothing waits on it behind a git hook.
	BulkImportTimeout = 30 * time.Minute
	// CallTimeout bounds one of the small calls (GitMap, ListRefs, UpdateRefs).
	CallTimeout = 30 * time.Second
	// BridgeWait is how long SetPullHead waits for asc's bridge_promote to move the bridge ref.
	BridgeWait = 30 * time.Second
	// bridgePollMin and bridgePollMax bound the delay between two looks at the bridge ref.
	bridgePollMin = 20 * time.Millisecond
	bridgePollMax = 250 * time.Millisecond
)

var (
	// ErrDisabled is returned for a storage_backend=asc repository while [asc] ENABLED is false. The
	// repository is not written through git instead.
	ErrDisabled = errors.New("ascsync: the repository is stored in asc but [asc] is not enabled")
	// ErrNotAsc is returned when an asc operation is asked of a repository that is not stored in asc.
	ErrNotAsc = errors.New("ascsync: the repository is not stored in asc")
	// ErrObjectFormat is returned for a repository that is not sha1; asc speaks 20-byte git ids.
	ErrObjectFormat = errors.New("ascsync: asc repositories use sha1 object ids")
	// ErrBadRequest marks an input that failed a local check. Nothing was sent to asc.
	ErrBadRequest = errors.New("ascsync: bad request")
	// ErrBadAnswer marks an asc answer that failed a check. Nothing from it is used.
	ErrBadAnswer = errors.New("ascsync: the asc server sent an answer that failed verification")
	// ErrBridgeBehind is returned when asc took a ref update but the bridge did not follow within BridgeWait.
	ErrBridgeBehind = errors.New("ascsync: the git bridge did not follow asc in time")
)

// RejectError is a refusal by asc whose text is meant for the pusher.
type RejectError struct {
	// Message is asc's text, without control characters, at most MaxRejectBytes long. It names the typed code
	// and the paths of the refusal when asc's own text does not.
	Message string
	// Code is the typed code of the refusal (SECRET_DETECTED, PATH_POLICY, REF_CONFLICT, ...), when asc sent one.
	Code string
	// Paths are the files (or refs) the refusal is about, as asc sent them.
	Paths []string
}

func (e *RejectError) Error() string { return "ascsync: asc refused: " + e.Message }

// RejectMessage returns the text for the pusher when err is (or wraps) a refusal by asc.
func RejectMessage(err error) (string, bool) {
	var rej *RejectError
	if errors.As(err, &rej) {
		return rej.Message, true
	}
	return "", false
}

// BackedBy reports whether repo is stored in asc (storage_backend=asc). git and dual repositories are
// not: dual answers from git and only compares reads against asc. An unknown value is an error, so a
// repository with a bad value is never treated as plain git.
func BackedBy(repo *repo_model.Repository) (bool, error) {
	if repo == nil {
		return false, fmt.Errorf("%w: no repository", ErrBadRequest)
	}
	sb, err := gitrepo.StorageBackendOf(repo)
	if err != nil {
		return false, err
	}
	return sb == gitrepo.StorageBackendAsc, nil
}

// ---------------------------------------------------------------------------------- client

var (
	apiMu  sync.Mutex
	apiVal ascv1.RepoClient

	// bulkVal is the client for imports of a whole history; its per-attempt bound is BulkImportTimeout.
	bulkVal ascv1.RepoClient

	// newAPI builds the Repo client. Tests replace it through UseClient.
	newAPI = func() (ascv1.RepoClient, error) { return buildAPI(ImportTimeout) }
	// newBulkAPI builds the client of WithBulkImport.
	newBulkAPI = func() (ascv1.RepoClient, error) { return buildAPI(BulkImportTimeout) }
)

// buildAPI builds a Repo client whose per-attempt bound is callTimeout.
func buildAPI(callTimeout time.Duration) (ascv1.RepoClient, error) {
	if setting.InternalToken == "" {
		return nil, errors.New("ascsync: the internal token is not set")
	}
	// CallTimeout is the per-attempt bound inside the client; the context deadline set by each
	// call is the bound that matters.
	c, err := ascclient.New(ascclient.Config{
		Target:        setting.Asc.ServerURL,
		InternalToken: setting.InternalToken,
		CallTimeout:   callTimeout,
	})
	if err != nil {
		return nil, err
	}
	return c.Repo(), nil
}

type bulkKey struct{}

// WithBulkImport marks ctx as the context of an import that carries a whole history (a migration or a first
// mirror run). ImportPush then allows BulkImportTimeout instead of ImportTimeout, because no git hook waits
// on it. It changes nothing about what asc checks.
func WithBulkImport(ctx context.Context) context.Context {
	return context.WithValue(ctx, bulkKey{}, true)
}

type importerKey struct{}

// WithImporter names the user an import is made for. ImportPush sends that user as the pusher of a request
// that names none, and remembers the push for the user, so the hub's fan-out of it is attributed to them. An
// import is a system action done for the user; it does not change what a later push of theirs is checked
// against.
func WithImporter(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, importerKey{}, userID)
}

func importerOf(ctx context.Context) int64 {
	id, _ := ctx.Value(importerKey{}).(int64)
	return max(id, 0)
}

func isBulk(ctx context.Context) bool {
	v, _ := ctx.Value(bulkKey{}).(bool)
	return v
}

// bulkAPI returns the client of WithBulkImport, building it on first use.
func bulkAPI() (ascv1.RepoClient, error) {
	apiMu.Lock()
	defer apiMu.Unlock()
	if bulkVal == nil {
		api, err := newBulkAPI()
		if err != nil {
			return nil, fmt.Errorf("ascsync: asc client: %w", err)
		}
		bulkVal = api
	}
	return bulkVal, nil
}

// UseClient replaces the Repo client until the returned function is called. It is for tests.
func UseClient(api ascv1.RepoClient) (restore func()) {
	apiMu.Lock()
	defer apiMu.Unlock()
	prev, prevBulk := apiVal, bulkVal
	apiVal, bulkVal = api, api
	return func() {
		apiMu.Lock()
		defer apiMu.Unlock()
		apiVal, bulkVal = prev, prevBulk
	}
}

// repoAPI returns the shared client, building it on first use. The client talks https, or plain http to
// a loopback host only (ascclient refuses anything else), and lives as long as the process.
func repoAPI() (ascv1.RepoClient, error) {
	apiMu.Lock()
	defer apiMu.Unlock()
	if apiVal == nil {
		api, err := newAPI()
		if err != nil {
			return nil, fmt.Errorf("ascsync: asc client: %w", err)
		}
		apiVal = api
	}
	return apiVal, nil
}

// ascRepo is what every operation needs: the client, the asc repository name and the bridge path.
type ascRepo struct {
	api      ascv1.RepoClient
	name     string
	bridge   string
	pusherID int64
}

// open checks that repo is an asc repository that may be used now and returns its handle.
func open(repo *repo_model.Repository) (*ascRepo, error) {
	ok, err := BackedBy(repo)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotAsc
	}
	if !setting.Asc.Enabled {
		return nil, ErrDisabled
	}
	if repo.ObjectFormatName != "" && repo.ObjectFormatName != "sha1" {
		return nil, ErrObjectFormat
	}
	bridge := repo.RepoPath()
	name, err := repoName(setting.RepoRootPath, bridge)
	if err != nil {
		return nil, err
	}
	api, err := repoAPI()
	if err != nil {
		return nil, err
	}
	return &ascRepo{api: api, name: name, bridge: bridge}, nil
}

// system authenticates ctx as Forgejo itself and bounds it by timeout.
func system(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ascclient.WithSystem(ctx), timeout)
}

// ---------------------------------------------------------------------------------- errors

// userFacing lists the status codes whose message asc wrote for the pusher. Everything else (an
// unavailable server, an internal error, a bad credential of ours) is shown as a fixed text.
func userFacing(c codes.Code) bool {
	switch c {
	case codes.InvalidArgument, codes.FailedPrecondition, codes.Aborted, codes.AlreadyExists,
		codes.PermissionDenied, codes.NotFound, codes.OutOfRange, codes.ResourceExhausted:
		return true
	}
	return false
}

// callError turns the error of an asc call into a *RejectError or an internal error.
func callError(op string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return fmt.Errorf("ascsync: %s: %w", op, err)
	}
	st, ok := status.FromError(err)
	if !ok {
		return fmt.Errorf("ascsync: %s failed", op)
	}
	code, text := st.Code(), st.Message()
	var typed ascclient.Refusal
	hasDetail := false
	if r, ok := ascclient.RefusalOf(err); ok {
		// The typed refusal of asc, read natively from the status details (or, for an older asc, out of the text).
		code, text, typed, hasDetail = r.Status, r.Message, r, true
	}
	msg := clean(text, MaxRejectBytes)
	if userFacing(code) {
		if msg == "" {
			msg = "asc refused the request (" + code.String() + ")"
		}
		rej := &RejectError{Message: msg}
		if hasDetail {
			typed.Message = msg
			rej.Code, rej.Paths = clean(typed.Code, 64), cleanAll(typed.Paths)
			typed.Code, typed.Paths = rej.Code, rej.Paths
			rej.Message = clean(typed.Text(), MaxRejectBytes)
		}
		return rej
	}
	return fmt.Errorf("ascsync: %s: asc answered %s: %s", op, code, clean(text, MaxLogMessageBytes))
}

// cleanAll is clean for every path, at most a few of them.
func cleanAll(paths []string) []string {
	var out []string
	for _, p := range paths {
		if len(out) == 8 {
			break
		}
		out = append(out, clean(p, 256))
	}
	return out
}

// clean makes s one line of printable text of at most limit bytes: control characters become spaces,
// invalid UTF-8 is dropped, and a cut never splits a rune.
func clean(s string, limit int) string {
	var b strings.Builder
	for _, r := range strings.ToValidUTF8(s, "") {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			r = ' '
		}
		if b.Len()+utf8.RuneLen(r) > limit {
			break
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

// bridgeWait is BridgeWait; tests shorten it.
var bridgeWait = BridgeWait

// repoName derives the asc repository name (owner/name, as on disk) from the bridge path, by the one rule
// the asc backend uses for reads.
var repoName = ascbackend.RepoName
