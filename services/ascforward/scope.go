// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascforward

import (
	"net/http"
	"strings"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/modules/log"
)

// codePermissionDenied is the gRPC status the gate answers with.
const codePermissionDenied = 7

// areaOf is the part of asc a service belongs to, which is the scope category a personal access token needs for it:
// the work on changesets, stacks and their review needs the scope changes, agent runs need the scope runs, and the
// rest (code, refs, search, policy, checks, signing, audit, dependencies) needs the scope repository.
func areaOf(service string) auth_model.AccessTokenScopeCategory {
	switch service {
	case "Changes", "Stacks", "Review", "MergeQueue", "Inbox", "Projects":
		return auth_model.AccessTokenScopeCategoryChanges
	case "Agents", "AgentTokens":
		return auth_model.AccessTokenScopeCategoryRuns
	}
	return auth_model.AccessTokenScopeCategoryRepository
}

// readPrefixes start the names of methods that only read. A method with any other name is held to the write level,
// so a method added later is never taken for a read by mistake.
var readPrefixes = []string{"Get", "List", "Search", "Find", "Read", "Stat", "Has", "Count", "Lookup", "Watch", "Subscribe", "Fetch", "Verify", "Evaluate", "Preview", "Estimate", "Export", "Download", "Compare", "Blame", "Log", "Diff", "Cross", "Symbol", "Status"}

func levelOf(method string) auth_model.AccessTokenScopeLevel {
	for _, p := range readPrefixes {
		if strings.HasPrefix(method, p) {
			return auth_model.Read
		}
	}
	return auth_model.Write
}

// TokenGate holds a personal access token to its scope before a call is forwarded. A call with no personal access
// token (an OAuth token, an agent token, a hub-signed claim, or nothing) is left to asc, which asks the hub. It
// returns the reason when the call must be refused, and "" otherwise. The hub's own authz answer
// (routers/private/asc.go) holds the token to the repository scope of the repository itself; this adds the scope of
// the part of asc the call is for, which that answer cannot see because it only learns the repository and the mode.
func TokenGate(r *http.Request, service, method string) string {
	token := bearerToken(r)
	if token == "" {
		return ""
	}
	t, err := auth_model.GetAccessTokenBySHA(r.Context(), token)
	if err != nil {
		if !auth_model.IsErrAccessTokenNotExist(err) && !auth_model.IsErrAccessTokenEmpty(err) {
			log.Error("asc forward: token lookup: %v", err)
			return "the hub could not check the token; try again"
		}
		// not a personal access token (or one that is gone or expired): asc asks the hub, which refuses it
		return ""
	}
	return scopeRefusal(t.Scope, service, method)
}

// scopeRefusal is the reason a token with this scope may not make the call, or "".
func scopeRefusal(scope auth_model.AccessTokenScope, service, method string) string {
	need := auth_model.GetRequiredScopes(levelOf(method), areaOf(service))
	ok, err := scope.HasScope(need...)
	if err != nil || !ok {
		return "this token has no " + string(need[0]) + " scope"
	}
	return ""
}

// bearerToken is the token of an Authorization header: Bearer or Token, or the password of a Basic login.
func bearerToken(r *http.Request) string {
	h := strings.Fields(r.Header.Get("Authorization"))
	if len(h) != 2 {
		return ""
	}
	switch strings.ToLower(h[0]) {
	case "bearer", "token":
		return h[1]
	}
	return ""
}
