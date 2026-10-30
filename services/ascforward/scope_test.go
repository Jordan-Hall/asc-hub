// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascforward

import (
	"net/http"
	"testing"

	auth_model "forgejo.org/models/auth"

	"github.com/stretchr/testify/assert"
)

// A token is held to the scope of the part of asc a call is for, at the level of the call.
func TestScopeRefusal(t *testing.T) {
	for _, c := range []struct {
		scope           auth_model.AccessTokenScope
		service, method string
		refused         bool
	}{
		{"write:repository", "Repo", "ListRefs", false},
		{"read:repository", "Repo", "ListRefs", false},
		{"read:repository", "Repo", "UpdateRefs", true},     // a write needs write
		{"read:repository", "Changes", "ListChanges", true}, // no changes scope at all
		{"read:changes", "Changes", "ListChanges", false},   // a read of changes
		{"read:changes", "Stacks", "Restack", true},         // a restack writes
		{"write:changes", "Stacks", "Restack", false},       // and write allows it
		{"write:changes", "Repo", "ListRefs", true},         // changes do not reach the code
		{"write:repository", "Agents", "ListRuns", true},    // agent runs are their own scope
		{"read:runs", "Agents", "ListRuns", false},          // read of runs
		{"read:runs", "Agents", "CreateRun", true},          // starting a run is a write
		{"write:runs", "AgentTokens", "CreateToken", false}, // run credentials belong to runs
		{"write:repository", "Review", "AddComment", true},  // reviews are changes
		{"all", "Agents", "CreateRun", false},               // all is all
		{"write:repository,read:user", "Policy", "Propose", false},
		{"", "Repo", "ListRefs", true},
	} {
		got := scopeRefusal(c.scope, c.service, c.method)
		assert.Equal(t, c.refused, got != "", "%q %s/%s: %q", c.scope, c.service, c.method, got)
	}
}

// A method whose name is not a known read is held to the write level.
func TestUnknownMethodsAreWrites(t *testing.T) {
	assert.Equal(t, auth_model.Write, levelOf("Frobnicate"))
	assert.Equal(t, auth_model.Write, levelOf("CreateRun"))
	assert.Equal(t, auth_model.Read, levelOf("ListRuns"))
	assert.Equal(t, auth_model.Read, levelOf("GetBrief"))
}

func TestBearerToken(t *testing.T) {
	for header, want := range map[string]string{"Bearer abc": "abc", "token abc": "abc", "Basic YTpi": "", "abc": "", "": ""} {
		r, _ := http.NewRequest(http.MethodPost, "/", nil)
		if header != "" {
			r.Header.Set("Authorization", header)
		}
		assert.Equal(t, want, bearerToken(r), header)
	}
}
