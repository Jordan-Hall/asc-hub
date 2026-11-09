// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascmoderation

import (
	"strings"
	"testing"

	"forgejo.org/models/moderation"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	moderation_service "forgejo.org/services/moderation"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) { unittest.MainTest(m) }

type mail struct{ to, subject, body string }

func capture(t *testing.T) *[]mail {
	t.Helper()
	var got []mail
	prev := Send
	Send = func(to, subject, body string) { got = append(got, mail{to, subject, body}) }
	t.Cleanup(func() { Send = prev })
	t.Cleanup(test.MockVariableValue(&setting.MailService, &setting.Mailer{}))
	return &got
}

func TestTargetOfNamesTheOwnerOfEachKindOfContent(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	got, err := TargetOf(ctx, moderation.ReportedContentTypeUser, 4)
	require.NoError(t, err)
	assert.Equal(t, Target{OwnerID: 4, Ref: "@user4"}, got)
	got, err = TargetOf(ctx, moderation.ReportedContentTypeRepository, 1)
	require.NoError(t, err)
	assert.Equal(t, Target{OwnerID: 2, Ref: "user2/repo1"}, got)
	got, err = TargetOf(ctx, moderation.ReportedContentTypeIssue, 1)
	require.NoError(t, err)
	assert.Equal(t, Target{OwnerID: 1, Ref: "user2/repo1#1"}, got)
	got, err = TargetOf(ctx, moderation.ReportedContentTypeComment, 1)
	require.NoError(t, err)
	assert.Equal(t, Target{OwnerID: 2, Ref: "user2/repo1#1"}, got)
	_, err = TargetOf(ctx, moderation.ReportedContentTypeUser, 99999)
	require.Error(t, err)
}

func TestCleanDropsControlCharactersAndBounds(t *testing.T) {
	assert.Equal(t, "a\nb", Clean("  a\x00\n\x1bb  ", 10))
	assert.Equal(t, "abc", Clean("abcdef", 3))
	assert.Equal(t, "é", Clean("éé", 1), "characters, not bytes")
}

func TestRecordEmailsTheOwnerHowToAppeal(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	mails := capture(t)
	admin := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})

	none, err := Record(ctx, admin, moderation.ReportedContentTypeUser, 4, moderation_service.ContentActionNone, "", Target{OwnerID: 4})
	require.NoError(t, err)
	assert.Nil(t, none, "an action of none is not a decision")
	assert.Empty(t, *mails)

	d, err := Record(ctx, admin, moderation.ReportedContentTypeRepository, 1, moderation_service.ContentActionDeleteRepo, "Phishing link in the README.", Target{OwnerID: 2, Ref: "user2/repo1"})
	require.NoError(t, err)
	require.NotNil(t, d)
	require.Len(t, *mails, 1)
	m := (*mails)[0]
	assert.Contains(t, m.to, "user2@example.com")
	assert.Contains(t, m.body, "user2/repo1")
	assert.Contains(t, m.body, "Phishing link in the README.")
	assert.Contains(t, m.body, "/-/moderation/decisions/", "the page where the owner appeals")

	s, err := Record(ctx, admin, moderation.ReportedContentTypeUser, 4, moderation_service.ContentActionSuspendAccount, "Spam.", Target{OwnerID: 4, Ref: "@user4"})
	require.NoError(t, err)
	require.Len(t, *mails, 2)
	assert.True(t, IsSuspension(s))
	assert.Contains(t, (*mails)[1].body, "/user/login", "a suspended account appeals from the sign-in page")

	gone, err := Record(ctx, admin, moderation.ReportedContentTypeUser, 5, moderation_service.ContentActionDeleteAccount, "Bot.", Target{OwnerID: 5, Ref: "@user5"})
	require.NoError(t, err)
	assert.False(t, Appealable(gone), "a deleted account cannot appeal")
	assert.Len(t, *mails, 2, "and is not told how to")

	*mails = (*mails)[:0]
	require.NoError(t, moderation.Appeal(ctx, d.ID, 2, "It was a test page."))
	require.NoError(t, moderation.DecideAppeal(ctx, d.ID, 1, true, "Sorry, our mistake."))
	answered, err := moderation.GetDecision(ctx, d.ID)
	require.NoError(t, err)
	TellAnswer(ctx, answered)
	require.Len(t, *mails, 1)
	assert.Contains(t, (*mails)[0].body, "mod_mail_overturned_removed", "an overturned removal says the content is not restored")
	assert.Contains(t, (*mails)[0].body, "Sorry, our mistake.")
}

func TestTheSignInAppealCodeIsUsedOnceForItsOwnSuspension(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	capture(t)
	admin := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
	s, err := Record(ctx, admin, moderation.ReportedContentTypeUser, 4, moderation_service.ContentActionSuspendAccount, "Spam.", Target{OwnerID: 4, Ref: "@user4"})
	require.NoError(t, err)
	other, err := Record(ctx, admin, moderation.ReportedContentTypeRepository, 1, moderation_service.ContentActionDeleteRepo, "x", Target{OwnerID: 2, Ref: "user2/repo1"})
	require.NoError(t, err)

	code, err := AppealCode(ctx, s)
	require.NoError(t, err)
	require.True(t, strings.Contains(code, ":"))
	require.ErrorIs(t, AppealWithCode(ctx, other.ID, code, "wrong decision"), moderation.ErrAppealNotAllowed, "a code is for one decision")
	require.ErrorIs(t, AppealWithCode(ctx, s.ID, "nope:nope", "forged"), moderation.ErrAppealNotAllowed)

	code, err = AppealCode(ctx, s)
	require.NoError(t, err)
	require.NoError(t, AppealWithCode(ctx, s.ID, code, "I am not a spammer."))
	require.ErrorIs(t, AppealWithCode(ctx, s.ID, code, "again"), moderation.ErrAppealNotAllowed, "the code is used up")
	got, err := moderation.GetDecision(ctx, s.ID)
	require.NoError(t, err)
	assert.Equal(t, moderation.AppealOpen, got.AppealStatus)
	assert.Equal(t, "I am not a spammer.", got.AppealText)

	// a code for a decision that is not a suspension is refused even when it is the owner's
	code, err = AppealCode(ctx, other)
	require.NoError(t, err)
	require.ErrorIs(t, AppealWithCode(ctx, other.ID, code, "x"), moderation.ErrAppealNotAllowed)
}
