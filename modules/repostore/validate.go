// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repostore

import (
	"fmt"
	"path/filepath"
	"strings"

	"forgejo.org/modules/git"
)

// maxRefNameBytes is the longest ref name a request may carry.
const maxRefNameBytes = 1024

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidRequest, fmt.Sprintf(format, args...))
}

// Validate checks the fields every store reads. Stores that need a path also call RequirePath.
func (r RepoRef) Validate() error {
	if r.Name == "" {
		return invalid("repository name is empty")
	}
	if hasControl(r.Owner) || hasControl(r.Name) {
		return invalid("repository name has a control character")
	}
	if r.ObjectFormat != "" && !git.IsValidObjectFormat(r.ObjectFormat) {
		return invalid("unknown object format %q", r.ObjectFormat)
	}
	return nil
}

// RequirePath checks that the repository has a usable absolute git directory.
func (r RepoRef) RequirePath() error {
	if r.Path == "" {
		return invalid("repository path is empty")
	}
	// A newline would add a second line to an alternates file and open another object directory.
	if hasControl(r.Path) {
		return invalid("repository path has a control character")
	}
	if !filepath.IsAbs(r.Path) {
		return invalid("repository path is not absolute")
	}
	return nil
}

// RequireObjectFormat checks that the object format is set, for a request that creates objects from nothing.
func (r RepoRef) RequireObjectFormat() error {
	if r.ObjectFormat == "" || !git.IsValidObjectFormat(r.ObjectFormat) {
		return invalid("object format %q is not usable", r.ObjectFormat)
	}
	return nil
}

func hasControl(s string) bool {
	for _, c := range s {
		if c < 0x20 || c == 0x7f {
			return true
		}
	}
	return false
}

// ValidateRefName checks a full ref name such as refs/heads/main against the rules of
// git check-ref-format, and also rejects characters that could change the meaning of a refspec.
func ValidateRefName(name string) error {
	switch {
	case name == "":
		return invalid("ref name is empty")
	case len(name) > maxRefNameBytes:
		return invalid("ref name is longer than %d bytes", maxRefNameBytes)
	case !strings.HasPrefix(name, "refs/"):
		return invalid("ref name %q is not a full ref", name)
	case strings.HasSuffix(name, "/") || strings.HasSuffix(name, "."):
		return invalid("ref name %q ends with / or .", name)
	case strings.Contains(name, "..") || strings.Contains(name, "//") || strings.Contains(name, "@{"):
		return invalid("ref name %q has a forbidden sequence", name)
	}
	for _, c := range name {
		if c < 0x20 || c == 0x7f || strings.ContainsRune(" ~^:?*[\\", c) {
			return invalid("ref name has a forbidden character")
		}
	}
	for part := range strings.SplitSeq(name, "/") {
		if strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return invalid("ref name %q has a forbidden component", name)
		}
	}
	return nil
}

// ValidateRev checks a full commit id or a full ref name.
func ValidateRev(rev string) error {
	if rev == "" {
		return invalid("revision is empty")
	}
	if _, err := git.NewIDFromString(rev); err == nil {
		return nil
	}
	return ValidateRefName(rev)
}

// ValidateFilePath checks a path inside a tree. It rejects what cannot be a
// plain relative path: empty, absolute or traversing paths, NUL bytes and any
// .git component.
func ValidateFilePath(p string) error {
	switch {
	case p == "":
		return &InvalidPathError{Path: p}
	case len(p) > MaxPathBytes:
		return invalid("path is longer than %d bytes", MaxPathBytes)
	case strings.ContainsRune(p, 0), strings.HasPrefix(p, "/"), strings.HasSuffix(p, "/"):
		return &InvalidPathError{Path: p}
	}
	for part := range strings.SplitSeq(p, "/") {
		if part == "" || part == "." || part == ".." || strings.EqualFold(part, ".git") {
			return &InvalidPathError{Path: p}
		}
	}
	return nil
}

func validateMeta(m *CommitMeta, needCommitter bool) error {
	if len(m.Message) > MaxMessageBytes {
		return invalid("commit message is longer than %d bytes", MaxMessageBytes)
	}
	if strings.ContainsRune(m.Message, 0) {
		return invalid("commit message has a NUL byte")
	}
	if needCommitter && m.Committer == nil {
		return invalid("committer is required")
	}
	for _, s := range []*Signature{m.Author, m.Committer} {
		if s == nil {
			continue
		}
		if s.Name == "" || strings.ContainsRune(s.Name, 0) || strings.ContainsRune(s.Email, 0) {
			return invalid("signature is not usable")
		}
	}
	return nil
}

func validateID(id git.ObjectID, name string, required bool) error {
	if id == nil {
		if required {
			return invalid("%s is required", name)
		}
		return nil
	}
	if id.IsZero() {
		return invalid("%s is the zero id", name)
	}
	return nil
}

func validateUpdate(u *RefUpdate) error {
	if u == nil {
		return nil
	}
	if err := u.Repo.Validate(); err != nil {
		return err
	}
	return ValidateRefName(u.Ref)
}

