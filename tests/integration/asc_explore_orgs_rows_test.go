// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/organization"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/asctrust"
	"forgejo.org/tests"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

type exploreOrgAgents struct {
	ascv1.UnimplementedAgentsServer
}

func (exploreOrgAgents) SummarizeAgents(_ context.Context, req *ascv1.SummarizeAgentsRequest) (*ascv1.SummarizeAgentsResponse, error) {
	resp := &ascv1.SummarizeAgentsResponse{}
	for i, r := range req.GetRepos() {
		if r.GetName() == "org3/repo3" {
			for _, name := range []string{"review", "docs"} {
				resp.Agents = append(resp.Agents, &ascv1.AgentSummary{Agent: name, Runs: 2, Places: []*ascv1.AgentPlace{{Repo: uint32(i), Runs: 2}}})
			}
		}
	}
	return resp, nil
}

// A row of the organization directory has a member stack and, for a person with agent features on, the agents.
func TestTheExploreOrgRowsHaveMembersAndAgents(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	srv := grpc.NewServer()
	ascv1.RegisterAgentsServer(srv, exploreOrgAgents{})
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	defer srv.Stop()
	c, err := ascclient.New(ascclient.Config{Target: l.Addr().String(), InternalToken: "internal-token-for-tests", Retry: ascclient.RetryConfig{MaxAttempts: 1}})
	require.NoError(t, err)
	defer c.Close()
	defer asctrust.UseConn(c)()

	_, err = db.GetEngine(db.DefaultContext).ID(3).Cols("storage_backend").Update(&repo_model.Repository{StorageBackend: "asc"})
	require.NoError(t, err)
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	session := loginUser(t, "user2")
	row := func(doc *HTMLDoc) *goquery.Selection {
		r := doc.Find("li.ds-user-row").FilterFunction(func(_ int, s *goquery.Selection) bool {
			return strings.TrimSpace(s.Find(".ds-li-title > a").First().Text()) == "org3"
		})
		require.Equal(t, 1, r.Length())
		return r
	}
	count := func(doer *user_model.User, member bool) int64 {
		n, err := organization.CountOrgMembers(db.DefaultContext, &organization.FindOrgMembersOpts{Doer: doer, IsDoerMember: member, OrgID: 3})
		require.NoError(t, err)
		return n
	}

	t.Run("A member sees every member, at most four avatars and +N", func(t *testing.T) {
		doc := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/explore/organizations?q=org3"), http.StatusOK).Body)
		r := row(doc)
		stack := r.Find("[data-asc-org-members]")
		require.Equal(t, 1, stack.Length())
		total := count(user2, true)
		avatars := stack.Find("a.ds-av").Length()
		assert.Equal(t, int(min(total, 4)), avatars)
		if total > 4 {
			assert.Equal(t, fmt.Sprintf("+%d", total-4), strings.TrimSpace(r.Find(".ds-av-more").Text()))
		} else {
			assert.Equal(t, 0, r.Find(".ds-av-more").Length())
		}
		assert.Contains(t, stack.AttrOr("aria-label", ""), " member")
		assert.Equal(t, 0, r.Find("[data-asc-org-agents]").Length(), "agent features off")
	})

	require.NoError(t, ascagentui.SetFeatures(t.Context(), user2, true))

	t.Run("With agent features on the row counts the agents", func(t *testing.T) {
		doc := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/explore/organizations?q=org3"), http.StatusOK).Body)
		r := row(doc)
		agents := r.Find("[data-asc-org-agents]")
		require.Equal(t, 1, agents.Length())
		assert.Equal(t, "2", strings.TrimSpace(agents.Find("b").Text()))
		assert.Equal(t, "agents", strings.TrimSpace(agents.Find("span").Text()))
	})

	t.Run("A visitor sees the public members and no agents", func(t *testing.T) {
		doc := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/explore/organizations?q=org3"), http.StatusOK).Body)
		r := row(doc)
		public := count(nil, false)
		if public == 0 {
			assert.Equal(t, 0, r.Find("[data-asc-org-members]").Length())
		} else {
			assert.Equal(t, int(min(public, 4)), r.Find("[data-asc-org-members] a.ds-av").Length())
		}
		assert.Equal(t, 0, r.Find("[data-asc-org-agents]").Length())
	})
}
