// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascdigest

import (
	"testing"
	"time"

	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/ascnotify"

	_ "forgejo.org/modules/testimport"

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

func TestTheSummaryIsSentOnceAtEightAndOnlyWithWhatThePersonMayRead(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	got := capture(t)
	const uid = int64(2)
	at8 := time.Date(2026, 10, 28, 8, 5, 0, 0, time.UTC)

	// nobody asked for a summary
	n, err := Run(ctx, at8)
	require.NoError(t, err)
	assert.Zero(t, n)

	require.NoError(t, ascnotify.Save(ctx, uid, ascnotify.Prefs{Digest: true, EmailOff: []string{"merge_queue"}}))
	now := time.Now()
	require.NoError(t, user_model.RecordAscNotification(ctx, uid, 1, "checks_failed", "build failed", "/user2/repo1/pulls/3", false))
	require.NoError(t, user_model.RecordAscNotification(ctx, uid, 1, "merge_queue", "merged", "/user2/repo1/pulls/2", false)) // email switched off for it
	require.NoError(t, user_model.RecordAscNotification(ctx, uid, 1, "agent_finished", "already emailed", "", true))          // not in the summary
	require.NoError(t, user_model.RecordAscNotification(ctx, 4, 1, "checks_failed", "somebody else", "", false))              // another person's
	_, err = db.GetEngine(ctx).Exec("UPDATE `asc_notify_log` SET sent_unix = ?", now.Unix())
	require.NoError(t, err)

	// not 08:00 in the zone of the person: nothing
	n, err = Run(ctx, time.Date(2026, 10, 28, 9, 5, 0, 0, time.UTC))
	require.NoError(t, err)
	assert.Zero(t, n)

	n, err = Run(ctx, at8)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	require.Len(t, *got, 1)
	body := (*got)[0].body
	assert.Contains(t, body, "build failed")
	assert.NotContains(t, body, "merged", "an event switched off for email is not in the summary")
	assert.NotContains(t, body, "already emailed")
	assert.NotContains(t, body, "somebody else")
	assert.Contains(t, body, "/user2/repo1/pulls/3")

	// the hour is checked again an hour later and the entries are used up: no second mail
	n, err = Run(ctx, at8.Add(10*time.Minute))
	require.NoError(t, err)
	assert.Zero(t, n)
}

func TestNoMailServiceKeepsTheEntries(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	got := capture(t)
	setting.MailService = nil
	require.NoError(t, ascnotify.Save(ctx, 2, ascnotify.Prefs{Digest: true}))
	require.NoError(t, user_model.RecordAscNotification(ctx, 2, 1, "checks_failed", "x", "", false))
	n, err := Run(ctx, time.Date(2026, 10, 28, 8, 5, 0, 0, time.UTC))
	require.NoError(t, err)
	assert.Zero(t, n)
	assert.Empty(t, *got)
	rows, err := user_model.PendingAscDigest(ctx, 2, 0)
	require.NoError(t, err)
	assert.Len(t, rows, 1)
}
