// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package byoweb

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	svc "forgejo.org/services/ascbyo"
	"forgejo.org/services/ascbyo/ascbyotest"
	"forgejo.org/services/context"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func feature(name string) func(*context.Context, *Level) {
	return func(c *context.Context, l *Level) {
		c.SetParams("feature", name)
		AIFeature(c, l)
	}
}

// dialogOf is the <dialog> element with the id, from its start tag to its end.
func dialogOf(body, id string) string {
	return regexp.MustCompile(`(?s)<dialog id="` + regexp.QuoteMeta(id) + `"[^>]*>.*?</dialog>`).FindString(body)
}

func TestRepoAIBoard(t *testing.T) {
	f := setup(t)
	repo := repoLevel(t, true)

	t.Run("without keys the features are off", func(t *testing.T) {
		r := run(t, AIIndex, repo, http.MethodGet, nil, "")
		require.Equal(t, http.StatusOK, r.resp.Code, r.body())
		body := r.body()
		assert.Contains(t, body, tr(t, "ascbyo.ai.repo_lead"))
		assert.Contains(t, body, tr(t, "ascbyo.ai.empty_title"))
		assert.Contains(t, body, `data-modal="#asc-add-key"`)
		assert.Contains(t, body, `data-modal="#asc-add-endpoint"`)
		assert.Contains(t, body, `name="provider" value="openai_compatible"`, "the endpoint dialog adds an OpenAI-compatible key")
		assert.NotContains(t, body, "data-asc-open", "no dialog opens by itself")
	})

	k := f.AddKey(repo.Scope.Owner(), "Anthropic", ascv1.AiProvider_AI_PROVIDER_ANTHROPIC, ascv1.AiKeyState_AI_KEY_STATE_ACTIVE)
	f.SetKeyFields(k.GetId(), func(k *ascv1.AiKey) { k.KeyHint = "c4fA" })

	t.Run("a card per key, and the features with their provider", func(t *testing.T) {
		r := run(t, AIIndex, repo, http.MethodGet, nil, "")
		require.Equal(t, http.StatusOK, r.resp.Code, r.body())
		body := r.body()
		assert.Contains(t, body, tr(t, "ascbyo.ai.banner"))
		assert.Contains(t, body, tr(t, "ascbyo.ai.card.connected"))
		assert.Contains(t, body, "…c4fA")
		assert.Contains(t, body, tr(t, "ascbyo.ai.card.more", "Anthropic"))
		assert.Contains(t, body, `data-modal="#asc-replace-`+k.GetId()+`"`)
		assert.Contains(t, body, `action="/alice/widgets/settings/asc/ai/keys/`+k.GetId()+`/replace"`)
		remove := dialogOf(body, "asc-remove-"+k.GetId())
		assert.Contains(t, remove, `name="confirm" value="on"`)
		assert.Contains(t, remove, `/keys/`+k.GetId()+`/delete"`)
		assert.Contains(t, body, tr(t, "ascbyo.ai.uses.title"))
		assert.Contains(t, body, tr(t, "ascbyo.ai.feature.agent_runs"))
		assert.Contains(t, body, `action="/alice/widgets/settings/asc/ai/features/agent_runs"`)
		assert.Regexp(t, `<option value="inherit" selected>`+regexp.QuoteMeta(tr(t, "ascbyo.ai.uses.instance_default")), body)
		assert.Contains(t, body, `<option value="`+k.GetId()+`">`)
		assert.NotContains(t, body, tr(t, "ascbyo.ai.card.used_for"), "nothing uses the key yet")
	})

	t.Run("a feature set to the key", func(t *testing.T) {
		r := run(t, feature("agent_runs"), repo, http.MethodPost, url.Values{"key_id": {k.GetId()}, "monthly_cap": {"200"}, "op_id": {ascbyotest.OpIDForTest}}, "")
		require.Equal(t, http.StatusSeeOther, r.resp.Code, r.body())
		assert.Equal(t, "/alice/widgets/settings/asc/ai", r.loc())
		assert.Equal(t, tr(t, "ascbyo.flash.feature_saved"), r.ctx.Flash.SuccessMsg)
		req := f.CallsTo("SetFeature")[0].Req.(*ascv1.SetAiFeatureRequest)
		assert.Equal(t, k.GetId(), req.GetKeyId())
		assert.Equal(t, uint64(200_000_000), req.GetMonthlyCapMicroUsd())
		assert.False(t, req.GetClear())
		assert.Equal(t, ascv1.OwnerKind_OWNER_KIND_REPO, req.GetOwner().GetKind())

		f.SetFeatureFields(repo.Scope.Owner(), ascv1.AiFeature_AI_FEATURE_AGENT_RUNS, func(s *ascv1.AiFeatureSetting) {
			s.MonthSpentMicroUsd = 141_300_000
		})
		r = run(t, AIIndex, repo, http.MethodGet, nil, "")
		body := r.body()
		assert.Contains(t, body, tr(t, "ascbyo.ai.card.used_for"))
		assert.Contains(t, body, "<b>$141.30</b> "+tr(t, "ascbyo.ai.uses.of_month", "$200.00"))
		assert.Contains(t, body, `<option value="`+k.GetId()+`" selected>`)
		assert.Contains(t, dialogOf(body, "asc-feature-agent_runs"), `value="200"`, "the cap dialog starts from the cap")
		assert.Contains(t, dialogOf(body, "asc-feature-agent_runs"), `name="clear" value="on"`)
	})

	t.Run("a reached cap says until when", func(t *testing.T) {
		f.SetFeatureFields(repo.Scope.Owner(), ascv1.AiFeature_AI_FEATURE_AGENT_RUNS, func(s *ascv1.AiFeatureSetting) {
			s.MonthSpentMicroUsd, s.Paused, s.ResumesAt = 200_000_000, true, 1_793_491_200
		})
		r := run(t, AIIndex, repo, http.MethodGet, nil, "")
		body := r.body()
		assert.Contains(t, body, "data-paused")
		assert.Contains(t, body, "asc-meter-bad")
		assert.Contains(t, body, `class="asc-ai-paused"`)
		assert.Contains(t, body, "Cap reached. Agent runs are paused until")

		f.SetFeatureFields(repo.Scope.Owner(), ascv1.AiFeature_AI_FEATURE_AGENT_RUNS, func(s *ascv1.AiFeatureSetting) {
			s.PausedBy = mustScope(t, svc.KindUser, "alice").Owner()
		})
		r = run(t, AIIndex, repo, http.MethodGet, nil, "")
		assert.Contains(t, r.body(), "The cap of alice is reached.")
		f.SetFeatureFields(repo.Scope.Owner(), ascv1.AiFeature_AI_FEATURE_AGENT_RUNS, func(s *ascv1.AiFeatureSetting) {
			s.Paused, s.PausedBy, s.MonthSpentMicroUsd = false, nil, 0
		})
	})

	t.Run("a refused cap shows its dialog again", func(t *testing.T) {
		n := len(f.CallsTo("SetFeature"))
		r := run(t, feature("agent_runs"), repo, http.MethodPost, url.Values{"key_id": {k.GetId()}, "monthly_cap": {"-3"}}, "")
		require.Equal(t, http.StatusUnprocessableEntity, r.resp.Code)
		d := dialogOf(r.body(), "asc-feature-agent_runs")
		assert.Contains(t, d, "data-asc-open")
		assert.Contains(t, d, tr(t, "ascbyo.ai.err.cap"))
		assert.Contains(t, d, `value="-3"`)
		assert.Len(t, f.CallsTo("SetFeature"), n)
	})

	t.Run("a key of another scope is refused", func(t *testing.T) {
		theirs := f.AddKey(mustScope(t, svc.KindUser, "bob").Owner(), "bobs", ascv1.AiProvider_AI_PROVIDER_OPENAI, ascv1.AiKeyState_AI_KEY_STATE_ACTIVE)
		n := len(f.CallsTo("SetFeature"))
		r := run(t, feature("agent_runs"), repo, http.MethodPost, url.Values{"key_id": {theirs.GetId()}}, "")
		require.Equal(t, http.StatusUnprocessableEntity, r.resp.Code)
		assert.Contains(t, dialogOf(r.body(), "asc-feature-agent_runs"), tr(t, "ascbyo.ai.err.feature_key"))
		assert.NotContains(t, r.body(), "bobs")
		assert.Len(t, f.CallsTo("SetFeature"), n)
	})

	t.Run("an unknown feature is a 404", func(t *testing.T) {
		r := run(t, feature("mind_reading"), repo, http.MethodPost, url.Values{"key_id": {k.GetId()}}, "")
		assert.Equal(t, http.StatusNotFound, r.resp.Code)
	})

	t.Run("inherit removes the setting", func(t *testing.T) {
		r := run(t, feature("agent_runs"), repo, http.MethodPost, url.Values{"key_id": {"inherit"}}, "")
		require.Equal(t, http.StatusSeeOther, r.resp.Code, r.body())
		assert.Equal(t, tr(t, "ascbyo.flash.feature_cleared"), r.ctx.Flash.SuccessMsg)
		calls := f.CallsTo("SetFeature")
		assert.True(t, calls[len(calls)-1].Req.(*ascv1.SetAiFeatureRequest).GetClear())
	})

	t.Run("a refused add opens the dialog it came from", func(t *testing.T) {
		r := run(t, AICreate, repo, http.MethodPost, url.Values{"slot": {"endpoint"}, "provider": {"openai_compatible"}, "label": {"Mine"}, "api_key": {"local-key-0123456789"}}, "")
		require.Equal(t, http.StatusUnprocessableEntity, r.resp.Code)
		ep := dialogOf(r.body(), "asc-add-endpoint")
		assert.Contains(t, ep, "data-asc-open")
		assert.Contains(t, ep, tr(t, "ascbyo.ai.err.base_url_required"))
		assert.Contains(t, ep, `value="Mine"`)
		key := dialogOf(r.body(), "asc-add-key")
		assert.NotContains(t, key, "data-asc-open")
		assert.NotContains(t, key, tr(t, "ascbyo.ai.err.base_url_required"), "the other dialog starts empty")
		assert.NotContains(t, key, `value="Mine"`)
		assert.NotContains(t, r.body(), "local-key-0123456789")
	})
}

