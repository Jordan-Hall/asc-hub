// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package private

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/json"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/ascnotify"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ascEventCall(t *testing.T, req ascEventRequest) (int, ascEventResponse) {
	t.Helper()
	body, err := json.Marshal(req)
	require.NoError(t, err)
	return ascEventRaw(t, string(body))
}

func ascEventRaw(t *testing.T, body string) (int, ascEventResponse) {
	t.Helper()
	ctx, resp := contexttest.MockPrivateContext(t, "POST /api/internal/asc/event")
	ctx.Req.Method = http.MethodPost
	ctx.Req.Body = io.NopCloser(strings.NewReader(body))
	AscEvent(ctx)
	var out ascEventResponse
	if resp.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &out))
	}
	return resp.Code, out
}

func TestAscEventRecordsForPeopleWhoMayReadTheRepository(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	// user2 owns repo1 (public); user5 is a person who can read it too; a private repository shows who may not
	base := ascEventRequest{Event: "checks_failed", Repo: "user2/repo1", Users: []string{"user2", "user4", "no_such_user"}, Title: "build failed on the third change", Path: "/user2/repo1/pulls/3"}

	status, out := ascEventCall(t, base)
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, 2, out.Notified, "the unknown login is skipped without a word about it")

	repo := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	last, err := user_model.LastAscNotified(t.Context(), repo.ID, []int64{1})
	require.NoError(t, err)
	assert.NotZero(t, last[1], "the Last notified column has a time for the repository")

	// a private repository: a person without access is told nothing
	priv := base
	priv.Repo = "user2/repo2"
	status, out = ascEventCall(t, priv)
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, 1, out.Notified, "only the owner can read the private repository")
	other, err := user_model.LastAscNotified(t.Context(), 4, []int64{2})
	require.NoError(t, err)
	assert.Zero(t, other[2])

	// an unknown repository answers like one nobody can read
	missing := base
	missing.Repo = "user2/no_such_repo"
	status, out = ascEventCall(t, missing)
	assert.Equal(t, http.StatusOK, status)
	assert.Zero(t, out.Notified)
}

func TestAscEventRefusesWhatItDoesNotKnow(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	ok := ascEventRequest{Event: "merge_queue", Repo: "user2/repo1", Users: []string{"user2"}, Title: "merged", Path: "/user2/repo1/pulls/1"}
	for name, mutate := range map[string]func(*ascEventRequest){
		"unknown event":         func(r *ascEventRequest) { r.Event = "mentioned" }, // the hub makes this one itself
		"no users":              func(r *ascEventRequest) { r.Users = nil },
		"too many users":        func(r *ascEventRequest) { r.Users = make([]string, ascEventMaxUsers+1) },
		"no title":              func(r *ascEventRequest) { r.Title = "" },
		"title with a new line": func(r *ascEventRequest) { r.Title = "a\nBcc: x@example.org" },
		"long title":            func(r *ascEventRequest) { r.Title = strings.Repeat("x", ascEventMaxTitle+1) },
		"other site in path":    func(r *ascEventRequest) { r.Path = "//evil.example/x" },
		"absolute url in path":  func(r *ascEventRequest) { r.Path = "https://evil.example/x" },
		"repo without owner":    func(r *ascEventRequest) { r.Repo = "repo1" },
	} {
		t.Run(name, func(t *testing.T) {
			r := ok
			mutate(&r)
			status, _ := ascEventCall(t, r)
			assert.Equal(t, http.StatusBadRequest, status)
		})
	}
	status, _ := ascEventRaw(t, `{"event":"merge_queue","repo":"user2/repo1","users":["user2"],"title":"x","extra":1}`)
	assert.Equal(t, http.StatusBadRequest, status, "unknown fields are refused")
	status, _ = ascEventRaw(t, "not json")
	assert.Equal(t, http.StatusBadRequest, status)
}

func TestAscEventHonoursTheChoiceOfThePerson(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	ctx := t.Context()
	at := time.Now()
	require.NoError(t, ascnotify.Save(ctx, 2, ascnotify.Prefs{EmailOff: []string{"agent_question"}}))
	assert.False(t, ascnotify.EmailAllowed(ctx, 2, "agent_question", at))
	assert.True(t, ascnotify.EmailAllowed(ctx, 2, "agent_finished", at))
	for _, e := range ascnotify.IngestEvents {
		assert.True(t, ascnotify.IsIngestEvent(e), e)
		assert.Contains(t, ascnotify.EmailEvents, e)
	}
	assert.False(t, ascnotify.IsIngestEvent("review_requested"))
}
