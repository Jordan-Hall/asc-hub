// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascexportmail

import (
	"context"
	"testing"
	"time"

	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/asctrust"

	_ "forgejo.org/modules/testimport"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestMain(m *testing.M) { unittest.MainTest(m) }

type mail struct{ to, subject, body string }

const (
	readyID   = "ex_aaaaaaaaaaaaaaaaaaaa"
	failedID  = "ex_bbbbbbbbbbbbbbbbbbbb"
	runningID = "ex_cccccccccccccccccccc"
	goneID    = "ex_dddddddddddddddddddd"
	expiredID = "ex_eeeeeeeeeeeeeeeeeeee"
	brokenID  = "ex_ffffffffffffffffffff"
)

func fake(t *testing.T, now time.Time) (*[]mail, *[]string) {
	t.Helper()
	var got []mail
	var asked []string
	t.Cleanup(test.MockVariableValue(&setting.MailService, &setting.Mailer{}))
	t.Cleanup(test.MockVariableValue(&setting.AppURL, "https://hub.example.org/"))
	t.Cleanup(test.MockVariableValue(&Send, func(to, subject, body string) { got = append(got, mail{to, subject, body}) }))
	t.Cleanup(test.MockVariableValue(&GetExport, func(_ context.Context, u *user_model.User, id string) (*asctrust.ExportRow, error) {
		asked = append(asked, u.Name+" "+id)
		switch id {
		case readyID:
			return &asctrust.ExportRow{ID: id, State: "ready", Expires: now.Add(7 * 24 * time.Hour)}, nil
		case failedID:
			return &asctrust.ExportRow{ID: id, State: "failed", Error: "disk full"}, nil
		case runningID:
			return &asctrust.ExportRow{ID: id, State: "running"}, nil
		case goneID:
			return nil, status.Error(codes.NotFound, "no export")
		case expiredID:
			return &asctrust.ExportRow{ID: id, State: "ready", Expires: now.Add(-time.Minute)}, nil
		}
		return nil, status.Error(codes.Unavailable, "asc serve is restarting")
	}))
	return &got, &asked
}

func watched(t *testing.T) []string {
	t.Helper()
	rows, err := user_model.AscExportWatches(db.DefaultContext, 50)
	require.NoError(t, err)
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ExportID)
	}
	return ids
}

func TestAReadyOrFailedExportIsEmailedOnce(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	now := time.Now()
	got, asked := fake(t, now)
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	for _, id := range []string{readyID, failedID, runningID, goneID, expiredID, brokenID} {
		Watch(ctx, user2, id)
	}

	n, err := Run(ctx, now)
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	require.Len(t, *got, 2)
	assert.Len(t, *asked, 6)

	ready := (*got)[0]
	assert.Equal(t, user2.EmailTo(), ready.to)
	assert.Equal(t, "asc_shell.export_mail_ready_subject", ready.subject)
	assert.Contains(t, ready.body, "https://hub.example.org/user/settings/asc/export/"+readyID)
	assert.Contains(t, ready.body, "asc_shell.export_mail_ready_until", "the mail says until when the link works")

	failed := (*got)[1]
	assert.Equal(t, "asc_shell.export_mail_failed_subject", failed.subject)
	assert.Contains(t, failed.body, "disk full")
	assert.Contains(t, failed.body, "https://hub.example.org/user/settings/asc/export\n")

	assert.Equal(t, []string{runningID, brokenID}, watched(t), "an export still being made, or one asc serve could not tell about, is asked about again")

	n, err = Run(ctx, now)
	require.NoError(t, err)
	assert.Zero(t, n, "nothing is emailed twice")

	// two days on, a watch that never ended is dropped
	n, err = Run(ctx, now.Add(49*time.Hour))
	require.NoError(t, err)
	assert.Zero(t, n)
	assert.Empty(t, watched(t))
}

func TestNoMailServiceWatchesNothing(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	got, _ := fake(t, time.Now())
	defer test.MockVariableValue(&setting.MailService, nil)()
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	Watch(ctx, user2, readyID)
	assert.Empty(t, watched(t))
	n, err := Run(ctx, time.Now())
	require.NoError(t, err)
	assert.Zero(t, n)
	assert.Empty(t, *got)
}

func TestAWatchOfAPersonWhoCannotSignInIsDropped(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	got, asked := fake(t, time.Now())
	require.NoError(t, user_model.WatchAscExport(ctx, 9999, readyID)) // no such person
	inactive := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 9})
	require.False(t, inactive.IsActive)
	require.NoError(t, user_model.WatchAscExport(ctx, inactive.ID, readyID))
	n, err := Run(ctx, time.Now())
	require.NoError(t, err)
	assert.Zero(t, n)
	assert.Empty(t, *got)
	assert.Empty(t, *asked)
	assert.Empty(t, watched(t))
}
