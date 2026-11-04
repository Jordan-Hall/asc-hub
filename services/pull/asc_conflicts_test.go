// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"context"
	"errors"
	"strings"
	"testing"

	"forgejo.org/services/ascbridge"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAscConflictHunks(t *testing.T) {
	marked := strings.Join([]string{
		"intro",
		"<<<<<<< ours",
		"Attempts: 3, base 200 ms",
		"||||||| base",
		"Attempts: 3",
		"=======",
		"Attempts: 5, base 100 ms",
		">>>>>>> theirs",
		"middle",
		"<<<<<<<",
		"a",
		"=======",
		"b\r",
		">>>>>>>",
		"<<<<<<< never closed",
		"x",
	}, "\n") + "\n"
	hunks := ParseAscConflictHunks(marked)
	require.Len(t, hunks, 2)
	assert.Equal(t, AscConflictHunk{Line: 2, Main: []string{"Attempts: 3, base 200 ms"}, Base: []string{"Attempts: 3"}, Yours: []string{"Attempts: 5, base 100 ms"}}, hunks[0])
	assert.Equal(t, AscConflictHunk{Line: 10, Main: []string{"a"}, Yours: []string{"b"}}, hunks[1])

	// Lines that only look like markers are text.
	assert.Empty(t, ParseAscConflictHunks("<<<<<<<< eight\n=======\n>>>>>>>x\n"))
	many := strings.Repeat("<<<<<<< a\n1\n=======\n2\n>>>>>>> b\n", ascConflictMaxHunks+5)
	assert.Len(t, ParseAscConflictHunks(many), ascConflictMaxHunks)
}

func TestHasAscConflictMarkers(t *testing.T) {
	for _, s := range []string{"<<<<<<< ours\n", "a\n>>>>>>> theirs", "|||||||\n", "x\r\n<<<<<<<\r\n"} {
		assert.True(t, HasAscConflictMarkers(s), "%q", s)
	}
	for _, s := range []string{"", "plain\n", "=======\n", "<<<<<< six\n", "<<<<<<<<\n", "a <<<<<<< b\n"} {
		assert.False(t, HasAscConflictMarkers(s), "%q", s)
	}
}

// resolvingOps answers a dry run without resolutions with a conflict of the top change, and any other call as ops does.
type resolvingOps struct {
	*fakeStackOps
	conflict *ascbridge.RestackStackResult
}

func (r *resolvingOps) RestackStack(ctx context.Context, req ascbridge.RestackStackRequest) (*ascbridge.RestackStackResult, error) {
	if req.DryRun && len(req.Resolutions) == 0 {
		r.restackReqs = append(r.restackReqs, req)
		return r.conflict, nil
	}
	return r.fakeStackOps.RestackStack(ctx, req)
}

const (
	cfChangeset = "3333333333333333333333333333333333333333333333333333333333333333"
	cfMarked    = "4444444444444444444444444444444444444444444444444444444444444444"
	cfMarkedBin = "5555555555555555555555555555555555555555555555555555555555555555"
	cfMarkedBig = "6666666666666666666666666666666666666666666666666666666666666666"
)

