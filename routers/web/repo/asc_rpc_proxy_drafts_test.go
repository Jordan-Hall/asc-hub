// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/ascidentity"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every spelling by which a request could name the draft ref of another person (the drafts of person 7), or any ref
// that is not heads, tags or the signed-in person's own drafts.
var ascRPCDraftAliases = []string{
	"refs/users/7/heads/draft", "REFS/Users/7/heads/draft", "refs/Users/7/heads/draft",
	"users/7/heads/draft", "Users/7/heads/draft", "USERS/7/heads/draft", "users/7",
	"refs//users/7/heads/draft", "refs/users//7/heads/draft", "refs/users/7//heads/draft", "//refs/users/7/heads/draft", "/refs/users/7/heads/draft",
	"refs/users/7/heads/draft/", "refs/./users/7/heads/draft", "refs/users/./7/heads/draft", "refs/users/7/../7/heads/draft",
	"refs/heads/../users/7/heads/draft", "refs/heads/x/../../users/7/heads/draft", `refs\users\7\heads\draft`, `refs/users\7/heads/draft`,
	"refs/users/7/heads/draft@{0}", "users/7/heads/draft@{0}", "users/7/heads/draft^{commit}", "users/7/heads/draft~1", "users/7/heads/draft^",
	"main..users/7/heads/draft", "users/7/heads/draft..main", "main...users/7/heads/draft", "^users/7/heads/draft main",
	"main users/7/heads/draft", "main\tusers/7/heads/draft", "main\nusers/7/heads/draft", "main,users/7/heads/draft",
	"users/7/heads/draft:a.txt", "refs/users/7/heads/draft:src/a.txt", "main:a.txt users/7/heads/draft",
	"refs/users/07/heads/draft", "refs/users/+7/heads/draft", "refs/users/7 /heads/draft", "refs/users/7\x00/heads/draft", "refs/users/7 /heads/draft",
	"refs/users/%37/heads/draft", "refs/users/7/heads/draft�", "refs/users/2/heads/x/../../../7/heads/draft",
	"refs/users/2", "refs/users/2/", "refs/users", "refs/users/", "refs", "users",
	"refs/pull/1/head", "refs/snapshots/x", "refs/changes/x", "refs/anything/else", "refs/heads/../../refs/users/7/heads/draft",
	"heads/../users/7/heads/draft", "refs/heads/a/refs/users/7/heads/draft",
}

func TestAscRpcProxyScreen(t *testing.T) {
	for _, alias := range ascRPCDraftAliases {
		for _, doer := range []int64{0, 4} {
			assert.False(t, ascRPCScreen(alias, doer), "doer %d: %q", doer, alias)
		}
	}
	// The person reads their own drafts, fully qualified; nobody else's.
	assert.True(t, ascRPCScreen("refs/users/2/heads/draft", 2))
	assert.True(t, ascRPCScreen("main..refs/users/2/heads/draft", 2))
	assert.True(t, ascRPCScreen("refs/users/2/heads/draft@{0}", 2))
	assert.True(t, ascRPCScreen("refs/users/2/heads/draft:a.txt", 2))
	assert.False(t, ascRPCScreen("refs/users/2/heads/draft", 0))
	assert.False(t, ascRPCScreen("refs/users/2/heads/draft", 22))
	assert.False(t, ascRPCScreen("refs/users/22/heads/draft", 2))
	assert.False(t, ascRPCScreen("users/2/heads/draft", 2), "the short alias is never accepted")
	assert.False(t, ascRPCScreen("refs/users/2/heads/a refs/users/7/heads/b", 2))
	assert.False(t, ascRPCScreen("refs/users/2/heads/a refs/users/2/../7/heads/b", 2))
	// What the islands send in normal use.
	for _, ok := range []string{"", "main", "trunk", "HEAD", "HEAD~3", "HEAD^", "HEAD^2", "feature/users-page", "feature/x", "refs/heads/main", "refs/tags/v1.0.0", "v1.2.3", "main..feature/x", "main...feature/x",
		"main@{u}", "main@{3}", "main^{tree}", "main:src/users/mod.rs", "^main", "0123456789abcdef0123456789abcdef01234567", "heads/main", "src/refs/user.rs", "a.b/c-d_e"} {
		assert.True(t, ascRPCScreen(ok, 2), "%q", ok)
	}
}

