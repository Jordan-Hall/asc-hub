// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascagentreport

import (
	"context"
	"errors"
	"strings"
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/issues"
	"forgejo.org/models/moderation"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/asctrust"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The secret every test agent presents, and another one asc does not know.
var (
	secret  = "ascat_" + strings.Repeat("A", 43)
	unknown = "ascat_" + strings.Repeat("B", 43)
)

// tokenConn answers AgentTokens.ResolveToken with one token for secret and NOT_FOUND for anything else, and keeps
// what it was asked.
type tokenConn struct {
	token *ascv1.AgentToken
	fail  error
	asked []string
}

func (c *tokenConn) Invoke(_ context.Context, method string, req, reply any, _ ...grpc.CallOption) error {
	if method != ascv1.AgentTokens_ResolveToken_FullMethodName {
		return errors.New("unexpected call " + method)
	}
	s := req.(*ascv1.ResolveAgentTokenRequest).GetSecret()
	c.asked = append(c.asked, s)
	if c.fail != nil {
		return c.fail
	}
	if s != secret {
		return status.Error(codes.NotFound, "no such token")
	}
	reply.(*ascv1.ResolveAgentTokenResponse).Token = c.token
	return nil
}

func (*tokenConn) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, errors.New("no streams")
}

// setup turns moderation and asc on, makes repo1 (user2/repo1) an asc repository and answers for a token of it.
func setup(t *testing.T) (*repo_model.Repository, *tokenConn) {
	t.Helper()
	require.NoError(t, unittest.PrepareTestDatabase())
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	t.Cleanup(test.MockVariableValue(&setting.Moderation.Enabled, true))
	_, err := db.GetEngine(t.Context()).Exec("UPDATE repository SET storage_backend = ? WHERE id = 1", string(gitrepo.StorageBackendAsc))
	require.NoError(t, err)
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	name, err := asctrust.RepoName(repo)
	require.NoError(t, err)
	c := &tokenConn{token: &ascv1.AgentToken{Id: "at_reviewer0000000000000", Repo: name, Name: "nightly review", Agent: "reviewer", State: ascv1.AgentTokenState_AGENT_TOKEN_STATE_ACTIVE}}
	t.Cleanup(UseConn(c))
	return repo, c
}

func spam(content string) *Request {
	return &Request{Content: content, Category: "spam", Remarks: "links to a fake download page"}
}

func TestAnAgentReportsAnIssueOfItsRepository(t *testing.T) {
	_, c := setup(t)
	req := spam(ContentIssue)
	req.Number = 1
	res, err := Report(t.Context(), "user2", "repo1", secret, req)
	require.NoError(t, err)
	assert.False(t, res.Duplicate)
	r := unittest.AssertExistsAndLoadBean(t, &moderation.AbuseReport{ID: res.ID})
	assert.Equal(t, moderation.ReportedContentTypeIssue, r.ContentType)
	assert.EqualValues(t, 1, r.ContentID, "issue #1 of repo1 has the id 1")
	assert.Equal(t, "reviewer", r.ReporterAgent)
	assert.Equal(t, "at_reviewer0000000000000", r.ReporterAgentToken)
	assert.Zero(t, r.ReporterID, "no person sent it")
	assert.Equal(t, moderation.ReportStatusTypeOpen, r.Status)
	assert.Equal(t, []string{secret}, c.asked, "the hub asks asc once and only about the secret it was sent")
}

func TestTheSameTokenReportsTheSameContentOnceWhileItIsOpen(t *testing.T) {
	setup(t)
	first, err := Report(t.Context(), "user2", "repo1", secret, spam(ContentRepository))
	require.NoError(t, err)
	again, err := Report(t.Context(), "user2", "repo1", secret, spam(ContentRepository))
	require.NoError(t, err)
	assert.True(t, again.Duplicate)
	assert.Equal(t, first.ID, again.ID)
	n, err := db.GetEngine(t.Context()).Where("reporter_agent_token = ?", "at_reviewer0000000000000").Count(new(moderation.AbuseReport))
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)
}

func TestAnAgentReportsAComment(t *testing.T) {
	setup(t)
	req := spam(ContentComment)
	req.CommentID = 2 // a comment on issue #1 of repo1
	res, err := Report(t.Context(), "user2", "repo1", secret, req)
	require.NoError(t, err)
	r := unittest.AssertExistsAndLoadBean(t, &moderation.AbuseReport{ID: res.ID})
	assert.Equal(t, moderation.ReportedContentTypeComment, r.ContentType)
	assert.EqualValues(t, 2, r.ContentID)
}

