// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

// Package asc is the repostore.Writer that changes a repository stored in `asc serve` (SPEC section 13, PLAN unit K12).
//
// Each Writer method is one RPC, or a short fixed sequence of them, of the asc.v1.Repo service:
//
//	CreateCommit            Commit (no ref), then UpdateRefs
//	CherryPick, Revert      Apply (no ref), then UpdateRefs
//	ApplyPatch              Apply (no ref), then UpdateRefs
//	Merge                   Merge (CopyClosure first for a head in another repository), then UpdateRefs
//	UpdateRefs              UpdateRefs (CopyClosure first for refs from another repository)
//	Init, Fork, Delete, Size, Verify, Rename   RepoAdmin
//
// A change is first built without a ref and then published with one UpdateRefs call. That keeps the rule
// of the git Writer: the commit exists before BeforePublish runs, no ref moves before it returns nil, and
// the repository's pre-receive and post-receive processing runs once, inside UpdateRefs. The publish is
// a fast-forward check, or a forced update when RefUpdate.Force is set; it has no compare-and-swap, as a
// git push has none.
//
// # Ids
//
// Callers speak git object ids (SPEC section 13.2, id_mode = GIT). asc.v1 mutations are served in id_mode ASC
// only, so every id is converted at this boundary with GitMap and answered ids are converted back. The
// converted ids are checked for length. An id that asc does not know is a *repostore.RefNotFoundError.
//
// # Credentials
//
// Forgejo has decided that the caller may write before it opens a Writer, so every call is a system call
// with the internal token (ascclient.WithSystem). Options.Auth replaces that.
//
// # What a repository without a git directory cannot do
//
// The repostore rules for fields marked "git stores only" apply as follows.
//
//   - CommitRequest.Preview is served: the commit is built without a ref and its diff against the parent is
//     streamed to the writer. The unreferenced changeset is garbage collected later.
//   - CommitMeta.Sign is called with SignInput.GitDir set to RepoRef.Path (the bridge, which may lack the
//     commit). A SignFunc that asks to sign fails with ErrUnsupported: asc serve has no signing key.
//   - BeforePublish is called with PublishInput.GitDir empty. It runs after the commit exists and before any
//     ref moves. A callback that needs the git directory (LFS pushes) must not be set for an asc repository.
//   - HookEnv is not forwarded. asc's outbox calls post-receive itself, for every ref change it holds.
//   - A submodule entry (EntryModeCommit) is refused: asc.v1 stores no gitlinks through Commit.
//   - The content of all files of one CreateCommit is limited to MaxCommitBytes, because it travels inline in
//     one request.
//   - Only sha1 repositories are served (id_mode GIT); the asc object format is K16.
package asc

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/repostore"
)

// Name is the name this Writer registers under (repostore.AscName).
const Name = repostore.AscName

// Named limits. Each bounds memory, time or message volume that one request or answer can cost.
const (
	// MaxCommitBytes bounds the file content of one CreateCommit (the server accepts 16 MiB per file and a
	// request message of 17 MiB).
	MaxCommitBytes = 15 << 20
	// MaxNameBytes bounds an asc repository name.
	MaxNameBytes = 255
	// MaxRevBytes bounds a revision sent to the server.
	MaxRevBytes = 4096
	// MaxMapIDs bounds the ids of one GitMap call (the server's MAX_LIST).
	MaxMapIDs = 4096
	// MaxLogCommits bounds the commits one authors or divergence query reads.
	MaxLogCommits = 1 << 20
	// MaxStatBytes bounds the numstat text a preview reads.
	MaxStatBytes = 64 << 20
	// MaxPreviewBytes bounds the diff text a preview writes.
	MaxPreviewBytes = 1 << 30
	// MaxMessageEcho caps how much of a server status message an error repeats.
	MaxMessageEcho = 512
	// MaxProtected bounds the protected files a dry run reports.
	MaxProtected = 10
	// DefaultTimeout bounds one Writer call (a merge of a long stack takes minutes).
	DefaultTimeout = 15 * time.Minute
)

var (
	// ErrNotConfigured is returned when the asc Writer is used before Configure. The request is not sent
	// anywhere else.
	ErrNotConfigured = errors.New("repostore/asc: the asc client is not configured")
	// ErrTooLarge marks a request or an answer over a named limit.
	ErrTooLarge = errors.New("repostore/asc: over a limit")
	// ErrBadAnswer marks a server answer that failed a check. Nothing from it is used.
	ErrBadAnswer = errors.New("repostore/asc: the asc server sent an answer that failed verification")
)

// Options tunes a Writer. The zero value is valid.
type Options struct {
	// Auth puts the credential for one call into ctx. The default is ascclient.WithSystem.
	Auth func(ctx context.Context) (context.Context, error)
	// Timeout bounds one Writer call (default DefaultTimeout).
	Timeout time.Duration
}

func (o Options) withDefaults() Options {
	if o.Auth == nil {
		o.Auth = func(ctx context.Context) (context.Context, error) { return ascclient.WithSystem(ctx), nil }
	}
	if o.Timeout <= 0 {
		o.Timeout = DefaultTimeout
	}
	return o
}

