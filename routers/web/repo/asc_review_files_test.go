// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/http"
	"strings"
	"testing"
	"time"

	actions_model "forgejo.org/models/actions"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/base"
	"forgejo.org/modules/git"
	"forgejo.org/modules/templates"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/contexttest"
	"forgejo.org/services/gitdiff"
	pull_service "forgejo.org/services/pull"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// renderPart renders one template with the page data the handlers would set. The mock locale answers the key, so the
// output names the words a person would read.
func renderPart(t *testing.T, name string, set func(d map[string]any)) string {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, "/user2/repo1/pulls/3/files", contexttest.MockContextOption{Render: templates.HTMLRenderer()})
	contexttest.LoadRepo(t, ctx, 1)
	issue := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 3})
	require.NoError(t, issue.LoadRepo(ctx))
	require.NoError(t, issue.LoadPullRequest(ctx))
	ctx.Data["Issue"] = issue
	ctx.Data["Repository"] = ctx.Repo.Repository
	ctx.Data["RepoLink"] = ctx.Repo.RepoLink
	ctx.Data["Link"] = issue.Link() + "/files"
	set(ctx.Data)
	ctx.HTML(http.StatusOK, base.TplName(name))
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	return resp.Body.String()
}

func TestAscTabStripOfAStackedChange(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	stacked := renderPart(t, "repo/pulls/tab_menu", func(d map[string]any) {
		d["AscStackPanel"] = &pull_service.AscStackPanel{}
		d["AscVersionsLatest"] = uint32(4)
		d["PageIsPullFiles"] = true
		d["NumFiles"] = 3
		d["NumCommits"] = 2
	})
	order := []string{"asc_review.rev_overview", "repo.pulls.tab_files", "asc_review.rev_tab_versions", "asc_review.rev_checks", "asc_review.rev_tab_how", "repo.pulls.tab_commits"}
	last := -1
	for _, key := range order {
		at := strings.Index(stacked, key)
		require.Greater(t, at, last, "%s comes after the one before it in the board's order", key)
		last = at
	}
	assert.Contains(t, stacked, "v1&gt;v4", "the versions tab names the range")
	assert.Contains(t, stacked, "/files#asc-versions")
	assert.Contains(t, stacked, "#asc-how-made")

	single := renderPart(t, "repo/pulls/tab_menu", func(d map[string]any) {
		d["AscStackPanel"] = &pull_service.AscStackPanel{}
		d["AscVersionsLatest"] = uint32(1)
	})
	assert.NotContains(t, single, "v1&gt;")

	plain := renderPart(t, "repo/pulls/tab_menu", func(d map[string]any) { d["NumFiles"] = 3 })
	assert.Contains(t, plain, "repo.pulls.tab_conversation", "a pull request that is not stacked keeps the Forgejo tabs")
	assert.NotContains(t, plain, "asc_review.rev_overview")
	assert.NotContains(t, plain, "rev_tab_versions")
}

func TestAscYourReviewCard(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	poster := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
	other := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	panel := func(viewer *user_model.User, all bool, extra func(map[string]any)) string {
		return renderPart(t, "repo/pulls/asc_panel", func(d map[string]any) {
			d["AscStackPanel"] = &pull_service.AscStackPanel{}
			d["PageIsPullFiles"] = true
			d["IsSigned"] = viewer != nil
			d["SignedUser"] = viewer
			d["IsShowingAllCommits"] = all
			d["AfterCommitID"] = "abc123"
			d["Diff"] = &gitdiff.Diff{NumFiles: 3, NumViewedFiles: 1}
			if extra != nil {
				extra(d)
			}
		})
	}

	reviewer := panel(other, true, nil)
	assert.Contains(t, reviewer, `data-testid="asc-your-review"`)
	assert.Contains(t, reviewer, `data-viewed="1"`)
	assert.Contains(t, reviewer, `data-total="3"`)
	assert.Contains(t, reviewer, "asc_review.rev_you_intent")
	assert.Contains(t, reviewer, `data-testid="asc-you-approve"`)
	assert.Contains(t, reviewer, `name="commit_id" value="abc123"`)
	assert.Contains(t, reviewer, `data-asc-open-review="reject"`)
	assert.Contains(t, reviewer, `data-asc-open-review="comment"`)

	assert.NotContains(t, panel(poster, true, nil), "asc-you-approve", "the author does not approve their own change")
	assert.NotContains(t, panel(poster, true, nil), `data-asc-open-review="reject"`)
	assert.Contains(t, panel(poster, true, nil), `data-asc-open-review="comment"`, "the author can still comment")
	assert.NotContains(t, panel(other, false, nil), "asc-you-approve", "a verdict is given on the whole change, not on a version range")
	assert.NotContains(t, panel(nil, true, nil), "asc-your-review", "signed out: no card")
	assert.Contains(t, panel(nil, true, nil), "asc-panel", "the links card stays")
	assert.NotContains(t, panel(other, true, func(d map[string]any) { d["PageIsPullFiles"] = false }), "asc-your-review")
}

