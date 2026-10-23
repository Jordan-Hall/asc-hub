// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

// Package repostore is Seam B: everything Forgejo does to change a repository.
//
// Seam A (modules/git, git.Backend) reads. Writer writes. Services describe a
// change (files to put and delete, a patch, a merge, a ref update) and a Writer
// makes it. They no longer clone to a temporary directory and run git commands
// themselves.
//
// The default implementation is modules/repostore/gitcli. It keeps the
// temporary-repository mechanics the services used before and runs the git
// binary on bare repositories. An asc implementation (K12) maps each method to
// one RPC.
//
// Rules every implementation follows:
//
//   - A request is validated before any side effect. An invalid request fails
//     with an error that wraps ErrInvalidRequest. Validation fails closed: what
//     it does not know to be safe, it rejects.
//   - A ref moves only after the commit exists and BeforePublish has returned nil.
//   - Publishing a ref runs the repository's pre-receive and post-receive
//     processing exactly once (for git stores, through the hooks).
//   - A rejected or lost-race publish returns *git.ErrPushRejected or
//     *git.ErrPushOutOfDate, the types the callers already handle.
//   - Errors carry no credentials and no environment. The text of git's own
//     output can appear in typed errors for logs, as before.
//
// Fields marked "git stores only" cannot be honoured by a store without a git
// directory. Such a store must return ErrUnsupported when a request sets them.
// It must never ignore them.
package repostore

import (
	"context"
	"io"

	"forgejo.org/modules/git"
)

// Writer changes repositories. An implementation is safe for concurrent use.
// It does not serialise calls: a caller that needs ordering holds its own lock,
// as the services do with their working pools.
type Writer interface {
	// CreateCommit builds one commit from file operations on Parent and, when
	// Update is set, publishes it.
	CreateCommit(ctx context.Context, req CommitRequest) (*CommitResult, error)
	// Merge merges Head into Base in one of the MergeStyles, or, with DryRun,
	// reports whether it would merge and what conflicts.
	Merge(ctx context.Context, req MergeRequest) (*MergeResult, error)
	// CherryPick applies the change made by one commit onto Parent.
	CherryPick(ctx context.Context, req PickRequest) (*CommitResult, error)
	// Revert applies the inverse of the change made by one commit onto Parent.
	Revert(ctx context.Context, req PickRequest) (*CommitResult, error)
	// ApplyPatch applies a unified diff to Parent.
	ApplyPatch(ctx context.Context, req PatchRequest) (*CommitResult, error)
	// UpdateRefs moves, creates and deletes refs.
	UpdateRefs(ctx context.Context, req UpdateRefsRequest) ([]RefResult, error)

	// Init creates an empty repository. It fails with ErrExists when the repository exists.
	Init(ctx context.Context, req InitRequest) error
	// Fork creates a repository that holds a copy of another. It fails with ErrExists when the target exists.
	Fork(ctx context.Context, req ForkRequest) error
	// Delete removes a repository. Deleting a repository that does not exist is not an error.
	Delete(ctx context.Context, repo RepoRef) error
	// Size reports how much storage a repository uses.
	Size(ctx context.Context, repo RepoRef) (Size, error)
}

// Limits that bound what one request can ask for. A request over a limit is
// rejected before it does any work.
const (
	// MaxFileOps is the largest number of file operations in one CommitRequest.
	MaxFileOps = 10_000
	// MaxPathBytes is the longest path of a file operation.
	MaxPathBytes = 4096
	// MaxMessageBytes is the longest commit message, trailers excluded.
	MaxMessageBytes = 1 << 20
	// MaxPatchBytes is the largest patch ApplyPatch accepts.
	MaxPatchBytes = 64 << 20
	// MaxRefUpdates is the largest number of updates in one UpdateRefsRequest.
	MaxRefUpdates = 1_000
	// MaxGlobs is the largest number of ProtectedGlobs in one MergeRequest.
	MaxGlobs = 1_000
	// MaxConflictsReported is the largest number of conflicting paths a MergeResult or ConflictError lists.
	MaxConflictsReported = 1_000
)

// RepoRef names one repository in a store.
type RepoRef struct {
	// ID is the Forgejo repository id. It is for diagnostics and for stores that key on it.
	ID int64
	// Owner and Name identify the repository. A wiki is a repository of its own:
	// its Name ends in ".wiki".
	Owner string
	Name  string
	// ObjectFormat is the object format name ("sha1" or "sha256").
	ObjectFormat string
	// Path is the absolute path of the bare git directory. Git stores only.
	Path string
}

// Signature is a person and a time. A zero When means the time of the request.
type Signature = git.Signature

// CommitMeta is what a store needs to write a commit.
type CommitMeta struct {
	// Author and Committer are required, except that a nil Author means Committer.
	Author    *Signature
	Committer *Signature
	// Message is the commit message. The store ends it with a newline.
	Message string
	// Signoff appends a Signed-off-by trailer for the committer.
	Signoff bool
	// Sign decides whether and how to sign. Nil means the commit is not signed.
	// Git stores only.
	Sign SignFunc
}

