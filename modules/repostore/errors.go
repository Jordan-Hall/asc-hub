// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repostore

import (
	"errors"
	"fmt"
)

var (
	// ErrInvalidRequest is wrapped by every validation failure.
	ErrInvalidRequest = errors.New("repostore: invalid request")
	// ErrUnsupported is returned by a store that cannot honour a request field.
	ErrUnsupported = errors.New("repostore: not supported by this store")
	// ErrExists is returned by Init and Fork when the repository exists.
	ErrExists = errors.New("repostore: repository exists")
	// ErrNoWriter is returned when no Writer is registered under the requested name.
	ErrNoWriter = errors.New("repostore: no writer registered (import forgejo.org/modules/repostore/gitcli)")
)

// InvalidPathError is returned when a file path cannot be stored in a tree.
type InvalidPathError struct {
	Path string
}

func (e *InvalidPathError) Error() string {
	return fmt.Sprintf("invalid path %q", e.Path)
}

// FileNotFoundError is returned when a file operation names a file that is not in the tree.
type FileNotFoundError struct {
	Path string
}

func (e *FileNotFoundError) Error() string {
	return fmt.Sprintf("file %q does not exist", e.Path)
}

// RefNotFoundError is returned when a ref or commit named by a request does not exist.
type RefNotFoundError struct {
	Ref string
}

func (e *RefNotFoundError) Error() string {
	return fmt.Sprintf("ref %q does not exist", e.Ref)
}

// HeadMismatchError is returned when the head of a merge is not the commit the caller expected.
type HeadMismatchError struct {
	Given, Current string
}

func (e *HeadMismatchError) Error() string {
	return fmt.Sprintf("head is %s, expected %s", e.Current, e.Given)
}

// ConflictError is returned when a merge, cherry-pick, revert or patch has conflicts.
type ConflictError struct {
	// Style is the merge style, empty outside a merge.
	Style MergeStyle
	// Files lists at most MaxConflictsReported conflicting paths when the store knows them.
	Files []string
	// CommitSHA is the commit that failed to apply during a rebase.
	CommitSHA string
	// Rebase is set when the conflict happened while rebasing.
	Rebase bool
	// Unrelated is set when the branches share no history.
	Unrelated bool
	// Diverging is set when a fast-forward-only merge cannot fast-forward.
	Diverging bool
	// StdOut and StdErr are the output of the failed operation, for logs.
	StdOut, StdErr string
	Err            error
}

func (e *ConflictError) Error() string {
	switch {
	case e.Unrelated:
		return "merge refused: unrelated histories"
	case e.Diverging:
		return "merge refused: not possible to fast-forward"
	case e.Rebase:
		return fmt.Sprintf("rebase conflict at commit %s", e.CommitSHA)
	default:
		return "merge conflict"
	}
}

func (e *ConflictError) Unwrap() error {
	return e.Err
}
