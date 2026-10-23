// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package git

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"sort"
	"sync"
	"time"
)

// Backend is everything modules/git needs from a repository store (Seam A).
//
// The concrete types Repository, Commit, Tree, TreeEntry, Blob and Tag keep
// their exported API; their methods dispatch to a Backend.
//
// Implementations: modules/git/gitcli (the git binary on a bare repository,
// the default) and, later, an asc server client. Object reads are on immutable,
// content-addressed ids and may be cached freely.
//
// A Backend is bound to one repository and is not safe for concurrent use
// unless an implementation says so; Repository has the same rule.
// Every ObjectID a caller passes must come from this repository's object format.
type Backend interface {
	ObjectFormat() ObjectFormat

	// Objects.

	// ReadCommit reads a commit. An annotated tag id is peeled to the commit it names.
	ReadCommit(ctx context.Context, id ObjectID) (*CommitData, error)
	// ReadTree lists one tree level. A commit id lists its root tree.
	ReadTree(ctx context.Context, id ObjectID) ([]TreeEntryData, error)
	ReadTreeRecursive(ctx context.Context, id ObjectID, opt TreeWalkOpts) iter.Seq2[TreeEntryData, error]
	// OpenBlob opens a blob and returns its full size. A limit above zero
	// truncates the stream to limit bytes (io.EOF at the limit); the size
	// returned is still the full size. Close discards any unread bytes.
	OpenBlob(ctx context.Context, id ObjectID, limit int64) (io.ReadCloser, int64, error)
	// StatObjects reports type and size for each id, in order. A missing object is not an error: its entry has Missing set.
	StatObjects(ctx context.Context, ids []ObjectID) ([]ObjectInfo, error)
	ReadTag(ctx context.Context, id ObjectID) (*TagData, error)
	// Resolve resolves a full ref, short id, "id^" or "id~n" to an object id and its type.
	Resolve(ctx context.Context, rev string) (ObjectID, ObjectType, error)

	// Refs.

	ListRefs(ctx context.Context, q RefQuery) ([]Reference, error)
	Head(ctx context.Context) (RefName, error)
	SetHead(ctx context.Context, branch RefName) error

	// History and derived data.

	Log(ctx context.Context, q LogQuery) iter.Seq2[*CommitData, error]
	Count(ctx context.Context, q LogQuery) (int64, error)
	MergeBase(ctx context.Context, a, b ObjectID) (ObjectID, error)
	IsAncestor(ctx context.Context, ancestor, desc ObjectID) (bool, error)
	// LastCommits returns, for each of names inside dir, the newest commit at or below at that touched it.
	LastCommits(ctx context.Context, at ObjectID, dir string, names []string) (map[string]ObjectID, error)
	Blame(ctx context.Context, at ObjectID, path string, opt BlameOpts) iter.Seq2[BlamePart, error]

	// Diff. UnifiedDiff writes git-compatible text so services/gitdiff keeps working.

	ChangedPaths(ctx context.Context, base, head ObjectID, opt DiffOpts) ([]PathChange, error)
	UnifiedDiff(ctx context.Context, base, head ObjectID, opt DiffOpts, w io.Writer) error
	DiffStat(ctx context.Context, base, head ObjectID) (DiffStat, error)

	// Bulk readers.

	Archive(ctx context.Context, at ObjectID, format ArchiveType, prefix string, w io.Writer) error
	Grep(ctx context.Context, at ObjectID, q GrepQuery) ([]*GrepResult, error)
	Attributes(ctx context.Context, at ObjectID, paths, names []string) (map[string]map[string]string, error)
	LanguageStats(ctx context.Context, at ObjectID) (map[string]int64, error)

	Close() error
}

// CommitData is the raw content of a commit object.
type CommitData struct {
	ID        ObjectID
	Tree      ObjectID
	Parents   []ObjectID
	Author    *Signature
	Committer *Signature
	Message   string
	Signature *ObjectSignature // nil when the commit is unsigned
}

// TreeEntryData is one entry of a tree. Size is -1 when the backend did not load it.
type TreeEntryData struct {
	ID   ObjectID
	Name string // for a recursive walk, the path relative to the walked tree
	Mode EntryMode
	Size int64
}

// TreeWalkOpts controls ReadTreeRecursive.
type TreeWalkOpts struct {
	// Trees includes the tree (directory) entries as well as the leaves.
	Trees bool
	// Sizes loads blob sizes (slower).
	Sizes bool
}

// ObjectInfo is the result of StatObjects for one id.
type ObjectInfo struct {
	ID      ObjectID
	Type    ObjectType
	Size    int64
	Missing bool
}

// TagData is the content of an annotated tag object.
type TagData struct {
	ID        ObjectID
	Object    ObjectID
	Type      string
	Name      string
	Tagger    *Signature
	Message   string
	Signature *ObjectSignature
}

// RefQuery selects references for ListRefs. Remote-tracking refs and refs/stash are never listed.
type RefQuery struct {
	// Prefix keeps references whose full name starts with it.
	Prefix string
	// Sort is a for-each-ref sort key such as "-creatordate"; empty means refname order.
	Sort string
	// Limit caps the result; zero means no cap.
	Limit int
	// Contains keeps references whose history contains this commit.
	Contains ObjectID
}