// SignInput is what a SignFunc is told about the commit being written.
type SignInput struct {
	// GitDir is a git directory that holds Base and Head, for the checks that read history.
	GitDir string
	// Base is the commit the new commit builds on. It is nil for a root commit.
	Base git.ObjectID
	// Head is the commit being merged in. It is nil unless the request is a merge.
	Head git.ObjectID
}

// SignDecision is the outcome of a SignFunc that decided to sign.
type SignDecision struct {
	// KeyID is the signing key.
	KeyID string
	// Committer replaces the committer when set (the trust model credits the key owner).
	// When it differs from the author, the original committer is named in trailers.
	Committer *Signature
}

// SignFunc decides whether to sign one commit. It returns nil to leave the
// commit unsigned. An error fails the request.
type SignFunc func(ctx context.Context, in SignInput) (*SignDecision, error)

// RefUpdate says where a ref is published to.
type RefUpdate struct {
	// Repo is where the ref lives. Objects are pushed there from the working repository.
	Repo RepoRef
	// Ref is the full ref name, for example refs/heads/main.
	Ref string
	// Force allows a non-fast-forward update.
	Force bool
}

// PublishInput is what BeforePublish is told about the change about to be published.
type PublishInput struct {
	// GitDir is a git directory that holds the new objects. Git stores only.
	GitDir string
	// Commit is the new tip.
	Commit git.ObjectID
	// Base is the tip the change builds on: the parent, or the base tip for a merge. It is nil for a root commit.
	Base git.ObjectID
	// Head is the head commit that was merged. It is nil unless the request is a merge.
	Head git.ObjectID
	// MergeBase is the merge base of Base and Head before the merge. It is nil unless the request is a merge
	// and the two share history.
	MergeBase git.ObjectID
}

// BeforePublish runs after the commit exists and before any ref moves. An error
// stops the request with no ref changed.
type BeforePublish func(ctx context.Context, in PublishInput) error

// FileOpKind is the kind of a FileOp.
type FileOpKind int

const (
	// OpPut writes a file. It replaces any file at the path.
	OpPut FileOpKind = iota + 1
	// OpDelete removes a file.
	OpDelete
)

// FileOp is one change to the tree.
type FileOp struct {
	Kind FileOpKind
	// Path is the slash-separated path from the repository root.
	Path string
	// Mode is the mode of an OpPut: git.EntryModeBlob, EntryModeExec, EntryModeSymlink or EntryModeCommit
	// (a submodule). Zero means EntryModeBlob.
	Mode git.EntryMode
	// Content is the file content of an OpPut. The store reads it once. For EntryModeCommit it is the
	// hexadecimal id of the commit the submodule points at, and no object is written.
	Content io.Reader
}

// CommitRequest is the input of Writer.CreateCommit.
type CommitRequest struct {
	Repo RepoRef
	// Parent is the commit to build on. Nil makes a root commit.
	Parent git.ObjectID
	// Ops are applied in order to the tree of Parent.
	Ops  []FileOp
	Meta CommitMeta
	// Update publishes the commit. Nil leaves it unpublished.
	Update *RefUpdate
	// BeforePublish runs before Update is applied. It is ignored when Update is nil.
	BeforePublish BeforePublish
	// Preview, when set, makes this a dry run: nothing is published, and the
	// unified diff of the new tree against Parent is written to it.
	// Git stores only. Update must be nil.
	Preview io.Writer
	// HookEnv is the environment the publish runs hooks with. Git stores only.
	HookEnv []string
}

// CommitResult is what a commit-building method returns.
type CommitResult struct {
	// Commit is the new commit. It is nil after a Preview.
	Commit git.ObjectID
	Tree   git.ObjectID
	// Stat is the size of the change, set after a Preview.
	Stat git.DiffStat
}

// PickRequest is the input of Writer.CherryPick and Writer.Revert.
type PickRequest struct {
	Repo RepoRef
	// Parent is the commit to apply the change onto. It is required.
	Parent git.ObjectID
	// Commit is the commit whose change is applied or reverted.
	Commit        git.ObjectID
	Meta          CommitMeta
	Update        *RefUpdate
	BeforePublish BeforePublish
	HookEnv       []string
}

// PatchRequest is the input of Writer.ApplyPatch.
type PatchRequest struct {
	Repo RepoRef
	// Parent is the commit to apply the patch onto. It is required.
	Parent git.ObjectID
	// Patch is a unified diff of at most MaxPatchBytes.
	Patch         string
	Meta          CommitMeta
	Update        *RefUpdate
	BeforePublish BeforePublish
	HookEnv       []string
}

// MergeStyle is how Merge combines the branches. The values match the pull request merge styles.
type MergeStyle string

