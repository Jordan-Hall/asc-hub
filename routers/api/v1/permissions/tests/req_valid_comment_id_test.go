// Copyright 2026 The Forgejo Authors.
// SPDX-License-Identifier: GPLv3-or-later

package tests_test

import (
	"testing"

	user_model "forgejo.org/models/user"
	apiv1_permissions "forgejo.org/routers/api/v1/permissions"
)

var _ = registerFunctionTestWithCall(apiv1_permissions.ReqValidCommentID, functionTest{
	sequenceFilter: []string{
		"APIAuthorization",
		"RepoAccess",
		"ReqValidCommentID",
	},
	fulfillNeeds: func(t *testing.T, data *fixtureData) {
		t.Helper()
		data.SetDefault("issue", "issueOne")
		data.SetDefault("issueAuthor", "issueAuthor")
		data.SetDefault("comment", "comment for ReqValidCommentID")
	},
	interpret: func(t *testing.T, permissions *apiv1_permissions.Permissions, data *fixtureData) {
		fixtureCreateUser(t, &user_model.User{Name: data.Get("issueAuthor")})
		fixtureSetIssue(t, permissions, data)
		fixtureCreateComment(t, permissions, data)
	},
	call: func(t *testing.T, ctx apiv1_permissions.Context, data *fixtureData, _ []any) {
		t.Helper()
		comment := fixtureGetComment(t, data)
		if data.Has("NilIssue") {
			comment.Issue = nil
		}
		if data.Has("InconsistentID") {
			comment.Issue.RepoID = 123456
		}
		t.Logf("calling ReqValidCommentID(ctx, %+v)", comment)
		apiv1_permissions.ReqValidCommentID(ctx, comment)
	},
	fixtures: []*fixtureType{
		{
			data: newFixtureData(map[string]string{
				"doer":        "doerregular",
				"repository":  "userowner/repositorypublic",
				"issue":       "issueOne",
				"issueAuthor": "issueAuthor",
				"comment":     "comment for ReqValidCommentID",
			}),
		},
		// This fixture is unreachable because this permissions function is always used after
		// a RepoAccess that enforces the same restriction for non admin users
		// {
		// 	data: newFixtureData(map[string]string{
		// 		"doer":        "doerregular",
		// 		"repository":  "userowner/repositoryprivate",
		// 		"issue":       "issueOne",
		// 		"issueAuthor": "issueAuthor",
		// 		"comment":     "comment for ReqValidCommentID",
		// 	}),
		// 	error: "Not Found",
		// },
		{
			data: newFixtureData(map[string]string{
				"doer":        "doerregular",
				"repository":  "userowner/repositorypublic",
				"issue":       "issueOne",
				"issueAuthor": "issueAuthor",
				"comment":     "comment for ReqValidCommentID",

				"NilIssue": "true",
			}),
			error: "Not Found",
		},
		{
			data: newFixtureData(map[string]string{
				"doer":        "doerregular",
				"repository":  "userowner/repositorypublic",
				"issue":       "issueOne",
				"issueAuthor": "issueAuthor",
				"comment":     "comment for ReqValidCommentID",

				"InconsistentID": "true",
			}),
			error: "Not Found",
		},
	},
})
