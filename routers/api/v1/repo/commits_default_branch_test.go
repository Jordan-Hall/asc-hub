// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"errors"
	"fmt"
	"testing"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/git"
	"forgejo.org/modules/git/ascbackend"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The commit list of a repository that has no branch named in the request starts at its HEAD. A repository in asc has
// no HEAD to read (the backend says it is unsupported), and the list answered 500 for it; it starts at the default
// branch the hub keeps.
func TestDefaultBranchFrom(t *testing.T) {
	repo := &repo_model.Repository{DefaultBranch: "trunk"}

	t.Run("a git repository: HEAD", func(t *testing.T) {
		got, err := defaultBranchFrom(func() (*git.Branch, error) { return &git.Branch{Name: "main"}, nil }, repo)
		require.NoError(t, err)
		assert.Equal(t, "main", got)
	})

	t.Run("an asc repository: the default branch", func(t *testing.T) {
		got, err := defaultBranchFrom(func() (*git.Branch, error) { return nil, ascbackend.ErrUnsupported }, repo)
		require.NoError(t, err)
		assert.Equal(t, "trunk", got)

		got, err = defaultBranchFrom(func() (*git.Branch, error) {
			return nil, fmt.Errorf("head: %w", ascbackend.ErrUnsupported)
		}, repo)
		require.NoError(t, err)
		assert.Equal(t, "trunk", got, "wrapped")
	})

	t.Run("any other failure stays a failure", func(t *testing.T) {
		boom := errors.New("boom")
		_, err := defaultBranchFrom(func() (*git.Branch, error) { return nil, boom }, repo)
		assert.ErrorIs(t, err, boom)
		_, err = defaultBranchFrom(func() (*git.Branch, error) { return nil, ascbackend.ErrUnsupported }, &repo_model.Repository{})
		assert.ErrorIs(t, err, ascbackend.ErrUnsupported, "no default branch to fall back to")
		_, err = defaultBranchFrom(func() (*git.Branch, error) { return nil, ascbackend.ErrUnsupported }, nil)
		assert.ErrorIs(t, err, ascbackend.ErrUnsupported)
	})
}
