// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package asctrust is the hub side of the trust surfaces of ASC: the repository policy, secret scanning,
// the audit log, signed changes and the CI failure insights (docs/BUILDOUT.md, sections 1, 3, 4, 5 and 8).
//
// The package holds everything the web handlers need that is not HTTP: the connection to `asc serve`, the
// short-lived credential of the signed-in person, the translation of an asc failure into a fixed text, the view
// models of the pages, and the small text edits of a policy file. It never draws a page and never reads a request.
//
// # Credentials
//
// A surface that shows data of the repository to everyone who may read it (signature badges, the checks page)
// makes system calls: Forgejo has decided that the viewer may read before the handler runs. A surface that is
// for administrators or that changes something (the audit log, findings, policy proposals, signing keys) makes
// the call as the signed-in person, with a short-lived access token that lives for that one request (UserCall),
// so asc decides again by its own rules and records the person in the audit log. A system call never stands in
// for a person (docs/BUILDOUT.md, "Who is calling").
//
// # Untrusted text
//
// Every text that comes from asc, from a runner or from a repository is data. CleanText strips control and
// bidirectional characters and cuts it, and the templates escape it. A failure of asc is shown as one of a few
// fixed texts (Error.Message); the text of the failure is never shown and never carries a token, a header or an
// endpoint.
package asctrust

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git/ascbackend"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/setting"

	"google.golang.org/grpc"
)

// Named limits. Each bounds the size, count or time of something a person, a runner or asc influences.
const (
	// CallTimeout bounds one unary call.
	CallTimeout = ascclient.DefaultCallTimeout
	// ExportTimeout bounds one audit export from the first byte to the last.
	ExportTimeout = 10 * time.Minute

	// MaxTextBytes bounds a short text from asc that a page shows (a title, a name, a note).
	MaxTextBytes = 512
	// MaxMessageBytes bounds a long text (a failure message of a test).
	MaxMessageBytes = 4096
	// MaxPolicyBytes is the largest policy file (asc-core MAX_POLICY_BYTES).
	MaxPolicyBytes = 64 * 1024
	// MaxListItems bounds the rows of any list a page draws from one answer.
	MaxListItems = 500

	// FeaturePolicy and the following are the strings of Version.features that switch a surface on.
	FeaturePolicy     = "policy-api.v1"
	FeatureAudit      = "audit.v1"
	FeatureSecretScan = "secretscan.v1"
	FeatureSigning    = "signing.v1"
	FeatureCiInsights = "ci-insights.v1"

	featureTTL = time.Minute
)

var (
	// ErrDisabled is returned when [asc] is not enabled, or the repository is not stored in asc.
	ErrDisabled = errors.New("asctrust: asc is not enabled for this repository")
	// ErrBadAnswer marks an answer of asc that failed a check. Nothing from it is used.
	ErrBadAnswer = errors.New("asctrust: the asc server sent an answer that failed verification")
	// ErrBadInput marks an input that failed a local check. Nothing was sent to asc.
	ErrBadInput = errors.New("asctrust: bad input")
)

var (
	connMu  sync.Mutex
	connVal grpc.ClientConnInterface

	// newConn builds the connection to asc serve. Tests replace the connection through UseConn.
	newConn = func() (grpc.ClientConnInterface, error) {
		if setting.InternalToken == "" {
			return nil, errors.New("asctrust: the internal token is not set")
		}
		return ascclient.New(ascclient.Config{Target: setting.Asc.ServerURL, InternalToken: setting.InternalToken})
	}
)

// Conn returns the shared connection to asc serve, building it on first use. Both kinds of call go through
// it: the credential is chosen per call by the context (ascclient.WithSystem or WithUserToken).
func Conn() (grpc.ClientConnInterface, error) {
	connMu.Lock()
	defer connMu.Unlock()
	if connVal == nil {
		c, err := newConn()
		if err != nil {
			return nil, err
		}
		connVal = c
	}
	return connVal, nil
}

// UseConn replaces the connection until the returned function is called. It is for tests.
func UseConn(c grpc.ClientConnInterface) (restore func()) {
	connMu.Lock()
	prev := connVal
	connVal = c
	connMu.Unlock()
	resetFeatures()
	return func() {
		connMu.Lock()
		connVal = prev
		connMu.Unlock()
		resetFeatures()
	}
}

