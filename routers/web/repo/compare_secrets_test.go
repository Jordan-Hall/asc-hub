// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"errors"
	"testing"

	perm_model "forgejo.org/models/perm"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscCompareSecrets(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ft := startFakeTrust(t, asctrust.FeatureSecretScan)
	var answer *ascv1.CheckSecretsResponse
	var fail error
	ft.checkSecrets = func(*ascv1.CheckSecretsRequest) (*ascv1.CheckSecretsResponse, error) { return answer, fail }
	key := &ascv1.SecretFinding{RuleId: "aws-access-key", Path: []byte("config/prod.env"), Line: 3}
	head, base := "1111111111111111111111111111111111111111", "2222222222222222222222222222222222222222"

	run := func(access perm_model.AccessMode) (refusal string, data map[string]any) {
		runTrust(t, trustReq{path: "/user2/repo1/compare/master...feature", user: 2, access: access}, func(ctx *context.Context) {
			prepareCompareSecrets(ctx, base, head)
			refusal = ascSecretRefusal(ctx, base, head)
			data = ctx.Data
		})
		return refusal, data
	}

	answer = &ascv1.CheckSecretsResponse{Blocks: true, BlockOnPush: true, Findings: []*ascv1.SecretFinding{key, key, {RuleId: "x", Path: []byte{0xff}}}}
	refusal, data := run(perm_model.AccessModeWrite)
	assert.Equal(t, "asc_rp.cmp_secret_text", refusal)
	block, _ := data["AscSecretBlock"].(*asctrust.SecretCheck)
	require.NotNil(t, block)
	assert.Equal(t, []string{"config/prod.env"}, block.Paths, "a path is named once, a path that is not text is left out")
	assert.EqualValues(t, "<code>config/prod.env</code>", data["AscSecretBlockPaths"])
	calls := ft.callsOf("Check")
	require.NotEmpty(t, calls)
	r := calls[len(calls)-1].Req.(*ascv1.CheckSecretsRequest)
	assert.Equal(t, base, r.GetRange().GetBase())
	assert.Equal(t, head, r.GetRange().GetHead())
	assert.NotEmpty(t, calls[len(calls)-1].Auth, "asked as the person")

	answer = &ascv1.CheckSecretsResponse{Blocks: true, Truncated: true}
	refusal, data = run(perm_model.AccessModeWrite)
	assert.Equal(t, "asc_rp.cmp_secret_unread_text", refusal, "a scan that could not read the whole change blocks too")
	assert.NotNil(t, data["AscSecretBlock"])

	answer = &ascv1.CheckSecretsResponse{BlockOnPush: false, Findings: []*ascv1.SecretFinding{key}}
	refusal, data = run(perm_model.AccessModeWrite)
	assert.Empty(t, refusal, "the policy does not block: the change opens")
	assert.Nil(t, data["AscSecretBlock"])

	fail = errors.New("down")
	refusal, data = run(perm_model.AccessModeWrite)
	assert.Empty(t, refusal, "asc cannot be asked: the push gate already guards the branch")
	assert.Nil(t, data["AscSecretBlock"])

	fail, answer = nil, &ascv1.CheckSecretsResponse{Blocks: true}
	n := len(ft.callsOf("Check"))
	refusal, _ = run(perm_model.AccessModeRead)
	assert.Empty(t, refusal)
	assert.Len(t, ft.callsOf("Check"), n, "a reader opens no change from a branch here: asc is not asked")

	setStorageBackend(t, 1, "git")
	refusal, _ = run(perm_model.AccessModeWrite)
	assert.Empty(t, refusal)
	assert.Len(t, ft.callsOf("Check"), n, "a git repository is not scanned by asc")
}

func TestAscCompareSecretBanner(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	body := func(block *asctrust.SecretCheck) string {
		return renderPart(t, "repo/issue/new_form", func(d map[string]any) {
			d["PageIsComparePull"] = true
			d["PullRequestWorkInProgressPrefixes"] = []string{"WIP:"}
			d["Repository"] = &repo_model.Repository{ID: 1, Name: "repo1", OwnerName: "user2", StorageBackend: "asc"}
			if block != nil {
				d["AscSecretBlock"] = block
				d["AscSecretBlockPaths"] = ascCodeList(block.Paths)
			}
		})
	}
	html := body(&asctrust.SecretCheck{Blocks: true, Paths: []string{"config/prod.env"}})
	assert.Contains(t, html, `role="note" id="asc-secret-block" data-testid="asc-secret-block"`)
	assert.Contains(t, html, "asc_rp.cmp_secret<")
	assert.Contains(t, html, `disabled aria-describedby="asc-secret-block"`, "the change cannot be opened")
	assert.Contains(t, body(&asctrust.SecretCheck{Blocks: true}), "asc_rp.cmp_secret_unread")
	clean := body(nil)
	assert.NotContains(t, clean, "asc-secret-block")
	assert.NotContains(t, clean, "disabled aria-describedby")
}