func conflictFixture(t *testing.T) (*restackFixture, *resolvingOps) {
	t.Helper()
	f := newRestackFixture(t)
	ops := &resolvingOps{fakeStackOps: f.ops, conflict: &ascbridge.RestackStackResult{Items: []ascbridge.RestackItem{
		{Change: ascMergeBottom, State: ascbridge.RestackRewritten},
		{Change: ascMergeTop, State: ascbridge.RestackConflict, Conflicts: []string{"docs/retries.md", "logo.png", "big.txt", "gone.txt"}, Files: []ascbridge.ConflictFile{
			{Path: "docs/retries.md", Kind: "content", ChangesetID: cfChangeset, MarkedID: cfMarked},
			{Path: "logo.png", Kind: "binary", ChangesetID: cfChangeset, MarkedID: cfMarkedBin},
			{Path: "big.txt", Kind: "content", ChangesetID: cfChangeset, MarkedID: cfMarkedBig},
			{Path: "gone.txt", Kind: "modify_delete", ChangesetID: cfChangeset},
		}},
	}}}
	useStackOps(t, ops)
	t.Cleanup(UseAscConflictReader(func(_ context.Context, repo, id string, limit int) ([]byte, bool, error) {
		assert.Equal(t, "user2/repo1", repo)
		assert.Equal(t, AscConflictMaxBytes, limit)
		switch id {
		case cfMarked:
			return []byte("a\n<<<<<<< ours\nmain\n||||||| base\nold\n=======\nyours\n>>>>>>> theirs\n"), false, nil
		case cfMarkedBin:
			return []byte("PNG\x00\x01"), false, nil
		case cfMarkedBig:
			return []byte("<<<<<<< ours\n"), true, nil
		}
		return nil, false, errors.New("unknown id")
	}))
	return f, ops
}

func TestAscPullConflicts(t *testing.T) {
	f, ops := conflictFixture(t)

	c, err := AscPullConflicts(t.Context(), t.Context(), f.top)
	require.NoError(t, err)
	require.Len(t, ops.restackReqs, 1)
	assert.True(t, ops.restackReqs[0].DryRun, "reading the conflicts writes nothing")
	assert.Equal(t, ascMergeTop, c.Change)
	assert.Equal(t, ascbridge.RestackConflict, c.State)
	require.Len(t, c.Files, 4)

	text := c.Files[0]
	assert.True(t, text.Editable)
	assert.Contains(t, text.Marked, "<<<<<<< ours")
	assert.Equal(t, []AscConflictHunk{{Line: 2, Main: []string{"main"}, Base: []string{"old"}, Yours: []string{"yours"}}}, text.Hunks)

	assert.False(t, c.Files[1].Editable, "a file that is not text has no editor")
	assert.Empty(t, c.Files[1].Marked)
	assert.False(t, c.Files[2].Editable, "a file longer than the bound has no editor")
	assert.NotEmpty(t, c.Files[2].Marked)
	assert.False(t, c.Files[3].Editable)
	assert.Empty(t, c.Files[3].Marked)
	assert.Nil(t, c.Below)

	// The bottom change has no conflict: the top one's is not its.
	c, err = AscPullConflicts(t.Context(), t.Context(), f.bottom)
	require.NoError(t, err)
	assert.Equal(t, ascbridge.RestackRewritten, c.State)
	assert.Empty(t, c.Files)
}

// A viewer without a read context (no code access, or no claim) sees each file by its path and kind only, and cannot
// resolve: no text of a file is read for them.
func TestAscPullConflictsWithoutReader(t *testing.T) {
	f, _ := conflictFixture(t)
	read := 0
	t.Cleanup(UseAscConflictReader(func(context.Context, string, string, int) ([]byte, bool, error) {
		read++
		return []byte("secret"), false, nil
	}))
	c, err := AscPullConflicts(t.Context(), nil, f.top)
	require.NoError(t, err)
	require.Len(t, c.Files, 4)
	for _, file := range c.Files {
		assert.NotEmpty(t, file.Path)
		assert.Empty(t, file.Marked)
		assert.False(t, file.Editable)
	}
	assert.Zero(t, read)

	doer, _ := ascMergePerm(t, f.top, 2)
	_, err = ResolveAscConflicts(t.Context(), nil, doer, f.top, map[string]string{"docs/retries.md": "x"})
	require.ErrorIs(t, err, ErrAscResolutionPath)
}

