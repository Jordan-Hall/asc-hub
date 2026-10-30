// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"forgejo.org/models/unittest"
	"forgejo.org/services/contexttest"
	files_service "forgejo.org/services/repository/files"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An edit of the policy file of an asc repository through the contents API is not a commit: 202 with the change when
// it was proposed, 403 with asc's reason for a person who may not edit the policy.
func TestContentsAPIAnswersAPolicyEdit(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	t.Run("proposed", func(t *testing.T) {
		ctx, resp := contexttest.MockAPIContext(t, "user2/repo1/contents/.asc/policy.toml")
		handleCreateOrUpdateFileError(ctx, fmt.Errorf("wrapped: %w", &files_service.ErrAscPolicyProposed{Change: "kz4npqrstuvw", Link: "/user2/repo1/pulls/7"}))
		assert.Equal(t, http.StatusAccepted, resp.Code)
		var got AscPolicyEditResponse
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &got))
		assert.Equal(t, AscPolicyEditResponse{Change: "kz4npqrstuvw", PullRequest: "/user2/repo1/pulls/7"}, got)
	})
	t.Run("proposed, the pull request not open yet", func(t *testing.T) {
		ctx, resp := contexttest.MockAPIContext(t, "user2/repo1/contents/.asc/policy.toml")
		handleCreateOrUpdateFileError(ctx, &files_service.ErrAscPolicyProposed{Change: "kz4npqrstuvw"})
		assert.Equal(t, http.StatusAccepted, resp.Code)
		assert.NotContains(t, resp.Body.String(), "pull_request")
	})
	t.Run("refused", func(t *testing.T) {
		ctx, resp := contexttest.MockAPIContext(t, "user2/repo1/contents/.asc/policy.toml")
		handleCreateOrUpdateFileError(ctx, &files_service.ErrAscPolicyRefused{Reason: "You may not change the policy: only a maintainer. Ask a maintainer."})
		assert.Equal(t, http.StatusForbidden, resp.Code)
		assert.Contains(t, resp.Body.String(), "Ask a maintainer")
	})
	t.Run("any other error is the old answer", func(t *testing.T) {
		ctx, resp := contexttest.MockAPIContext(t, "user2/repo1/contents/a.txt")
		assert.False(t, respondAscPolicyEdit(ctx, errors.New("boom")))
		assert.Equal(t, http.StatusOK, resp.Code)
	})
}
