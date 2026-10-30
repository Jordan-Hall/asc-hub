// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"context"
	"errors"
	"testing"

	"forgejo.org/models/db"
	git_model "forgejo.org/models/git"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git/ascbackend"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/modules/translation"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	clsDocs  = ascv1.ChangeClass_CHANGE_CLASS_DOCS
	clsLock  = ascv1.ChangeClass_CHANGE_CLASS_LOCKFILE
	clsLogic = ascv1.ChangeClass_CHANGE_CLASS_LOGIC
	clsTest  = ascv1.ChangeClass_CHANGE_CLASS_TEST
)

// ascFixture is a pull request on an asc repository whose base branch has a protection rule.
type ascFixture struct {
	pr    *issues_model.PullRequest
	pb    *git_model.ProtectedBranch
	calls int
	// answer and err are what the fake Classify returns.
	answer *ascbackend.Classification
	err    error
	// repoPath, base and head are the last request.
	repoPath, base, head string
}

// newAscFixture prepares the database and replaces ascClassify. The caller must have set
// setting.Asc.Enabled (and restore it).
func newAscFixture(t *testing.T, rule git_model.ProtectedBranch, approvals int) *ascFixture {
	t.Helper()
	require.NoError(t, unittest.PrepareTestDatabase())

	pr := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})
	require.NoError(t, pr.LoadBaseRepo(db.DefaultContext))
	_, err := db.GetEngine(db.DefaultContext).ID(pr.BaseRepoID).Cols("storage_backend").Update(&repo_model.Repository{StorageBackend: "asc"})
	require.NoError(t, err)
	pr.BaseRepo.StorageBackend = "asc"

	// Only the approvals this test grants.
	_, err = db.GetEngine(db.DefaultContext).Where("issue_id = ?", pr.IssueID).Delete(new(issues_model.Review))
	require.NoError(t, err)
	for i := range approvals {
		_, err = db.GetEngine(db.DefaultContext).Insert(&issues_model.Review{
			Type: issues_model.ReviewTypeApprove, ReviewerID: int64(100 + i), IssueID: pr.IssueID, Official: true,
		})
		require.NoError(t, err)
	}

	rule.RepoID, rule.RuleName = pr.BaseRepoID, pr.BaseBranch
	_, err = db.GetEngine(db.DefaultContext).Insert(&rule)
	require.NoError(t, err)

	f := &ascFixture{pr: pr, pb: &rule}
	old := ascClassify
	t.Cleanup(func() { ascClassify = old })
	ascClassify = func(_ context.Context, repoPath, base, head string) (*ascbackend.Classification, error) {
		f.calls++
		f.repoPath, f.base, f.head = repoPath, base, head
		return f.answer, f.err
	}
	return f
}

func classification(overall ascv1.ChangeClass, files map[string]ascv1.ChangeClass) *ascbackend.Classification {
	c := &ascbackend.Classification{Overall: overall}
	for p, cl := range files {
		c.Files = append(c.Files, ascbackend.FileClass{Path: p, Class: cl})
	}
	return c
}