func TestAscDiffBoxClassBarAndStates(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	stub := func(d map[string]any) {
		d["PageIsPullFiles"] = true
		d["IsShowingAllCommits"] = true
		d["GetBlobByPathForCommit"] = func(*git.Commit, string) *git.Blob { return nil }
		d["GetSniffedTypeForBlob"] = func(*git.Blob) any { return nil }
		d["IsSniffedTypeAnImage"] = func(any) bool { return false }
		d["IsCsvFile"] = func(*gitdiff.DiffFile) bool { return false }
		d["Diff"] = &gitdiff.Diff{NumFiles: 2, Files: []*gitdiff.DiffFile{
			{Name: "src/a.rs", NameHash: "h1", IsBin: true},
			{Name: "Cargo.lock", NameHash: "h2", IsBin: true},
		}}
	}
	body := renderPart(t, "repo/diff/box", func(d map[string]any) {
		stub(d)
		d["AscFileClassBadges"] = map[string]string{"src/a.rs": "Logic", "Cargo.lock": "Lockfile"}
		d["AscFileClassCounts"] = []pull_service.AscClassCount{{Label: "Lockfile", Count: 1}, {Label: "Logic", Count: 1}}
		d["AscPrevVersion"] = uint32(3)
	})
	assert.Contains(t, body, `data-testid="asc-classbar"`)
	assert.Contains(t, body, `data-asc-class="Lockfile" aria-pressed="false"`)
	assert.Contains(t, body, `id="diff-h1" data-asc-class="Logic"`, "each file box names its class")
	assert.Contains(t, body, `id="diff-h2" data-asc-class="Lockfile"`)
	assert.Contains(t, body, "data-asc-diff-state")
	assert.Contains(t, body, "data-asc-skeleton")
	assert.Contains(t, body, "data-asc-diff-retry")
	assert.Contains(t, body, "/pulls/3/files?asc_to=3", "Open previous version names the version before")
	assert.NotContains(t, body, "asc-changed-in", "the whole change has no Changed in badge")

	none := renderPart(t, "repo/diff/box", func(d map[string]any) {
		stub(d)
		d["AscVersionFrom"] = uint32(3)
		d["AscVersionTo"] = uint32(4)
	})
	assert.NotContains(t, none, "asc-classbar", "no classes: no bar")
	assert.NotContains(t, none, "asc-diff-prev", "no earlier version: no link")
	assert.Contains(t, none, "asc-changed-in", "in the step from v3 to v4 each file is marked")
	assert.Contains(t, none, "asc_review.rev_changed_in")
}

func TestAscVersionsPanelChangedSince(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	body := func(n int) string {
		return renderPart(t, "repo/pulls/asc_versions", func(d map[string]any) {
			d["AscVersions"] = &pull_service.AscVersionList{Versions: []pull_service.AscVersion{{N: 1}, {N: 2}}}
			d["AscChangedSince"] = n
			d["AscVersionDetails"] = map[uint32]pull_service.AscVersionDetail{}
			d["AscVersionChecks"] = map[uint32][]AscVersionCheck{}
		})
	}
	with := body(2)
	assert.Contains(t, with, `data-testid="asc-changed-since-count"`)
	assert.Contains(t, with, `data-asc-changed-only`)
	assert.Contains(t, with, `id="asc-versions"`)
	assert.NotContains(t, body(0), "asc-changed-since-count")
}

func TestAscChangedSinceCount(t *testing.T) {
	assert.Equal(t, 0, ascChangedSinceCount(nil))
	assert.Equal(t, 2, ascChangedSinceCount(&gitdiff.Diff{Files: []*gitdiff.DiffFile{
		{Name: "a", HasChangedSinceLastReview: true}, {Name: "b"}, {Name: "c", HasChangedSinceLastReview: true},
	}}))
}