func TestAscRpcProxyRefusesTheRefsOfOtherUsers(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	fake, addr := startFakeAsc(t)
	useAsc(t, addr)
	setStorageBackend(t, 1, "asc")
	repo := &ascv1.RepoRef{Name: "user2/repo1"}

	type request func(alias string) (service, method string, body []byte)
	// One request per field that carries a revision or a ref, in lists, single fields and nested messages.
	fields := map[string]request{
		"Log.include": func(a string) (string, string, []byte) {
			return "Repo", "Log", mustMarshal(t, &ascv1.LogRequest{Repo: repo, Include: []string{"main", a}})
		},
		"Log.exclude": func(a string) (string, string, []byte) {
			return "Repo", "Log", mustMarshal(t, &ascv1.LogRequest{Repo: repo, Exclude: []string{a}})
		},
		"CommitGraph.include": func(a string) (string, string, []byte) {
			return "Repo", "CommitGraph", mustMarshal(t, &ascv1.CommitGraphRequest{Repo: repo, Include: []string{a}})
		},
		"ReadFile.rev_path": func(a string) (string, string, []byte) {
			return "Repo", "ReadFile", mustMarshal(t, &ascv1.ReadFileRequest{Repo: repo, At: &ascv1.ReadFileRequest_RevPath{RevPath: a + ":a.txt"}})
		},
		"ReadChangeset.rev": func(a string) (string, string, []byte) {
			return "Repo", "ReadChangeset", mustMarshal(t, &ascv1.ReadChangesetRequest{Repo: repo, Rev: a})
		},
		"Blame.rev": func(a string) (string, string, []byte) {
			return "Repo", "Blame", mustMarshal(t, &ascv1.BlameRequest{Repo: repo, Rev: a})
		},
		"Diff.head": func(a string) (string, string, []byte) {
			return "Repo", "Diff", mustMarshal(t, &ascv1.DiffRequest{Repo: repo, Base: "main", Head: a})
		},
		"Diff.base": func(a string) (string, string, []byte) {
			return "Repo", "Diff", mustMarshal(t, &ascv1.DiffRequest{Repo: repo, Base: a, Head: "main"})
		},
		"Compare.b": func(a string) (string, string, []byte) {
			return "Repo", "Compare", mustMarshal(t, &ascv1.CompareRequest{Repo: repo, A: "main", B: a})
		},
		"Compare.a": func(a string) (string, string, []byte) {
			return "Repo", "Compare", mustMarshal(t, &ascv1.CompareRequest{Repo: repo, A: a, B: "main"})
		},
		"Grep.rev": func(a string) (string, string, []byte) {
			return "Repo", "Grep", mustMarshal(t, &ascv1.GrepRequest{Repo: repo, Rev: a, Pattern: "x"})
		},
		"RepoConfig.rev": func(a string) (string, string, []byte) {
			return "Repo", "RepoConfig", mustMarshal(t, &ascv1.RepoConfigRequest{Repo: repo, Rev: a})
		},
		"ResolveChanges.revs": func(a string) (string, string, []byte) {
			return "Changes", "ResolveChanges", mustMarshal(t, &ascv1.ResolveChangesRequest{Repo: repo, Revs: []string{a}})
		},
		"Stack.rev (oneof)": func(a string) (string, string, []byte) {
			return "Changes", "Stack", mustMarshal(t, &ascv1.StackRequest{Repo: repo, At: &ascv1.StackRequest_Rev{Rev: a}})
		},
		"Stack.target": func(a string) (string, string, []byte) {
			return "Changes", "Stack", mustMarshal(t, &ascv1.StackRequest{Repo: repo, Target: a})
		},
		"Mergeability.target": func(a string) (string, string, []byte) {
			return "Changes", "Mergeability", mustMarshal(t, &ascv1.MergeabilityRequest{Repo: repo, Target: a})
		},
		"Overlaps.target": func(a string) (string, string, []byte) {
			return "Changes", "Overlaps", mustMarshal(t, &ascv1.OverlapsRequest{Repo: repo, Target: a})
		},
		"Search.scope.rev": func(a string) (string, string, []byte) {
			return "CodeIntel", "Search", mustMarshal(t, &ascv1.SearchRequest{Scope: &ascv1.Scope{Repo: repo, At: &ascv1.Scope_Rev{Rev: a}}, Pattern: "x"})
		},
		"Affected.head": func(a string) (string, string, []byte) {
			return "Projects", "Affected", mustMarshal(t, &ascv1.AffectedRequest{Repo: repo, Base: "main", Head: a})
		},
		"ProjectGraph.rev": func(a string) (string, string, []byte) {
			return "Projects", "ProjectGraph", mustMarshal(t, &ascv1.ProjectGraphRequest{Repo: repo, Rev: a})
		},
	}
	for field, mk := range fields {
		for _, alias := range ascRPCDraftAliases {
			for _, doer := range []int64{0, 4} { // a visitor and a signed-in person who is not the owner
				before := fake.count()
				service, method, body := mk(alias)
				res := callProxy(t, doer, service, method, body)
				require.NotEqual(t, "0", res.trailer["grpc-status"], "%s as %d: %q", field, doer, alias)
				require.Equal(t, before, fake.count(), "%s as %d: %q reached asc", field, doer, alias)
			}
		}
	}

	t.Run("bytes that could be a name, and ids of the wrong size", func(t *testing.T) {
		before := fake.count()
		for _, change := range []string{"refs/users/7/heads/x", "short", strings.Repeat("a", 21), "users/7/heads/draft/aaaaaaaaaaaa"} {
			res := callProxy(t, 4, "asc.v1.Changes", "Overlaps", mustMarshal(t, &ascv1.OverlapsRequest{Repo: repo, Change: []byte(change)}))
			assert.NotEqual(t, "0", res.trailer["grpc-status"], change)
		}
		assert.Equal(t, before, fake.count())
	})
}