// LogQuery selects commits for Log and Count.
type LogQuery struct {
	From     []ObjectID // start points; ignored when All is set
	Not      []ObjectID // exclude everything reachable from these
	All      bool       // start from every reference
	Paths    []string   // limit to commits touching these paths
	Follow   bool       // follow renames; needs exactly one path
	Skip     int
	Limit    int // zero means no cap
	Author   string
	Grep     string
	Since    time.Time
	Until    time.Time
	NoMerges bool
}

// BlameOpts controls Blame.
type BlameOpts struct {
	// BypassIgnoreRevs ignores the repository's .git-blame-ignore-revs file.
	BypassIgnoreRevs bool
}

// DiffMode selects the text produced by UnifiedDiff.
type DiffMode int

const (
	// DiffModeText is "git diff -p", optimised for reading.
	DiffModeText DiffMode = iota
	// DiffModeBinary is "git diff -p --binary", usable with "git apply".
	DiffModeBinary
	// DiffModePatch is "git format-patch --stdout" (a mailbox of patches).
	DiffModePatch
)

// DiffOpts controls ChangedPaths and UnifiedDiff.
type DiffOpts struct {
	Mode DiffMode
	// MergeBase compares head against the merge base of base and head (the
	// "base...head" form), falling back to a direct comparison when the two share no history.
	MergeBase bool
}

// PathChange is one changed path from ChangedPaths.
type PathChange struct {
	Path string
}

// DiffStat is a short statistic of a diff.
type DiffStat struct {
	NumFiles  int
	Additions int
	Deletions int
}

// GrepQuery is one Backend.Grep request. Options.RefName is ignored: the search runs at the commit passed to Grep.
type GrepQuery struct {
	Search  string
	Options GrepOptions
}

// DefaultBackendName names the backend that OpenRepository selects.
const DefaultBackendName = "gitcli"

// ErrNoBackend is returned when no backend is registered under the requested name.
// The default backend registers itself when forgejo.org/modules/git/gitcli is linked in.
var ErrNoBackend = errors.New("git: no repository backend registered (import forgejo.org/modules/git/gitcli)")

// BackendFactory binds a backend to an opened repository.
type BackendFactory func(repo *Repository) (Backend, error)

var (
	backendMu        sync.RWMutex
	backendFactories = map[string]BackendFactory{}
)

// RegisterBackend registers a backend factory under name. It panics on a nil factory or a duplicate name,
// because both are programming errors found at start-up.
func RegisterBackend(name string, factory BackendFactory) {
	if factory == nil {
		panic("git: RegisterBackend with nil factory")
	}
	backendMu.Lock()
	defer backendMu.Unlock()
	if _, dup := backendFactories[name]; dup {
		panic("git: RegisterBackend called twice for " + name)
	}
	backendFactories[name] = factory
}

// Backends returns the registered backend names, sorted.
func Backends() []string {
	backendMu.RLock()
	defer backendMu.RUnlock()
	names := make([]string, 0, len(backendFactories))
	for name := range backendFactories {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func backendFactory(name string) (BackendFactory, error) {
	backendMu.RLock()
	defer backendMu.RUnlock()
	factory, ok := backendFactories[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNoBackend, name)
	}
	return factory, nil
}

// bindBackend creates the backend registered under name for repo.
func (repo *Repository) bindBackend(name string) error {
	factory, err := backendFactory(name)
	if err != nil {
		return err
	}
	be, err := factory(repo)
	if err != nil {
		return err
	}
	repo.backend = be
	return nil
}

// Backend returns the backend of this repository, binding the default one for a Repository that was not made by OpenRepository.
func (repo *Repository) Backend() (Backend, error) {
	if repo.backend == nil {
		if err := repo.bindBackend(DefaultBackendName); err != nil {
			return nil, err
		}
	}
	return repo.backend, nil
}

// NewBackend creates the backend registered under name for repo without binding it to repo. A backend that
// wraps others (modules/git/dual) uses it to build the backends it combines.
func NewBackend(name string, repo *Repository) (Backend, error) {
	factory, err := backendFactory(name)
	if err != nil {
		return nil, err
	}
	return factory(repo)
}

// OpenRepositoryWithBackend opens the repository at repoPath like OpenRepository, bound to the named backend.
func OpenRepositoryWithBackend(ctx context.Context, repoPath, backend string) (*Repository, error) {
	repo, err := openRepository(ctx, repoPath)
	if err != nil {
		return nil, err
	}
	if err := repo.bindBackend(backend); err != nil {
		return nil, err
	}
	return repo, nil
}

// revToID turns a revision into an object id: a full id is used as given, anything else is resolved by the backend.
func (repo *Repository) revToID(ctx context.Context, be Backend, rev string) (ObjectID, error) {
	if id, err := NewIDFromString(rev); err == nil {
		return id, nil
	}
	id, _, err := be.Resolve(ctx, rev)
	return id, err
}

// Exports for backend implementations that live outside this package.

// ParseTagData parses the uncompressed content of an annotated tag object.
func ParseTagData(objectFormat ObjectFormat, data []byte) (*Tag, error) {
	return parseTagData(objectFormat, data)
}

// ParseEntryMode parses the octal mode of a tree entry.
func ParseEntryMode(modeStr string) (EntryMode, error) {
	return parseMode(modeStr)
}
