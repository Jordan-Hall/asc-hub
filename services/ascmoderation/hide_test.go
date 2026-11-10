// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascmoderation

import (
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/moderation"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/structs"
	issue_service "forgejo.org/services/issue"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHideAndUnhideAProfile(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	admin := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
	require.Equal(t, structs.VisibleTypePublic, unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4}).Visibility)

	require.NoError(t, Hide(ctx, admin, moderation.ReportedContentTypeUser, 4))
	assert.Equal(t, structs.VisibleTypePrivate, unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4}).Visibility)
	require.ErrorIs(t, Hide(ctx, admin, moderation.ReportedContentTypeUser, 4), ErrAlreadyHidden)
	h, err := moderation.GetHidden(ctx, moderation.ReportedContentTypeUser, 4)
	require.NoError(t, err)
	require.NotNil(t, h)
	assert.Equal(t, int64(1), h.ModeratorID)

	require.NoError(t, Unhide(ctx, moderation.ReportedContentTypeUser, 4))
	assert.Equal(t, structs.VisibleTypePublic, unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4}).Visibility)
	hidden, err := moderation.IsHidden(ctx, moderation.ReportedContentTypeUser, 4)
	require.NoError(t, err)
	assert.False(t, hidden)
	require.NoError(t, Unhide(ctx, moderation.ReportedContentTypeUser, 4), "undoing what is not hidden changes nothing")
}

func TestHideMakesARepositoryPrivateAndUnhidePutsBackWhatItWas(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	admin := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})

	require.NoError(t, Hide(ctx, admin, moderation.ReportedContentTypeRepository, 1))
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	assert.True(t, repo.IsPrivate)
	require.NoError(t, Unhide(ctx, moderation.ReportedContentTypeRepository, 1))
	repo = unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	assert.False(t, repo.IsPrivate)
	assert.False(t, repo.IsInternal)

	// an internal repository is private while hidden and internal again after
	_, err := db.GetEngine(ctx).Exec("UPDATE `repository` SET is_private = ?, is_internal = ? WHERE id = 1", true, true)
	require.NoError(t, err)
	require.NoError(t, Hide(ctx, admin, moderation.ReportedContentTypeRepository, 1))
	repo = unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	assert.True(t, repo.IsPrivate)
	assert.False(t, repo.IsInternal, "hidden is private to everyone, not visible to signed-in users")
	require.NoError(t, Unhide(ctx, moderation.ReportedContentTypeRepository, 1))
	repo = unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	assert.True(t, repo.IsPrivate)
	assert.True(t, repo.IsInternal)
}

func TestHideReplacesTheTextOfAnIssueAndACommentAndTheOwnerCannotChangeIt(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	admin := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
	before := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 1})
	comment := unittest.AssertExistsAndLoadBean(t, &issues_model.Comment{ID: 2})
	owner := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: comment.PosterID})
	require.False(t, owner.IsAdmin)

	require.NoError(t, Hide(ctx, admin, moderation.ReportedContentTypeIssue, 1))
	got := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 1})
	assert.Equal(t, HiddenTitle, got.Title)
	assert.Equal(t, HiddenText, got.Content)
	require.NoError(t, Hide(ctx, admin, moderation.ReportedContentTypeComment, 2))
	gotComment := unittest.AssertExistsAndLoadBean(t, &issues_model.Comment{ID: 2})
	assert.Equal(t, HiddenText, gotComment.Content)
	assert.Equal(t, comment.UpdatedUnix, gotComment.UpdatedUnix, "hiding is not an edit by the owner")

	// the owner cannot write the text back while the hide stands
	poster := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2}) // the owner of the repository
	require.False(t, poster.IsAdmin)
	require.NoError(t, got.LoadRepo(ctx))
	err := issue_service.ChangeTitle(ctx, got, poster, "Back again")
	require.ErrorIs(t, err, moderation.ErrHidden)
	err = issue_service.ChangeContent(ctx, got, poster, "back", got.ContentVersion)
	require.ErrorIs(t, err, moderation.ErrHidden)
	gotComment.Content = "back"
	err = issue_service.UpdateComment(ctx, gotComment, gotComment.ContentVersion, owner, HiddenText)
	require.ErrorIs(t, err, moderation.ErrHidden)
	assert.Equal(t, HiddenText, unittest.AssertExistsAndLoadBean(t, &issues_model.Comment{ID: 2}).Content)

	require.NoError(t, Unhide(ctx, moderation.ReportedContentTypeIssue, 1))
	require.NoError(t, Unhide(ctx, moderation.ReportedContentTypeComment, 2))
	got = unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 1})
	assert.Equal(t, before.Title, got.Title)
	assert.Equal(t, before.Content, got.Content)
	assert.Equal(t, comment.Content, unittest.AssertExistsAndLoadBean(t, &issues_model.Comment{ID: 2}).Content)
}

func TestAnAdministratorMayChangeHiddenContent(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	admin := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
	require.NoError(t, Hide(ctx, admin, moderation.ReportedContentTypeUser, 4))
	require.ErrorIs(t, moderation.CheckNotHidden(ctx, false, moderation.ReportedContentTypeUser, 4), moderation.ErrHidden)
	require.NoError(t, moderation.CheckNotHidden(ctx, true, moderation.ReportedContentTypeUser, 4))
	require.NoError(t, moderation.CheckNotHidden(ctx, false, moderation.ReportedContentTypeUser, 5), "not hidden")
}

func TestUnhideLeavesContentThatIsGoneAlone(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	admin := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
	require.NoError(t, Hide(ctx, admin, moderation.ReportedContentTypeComment, 2))
	_, err := db.GetEngine(ctx).ID(2).Delete(new(issues_model.Comment))
	require.NoError(t, err)
	require.NoError(t, Unhide(ctx, moderation.ReportedContentTypeComment, 2))
	hidden, err := moderation.IsHidden(ctx, moderation.ReportedContentTypeComment, 2)
	require.NoError(t, err)
	assert.False(t, hidden)
}