func TestAscRpcProxyCarriesThePersonAndOnlyTheirDrafts(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	fake, addr := startFakeAsc(t)
	useAsc(t, addr)
	setStorageBackend(t, 1, "asc")
	body := func(rev string) []byte {
		return mustMarshal(t, &ascv1.LogRequest{Repo: &ascv1.RepoRef{Name: "User2/Repo1"}, Include: []string{rev}})
	}
	verify := func(call fakeAscCall, subject string) (string, string, error) {
		return ascidentity.Verify(testForwardedKey, func(k string) string { return call.claim[k] }, subject, time.Now())
	}

	t.Run("a signed-in call carries the claim of the person for the repository, as read", func(t *testing.T) {
		res := callProxy(t, 2, "asc.v1.Repo", "Log", body("main"))
		require.Equal(t, "0", res.trailer["grpc-status"])
		call := fake.last(t)
		require.NotNil(t, call.claim)
		assert.Empty(t, call.other)
		user, role, err := verify(call, "user2/repo1")
		require.NoError(t, err)
		assert.Equal(t, "2:user2", user)
		assert.Equal(t, ascidentity.RoleRead, role)
		_, _, err = verify(call, "user2/repo2")
		assert.Error(t, err, "the claim is for this repository only")
	})
	t.Run("a visitor carries no claim", func(t *testing.T) {
		res := callProxy(t, 0, "asc.v1.Repo", "Log", body("main"))
		require.Equal(t, "0", res.trailer["grpc-status"])
		assert.Nil(t, fake.last(t).claim)
	})
	t.Run("a browser cannot bring a claim of its own", func(t *testing.T) {
		callProxy(t, 4, "asc.v1.Repo", "Log", body("main"), func(r *http.Request) {
			for _, k := range []string{ascidentity.HeaderUser, ascidentity.HeaderRole, ascidentity.HeaderTS, ascidentity.HeaderMAC} {
				r.Header.Set(k, "2:user2")
			}
		})
		call := fake.last(t)
		assert.Empty(t, call.other)
		user, _, err := verify(call, "user2/repo1")
		require.NoError(t, err)
		assert.Equal(t, "4:user4", user, "the claim is the signed-in person's")
	})
	t.Run("the person reads their own draft refs, fully qualified, and not those of others", func(t *testing.T) {
		before := fake.count()
		res := callProxy(t, 2, "asc.v1.Repo", "Log", body("refs/users/2/heads/draft"))
		assert.Equal(t, "0", res.trailer["grpc-status"])
		assert.Equal(t, before+1, fake.count())
		for _, rev := range []string{"refs/users/4/heads/draft", "users/4/heads/draft", "users/2/heads/draft"} {
			res = callProxy(t, 2, "asc.v1.Repo", "Log", body(rev))
			assert.NotEqual(t, "0", res.trailer["grpc-status"], rev)
		}
		// Person 4 asks for the draft of person 2, by every spelling of it.
		for _, rev := range []string{"refs/users/2/heads/draft", "users/2/heads/draft", "refs//users/2/heads/draft", "REFS/USERS/2/heads/draft"} {
			res = callProxy(t, 4, "asc.v1.Repo", "Log", body(rev))
			assert.NotEqual(t, "0", res.trailer["grpc-status"], rev)
		}
		assert.Equal(t, before+1, fake.count(), "only the owner's call reached asc")
	})
	t.Run("without a key the claim cannot be made and a signed-in call is not made", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Asc.ForwardedKey, []byte(nil))()
		before := fake.count()
		res := callProxy(t, 2, "asc.v1.Repo", "Log", body("main"))
		assert.Equal(t, "14", res.trailer["grpc-status"])
		assert.Equal(t, before, fake.count())
		res = callProxy(t, 0, "asc.v1.Repo", "Log", body("main"))
		assert.Equal(t, "0", res.trailer["grpc-status"], "a visitor needs no claim")
	})
}
