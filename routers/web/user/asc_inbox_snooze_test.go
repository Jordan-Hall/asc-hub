// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"net/http"
	"testing"
	"time"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscInboxSnoozeChoices(t *testing.T) {
	defer test.MockVariableValue(&setting.DefaultUILocation, time.UTC)()
	// a Wednesday
	morning := time.Date(2026, 11, 4, 10, 30, 0, 0, time.UTC)
	evening := time.Date(2026, 11, 4, 18, 0, 0, 0, time.UTC)
	at := func(choice, on string, now time.Time) time.Time {
		t.Helper()
		got, ok := ascInboxSnoozeUntil(choice, on, now)
		require.True(t, ok, choice)
		return got
	}
	assert.Equal(t, time.Date(2026, 11, 4, 17, 0, 0, 0, time.UTC), at("today", "", morning))
	_, ok := ascInboxSnoozeUntil("today", "", evening)
	assert.False(t, ok, "later today is gone after 17:00")
	assert.Equal(t, time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC), at("tomorrow", "", evening))
	assert.Equal(t, time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC), at("monday", "", morning))
	monday := time.Date(2026, 11, 9, 8, 0, 0, 0, time.UTC)
	assert.Equal(t, time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC), at("monday", "", monday), "on a Monday it is the next one")
	assert.Equal(t, time.Date(2026, 12, 24, 9, 0, 0, 0, time.UTC), at("date", "2026-12-24", morning))
	assert.Equal(t, morning.Add(time.Hour), at("1h", "", morning))
	assert.Equal(t, morning.AddDate(0, 0, 7), at("week", "", morning))

	for name, c := range map[string][2]string{
		"a day in the past":       {"date", "2026-11-03"},
		"a day too far":           {"date", "2028-01-01"},
		"not a day":               {"date", "24/12/2026"},
		"no day":                  {"date", ""},
		"an unknown choice":       {"forever", ""},
		"the checks are not here": {"checks", ""},
	} {
		_, ok := ascInboxSnoozeUntil(c[0], c[1], morning)
		assert.False(t, ok, name)
	}

	first, last := ascInboxSnoozeDays(morning)
	assert.Equal(t, "2026-11-05", first)
	assert.Equal(t, morning.Add(issues_model.AscInboxMaxSnooze).Format("2006-01-02"), last)

	// the hours are those of the hub
	berlin := time.FixedZone("CET", 3600)
	defer test.MockVariableValue(&setting.DefaultUILocation, berlin)()
	got := at("tomorrow", "", evening)
	assert.Equal(t, 9, got.In(berlin).Hour())
	assert.Equal(t, 8, got.UTC().Hour())
}

func TestAscInboxSnoozeMenu(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	now := time.Now()
	useAscInboxEntries(t,
		&AscInboxEntry{PullID: 81, Title: "checks run", Link: "/o/r/pulls/1", RepoFullName: "o/r", Index: 1, Section: issues_model.AscInboxNeedsReview, Updated: now, HeadCommitID: "abc", ChecksRunning: true},
		&AscInboxEntry{PullID: 82, Title: "checks done", Link: "/o/r/pulls/2", RepoFullName: "o/r", Index: 2, Section: issues_model.AscInboxNeedsReview, Updated: now.Add(-time.Hour), HeadCommitID: "def"},
	)

	_, body := getAscInbox(t, user2, "?pull=81")
	for _, k := range []string{"today", "tomorrow", "monday", "checks"} {
		assert.Contains(t, body, `data-asc-snooze-for="`+k+`"`)
	}
	assert.Contains(t, body, "user.asc_inbox.snooze_tomorrow")
	assert.Contains(t, body, "user.asc_inbox.snooze_monday")
	assert.Contains(t, body, "user.asc_inbox.snooze_checks")
	assert.Contains(t, body, "user.asc_inbox.snooze_date")
	assert.Contains(t, body, `data-asc-snooze-for="checks">`, "offered while a check runs")
	assert.NotContains(t, body, "wk-snooze-checks-off")
	assert.Contains(t, body, `type="date" name="on" required min="`)
	assert.NotContains(t, body, "user.asc_inbox.snooze_4h")

	_, body = getAscInbox(t, user2, "?pull=82")
	assert.Contains(t, body, `data-asc-snooze-for="checks" disabled aria-describedby="wk-snooze-checks-off"`)
	assert.Contains(t, body, "user.asc_inbox.snooze_checks_off")
}

func TestAscInboxSnoozePost(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	useAscInboxEntries(t,
		&AscInboxEntry{PullID: 83, Title: "t", Section: issues_model.AscInboxNeedsReview, HeadCommitID: "abc", ChecksRunning: true},
		&AscInboxEntry{PullID: 84, Title: "u", Section: issues_model.AscInboxNeedsReview, HeadCommitID: "def"},
	)
	mark := func(pull int64) *issues_model.AscInboxState {
		st, err := issues_model.GetAscInboxState(db.DefaultContext, user2.ID, pull)
		require.NoError(t, err)
		return st
	}

	code, loc := postAscInboxState(t, user2, map[string]string{"pull": "83", "do": "snooze", "for": "checks", "tab": "waiting"})
	assert.Equal(t, http.StatusSeeOther, code)
	assert.Equal(t, "/-/asc/inbox?tab=waiting", loc)
	st := mark(83)
	require.NotNil(t, st)
	assert.Equal(t, "abc", st.SnoozeHead)
	assert.True(t, st.IsSnoozedFor(time.Now(), "abc", true))
	assert.False(t, st.IsSnoozedFor(time.Now(), "abc", false), "the checks finished")
	assert.False(t, st.IsSnoozedFor(time.Now().Add(issues_model.AscInboxChecksSnooze+time.Minute), "abc", true), "a day at most")

	// no check runs: nothing is set and the item stays
	code, _ = postAscInboxState(t, user2, map[string]string{"pull": "84", "do": "snooze", "for": "checks"})
	assert.Equal(t, http.StatusSeeOther, code)
	assert.Nil(t, mark(84))

	// a day of the date field
	on := time.Now().In(setting.DefaultUILocation).AddDate(0, 0, 3).Format("2006-01-02")
	code, _ = postAscInboxState(t, user2, map[string]string{"pull": "84", "do": "snooze", "for": "date", "on": on})
	assert.Equal(t, http.StatusSeeOther, code)
	require.NotNil(t, mark(84))
	until := mark(84).SnoozedUntil.AsTime().In(setting.DefaultUILocation)
	assert.Equal(t, on, until.Format("2006-01-02"))
	assert.Equal(t, 9, until.Hour())

	// another snooze replaces the one of the checks
	code, _ = postAscInboxState(t, user2, map[string]string{"pull": "83", "do": "snooze", "for": "tomorrow"})
	assert.Equal(t, http.StatusSeeOther, code)
	assert.Empty(t, mark(83).SnoozeHead)

	code, _ = postAscInboxState(t, user2, map[string]string{"pull": "84", "do": "snooze", "for": "date", "on": "2001-01-01"})
	assert.Equal(t, http.StatusBadRequest, code, "a day in the past")
}
