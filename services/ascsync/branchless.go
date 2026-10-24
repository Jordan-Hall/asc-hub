// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascsync

import (
	"context"
	"errors"
	"fmt"
	"strings"

	repo_model "forgejo.org/models/repo"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
)

// Branchless repository settings (SPEC section 21.5.2, PLAN unit B17).
//
// asc is the owner of the settings (repos.branchless, repos.cloud, repos.shared_refs). Forgejo only reads
// them through RepoConfig, and writes them through the settings write of RepoAdmin. Forgejo keeps no copy
// that could disagree with asc. Everything here is pure policy; the web handler is in
// routers/web/repo/setting/asc_branchless.go.

// Named limits for shared_refs, equal to asc's (crates/asc-serve/src/refpolicy.rs).
const (
	// MaxSharedRefs is the most shared_refs patterns a repository has.
	MaxSharedRefs = 256
	// MaxSharedRefBytes is the longest shared_refs pattern.
	MaxSharedRefBytes = 1024

	userNamespace = "refs/users/"
	headsPrefix   = "refs/heads/"
)

// ErrSettingsUnsupported is returned by the settings writer until asc's RepoAdmin carries the settings op.
var ErrSettingsUnsupported = errors.New("ascsync: asc does not accept repository settings changes yet")

// BranchlessSettings is the per-repository policy asc stores.
type BranchlessSettings struct {
	Branchless bool
	Cloud      bool
	SharedRefs []string
}

// SettingsWriter stores the settings in asc.
type SettingsWriter interface {
	SetRepoSettings(ctx context.Context, ascName string, s BranchlessSettings) error
}

// unsupportedWriter is the thin adapter for the missing RepoAdmin settings op.
// TODO(B07): replace by a RepoAdmin call once repo.proto carries the settings op; the proto change is asc-rpc's.
type unsupportedWriter struct{}

func (unsupportedWriter) SetRepoSettings(context.Context, string, BranchlessSettings) error {
	return ErrSettingsUnsupported
}

// settingsWriter is what SaveBranchlessSettings uses. Tests replace it through UseSettingsWriter.
var settingsWriter SettingsWriter = unsupportedWriter{}

// UseSettingsWriter replaces the settings writer until the returned function is called. It is for tests.
func UseSettingsWriter(w SettingsWriter) (restore func()) {
	apiMu.Lock()
	defer apiMu.Unlock()
	prev := settingsWriter
	settingsWriter = w
	return func() {
		apiMu.Lock()
		defer apiMu.Unlock()
		settingsWriter = prev
	}
}

