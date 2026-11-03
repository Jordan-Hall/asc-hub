// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package issue

import (
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	project_model "forgejo.org/models/project"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseProjectCardRef(t *testing.T) {
	for _, c := range []struct {
		ref, owner, repo string
		index            int64
		ok               bool
	}{
		{"#12", "", "", 12, true},
		{" 12 ", "", "", 12, true},
		{"nova#3", "", "nova", 3, true},
		{"acme/nova#3", "acme", "nova", 3, true},
		{"acme/nova.go#7", "acme", "nova.go", 7, true},
		{"nova3", "", "", 0, false},
		{"#0", "", "", 0, false},
		{"#-2", "", "", 0, false},
		{"#+2", "", "", 0, false},
		{"a/b/c#1", "", "", 0, false},
		{"/nova#1", "", "", 0, false},
		{"nova#", "", "", 0, false},
		{"", "", "", 0, false},
	} {
		owner, repo, index, ok := ParseProjectCardRef(c.ref)
		assert.Equal(t, c.ok, ok, c.ref)
		assert.Equal(t, c.owner, owner, c.ref)
		assert.Equal(t, c.repo, repo, c.ref)
		assert.Equal(t, c.index, index, c.ref)
	}
}

func cardErrKey(err error) string {
	if e, ok := err.(ProjectCardError); ok {
		return e.Key
	}
	return ""
}

func TestAddProjectCardToARepositoryProject(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	user4 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})
	project := unittest.AssertExistsAndLoadBean(t, &project_model.Project{ID: 1})

	issue, err := AddProjectCard(db.DefaultContext, user2, project, "#5")
	require.NoError(t, err)
	assert.EqualValues(t, 11, issue.ID)
	column, err := project.GetDefaultColumn(db.DefaultContext)
	require.NoError(t, err)
	unittest.AssertExistsAndLoadBean(t, &project_model.ProjectIssue{IssueID: 11, ProjectID: 1, ProjectColumnID: column.ID})
	unittest.AssertExistsAndLoadBean(t, &issues_model.Comment{IssueID: 11, Type: issues_model.CommentTypeProject, ProjectID: 1})

	for ref, key := range map[string]string{
		"#1":            "asc_work.pj_card_already",
		"user2/repo2#1": "asc_work.pj_card_other_repo",
		"repo2#1":       "asc_work.pj_card_other_repo",
		"#999":          "asc_work.pj_card_not_found",
		"not a ref":     "asc_work.pj_card_bad_ref",
	} {
		_, err := AddProjectCard(db.DefaultContext, user2, project, ref)
		assert.Equal(t, key, cardErrKey(err), ref)
	}

	_, err = AddProjectCard(db.DefaultContext, user4, project, "user2/repo1#1")
	assert.Equal(t, "asc_work.pj_card_no_permission", cardErrKey(err), "a reader of the repository cannot change an issue's project")
}

func TestAddProjectCardToAUserProject(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	user4 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})
	project := unittest.AssertExistsAndLoadBean(t, &project_model.Project{ID: 5})

	_, err := AddProjectCard(db.DefaultContext, user4, project, "repo2#2")
	assert.Equal(t, "asc_work.pj_card_not_found", cardErrKey(err), "a private repository is not told apart from a missing one")

	issue, err := AddProjectCard(db.DefaultContext, user2, project, "user2/repo2#2")
	require.NoError(t, err)
	assert.EqualValues(t, 7, issue.ID)
	unittest.AssertExistsAndLoadBean(t, &project_model.ProjectIssue{IssueID: 7, ProjectID: 5})

	for ref, key := range map[string]string{
		"#2":            "asc_work.pj_card_need_repo",
		"user3/repo3#1": "asc_work.pj_card_other_owner",
		"nothere#1":     "asc_work.pj_card_not_found",
	} {
		_, err := AddProjectCard(db.DefaultContext, user2, project, ref)
		assert.Equal(t, key, cardErrKey(err), ref)
	}
}
