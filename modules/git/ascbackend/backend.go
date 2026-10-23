// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

// Package ascbackend is the git.Backend that reads a repository from `asc serve` (SPEC section 13, PLAN unit K10).
//
// It speaks the asc.v1.Repo read RPCs with id_mode = GIT, so every id that crosses this package is
// a git object id and no database row changes when a repository moves to storage_backend = asc.
// It is read only: writes go through modules/repostore (K12).
//
// # Trust and failure
//
// Everything the server returns is checked before it is used: object ids and sizes, tree and commit
// hashes, entry names, counts and totals. A server answer that does not check out is an error, never
// a partial result. An asc repository never falls back to the git directory on failure (see
// modules/gitrepo/backend_select.go).
//
// Reads are made with the internal token (ascclient.WithSystem): Forgejo has already decided that the
// caller may read the repository before it opens it. Options.Auth replaces that.
//
// # What the asc.v1.Repo surface cannot answer
//
// These are documented limits, not silent differences:
//
//   - A tree is listed through its commit and path, because ReadTree takes a revision. The backend
//     learns where each tree id lives from the commits and listings it has already read, so a tree id
//     it has never seen is reported as missing. A Repository (and so a Backend) lives for one request,
//     which is the scope a tree id travels in.
//   - Annotated tag objects are not served: ListRefs reports an annotated tag ref as a reference to
//     the commit it peels to, and ReadTag fails with ErrUnsupported.
//   - Log, MergeBase and the like return what git returns for the same repository, but Log reads every
//     commit again with ReadChangeset to get its tree and signature, so it costs one RPC per commit.
//   - Blame cannot honour .git-blame-ignore-revs, and PreviousPath is the path in the origin commit.
//   - UnifiedDiff supports DiffModeText and DiffModeBinary; format-patch mailboxes (DiffModePatch)
//     and bundle archives are ErrUnsupported.
//   - Head needs the server to report HEAD; SetHead is ErrUnsupported.
package ascbackend

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git"
	"forgejo.org/modules/setting"
)

// Name is the name this backend registers under (see modules/gitrepo/backend_select.go).
const Name = "asc"

// Named limits. Each bounds the memory, time or message volume that one untrusted answer can cost.
const (
	// maxTreeEntries caps the entries one listing may return (the server's own cap is the same).
	maxTreeEntries = 1_000_000
	// maxTreeDepth caps the directory depth of a walk (the server's cap is the same).
	maxTreeDepth = 2048
	// maxTreeDirs caps the directories one level-by-level walk may open.
	maxTreeDirs = 100_000
	// maxEntryNameBytes caps one tree entry name.
	maxEntryNameBytes = 4096
	// maxPathBytes caps one repository path.
	maxPathBytes = 4096
	// maxRevBytes caps a revision string sent to the server.
	maxRevBytes = 4096
	// maxRawCommitBytes caps the raw commit a ReadChangeset answer may carry.
	maxRawCommitBytes = 64 << 20
	// maxLogCommits bounds the commits one Log or Count call reads.
	maxLogCommits = 1 << 20
	// maxQueryItems bounds the start points, exclusions and paths of one query, and the batch sizes
	// of the id-list RPCs (the server's MAX_LIST).
	maxQueryItems = 4096
	// maxRefs bounds the references one ListRefs call may collect.
	maxRefs = 500_000
	// refPage is the page size asked of ListRefs.
	refPage = 1000
	// maxContainsChecks bounds the IsAncestor calls one ListRefs(Contains) may make.
	maxContainsChecks = 10_000
	// maxChangedPaths bounds the files one ChangedPaths call may list (the server's DIFF_MAX_FILES).
	maxChangedPaths = 100_000
	// maxNumstatBytes bounds the numstat text one DiffStat call reads.
	maxNumstatBytes = 64 << 20
	// maxDiffBytes bounds the text one UnifiedDiff call may write.
	maxDiffBytes = 1 << 30
	// maxArchiveBytes bounds the bytes one Archive call may write.
	maxArchiveBytes = 16 << 30
	// maxBlameFileBytes bounds the file a Blame call reads to cut it into lines.
	maxBlameFileBytes = 32 << 20
	// maxBlameParts bounds the runs one Blame call may return.
	maxBlameParts = 1 << 20
	// maxBlamePreviousChecks bounds the Stat calls one Blame makes to learn where a file existed before.
	maxBlamePreviousChecks = 1000
	// maxGrepMatches is the number of matching lines asked of one Grep (the server's GREP_MAX).
	maxGrepMatches = 100_000
	// maxContextFileBytes bounds the file read to add context lines to a Grep result.
	maxContextFileBytes = 2 << 20
	// maxAttributePaths bounds the paths one Attributes call asks about.
	maxAttributePaths = 4096
	// maxAttributeNames bounds the attribute names one request may ask for (the server's MAX_ATTR_NAMES).
	maxAttributeNames = 64
	// maxLanguageContentReads bounds the files LanguageStats reads content for; later files are
	// detected by name only, as git.Repository.GetLanguageStats does for a file it cannot read.
	maxLanguageContentReads = 100_000
	// languageReadWorkers is the number of concurrent content reads of LanguageStats.
	languageReadWorkers = 8
	// maxLastCommitNames bounds the names one LastCommits call asks about.
	maxLastCommitNames = 4096
	// maxMessageEcho caps how much of a server status message an error repeats.
	maxMessageEcho = 200
	// logPrefetch is the number of commits Log reads ahead.
	logPrefetch = 16
	// commitCacheEntries is the number of decoded commits one Backend keeps.
	commitCacheEntries = 2048
)