const (
	// MergeStyleMerge makes a merge commit.
	MergeStyleMerge MergeStyle = "merge"
	// MergeStyleRebase rebases the head on the base and fast-forwards.
	MergeStyleRebase MergeStyle = "rebase"
	// MergeStyleRebaseMerge rebases the head on the base, then makes a merge commit.
	MergeStyleRebaseMerge MergeStyle = "rebase-merge"
	// MergeStyleSquash makes one commit with the whole change.
	MergeStyleSquash MergeStyle = "squash"
	// MergeStyleFastForwardOnly moves the base to the head, and fails if that is not a fast-forward.
	MergeStyleFastForwardOnly MergeStyle = "fast-forward-only"
	// MergeStyleRebaseUpdate rebases the head on the base and publishes the rebased head.
	MergeStyleRebaseUpdate MergeStyle = "rebase-update-only"
)

// MergeRequest is the input of Writer.Merge.
type MergeRequest struct {
	// Base is the repository and full ref that receives the merge. Head names what is merged in.
	Base    RepoRef
	BaseRef string
	Head    RepoRef
	// HeadRev is a full ref name or a full commit id in Head.
	HeadRev string

	// Style is required unless DryRun is set.
	Style MergeStyle
	// DryRun reports mergeability and publishes nothing.
	DryRun bool

	// ExpectHead, when set, makes Merge fail with *HeadMismatchError unless HeadRev resolves to it.
	ExpectHead git.ObjectID

	// Meta gives the merge commit. Meta.Author is the author of merge and rebase-merge commits,
	// Meta.Committer is the committer of every commit the merge writes.
	Meta CommitMeta
	// SquashAuthor chooses the author of a squash commit from the authors of the squashed commits.
	// Nil means Meta.Author.
	SquashAuthor func(ctx context.Context, authors []*Signature) (*Signature, error)
	// AmendMessage returns the message for the tip of a rebase fast-forward from the message it has.
	// An empty result keeps the commits as they are.
	AmendMessage func(ctx context.Context, last string) (string, error)

	// Publish says where the result goes. It is required unless DryRun is set.
	Publish *RefUpdate
	// BeforePublish runs before Publish is applied. Git stores only.
	BeforePublish BeforePublish
	HookEnv       []string

	// The fields below apply to DryRun only.

	// ProtectedGlobs are lower-case path globs. The result lists the changed paths that match one.
	ProtectedGlobs []string
	// WantDivergence asks for Ahead and Behind.
	WantDivergence bool
	// DivergenceOnly skips the mergeability check: the result has Head, MergeBase, Ahead and Behind only.
	// It implies WantDivergence.
	DivergenceOnly bool
}

// DryRunStatus is the outcome of a dry-run merge.
type DryRunStatus int

const (
	// DryRunMergeable means the merge applies and changes something.
	DryRunMergeable DryRunStatus = iota + 1
	// DryRunConflict means the merge has conflicts.
	DryRunConflict
	// DryRunEmpty means the merge applies and changes nothing.
	DryRunEmpty
	// DryRunAncestor means Head is already part of Base.
	DryRunAncestor
)

// MergeResult is what Merge returns.
type MergeResult struct {
	// Commit is the published tip. It is nil for a dry run.
	Commit git.ObjectID
	// BaseBefore is the base tip the merge started from.
	BaseBefore git.ObjectID

	// The fields below are set by a dry run.

	Status DryRunStatus
	// MergeBase is the merge base of base and head, or the base tip when they share no history.
	MergeBase git.ObjectID
	// Head is the commit HeadRev resolved to.
	Head git.ObjectID
	// Conflicts lists at most MaxConflictsReported conflicting paths.
	Conflicts []string
	// ProtectedFiles lists the changed paths that match ProtectedGlobs, lower-cased.
	ProtectedFiles []string
	// Ahead and Behind count the commits the head has that the base lacks, and the reverse.
	Ahead, Behind int
}

// UpdateRefsRequest is the input of Writer.UpdateRefs.
type UpdateRefsRequest struct {
	// Repo is where the refs live.
	Repo RepoRef
	// From is where new objects come from. Nil means Repo.
	From    *RepoRef
	Updates []RefUpdateSpec
	// HookEnv is the environment hooks run with. Git stores only.
	HookEnv []string
}

// RefUpdateSpec is one ref change in UpdateRefs.
type RefUpdateSpec struct {
	// Name is the full ref name in Repo.
	Name string
	// NewRev is a full ref name or a full commit id in From. Ignored when Delete is set.
	NewRev string
	Delete bool
	// Force allows a non-fast-forward update. There is no compare-and-swap in a git store.
	Force bool
}

// RefResult is the outcome of one RefUpdateSpec.
type RefResult struct {
	Name string
	// Old is nil when the ref did not exist.
	Old git.ObjectID
	// New is nil when the ref was deleted.
	New git.ObjectID
}

// InitRequest is the input of Writer.Init.
type InitRequest struct {
	Repo RepoRef
	// DefaultBranch is the branch HEAD points at. Empty keeps the store default.
	DefaultBranch string
}

// ForkRequest is the input of Writer.Fork.
type ForkRequest struct {
	// Repo is the new repository, From the one it copies.
	Repo RepoRef
	From RepoRef
	// SingleBranch limits the copy to one branch.
	SingleBranch string
}

// Size is the storage a repository uses, in bytes.
type Size struct {
	// Logical is what counts against a quota.
	Logical int64
	// Unique is what the repository alone occupies.
	Unique int64
}
