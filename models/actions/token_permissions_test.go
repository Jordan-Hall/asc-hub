// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package actions

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func permissionsNode(t *testing.T, s string) *yaml.Node {
	t.Helper()
	if s == "" {
		return &yaml.Node{}
	}
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(s), &doc))
	return doc.Content[0]
}

func TestActionsPermissionsResolve(t *testing.T) {
	open := TokenCeiling{AllowPackagesWrite: true}
	closed := TokenCeiling{}
	fork := TokenCeiling{IsForkPullRequest: true, AllowPackagesWrite: true}

	allRead := map[string]TokenPermissionLevel{"contents": TokenPermissionRead, "issues": TokenPermissionRead, "pull-requests": TokenPermissionRead, "actions": TokenPermissionRead, "repository-projects": TokenPermissionRead, "packages": TokenPermissionRead}
	allWrite := map[string]TokenPermissionLevel{"contents": TokenPermissionWrite, "issues": TokenPermissionWrite, "pull-requests": TokenPermissionWrite, "actions": TokenPermissionWrite, "repository-projects": TokenPermissionWrite, "packages": TokenPermissionWrite}
	allWriteNoPackages := map[string]TokenPermissionLevel{"contents": TokenPermissionWrite, "issues": TokenPermissionWrite, "pull-requests": TokenPermissionWrite, "actions": TokenPermissionWrite, "repository-projects": TokenPermissionWrite, "packages": TokenPermissionRead}

	for _, tc := range []struct {
		name     string
		workflow string
		job      string
		ceiling  TokenCeiling
		want     map[string]TokenPermissionLevel // nil: no block
	}{
		{name: "no block", ceiling: open, want: nil},
		{name: "empty mapping is nothing", workflow: "{}", ceiling: open, want: map[string]TokenPermissionLevel{}},
		{name: "null is nothing", workflow: "~", ceiling: open, want: map[string]TokenPermissionLevel{}},
		{name: "read-all", workflow: "read-all", ceiling: open, want: allRead},
		{name: "write-all is the ceiling", workflow: "write-all", ceiling: open, want: allWrite},
		{name: "write-all never grants packages while the setting is off", workflow: "write-all", ceiling: closed, want: allWriteNoPackages},
		{name: "write-all of a fork pull request is read", workflow: "write-all", ceiling: fork, want: allRead},
		{
			name: "scopes the block does not name are none", workflow: "{contents: read, packages: write}", ceiling: open,
			want: map[string]TokenPermissionLevel{"contents": TokenPermissionRead, "packages": TokenPermissionWrite},
		},
		{
			name: "packages write is capped by the setting", workflow: "{packages: write}", ceiling: closed,
			want: map[string]TokenPermissionLevel{"packages": TokenPermissionRead},
		},
		{
			name: "a scope can only narrow", workflow: "{issues: none, contents: write}", ceiling: fork,
			want: map[string]TokenPermissionLevel{"contents": TokenPermissionRead},
		},
		{
			name: "scopes that are not enforced never grant and are not stored", workflow: "{contents: read, checks: write, id-token: write, statuses: write}", ceiling: open,
			want: map[string]TokenPermissionLevel{"contents": TokenPermissionRead},
		},
		{
			name: "job overrides workflow", workflow: "write-all", job: "{contents: read}", ceiling: open,
			want: map[string]TokenPermissionLevel{"contents": TokenPermissionRead},
		},
		{name: "job can narrow to nothing", workflow: "write-all", job: "{}", ceiling: open, want: map[string]TokenPermissionLevel{}},
		{name: "job cannot go above the ceiling", workflow: "{contents: read}", job: "write-all", ceiling: closed, want: allWriteNoPackages},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveTokenPermissions(permissionsNode(t, tc.workflow), permissionsNode(t, tc.job), tc.ceiling)
			require.NoError(t, err)
			if tc.want == nil {
				assert.Nil(t, got)
				assert.Empty(t, got.String())
				return
			}
			require.NotNil(t, got)
			for scope, level := range tc.want {
				assert.Equal(t, level, got.Level(scope), scope)
			}
			// Everything the block does not grant is none.
			for _, scope := range enforcedTokenScopes {
				if _, ok := tc.want[scope]; !ok {
					assert.Equal(t, TokenPermissionNone, got.Level(scope), scope)
				}
			}
			// What is stored on the task reads back as the same permissions.
			stored, err := ParseTokenPermissions(got.String())
			require.NoError(t, err)
			assert.Equal(t, got, stored)
		})
	}
}

func TestActionsPermissionsUnknownValues(t *testing.T) {
	for _, tc := range []struct {
		name, permissions, message string
	}{
		{"unknown level", "{contents: admin}", `unknown value "admin" for "contents"`},
		{"unknown keyword", "everything", `unknown value "everything"`},
		{"unknown scope", "{content: read}", `unknown scope "content"`},
		{"level is not a scalar", "{contents: [read]}", `the value of "contents" must be none, read or write`},
		{"list", "[contents]", "expected read-all, write-all or a mapping"},
		{"read-all is not a level", "{contents: read-all}", `unknown value "read-all" for "contents"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveTokenPermissions(permissionsNode(t, tc.permissions), &yaml.Node{}, TokenCeiling{AllowPackagesWrite: true})
			require.ErrorContains(t, err, tc.message)
			assert.Nil(t, got)

			// The job's block is checked too, and so is a workflow's that the job overrides.
			require.Error(t, ValidateTokenPermissions(&yaml.Node{}, permissionsNode(t, tc.permissions)))
			require.Error(t, ValidateTokenPermissions(permissionsNode(t, tc.permissions), permissionsNode(t, "{}")))
		})
	}
}

func TestActionsPermissionsUnenforcedScopes(t *testing.T) {
	assert.Equal(t, []string{"checks", "statuses"}, UnenforcedTokenScopes(permissionsNode(t, "{statuses: write, contents: read, checks: read}")))
	assert.Empty(t, UnenforcedTokenScopes(permissionsNode(t, "{contents: read, packages: write}")))
	assert.Empty(t, UnenforcedTokenScopes(permissionsNode(t, "write-all")))
}