var (
	// ErrNotConfigured is returned when the asc backend is selected but no client was set up.
	// It never falls back to the git directory.
	ErrNotConfigured = errors.New("ascbackend: the asc client is not configured")
	// ErrUnsupported marks an operation the asc.v1.Repo read surface cannot answer.
	ErrUnsupported = errors.New("ascbackend: not supported by the asc backend")
	// ErrTooLarge marks a result that is over a named limit.
	ErrTooLarge = errors.New("ascbackend: result is over a limit")
	// ErrBadAnswer marks a server answer that failed a check. Nothing from it is used.
	ErrBadAnswer = errors.New("ascbackend: the asc server sent an answer that failed verification")
	// ErrClosed is returned by calls made after Close.
	ErrClosed = errors.New("ascbackend: backend is closed")
)

// Options tunes a Backend. The zero value is valid.
type Options struct {
	// Auth puts the credential for one call into ctx. The default is ascclient.WithSystem.
	Auth func(ctx context.Context) (context.Context, error)
	// StreamTimeout bounds one streaming read (default 2 minutes).
	StreamTimeout time.Duration
	// BulkTimeout bounds a read that moves bulk data: blobs, archives, diffs (default 30 minutes).
	BulkTimeout time.Duration
	// FactEntries is the number of object facts one Backend keeps (default 1<<18).
	FactEntries int
}

const (
	defaultStreamTimeout = ascclient.DefaultStreamTimeout
	defaultBulkTimeout   = 30 * time.Minute
	defaultFactEntries   = 1 << 18
)

func (o Options) withDefaults() Options {
	if o.Auth == nil {
		o.Auth = func(ctx context.Context) (context.Context, error) { return ascclient.WithSystem(ctx), nil }
	}
	if o.StreamTimeout <= 0 {
		o.StreamTimeout = defaultStreamTimeout
	}
	if o.BulkTimeout <= 0 {
		o.BulkTimeout = defaultBulkTimeout
	}
	if o.FactEntries <= 0 {
		o.FactEntries = defaultFactEntries
	}
	return o
}

// shared is what the registered factory needs: the process-wide client and options.
var shared struct {
	mu   sync.RWMutex
	api  ascv1.RepoClient
	opts Options
}

func init() {
	git.RegisterBackend(Name, newFromRegistry)
}