// Validate checks the request. A store calls it before any side effect.
func (r *CommitRequest) Validate() error {
	if err := r.Repo.Validate(); err != nil {
		return err
	}
	if err := validateID(r.Parent, "parent", false); err != nil {
		return err
	}
	if r.Parent == nil {
		if err := r.Repo.RequireObjectFormat(); err != nil {
			return err
		}
	}
	if len(r.Ops) > MaxFileOps {
		return invalid("more than %d file operations", MaxFileOps)
	}
	for i := range r.Ops {
		op := &r.Ops[i]
		if err := ValidateFilePath(op.Path); err != nil {
			return err
		}
		switch op.Kind {
		case OpPut:
			if op.Content == nil {
				return invalid("put of %q has no content", op.Path)
			}
			switch op.Mode {
			case 0, git.EntryModeBlob, git.EntryModeExec, git.EntryModeSymlink, git.EntryModeCommit:
			default:
				return invalid("put of %q has mode %s", op.Path, op.Mode)
			}
		case OpDelete:
			if r.Parent == nil {
				return invalid("delete of %q has nothing to delete from", op.Path)
			}
		default:
			return invalid("file operation %d has unknown kind %d", i, op.Kind)
		}
	}
	if err := validateMeta(&r.Meta, r.Preview == nil); err != nil {
		return err
	}
	if r.Preview != nil && r.Update != nil {
		return invalid("a preview cannot publish")
	}
	if r.Preview != nil && r.Parent == nil {
		return invalid("a preview needs a parent")
	}
	return validateUpdate(r.Update)
}

// Validate checks the request. A store calls it before any side effect.
func (r *PickRequest) Validate() error {
	if err := r.Repo.Validate(); err != nil {
		return err
	}
	if err := validateID(r.Parent, "parent", true); err != nil {
		return err
	}
	if err := validateID(r.Commit, "commit", true); err != nil {
		return err
	}
	if err := validateMeta(&r.Meta, true); err != nil {
		return err
	}
	return validateUpdate(r.Update)
}

// Validate checks the request. A store calls it before any side effect.
func (r *PatchRequest) Validate() error {
	if err := r.Repo.Validate(); err != nil {
		return err
	}
	if err := validateID(r.Parent, "parent", true); err != nil {
		return err
	}
	if r.Patch == "" {
		return invalid("patch is empty")
	}
	if len(r.Patch) > MaxPatchBytes {
		return invalid("patch is larger than %d bytes", MaxPatchBytes)
	}
	if err := validateMeta(&r.Meta, true); err != nil {
		return err
	}
	return validateUpdate(r.Update)
}

// Validate checks the request. A store calls it before any side effect.
func (r *MergeRequest) Validate() error {
	if err := r.Base.Validate(); err != nil {
		return err
	}
	if err := r.Head.Validate(); err != nil {
		return err
	}
	if err := ValidateRefName(r.BaseRef); err != nil {
		return err
	}
	if err := ValidateRev(r.HeadRev); err != nil {
		return err
	}
	if err := validateID(r.ExpectHead, "expected head", false); err != nil {
		return err
	}
	if len(r.ProtectedGlobs) > MaxGlobs {
		return invalid("more than %d protected globs", MaxGlobs)
	}
	for _, g := range r.ProtectedGlobs {
		if g == "" || strings.ContainsRune(g, 0) {
			return invalid("protected glob is not usable")
		}
	}
	if r.DryRun {
		if r.Publish != nil || r.BeforePublish != nil {
			return invalid("a dry run cannot publish")
		}
		return nil
	}
	switch r.Style {
	case MergeStyleMerge, MergeStyleRebase, MergeStyleRebaseMerge, MergeStyleSquash, MergeStyleFastForwardOnly, MergeStyleRebaseUpdate:
	default:
		return invalid("unknown merge style %q", r.Style)
	}
	if r.Publish == nil {
		return invalid("a merge needs a publish target")
	}
	if err := validateMeta(&r.Meta, true); err != nil {
		return err
	}
	return validateUpdate(r.Publish)
}

// Validate checks the request. A store calls it before any side effect.
func (r *UpdateRefsRequest) Validate() error {
	if err := r.Repo.Validate(); err != nil {
		return err
	}
	if r.From != nil {
		if err := r.From.Validate(); err != nil {
			return err
		}
	}
	if len(r.Updates) == 0 {
		return invalid("no ref updates")
	}
	if len(r.Updates) > MaxRefUpdates {
		return invalid("more than %d ref updates", MaxRefUpdates)
	}
	seen := make(map[string]struct{}, len(r.Updates))
	for _, u := range r.Updates {
		if err := ValidateRefName(u.Name); err != nil {
			return err
		}
		if _, dup := seen[u.Name]; dup {
			return invalid("ref %q is updated twice", u.Name)
		}
		seen[u.Name] = struct{}{}
		if !u.Delete {
			if err := ValidateRev(u.NewRev); err != nil {
				return err
			}
		}
	}
	return nil
}

// Validate checks the request. A store calls it before any side effect.
func (r *InitRequest) Validate() error {
	if err := r.Repo.Validate(); err != nil {
		return err
	}
	if err := r.Repo.RequireObjectFormat(); err != nil {
		return err
	}
	return validateBranch(r.DefaultBranch)
}

// Validate checks the request. A store calls it before any side effect.
func (r *ForkRequest) Validate() error {
	if err := r.Repo.Validate(); err != nil {
		return err
	}
	if err := r.From.Validate(); err != nil {
		return err
	}
	return validateBranch(r.SingleBranch)
}

// validateBranch checks a branch name that a store passes to a command as a plain argument.
func validateBranch(name string) error {
	if name == "" {
		return nil
	}
	if strings.HasPrefix(name, "-") {
		return invalid("branch name %q looks like an option", name)
	}
	return ValidateRefName(git.BranchPrefix + name)
}