func TestAscRelaxedApprovals(t *testing.T) {
	docsRule := func() git_model.ProtectedBranch {
		return git_model.ProtectedBranch{
			RequiredApprovals: 2, AscRelaxedApprovals: 1, AscRelaxedClasses: []string{"docs", "lockfile"},
		}
	}
	ctx := db.DefaultContext
	readme := map[string]ascv1.ChangeClass{"README.md": clsDocs}

	t.Run("docs and lockfiles with the relaxed count pass", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Asc.Enabled, true)()
		f := newAscFixture(t, docsRule(), 1)
		f.answer = classification(clsLock, map[string]ascv1.ChangeClass{"README.md": clsDocs, "Cargo.lock": clsLock})
		assert.True(t, HasEnoughApprovals(ctx, f.pb, f.pr))
		assert.Equal(t, 1, f.calls)
		assert.Equal(t, "refs/heads/master", f.base)
		assert.Equal(t, "refs/pull/3/head", f.head)
		assert.Equal(t, f.pr.BaseRepo.RepoPath(), f.repoPath)
	})

	t.Run("the normal count passes without asking asc", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Asc.Enabled, true)()
		f := newAscFixture(t, docsRule(), 2)
		assert.True(t, HasEnoughApprovals(ctx, f.pb, f.pr))
		assert.Zero(t, f.calls)
	})

	t.Run("below the relaxed count does not ask asc", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Asc.Enabled, true)()
		f := newAscFixture(t, docsRule(), 0)
		f.answer = classification(clsDocs, readme)
		assert.False(t, HasEnoughApprovals(ctx, f.pb, f.pr))
		assert.Zero(t, f.calls)
	})

	t.Run("relaxed to zero approvals", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Asc.Enabled, true)()
		rule := docsRule()
		rule.AscRelaxedApprovals = 0
		f := newAscFixture(t, rule, 0)
		f.answer = classification(clsDocs, readme)
		assert.True(t, HasEnoughApprovals(ctx, f.pb, f.pr))
	})

	cases := []struct {
		name   string
		rule   func(*git_model.ProtectedBranch)
		answer *ascbackend.Classification
		err    error
		setup  func(*ascFixture)
		calls  int // asc calls expected when neither rule nor setup is given
	}{
		{
			name:   "one logic file",
			answer: classification(clsLogic, map[string]ascv1.ChangeClass{"README.md": clsDocs, "main.go": clsLogic}),
			calls:  1,
		},
		{
			name:   "a class that is not listed",
			answer: classification(clsTest, map[string]ascv1.ChangeClass{"a_test.go": clsTest}),
			calls:  1,
		},
		{
			name:   "an unknown class value is logic",
			answer: classification(ascv1.ChangeClass(99), map[string]ascv1.ChangeClass{"README.md": ascv1.ChangeClass(99)}),
			calls:  1,
		},
		{
			name:   "overall that is not listed",
			answer: classification(clsLogic, readme),
			calls:  1,
		},
		{
			name:   "no changed files",
			answer: classification(clsDocs, nil),
			calls:  1,
		},
		{
			name:   "protected file pattern wins",
			rule:   func(pb *git_model.ProtectedBranch) { pb.ProtectedFilePatterns = "docs/secret/**;*.lock" },
			answer: classification(clsLock, map[string]ascv1.ChangeClass{"README.md": clsDocs, "Cargo.lock": clsLock}),
		},
		{
			name:   "protected file pattern is case-insensitive",
			rule:   func(pb *git_model.ProtectedBranch) { pb.ProtectedFilePatterns = "docs/secret/**" },
			answer: classification(clsDocs, map[string]ascv1.ChangeClass{"Docs/Secret/a.md": clsDocs}),
		},
		{
			name:   "protected files recorded by the merge check",
			answer: classification(clsDocs, readme),
			setup:  func(f *ascFixture) { f.pr.ChangedProtectedFiles = []string{"ci.yml"} },
		},
		{
			name:  "asc fails",
			err:   errors.New("unavailable"),
			calls: 1,
		},
		{
			name: "relaxed count not below the required count",
			rule: func(pb *git_model.ProtectedBranch) { pb.AscRelaxedApprovals = 2 },
		},
		{
			name: "negative relaxed count",
			rule: func(pb *git_model.ProtectedBranch) { pb.AscRelaxedApprovals = -1 },
		},
		{
			name: "no classes",
			rule: func(pb *git_model.ProtectedBranch) { pb.AscRelaxedClasses = nil },
		},
		{
			name: "logic in the stored list is ignored",
			rule: func(pb *git_model.ProtectedBranch) { pb.AscRelaxedClasses = []string{"logic", "nonsense"} },
		},
		{
			name:  "repository is not on asc",
			setup: func(f *ascFixture) { f.pr.BaseRepo.StorageBackend = "git" },
		},
		{
			name:  "repository is dual",
			setup: func(f *ascFixture) { f.pr.BaseRepo.StorageBackend = "dual" },
		},
		{
			name:  "repository has an unknown backend",
			setup: func(f *ascFixture) { f.pr.BaseRepo.StorageBackend = "zzz" },
		},
		{
			name:  "asc is disabled",
			setup: func(*ascFixture) { setting.Asc.Enabled = false },
		},
	}
	for _, tc := range cases {
		t.Run("not relaxed: "+tc.name, func(t *testing.T) {
			defer test.MockVariableValue(&setting.Asc.Enabled, true)()
			rule := docsRule()
			if tc.rule != nil {
				tc.rule(&rule)
			}
			f := newAscFixture(t, rule, 1)
			f.answer, f.err = tc.answer, tc.err
			if tc.answer == nil && tc.err == nil {
				f.answer = classification(clsDocs, readme)
			}
			if tc.setup != nil {
				tc.setup(f)
			}
			assert.False(t, HasEnoughApprovals(ctx, f.pb, f.pr))
			if tc.setup == nil && tc.rule == nil {
				assert.Equal(t, tc.calls, f.calls)
			}
		})
	}

	t.Run("a merge goes through the same gate", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Asc.Enabled, true)()
		f := newAscFixture(t, docsRule(), 1)
		f.answer = classification(clsDocs, readme)
		rule, err := CheckPullBranchProtections(ctx, f.pr, true)
		require.NoError(t, err)
		assert.Nil(t, rule)

		f.answer = classification(clsLogic, map[string]ascv1.ChangeClass{"main.go": clsLogic})
		rule, err = CheckPullBranchProtections(ctx, f.pr, true)
		require.Error(t, err)
		assert.NotNil(t, rule)
	})
}

func TestAscFileClassBadges(t *testing.T) {
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	f := newAscFixture(t, git_model.ProtectedBranch{RequiredApprovals: 1}, 0)
	f.answer = classification(clsLogic, map[string]ascv1.ChangeClass{"README.md": clsDocs, "main.go": clsLogic})
	locale := &translation.MockLocale{}

	got := AscFileClassBadges(db.DefaultContext, locale, f.pr)
	assert.Equal(t, map[string]string{"README.md": "repo.diff.asc_class.docs", "main.go": "repo.diff.asc_class.logic"}, got)

	f.err = errors.New("unavailable")
	assert.Nil(t, AscFileClassBadges(db.DefaultContext, locale, f.pr))

	f.err = nil
	f.pr.BaseRepo.StorageBackend = "git"
	assert.Nil(t, AscFileClassBadges(db.DefaultContext, locale, f.pr))
}

func TestAscFileClassCounts(t *testing.T) {
	assert.Nil(t, AscFileClassCounts(nil))
	got := AscFileClassCounts(map[string]string{"a.rs": "Logic", "b.rs": "Logic", "c.md": "Docs", "Cargo.lock": "Lockfile", "d.md": "Docs", "e.rs": "Logic"})
	assert.Equal(t, []AscClassCount{{"Logic", 3}, {"Docs", 2}, {"Lockfile", 1}}, got, "most files first")
	tied := AscFileClassCounts(map[string]string{"x": "Test", "y": "Docs"})
	assert.Equal(t, []AscClassCount{{"Docs", 1}, {"Test", 1}}, tied, "equal counts in alphabetical order")
}
