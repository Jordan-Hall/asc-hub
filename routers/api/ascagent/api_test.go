// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascagent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/moderation"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/ascagentreport"
	"forgejo.org/services/asctrust"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestMain(m *testing.M) {
	unittest.MainTest(m)
}

var secret = "ascat_" + strings.Repeat("Q", 43)

// oneToken answers AgentTokens.ResolveToken for secret only.
type oneToken struct{ token *ascv1.AgentToken }

func (c *oneToken) Invoke(_ context.Context, method string, req, reply any, _ ...grpc.CallOption) error {
	if method != ascv1.AgentTokens_ResolveToken_FullMethodName {
		return errors.New("unexpected call " + method)
	}
	if req.(*ascv1.ResolveAgentTokenRequest).GetSecret() != secret {
		return status.Error(codes.NotFound, "no such token")
	}
	reply.(*ascv1.ResolveAgentTokenResponse).Token = c.token
	return nil
}

func (*oneToken) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, errors.New("no streams")
}

func setup(t *testing.T) http.Handler {
	t.Helper()
	require.NoError(t, unittest.PrepareTestDatabase())
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	t.Cleanup(test.MockVariableValue(&setting.Moderation.Enabled, true))
	_, err := db.GetEngine(t.Context()).Exec("UPDATE repository SET storage_backend = ? WHERE id = 1", string(gitrepo.StorageBackendAsc))
	require.NoError(t, err)
	name, err := asctrust.RepoName(unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1}))
	require.NoError(t, err)
	t.Cleanup(ascagentreport.UseConn(&oneToken{token: &ascv1.AgentToken{Id: "at_api00000000000000000", Repo: name, Agent: "triage", State: ascv1.AgentTokenState_AGENT_TOKEN_STATE_ACTIVE}}))
	return Routes()
}

func post(h http.Handler, path, auth, contentType, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

const (
	reportPath = "/repos/user2/repo1/reports"
	issueBody  = `{"content":"issue","number":2,"category":"malware","remarks":"the attachment is a dropper"}`
)

func TestAnAgentSendsAReportAndASecondOneIsTheSame(t *testing.T) {
	h := setup(t)
	rec := post(h, reportPath, "Bearer "+secret, "application/json", issueBody)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	var first ascagentreport.Result
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &first))
	assert.False(t, first.Duplicate)
	r := unittest.AssertExistsAndLoadBean(t, &moderation.AbuseReport{ID: first.ID})
	assert.Equal(t, "triage", r.ReporterAgent)
	assert.Equal(t, moderation.AbuseCategoryTypeMalware, r.Category)

	rec = post(h, reportPath, "bearer "+secret, "application/json; charset=utf-8", issueBody)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var again ascagentreport.Result
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &again))
	assert.True(t, again.Duplicate)
	assert.Equal(t, first.ID, again.ID)
}

func TestEveryUnusableTokenIs401(t *testing.T) {
	h := setup(t)
	for name, auth := range map[string]string{
		"none":      "",
		"basic":     "Basic dXNlcjI6cGFzc3dvcmQ=",
		"not asc":   "Bearer 4b1d0c9e2f",
		"unknown":   "Bearer ascat_" + strings.Repeat("Z", 43),
		"two words": "Bearer " + secret + " extra",
	} {
		rec := post(h, reportPath, auth, "application/json", issueBody)
		assert.Equal(t, http.StatusUnauthorized, rec.Code, name)
		assert.Contains(t, rec.Header().Get("WWW-Authenticate"), "Bearer", name)
	}
	rec := post(h, "/repos/user2/repo2/reports", "Bearer "+secret, "application/json", issueBody)
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "a token of repo1 is not one of repo2")
	rec = post(h, "/repos/user2/missing/reports", "Bearer "+secret, "application/json", issueBody)
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "a missing repository answers like a wrong token")
}

func TestTheBodyIsChecked(t *testing.T) {
	h := setup(t)
	auth := "Bearer " + secret
	assert.Equal(t, http.StatusUnsupportedMediaType, post(h, reportPath, auth, "text/plain", issueBody).Code)
	assert.Equal(t, http.StatusBadRequest, post(h, reportPath, auth, "application/json", `{"content":"issue"`).Code)
	assert.Equal(t, http.StatusBadRequest, post(h, reportPath, auth, "application/json", `{"content":"issue","number":2,"category":"spam","remarks":"x","reporter_id":1}`).Code, "unknown fields are refused")
	assert.Equal(t, http.StatusBadRequest, post(h, reportPath, auth, "application/json", `{"content":"issue","number":2,"category":"rude","remarks":"x"}`).Code)
	assert.Equal(t, http.StatusRequestEntityTooLarge, post(h, reportPath, auth, "application/json", `{"remarks":"`+strings.Repeat("x", maxBody)+`"}`).Code)
	assert.Equal(t, http.StatusNotFound, post(h, reportPath, auth, "application/json", `{"content":"issue","number":999,"category":"spam","remarks":"x"}`).Code)
}

func TestTheRouteIsGoneWithModerationOff(t *testing.T) {
	h := setup(t)
	t.Cleanup(test.MockVariableValue(&setting.Moderation.Enabled, false))
	assert.Equal(t, http.StatusNotFound, post(h, reportPath, "Bearer "+secret, "application/json", issueBody).Code)
}