func TestAIReplace(t *testing.T) {
	f := setup(t)
	repo := repoLevel(t, true)
	k := f.AddKey(repo.Scope.Owner(), "Anthropic", ascv1.AiProvider_AI_PROVIDER_ANTHROPIC, ascv1.AiKeyState_AI_KEY_STATE_ACTIVE)
	secret := "sk-ant-api03-NEWSECRETVALUE1234"

	t.Run("the provider refuses: the old key stays and the dialog says why", func(t *testing.T) {
		f.ReplaceStatus = "unauthorized"
		defer func() { f.ReplaceStatus = "" }()
		r := run(t, AIReplace, repo, http.MethodPost, url.Values{"api_key": {secret}, "op_id": {ascbyotest.OpIDForTest}}, k.GetId())
		require.Equal(t, http.StatusUnprocessableEntity, r.resp.Code, r.body())
		d := dialogOf(r.body(), "asc-replace-"+k.GetId())
		assert.Contains(t, d, "data-asc-open")
		assert.Contains(t, d, tr(t, "ascbyo.ai.replace.rejected.unauthorized", tr(t, "ascbyo.ai.provider.anthropic")), "the refusal names the provider")
		assert.Contains(t, d, `aria-invalid="true"`)
		assert.NotContains(t, r.body(), "NEWSECRET")
		assert.Empty(t, r.ctx.Flash.SuccessMsg)
	})

	t.Run("a key too short is refused before asc is asked", func(t *testing.T) {
		n := len(f.CallsTo("ReplaceKey"))
		r := run(t, AIReplace, repo, http.MethodPost, url.Values{"api_key": {"abc"}}, k.GetId())
		require.Equal(t, http.StatusUnprocessableEntity, r.resp.Code)
		assert.Contains(t, dialogOf(r.body(), "asc-replace-"+k.GetId()), tr(t, "ascbyo.ai.err.api_key"))
		assert.Len(t, f.CallsTo("ReplaceKey"), n)
	})

	t.Run("accepted", func(t *testing.T) {
		r := run(t, AIReplace, repo, http.MethodPost, url.Values{"api_key": {secret}, "op_id": {ascbyotest.OpIDForTest}}, k.GetId())
		require.Equal(t, http.StatusSeeOther, r.resp.Code, r.body())
		assert.Equal(t, "/alice/widgets/settings/asc/ai", r.loc())
		assert.Equal(t, tr(t, "ascbyo.flash.ai_replaced"), r.ctx.Flash.SuccessMsg)
		calls := f.CallsTo("ReplaceKey")
		assert.Equal(t, secret, calls[len(calls)-1].Req.(*ascv1.ReplaceAiKeyRequest).GetApiKey())
		r = run(t, AIIndex, repo, http.MethodGet, nil, "")
		assert.Contains(t, r.body(), "…1234")
		assert.NotContains(t, r.body(), "NEWSECRET")
	})

	t.Run("another owner's key is a 404", func(t *testing.T) {
		theirs := f.AddKey(mustScope(t, svc.KindUser, "alice").Owner(), "owner key", ascv1.AiProvider_AI_PROVIDER_OPENAI, ascv1.AiKeyState_AI_KEY_STATE_ACTIVE)
		n := len(f.CallsTo("ReplaceKey"))
		r := run(t, AIReplace, repo, http.MethodPost, url.Values{"api_key": {secret}}, theirs.GetId())
		assert.Equal(t, http.StatusNotFound, r.resp.Code)
		assert.Len(t, f.CallsTo("ReplaceKey"), n)
	})
}