func TestAscPullConflictsBlockedNamesTheChangeBelow(t *testing.T) {
	f, ops := conflictFixture(t)
	ops.conflict = &ascbridge.RestackStackResult{Items: []ascbridge.RestackItem{
		{Change: ascMergeBottom, State: ascbridge.RestackConflict, Conflicts: []string{"a.go"}},
		{Change: ascMergeTop, State: ascbridge.RestackBlocked},
	}}
	c, err := AscPullConflicts(t.Context(), t.Context(), f.top)
	require.NoError(t, err)
	assert.Equal(t, ascbridge.RestackBlocked, c.State)
	require.NotNil(t, c.Below)
	assert.Equal(t, f.bottom.ID, c.Below.ID)
}

func TestResolveAscConflicts(t *testing.T) {
	f, ops := conflictFixture(t)
	doer, _ := ascMergePerm(t, f.top, 2)

	t.Run("restacks with the text written for the file", func(t *testing.T) {
		ops.restackReqs = nil
		res, err := ResolveAscConflicts(t.Context(), t.Context(), doer, f.top, map[string]string{"docs/retries.md": "a\r\nmerged\r\n"})
		require.NoError(t, err)
		assert.True(t, res.Applied)
		require.Len(t, ops.restackReqs, 3, "the conflicts, then the dry run and the apply with the resolution")
		for _, r := range ops.restackReqs[1:] {
			assert.Equal(t, []ascbridge.Resolution{{ChangesetID: cfChangeset, Path: "docs/retries.md", Content: []byte("a\nmerged\n")}}, r.Resolutions,
				"the line ends of the browser become those of the file")
		}
	})

	t.Run("refusals", func(t *testing.T) {
		for name, tc := range map[string]struct {
			contents map[string]string
			err      error
		}{
			"nothing":                 {nil, ErrAscNoConflict},
			"a file not in conflict":  {map[string]string{"other.go": "x"}, ErrAscResolutionPath},
			"a file that is not text": {map[string]string{"logo.png": "x"}, ErrAscResolutionPath},
			"a file too long":         {map[string]string{"big.txt": "x"}, ErrAscResolutionPath},
			"a delete conflict":       {map[string]string{"gone.txt": "x"}, ErrAscResolutionPath},
			"markers left":            {map[string]string{"docs/retries.md": "a\n<<<<<<< ours\nb\n"}, ErrAscResolutionMarkers},
			"invalid UTF-8":           {map[string]string{"docs/retries.md": "a\xff"}, ErrAscResolutionPath},
			"too much text":           {map[string]string{"docs/retries.md": strings.Repeat("x", ascbridge.MaxResolutionBytes+1)}, ErrAscResolutionPath},
		} {
			ops.restackReqs = nil
			_, err := ResolveAscConflicts(t.Context(), t.Context(), doer, f.top, tc.contents)
			require.ErrorIs(t, err, tc.err, name)
			for _, r := range ops.restackReqs {
				assert.True(t, r.DryRun, "%s: nothing is written", name)
			}
		}
	})

	t.Run("a conflict that is gone", func(t *testing.T) {
		prev := ops.conflict
		ops.conflict = ops.restackDry
		t.Cleanup(func() { ops.conflict = prev })
		_, err := ResolveAscConflicts(t.Context(), t.Context(), doer, f.top, map[string]string{"docs/retries.md": "x"})
		require.ErrorIs(t, err, ErrAscNoConflict)
	})

	t.Run("only a person who may restack", func(t *testing.T) {
		ops.restackReqs = nil
		reader, _ := ascMergePerm(t, f.top, 5)
		_, err := ResolveAscConflicts(t.Context(), t.Context(), reader, f.top, map[string]string{"docs/retries.md": "x"})
		require.ErrorIs(t, err, ErrAscRestackNotAllowed)
		_, err = ResolveAscConflicts(t.Context(), t.Context(), nil, f.top, map[string]string{"docs/retries.md": "x"})
		require.ErrorIs(t, err, ErrAscRestackNotAllowed)
		assert.Empty(t, ops.restackReqs, "asc is not asked")
	})
}