func TestAscPullChecksRunsAndQuarantine(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	started := time.Now().Add(-time.Minute)
	checks := asctrust.BuildPullChecks(asctrust.PullChecksIn{
		Version: 4, Head: "abcdef1234567890", Now: time.Now(),
		Statuses: []asctrust.StatusIn{
			{Context: "unit", State: "failure", RunID: 7},
			{Context: "manual", State: "success"},
		},
		Runs: []asctrust.RunIn{{ID: 7, State: "failure", Started: started, Stopped: started.Add(12 * time.Second)}},
		Causes: []asctrust.CauseIn{
			{Group: asctrust.GroupRow{ID: 5, Title: "assertion failed", StateKey: "open"}, Occurrences: []asctrust.OccurrenceRow{{RunID: 7, Message: "thread panicked"}}},
			{Group: asctrust.GroupRow{ID: 9, TestName: "flaky_one", StateKey: "open", Flaky: true}, Occurrences: []asctrust.OccurrenceRow{{RunID: 7, Message: "timeout"}}},
		},
		CausesKnown: true,
	})
	page := func(allow bool, canRerun bool, repro string) string {
		return renderPart(t, "repo/pulls/asc_checks", func(d map[string]any) {
			d["Title"] = "Checks"
			d["AscPullChecks"] = &ascPullChecksPage{
				PullURL: "/user2/repo1/pulls/3", ChecksURL: "/user2/repo1/asc/checks", RerunURL: "/user2/repo1/pulls/3/asc/checks/rerun",
				NewIssueURL: "/user2/repo1/issues/new", CanRerun: canRerun, CanWrite: true, AllowQuarantine: allow, Repro: repro, Version: 4,
				Ops: map[uint64]string{9: "op"}, Index: 3,
				Checks: checks,
			}
		})
	}
	body := page(true, true, "asc ci checkout --repo user2/repo1 --rev abc --dir repro && cd repro")
	assert.Contains(t, body, `data-testid="asc-copy-repro"`)
	assert.Contains(t, body, `data-clipboard-text="asc ci checkout --repo user2/repo1 --rev abc --dir repro &amp;&amp; cd repro"`)
	assert.Contains(t, body, `data-testid="asc-quarantine"`)
	assert.Contains(t, body, `data-testid="asc-run-row"`)
	assert.Contains(t, body, `name="run" value="7"`)
	assert.Contains(t, body, "asc_trust.pull.run_on")
	assert.Contains(t, body, `data-testid="asc-status-took"`)
	assert.Contains(t, body, "12 s")
	assert.Equal(t, 1, strings.Count(body, `data-testid="asc-run-row"`), "a row with no run has no button")

	assert.NotContains(t, page(false, true, ""), "asc-quarantine", "the policy does not allow quarantine")
	assert.NotContains(t, page(false, true, ""), "asc-copy-repro", "no head: no command")
	assert.NotContains(t, page(true, false, ""), "asc-run-row", "no right to run: no button")
}

func TestAscStatusRunIDAndRepro(t *testing.T) {
	runs := []*actions_model.ActionRun{{ID: 70, Index: 7}, {ID: 80, Index: 12}}
	assert.Equal(t, int64(70), ascStatusRunID("https://hub.example/user2/repo1/actions/runs/7/jobs/0", runs))
	assert.Equal(t, int64(80), ascStatusRunID("/user2/repo1/actions/runs/12", runs))
	assert.Equal(t, int64(0), ascStatusRunID("/user2/repo1/actions/runs/70/jobs/1", runs), "the index is matched, not the id")
	assert.Equal(t, int64(0), ascStatusRunID("https://ci.example/build/7", runs))
	assert.Equal(t, int64(0), ascStatusRunID("", runs))

	assert.Equal(t, "asc ci checkout --repo user2/repo1 --rev abc --dir repro && cd repro", ascReproCommand("user2/repo1", "abc"))
	assert.Empty(t, ascReproCommand("user2/repo1", ""))
	assert.Empty(t, ascReproCommand("", "abc"))
	assert.NotContains(t, ascReproCommand("user2/repo1", "abc"), "git ", "an asc command, not git")
}

func TestAscCompareNotesOnOpen(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	body := func(compare bool, backend string) string {
		return renderPart(t, "repo/issue/new_form", func(d map[string]any) {
			d["PageIsComparePull"] = compare
			d["PullRequestWorkInProgressPrefixes"] = []string{"WIP:"}
			d["Repository"] = &repo_model.Repository{ID: 1, Name: "repo1", OwnerName: "user2", StorageBackend: backend}
		})
	}
	on := body(true, "asc")
	assert.Contains(t, on, `data-testid="asc-open-notes"`)
	assert.Contains(t, on, "asc_rp.cmp_checks_start")
	assert.Contains(t, on, "asc_rp.cmp_signed_note")
	assert.NotContains(t, body(true, "git"), "asc-open-notes", "a git repository is not told about asc")
	assert.NotContains(t, body(false, "asc"), "asc-open-notes", "a new issue has no checks or signature note")
}
