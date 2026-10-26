// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"
	pull_service "forgejo.org/services/pull"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscRestoreFailure(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		key    string
		known  bool
	}{
		{pull_service.ErrAscRestoreForbidden, http.StatusForbidden, "", true},
		{pull_service.ErrAscNotStacked, http.StatusNotFound, "", true},
		{pull_service.ErrAscBadVersion, http.StatusNotFound, "", true},
		{pull_service.ErrAscRestoreNotOpen, 0, "repo.pulls.asc_versions.restore_not_open", true},
		{pull_service.ErrAscRestoreCurrent, 0, "repo.pulls.asc_versions.restore_current", true},
		{pull_service.ErrAscRestoreGone, 0, "repo.pulls.asc_versions.restore_gone", true},
		{pull_service.ErrAscUnavailable, 0, "repo.pulls.asc_versions.restore_unavailable", true},
		{pull_service.ErrAscBadAnswer, 0, "repo.pulls.asc_versions.restore_unavailable", true},
		{fmt.Errorf("wrapped: %w", pull_service.ErrAscRestoreGone), 0, "repo.pulls.asc_versions.restore_gone", true},
		{errors.New("database is down"), 0, "", false},
	} {
		status, key, known := ascRestoreFailure(tc.err)
		assert.Equal(t, tc.status, status, tc.err.Error())
		assert.Equal(t, tc.key, key, tc.err.Error())
		assert.Equal(t, tc.known, known, tc.err.Error())
	}
}

// restoreRequest runs the handler for pull request 5 of repository 1 (not a stacked pull request) as the
// user, or as nobody when userID is 0, with the given "version" field.
func restoreRequest(t *testing.T, userID int64, version string) (*context.Context, *httptest.ResponseRecorder) {
	t.Helper()
	pr := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 5})
	require.NoError(t, pr.LoadIssue(t.Context()))
	ctx, resp := contexttest.MockContext(t, "user2/repo1/pulls/5/asc/restore_version")
	if userID != 0 {
		contexttest.LoadUser(t, ctx, userID)
		ctx.IsSigned = true
	}
	contexttest.LoadRepo(t, ctx, pr.BaseRepoID)
	ctx.SetParams(":index", fmt.Sprint(pr.Issue.Index))
	ctx.Req.Form.Set("version", version)
	AscRestoreVersion(ctx)
	return ctx, resp
}

func TestAscRestoreVersionHandler(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))

	t.Run("signed out is refused", func(t *testing.T) {
		_, resp := restoreRequest(t, 0, "1")
		assert.Equal(t, http.StatusForbidden, resp.Code)
	})

	t.Run("asc off is a 404", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Asc.Enabled, false)()
		_, resp := restoreRequest(t, 1, "1")
		assert.Equal(t, http.StatusNotFound, resp.Code)
	})

	t.Run("a version that is not a number is a 404", func(t *testing.T) {
		for _, v := range []string{"", "0", "-1", "x", "1;2", "99999999999"} {
			_, resp := restoreRequest(t, 1, v)
			assert.Equal(t, http.StatusNotFound, resp.Code, v)
		}
	})

	t.Run("a user with no right on the pull request is refused", func(t *testing.T) {
		// user4 is neither the poster, an owner or admin, nor a writer.
		_, resp := restoreRequest(t, 4, "1")
		assert.Equal(t, http.StatusForbidden, resp.Code)
	})

	t.Run("a pull request that is not stacked is a 404", func(t *testing.T) {
		// user1 is a site admin: the permission passes, and the pull request has no asc change.
		_, resp := restoreRequest(t, 1, "1")
		assert.Equal(t, http.StatusNotFound, resp.Code)
	})
}