// Configure sets the client every asc Backend opened from now on uses. Calling it again replaces it.
func Configure(api ascv1.RepoClient, opts Options) {
	shared.mu.Lock()
	defer shared.mu.Unlock()
	shared.api, shared.opts = api, opts.withDefaults()
}

// Reset removes the configuration, so that opening an asc repository fails with ErrNotConfigured.
func Reset() {
	shared.mu.Lock()
	defer shared.mu.Unlock()
	shared.api, shared.opts = nil, Options{}
}

func newFromRegistry(repo *git.Repository) (git.Backend, error) {
	shared.mu.RLock()
	api, opts := shared.api, shared.opts
	shared.mu.RUnlock()
	if api == nil {
		return nil, ErrNotConfigured
	}
	name, err := RepoName(setting.RepoRootPath, repo.Path)
	if err != nil {
		return nil, err
	}
	return New(api, name, repo, opts)
}

// Backend serves one repository from asc serve. It is safe for concurrent use.
type Backend struct {
	api   ascv1.RepoClient
	name  string
	repo  *git.Repository
	opts  Options
	facts *factCache

	mu      sync.Mutex
	closed  bool
	commits *commitCache
}

var _ git.Backend = (*Backend)(nil)

// New binds a Backend to the asc repository name ("owner/repo"). repo is the git.Repository it is
// bound to; it is only used to parse commit objects.
func New(api ascv1.RepoClient, name string, repo *git.Repository, opts Options) (*Backend, error) {
	if api == nil {
		return nil, ErrNotConfigured
	}
	if err := validRepoName(name); err != nil {
		return nil, err
	}
	opts = opts.withDefaults()
	return &Backend{
		api:     api,
		name:    name,
		repo:    repo,
		opts:    opts,
		facts:   newFactCache(opts.FactEntries),
		commits: newCommitCache(commitCacheEntries),
	}, nil
}

// ObjectFormat is sha1: in id_mode GIT the server speaks 20-byte git ids.
func (b *Backend) ObjectFormat() git.ObjectFormat { return git.Sha1ObjectFormat }

// Close stops the Backend from serving more calls. The client belongs to the process and stays open.
func (b *Backend) Close() error {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
	return nil
}

func (b *Backend) isClosed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed
}

// gitRef is the RepoRef of a call that speaks git ids.
func (b *Backend) gitRef() *ascv1.RepoRef {
	return &ascv1.RepoRef{Name: b.name, IdMode: ascv1.IdMode_ID_MODE_GIT}
}

// ascRef is the RepoRef of the calls that only exist in asc id mode (ListRefs).
func (b *Backend) ascRef() *ascv1.RepoRef {
	return &ascv1.RepoRef{Name: b.name, IdMode: ascv1.IdMode_ID_MODE_ASC}
}

// callCtx authenticates ctx for one call and bounds it by timeout (zero means the caller's deadline only).
// The returned cancel must always be called.
func (b *Backend) callCtx(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc, error) {
	if b.isClosed() {
		return nil, nil, ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	actx, err := b.opts.Auth(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("ascbackend: credential: %w", err)
	}
	if timeout > 0 {
		actx, cancel := context.WithTimeout(actx, timeout)
		return actx, cancel, nil
	}
	actx, cancel := context.WithCancel(actx)
	return actx, cancel, nil
}

// unary authenticates ctx for a unary call (the client applies its own call timeout).
func (b *Backend) unary(ctx context.Context) (context.Context, context.CancelFunc, error) {
	return b.callCtx(ctx, 0)
}

// stream authenticates ctx for a streaming read bounded by the stream timeout.
func (b *Backend) stream(ctx context.Context) (context.Context, context.CancelFunc, error) {
	return b.callCtx(ctx, b.opts.StreamTimeout)
}

// bulk authenticates ctx for a streaming read that moves bulk data.
func (b *Backend) bulk(ctx context.Context) (context.Context, context.CancelFunc, error) {
	return b.callCtx(ctx, b.opts.BulkTimeout)
}