func TestContentOfAnotherRepositoryIsNotFound(t *testing.T) {
	setup(t)
	// issue 4 is #1 of repo2
	c := &issues.Comment{Type: issues.CommentTypeComment, PosterID: 2, IssueID: 4, Content: "elsewhere"}
	_, err := db.GetEngine(t.Context()).Insert(c)
	require.NoError(t, err)
	req := spam(ContentComment)
	req.CommentID = c.ID
	_, err = Report(t.Context(), "user2", "repo1", secret, req)
	require.ErrorIs(t, err, ErrNotFound)

	req = spam(ContentIssue)
	req.Number = 999
	_, err = Report(t.Context(), "user2", "repo1", secret, req)
	require.ErrorIs(t, err, ErrNotFound)

	req = spam(ContentComment)
	req.CommentID = 1 // a label event, which has no content of its own
	_, err = Report(t.Context(), "user2", "repo1", secret, req)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestATokenWorksOnlyForItsRepositoryWhileActive(t *testing.T) {
	_, c := setup(t)
	_, err := Report(t.Context(), "user2", "repo2", secret, spam(ContentRepository))
	require.ErrorIs(t, err, ErrToken, "a token of repo1 cannot report in repo2")
	_, err = Report(t.Context(), "user2", "no-such-repo", secret, spam(ContentRepository))
	require.ErrorIs(t, err, ErrToken, "a repository that does not exist answers like a wrong token")
	_, err = Report(t.Context(), "user2", "repo1", unknown, spam(ContentRepository))
	require.ErrorIs(t, err, ErrToken)
	_, err = Report(t.Context(), "user2", "repo1", "not-a-token", spam(ContentRepository))
	require.ErrorIs(t, err, ErrToken)
	assert.NotContains(t, c.asked, "not-a-token", "a string that is no token is not sent to asc")

	for _, s := range []ascv1.AgentTokenState{ascv1.AgentTokenState_AGENT_TOKEN_STATE_EXPIRED, ascv1.AgentTokenState_AGENT_TOKEN_STATE_REVOKED, ascv1.AgentTokenState_AGENT_TOKEN_STATE_EXHAUSTED} {
		c.token.State = s
		_, err = Report(t.Context(), "user2", "repo1", secret, spam(ContentRepository))
		require.ErrorIs(t, err, ErrToken, s.String())
	}
}

func TestTheTokenIsCheckedBeforeTheReport(t *testing.T) {
	setup(t)
	bad := &Request{Content: "user", Category: "nonsense"}
	_, err := Report(t.Context(), "user2", "repo1", unknown, bad)
	require.ErrorIs(t, err, ErrToken, "a caller without a token learns nothing from its report")
	_, err = Report(t.Context(), "user2", "repo1", secret, bad)
	require.ErrorIs(t, err, ErrInvalid)
	_, err = Report(t.Context(), "user2", "repo1", secret, &Request{Content: ContentRepository, Category: "spam", Remarks: " \x00 "})
	require.ErrorIs(t, err, ErrInvalid, "remarks are required")
	_, err = Report(t.Context(), "user2", "repo1", secret, &Request{Content: "user", Category: "spam", Remarks: "x"})
	require.ErrorIs(t, err, ErrInvalid, "an agent cannot report an account")
}

func TestAscThatCannotBeAskedIsUnavailable(t *testing.T) {
	_, c := setup(t)
	c.fail = status.Error(codes.Unavailable, "down")
	_, err := Report(t.Context(), "user2", "repo1", secret, spam(ContentRepository))
	require.ErrorIs(t, err, ErrUnavailable)
}

func TestReportsAreOffWithModeration(t *testing.T) {
	_, c := setup(t)
	t.Cleanup(test.MockVariableValue(&setting.Moderation.Enabled, false))
	_, err := Report(t.Context(), "user2", "repo1", secret, spam(ContentRepository))
	require.ErrorIs(t, err, ErrDisabled)
	assert.Empty(t, c.asked)
}

func TestTheAgentNameIsCleanedAndBounded(t *testing.T) {
	assert.Equal(t, "nightly review", agentName(&ascv1.AgentToken{Name: "nightly\nreview"}))
	assert.Equal(t, "agent", agentName(&ascv1.AgentToken{}))
	long := agentName(&ascv1.AgentToken{Agent: strings.Repeat("é", 64)})
	assert.LessOrEqual(t, len(long), maxAgentName)
}

func TestTheQueueCountsPeopleAndAgentsApart(t *testing.T) {
	// the queue joins the user and repository tables, which this package has
	require.NoError(t, unittest.PrepareTestDatabase())
	require.NoError(t, moderation.ReportAbuse(t.Context(), &moderation.AbuseReport{
		ReporterID: 2, ContentType: moderation.ReportedContentTypeRepository, ContentID: 7,
		Category: moderation.AbuseCategoryTypeSpam, Remarks: "a person saw it",
	}))
	for _, token := range []string{"at_a", "at_b"} {
		_, err := moderation.ReportAbuseByAgent(t.Context(), &moderation.AbuseReport{ReporterAgent: "reviewer", ReporterAgentToken: token, ContentType: moderation.ReportedContentTypeRepository, ContentID: 7, Category: moderation.AbuseCategoryTypeSpam, Remarks: "spam links"})
		require.NoError(t, err)
	}
	reports, err := moderation.GetOpenReports(t.Context())
	require.NoError(t, err)
	var found *moderation.AbuseReportDetailed
	for _, r := range reports {
		if r.ContentType == moderation.ReportedContentTypeRepository && r.ContentID == 7 {
			found = r
		}
	}
	require.NotNil(t, found)
	assert.Equal(t, 3, found.ReportedTimes)
	assert.Equal(t, 2, found.ReportedByAgents)
	assert.Equal(t, 1, found.ReportedByPeople())

	details, err := moderation.GetOpenReportsByTypeAndContentID(t.Context(), moderation.ReportedContentTypeRepository, 7)
	require.NoError(t, err)
	require.Len(t, details, 3)
	assert.Empty(t, details[0].ReporterAgent)
	assert.Equal(t, "reviewer", details[1].ReporterAgent)
}