func TestUserAIPageShowsFeatures(t *testing.T) {
	f := setup(t)
	lvl := userLevel(t)
	f.AddKey(lvl.Scope.Owner(), "Team", ascv1.AiProvider_AI_PROVIDER_OPENAI, ascv1.AiKeyState_AI_KEY_STATE_ACTIVE)
	r := run(t, AIIndex, lvl, http.MethodGet, nil, "")
	require.Equal(t, http.StatusOK, r.resp.Code, r.body())
	body := r.body()
	assert.Contains(t, body, tr(t, "ascbyo.ai.lead"), "a person's page keeps its list")
	assert.Contains(t, body, `data-testid="asc-ai-features"`)
	assert.Contains(t, body, `action="/user/settings/asc/ai/features/agent_runs"`)
	// meaning search uses no key: its row says where it runs and has nothing to choose or cap
	i := strings.Index(body, `data-testid="asc-feature-local"`)
	require.GreaterOrEqual(t, i, 0)
	row := body[i : i+strings.Index(body[i:], "</tr>")]
	assert.Contains(t, row, tr(t, "ascbyo.ai.uses.meaning"))
	assert.Contains(t, row, tr(t, "ascbyo.ai.uses.meaning_provider"))
	assert.Contains(t, row, tr(t, "ascbyo.ai.uses.meaning_spend"))
	assert.NotContains(t, row, "<select")
	assert.NotContains(t, row, "<form")

	f.SetFail("/asc.v1.AiProviders/ListFeatures", status.Error(codes.Unavailable, "down"))
	defer f.SetFail("/asc.v1.AiProviders/ListFeatures", nil)
	r = run(t, AIIndex, lvl, http.MethodGet, nil, "")
	require.Equal(t, http.StatusOK, r.resp.Code)
	assert.Contains(t, r.body(), tr(t, "ascbyo.ai.uses.failed"))
	assert.Contains(t, r.body(), "Team", "the keys still show")
}