// RepoName returns the asc name ("owner/repo", lower case) of repo. It fails with ErrDisabled when [asc] is off
// or the repository is not stored in asc, so a handler that starts with it never talks to asc about a git
// repository.
func RepoName(repo *repo_model.Repository) (string, error) {
	if !setting.Asc.Enabled || repo == nil || repo.GetStorageBackend() != string(gitrepo.StorageBackendAsc) {
		return "", ErrDisabled
	}
	name, err := ascbackend.RepoName(setting.RepoRootPath, repo_model.RepoPath(repo.OwnerName, repo.Name))
	if err != nil {
		return "", fmt.Errorf("asctrust: repository name: %w", err)
	}
	return name, nil
}

// RepoRef is the RepoRef of the asc repository called name.
func RepoRef(name string) *ascv1.RepoRef { return &ascv1.RepoRef{Name: name} }

// SystemContext authenticates ctx as the hub itself and bounds it by timeout.
func SystemContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ascclient.WithSystem(ctx), timeout)
}

var (
	featuresMu    sync.Mutex
	featuresCache *featureSet
)

type featureSet struct {
	at   time.Time
	list []string
}

func resetFeatures() {
	featuresMu.Lock()
	featuresCache = nil
	featuresMu.Unlock()
}

// HasFeature reports whether asc serve advertises feature (Version.features). It answers false, and does not
// fail, when the server cannot be asked: a surface is then shown as unavailable, never half drawn. The answer is
// kept for a minute.
func HasFeature(ctx context.Context, feature string) bool {
	featuresMu.Lock()
	if featuresCache != nil && time.Since(featuresCache.at) < featureTTL {
		ok := slices.Contains(featuresCache.list, feature)
		featuresMu.Unlock()
		return ok
	}
	featuresMu.Unlock()

	conn, err := Conn()
	if err != nil {
		return false
	}
	cctx, cancel := SystemContext(ctx, CallTimeout)
	defer cancel()
	resp, err := ascv1.NewRepoClient(conn).Version(cctx, &ascv1.VersionRequest{})
	if err != nil {
		return false
	}
	featuresMu.Lock()
	featuresCache = &featureSet{at: time.Now(), list: resp.GetFeatures()}
	featuresMu.Unlock()
	return slices.Contains(resp.GetFeatures(), feature)
}

// CleanText makes a text of an untrusted source safe to draw: invalid UTF-8 is dropped, control characters and
// the bidirectional and invisible format characters that disguise text are removed, new lines and tabs become
// spaces, and the result is cut at max bytes on a character boundary. The template still escapes it.
func CleanText(s string, max int) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range strings.ToValidUTF8(s, "") {
		switch {
		case r == '\n' || r == '\t' || r == '\r':
			r = ' '
		case unicode.IsControl(r), isFormatRune(r):
			continue
		}
		if b.Len()+utf8.RuneLen(r) > max {
			break
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

// CleanBlock is CleanText for a text that keeps its lines (a log excerpt): new lines stay, other control
// characters go, and the result is cut at max bytes.
func CleanBlock(s string, max int) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range strings.ToValidUTF8(s, "") {
		switch {
		case r == '\r':
			continue
		case r == '\t':
			r = ' '
		case r == '\n':
		case unicode.IsControl(r), isFormatRune(r):
			continue
		}
		if b.Len()+utf8.RuneLen(r) > max {
			break
		}
		b.WriteRune(r)
	}
	return strings.TrimRight(b.String(), " \n")
}

// isFormatRune reports the invisible characters that can reorder or hide text: the bidirectional controls and
// isolates, the zero-width characters, the byte order mark and the line and paragraph separators.
func isFormatRune(r rune) bool {
	switch {
	case r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069:
		return true
	case r == 0x200B, r == 0x200C, r == 0x200D, r == 0x200E, r == 0x200F, r == 0x061C, r == 0x2060, r == 0xFEFF, r == 0x2028, r == 0x2029:
		return true
	}
	return false
}

// System authenticates ctx as the hub itself. Use it only for what everyone who may read the repository may see.
func System(ctx context.Context) context.Context { return ascclient.WithSystem(ctx) }