// Bridge is what the Writer asks of the git bridge directory of a repository (RepoRef.Path). The directory is
// where legacy git clients read the repository; services/ascbridge implements this.
type Bridge interface {
	// Prepare makes sure the directory exists after Init or Fork. defaultBranch may be empty.
	Prepare(ctx context.Context, repo repostore.RepoRef, defaultBranch string) error
	// Remove deletes the directory after Delete.
	Remove(ctx context.Context, repo repostore.RepoRef) error
	// Rename moves the directory after Rename.
	Rename(ctx context.Context, from, to repostore.RepoRef) error
}

// Writer is the asc repostore.Writer. It is safe for concurrent use.
type Writer struct {
	mu     sync.RWMutex
	api    ascv1.RepoClient
	opts   Options
	bridge Bridge
}

var _ repostore.Writer = (*Writer)(nil)

// shared is the Writer registered under Name; Configure fills it in.
var shared = &Writer{}

func init() {
	repostore.Register(Name, shared)
}

// Configure sets the client the registered Writer uses. Calling it again replaces it.
func Configure(api ascv1.RepoClient, opts Options) {
	shared.configure(api, opts)
}

// SetBridge sets the bridge directory handler of the registered Writer. Nil removes it, and then the
// Writer leaves the directory alone.
func SetBridge(b Bridge) {
	shared.mu.Lock()
	defer shared.mu.Unlock()
	shared.bridge = b
}

// SetBridge sets the bridge directory handler of this Writer.
func (w *Writer) SetBridge(b Bridge) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.bridge = b
}

func (w *Writer) bridgeHandler() Bridge {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.bridge
}

// Reset removes the configuration: the registered Writer then fails with ErrNotConfigured.
func Reset() {
	shared.configure(nil, Options{})
}

// New returns a Writer bound to api. It is not registered; tests and embedders use it directly.
func New(api ascv1.RepoClient, opts Options) (*Writer, error) {
	if api == nil {
		return nil, ErrNotConfigured
	}
	w := &Writer{}
	w.configure(api, opts)
	return w, nil
}

func (w *Writer) configure(api ascv1.RepoClient, opts Options) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.api, w.opts = api, opts.withDefaults()
}

// session is one Writer call: the client, the authenticated context and its bound.
type session struct {
	api    ascv1.RepoClient
	ctx    context.Context // authenticated; for RPCs
	caller context.Context // as given; for callbacks
	cancel context.CancelFunc
}

func (s *session) close() { s.cancel() }

// begin authenticates ctx and bounds it. The returned session must be closed.
func (w *Writer) begin(ctx context.Context) (*session, error) {
	w.mu.RLock()
	api, opts := w.api, w.opts
	w.mu.RUnlock()
	if api == nil {
		return nil, ErrNotConfigured
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	actx, err := opts.Auth(ctx)
	if err != nil {
		return nil, fmt.Errorf("repostore/asc: credential: %w", err)
	}
	actx, cancel := context.WithTimeout(actx, opts.Timeout)
	return &session{api: api, ctx: actx, caller: ctx, cancel: cancel}, nil
}

// repoHandle names one repository on the wire.
type repoHandle struct {
	name string
}

func (h *repoHandle) ascRef() *ascv1.RepoRef {
	return &ascv1.RepoRef{Name: h.name, IdMode: ascv1.IdMode_ID_MODE_ASC}
}

func (h *repoHandle) gitRef() *ascv1.RepoRef {
	return &ascv1.RepoRef{Name: h.name, IdMode: ascv1.IdMode_ID_MODE_GIT}
}

// handle derives the asc repository name ("owner/repo", lower case as on disk) from a RepoRef and refuses
// what asc does not serve. A wiki keeps its ".wiki" suffix: it is a repository of its own (SPEC 13.4).
func handle(ref repostore.RepoRef) (*repoHandle, error) {
	if err := ref.Validate(); err != nil {
		return nil, err
	}
	if ref.ObjectFormat != "" && ref.ObjectFormat != "sha1" {
		return nil, fmt.Errorf("%w: asc serves sha1 repositories only", repostore.ErrUnsupported)
	}
	owner, name := strings.ToLower(ref.Owner), strings.ToLower(ref.Name)
	if !namePart(owner) || !namePart(name) || len(owner)+len(name)+1 > MaxNameBytes {
		return nil, fmt.Errorf("%w: %q is not an asc repository name", repostore.ErrInvalidRequest, ref.Owner+"/"+ref.Name)
	}
	return &repoHandle{name: owner + "/" + name}, nil
}

func namePart(s string) bool {
	if s == "" || s == "." || s == ".." {
		return false
	}
	for i := range len(s) {
		switch c := s[i]; {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_', c == '.':
		default:
			return false
		}
	}
	return true
}

// same reports whether two handles name one repository.
func same(a, b *repoHandle) bool { return a.name == b.name }