// NormalizeSharedRefs validates a shared_refs list and returns it in asc's normal form: a short name means
// refs/heads/<name>, duplicates are dropped. Fails closed: a pattern that could reach the server-managed
// user namespace, a control character, "..", an empty segment, or a glob other than "*" is refused.
func NormalizeSharedRefs(in []string) ([]string, error) {
	if len(in) > MaxSharedRefs {
		return nil, fmt.Errorf("%w: more than %d shared_refs patterns", ErrBadRequest, MaxSharedRefs)
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, p := range in {
		p = strings.TrimSpace(p)
		if p == "" || len(p) > MaxSharedRefBytes {
			return nil, fmt.Errorf("%w: a shared_refs pattern is empty or too long", ErrBadRequest)
		}
		full := p
		if !strings.HasPrefix(p, "refs/") {
			full = headsPrefix + p
		}
		if err := checkPattern(full); err != nil {
			return nil, err
		}
		if !seen[full] {
			seen[full] = true
			out = append(out, full)
		}
	}
	return out, nil
}

func checkPattern(p string) error {
	bad := func(why string) error {
		return fmt.Errorf("%w: shared_refs pattern %q: %s", ErrBadRequest, clean(p, 80), why)
	}
	for _, r := range p {
		if r < 0x21 || r == 0x7f || strings.ContainsRune("~^:?[\\", r) {
			return bad("character not allowed in a ref name")
		}
	}
	if strings.Contains(p, "..") || strings.Contains(p, "//") || strings.HasSuffix(p, "/") ||
		strings.HasSuffix(p, ".") || strings.HasSuffix(p, ".lock") || strings.Contains(p, "@{") {
		return bad("not a valid ref name")
	}
	// The literal prefix before the first "*" must not be a prefix of, or start with, refs/users/.
	lit, _, _ := strings.Cut(p, "*")
	if strings.HasPrefix(lit, userNamespace) || strings.HasPrefix(userNamespace, lit) {
		return bad("reaches the server-managed refs/users/ namespace")
	}
	return nil
}

// ValidateBranchlessSettings checks a settings change for repo. cloud needs branchless and a non-public
// repository (SPEC section 21.6.1). It returns the settings in normal form.
func ValidateBranchlessSettings(repo *repo_model.Repository, s BranchlessSettings) (BranchlessSettings, error) {
	if repo == nil {
		return s, fmt.Errorf("%w: no repository", ErrBadRequest)
	}
	refs, err := NormalizeSharedRefs(s.SharedRefs)
	if err != nil {
		return s, err
	}
	s.SharedRefs = refs
	if s.Cloud && !s.Branchless {
		return s, fmt.Errorf("%w: cloud needs branchless", ErrBadRequest)
	}
	if s.Cloud && !repo.IsPrivate {
		return s, fmt.Errorf("%w: cloud is off for a public repository", ErrBadRequest)
	}
	return s, nil
}

// LoadBranchlessSettings reads the settings from asc (RepoConfig).
func LoadBranchlessSettings(ctx context.Context, repo *repo_model.Repository) (BranchlessSettings, error) {
	r, err := open(repo)
	if err != nil {
		return BranchlessSettings{}, err
	}
	ctx, cancel := system(ctx, CallTimeout)
	defer cancel()
	resp, err := r.api.RepoConfig(ctx, &ascv1.RepoConfigRequest{Repo: &ascv1.RepoRef{Name: r.name}})
	if err != nil {
		return BranchlessSettings{}, callError("RepoConfig", err)
	}
	return BranchlessSettings{Branchless: resp.GetBranchless(), Cloud: resp.GetCloud(), SharedRefs: resp.GetSharedRefs()}, nil
}

// SaveBranchlessSettings validates s and stores it in asc. The caller has already checked that the user
// administers repo. Nothing is stored in Forgejo.
func SaveBranchlessSettings(ctx context.Context, repo *repo_model.Repository, s BranchlessSettings) error {
	s, err := ValidateBranchlessSettings(repo, s)
	if err != nil {
		return err
	}
	r, err := open(repo)
	if err != nil {
		return err
	}
	ctx, cancel := system(ctx, CallTimeout)
	defer cancel()
	apiMu.Lock()
	w := settingsWriter
	apiMu.Unlock()
	return w.SetRepoSettings(ctx, r.name, s)
}

// RecordsBranchRow reports whether a push to ref creates or moves a Forgejo branch row. In a branchless
// repository only shared refs do; a user ref (anything that is not shared, and all of refs/users/) never
// does. In branch mode every ref under refs/heads/ does.
func (s BranchlessSettings) RecordsBranchRow(ref string) bool {
	if !strings.HasPrefix(ref, headsPrefix) {
		return false
	}
	if !s.Branchless {
		return true
	}
	return s.IsShared(ref)
}

// EmitsPushEvent reports whether a push to ref raises a push webhook and notification. User refs and
// anything in the user namespace never do in a branchless repository (SPEC section 21.5.2).
func (s BranchlessSettings) EmitsPushEvent(ref string) bool {
	if strings.HasPrefix(ref, userNamespace) {
		return false
	}
	if !s.Branchless {
		return true
	}
	return s.IsShared(ref)
}

// IsShared reports whether ref matches shared_refs. "*" matches any run of bytes, slashes included.
func (s BranchlessSettings) IsShared(ref string) bool {
	if strings.HasPrefix(ref, userNamespace) {
		return false
	}
	for _, p := range s.SharedRefs {
		if globMatch(p, ref) {
			return true
		}
	}
	return false
}

func globMatch(p, t string) bool {
	pi, ti, star, mark := 0, 0, -1, 0
	for ti < len(t) {
		switch {
		case pi < len(p) && p[pi] == '*':
			star, mark = pi, ti
			pi++
		case pi < len(p) && p[pi] == t[ti]:
			pi++
			ti++
		case star >= 0:
			pi = star + 1
			mark++
			ti = mark
		default:
			return false
		}
	}
	for pi < len(p) && p[pi] == '*' {
		pi++
	}
	return pi == len(p)
}
