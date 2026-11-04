// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"testing"

	perm_model "forgejo.org/models/perm"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChangeUndoOf(t *testing.T) {
	repo := &repo_model.Repository{Name: "repo1", OwnerName: "user2"}
	row := &asctrust.OpRow{Seq: 7, Ref: "refs/heads/main", ViaKey: "land", Mine: true, Undoable: true}

	u := changeUndoOf(repo, "abc", "abc", row)
	require.NotNil(t, u)
	assert.Equal(t, "/user2/repo1/asc/ops/undo?ref=refs%2Fheads%2Fmain&seq=7", u.URL)
	assert.Empty(t, u.Late)

	assert.Equal(t, &changeUndo{Late: changeUndoNewer}, changeUndoOf(repo, "def", "abc", row), "the branch moved on: too late")
	assert.Equal(t, &changeUndo{Late: changeUndoNewer}, changeUndoOf(repo, "def", "abc", nil), "known without asking asc")

	expired := *row
	expired.Undoable, expired.BlockKey = false, "expired"
	assert.Equal(t, &changeUndo{Late: "expired"}, changeUndoOf(repo, "abc", "abc", &expired))

	for _, via := range []string{"undo", "restack"} {
		other := *row
		other.ViaKey = via
		assert.Nil(t, changeUndoOf(repo, "abc", "abc", &other), "undoing a %s would not take the change away", via)
	}
	assert.Nil(t, changeUndoOf(repo, "abc", "abc", nil), "the log could not be read: the revert alone")
}

func TestChangeUndoCard(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	rail := func(set func(d map[string]any)) string {
		return renderPart(t, "repo/change_rail", func(d map[string]any) {
			d["ChangeFacts"] = &struct{ Additions, Deletions, Files, ChecksPass, ChecksTotal int }{}
			set(d)
		})
	}

	undo := rail(func(d map[string]any) {
		d["ChangeRevertLink"] = "/user2/repo1/pulls/2/asc/revert"
		d["ChangeUndo"] = &changeUndo{URL: "/user2/repo1/asc/ops/undo?ref=refs%2Fheads%2Fmaster&seq=7"}
	})
	assert.Contains(t, undo, `data-testid="change-undo-log"`)
	assert.Contains(t, undo, "asc_rp.change_undo_log_body")
	assert.NotContains(t, undo, "/asc/revert", "while the undo is there it replaces the revert")
	assert.NotContains(t, undo, "change-undo-late")

	late := rail(func(d map[string]any) {
		d["ChangeRevertLink"] = "/user2/repo1/pulls/2/asc/revert"
		d["ChangeUndo"] = &changeUndo{Late: changeUndoNewer}
	})
	assert.Contains(t, late, `data-testid="change-undo-late"`)
	assert.Contains(t, late, "asc_rp.change_undo_late_newer")
	assert.Contains(t, late, `action="/user2/repo1/pulls/2/asc/revert"`)
	assert.NotContains(t, late, "change-undo-log")

	blocked := rail(func(d map[string]any) { d["ChangeUndo"] = &changeUndo{Late: "not_yours"} })
	assert.Contains(t, blocked, "asc_rp.change_undo_late_blocked")
	assert.NotContains(t, blocked, "/asc/revert", "no pull request: no revert")

	assert.NotContains(t, rail(func(map[string]any) {}), `data-testid="change-undo"`)
}

func TestPrepareAscChangeUndo(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureStacks)
	newest := opsEntry(7, "refs/heads/master", "user2", 1, 2, true, "")
	newest.Via = "land"
	f.listRefLog = func(*ascv1.ListRefLogRequest) (*ascv1.ListRefLogResponse, error) {
		return &ascv1.ListRefLogResponse{Entries: []*ascv1.RefLogEntry{newest}}, nil
	}
	run := func(user int64, access perm_model.AccessMode, head bool) *changeUndo {
		t.Helper()
		var got *changeUndo
		opsGet(t, "/user2/repo1/commit/x", user, access, func(ctx *context.Context) {
			// the git backend directly: with asc on, the repository's own backend would ask asc
			gitRepo, err := git.OpenRepository(ctx, ctx.Repo.Repository.RepoPath())
			require.NoError(t, err)
			defer gitRepo.Close()
			ctx.Repo.GitRepo = gitRepo
			id, err := gitRepo.GetBranchCommitID("master")
			require.NoError(t, err)
			if !head {
				id = "2a47ca4b614a9f5a43abbd5ad851a54a616ffee6"
			}
			prepareAscChangeUndo(ctx, id)
			got, _ = ctx.Data["ChangeUndo"].(*changeUndo)
		})
		return got
	}

	u := run(2, perm_model.AccessModeWrite, true)
	require.NotNil(t, u)
	assert.Equal(t, "/user2/repo1/asc/ops/undo?ref=refs%2Fheads%2Fmaster&seq=7", u.URL)
	calls := f.callsOf("ListRefLog")
	require.Len(t, calls, 1)
	req := calls[0].Req.(*ascv1.ListRefLogRequest)
	assert.Equal(t, "refs/heads/master", req.GetRef())
	assert.EqualValues(t, 1, req.GetLimit())

	assert.Equal(t, &changeUndo{Late: changeUndoNewer}, run(2, perm_model.AccessModeWrite, false))
	assert.Len(t, f.callsOf("ListRefLog"), 1, "an older change needs no call")

	assert.Nil(t, run(2, perm_model.AccessModeRead, true), "a reader gets no undo")
	assert.Nil(t, run(0, perm_model.AccessModeRead, true), "nor does a visitor")
	assert.Len(t, f.callsOf("ListRefLog"), 1)

	newest.Actor = "user5"
	u = run(2, perm_model.AccessModeWrite, true)
	require.NotNil(t, u)
	assert.Empty(t, u.URL, "another person's landing")
	assert.Equal(t, "not_yours", u.Late)
}
