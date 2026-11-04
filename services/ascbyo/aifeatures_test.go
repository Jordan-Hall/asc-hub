// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascbyo_test

import (
	"fmt"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/services/ascbyo"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestReplaceKey(t *testing.T) {
	s, f := newService(t)
	alice := scope(t, ascbyo.KindUser, "alice")
	bob := scope(t, ascbyo.KindUser, "bob")
	k := f.AddKey(alice.Owner(), "Team", ascv1.AiProvider_AI_PROVIDER_ANTHROPIC, ascv1.AiKeyState_AI_KEY_STATE_ACTIVE)
	newKey := "sk-ant-api03-NEWSECRETNEWSECRET9876"

	t.Run("accepted", func(t *testing.T) {
		res, fe, err := s.ReplaceKey(t.Context(), alice, k.GetId(), "  "+newKey+"\n", ascbyo.NewOpID())
		require.NoError(t, err)
		require.Empty(t, fe)
		assert.True(t, res.Replaced)
		assert.Equal(t, "ok", res.Status)
		require.NotNil(t, res.Key)
		assert.Equal(t, "9876", res.Key.GetKeyHint())
		req := f.CallsTo("ReplaceKey")[0].Req.(*ascv1.ReplaceAiKeyRequest)
		assert.Equal(t, newKey, req.GetApiKey(), "the secret is sent trimmed")
		assert.NotEmpty(t, f.CallsTo("ReplaceKey")[0].OpID)
	})

	t.Run("refused keeps the old key and never repeats the new one", func(t *testing.T) {
		f.ReplaceStatus, f.SecretEcho = "unauthorized", true
		defer func() { f.ReplaceStatus, f.SecretEcho = "", false }()
		other := "sk-ant-api03-REFUSEDREFUSED00001111"
		res, fe, err := s.ReplaceKey(t.Context(), alice, k.GetId(), other, ascbyo.NewOpID())
		require.NoError(t, err)
		require.Empty(t, fe)
		assert.False(t, res.Replaced)
		assert.Equal(t, "unauthorized", res.Status)
		assert.NotContains(t, fmt.Sprintf("%+v", res), "REFUSED")
		assert.Equal(t, "9876", res.Key.GetKeyHint(), "the key in place is the one accepted before")
	})

	t.Run("unknown status", func(t *testing.T) {
		f.ReplaceStatus = "<b>odd</b>"
		defer func() { f.ReplaceStatus = "" }()
		res, _, err := s.ReplaceKey(t.Context(), alice, k.GetId(), newKey, ascbyo.NewOpID())
		require.NoError(t, err)
		assert.Equal(t, "bad_response", res.Status)
	})

	t.Run("form errors send nothing", func(t *testing.T) {
		before := len(f.CallsTo("ReplaceKey"))
		for _, bad := range []string{"", "short", "sk-ant-with space inside-0123456789", "sk-ant-\x00-0123456789abcdef"} {
			res, fe, err := s.ReplaceKey(t.Context(), alice, k.GetId(), bad, ascbyo.NewOpID())
			require.NoError(t, err)
			assert.Nil(t, res)
			assert.Equal(t, "ascbyo.ai.err.api_key", fe["api_key"], "%q", bad)
		}
		assert.Len(t, f.CallsTo("ReplaceKey"), before)
	})

	t.Run("another owner's key", func(t *testing.T) {
		mutations := f.Mutations()
		_, _, err := s.ReplaceKey(t.Context(), bob, k.GetId(), newKey, ascbyo.NewOpID())
		assert.True(t, ascbyo.IsKind(err, ascbyo.FailNotFound))
		assert.Equal(t, mutations, f.Mutations())
	})

	t.Run("a key that moved to another owner", func(t *testing.T) {
		other := f.AddKey(bob.Owner(), "Bob", ascv1.AiProvider_AI_PROVIDER_OPENAI, ascv1.AiKeyState_AI_KEY_STATE_ACTIVE)
		f.SetKeyFields(k.GetId(), func(p *ascv1.AiKey) { p.Owner = other.GetOwner() })
		defer f.SetKeyFields(k.GetId(), func(p *ascv1.AiKey) { p.Owner = alice.Owner() })
		_, _, err := s.ReplaceKey(t.Context(), alice, k.GetId(), newKey, ascbyo.NewOpID())
		assert.True(t, ascbyo.IsKind(err, ascbyo.FailNotFound), "GetKey checks the owner first")
	})

	t.Run("asc failure", func(t *testing.T) {
		f.SetFail("/asc.v1.AiProviders/ReplaceKey", status.Error(codes.Unavailable, "down "+newKey))
		defer f.SetFail("/asc.v1.AiProviders/ReplaceKey", nil)
		_, _, err := s.ReplaceKey(t.Context(), alice, k.GetId(), newKey, ascbyo.NewOpID())
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "NEWSECRET")
	})
}

func TestFeatures(t *testing.T) {
	s, f := newService(t)
	alice := scope(t, ascbyo.KindUser, "alice")
	bob := scope(t, ascbyo.KindUser, "bob")
	repo := scope(t, ascbyo.KindRepo, "alice/app")
	k := f.AddKey(alice.Owner(), "Team", ascv1.AiProvider_AI_PROVIDER_ANTHROPIC, ascv1.AiKeyState_AI_KEY_STATE_ACTIVE)
	bobs := f.AddKey(bob.Owner(), "Bob", ascv1.AiProvider_AI_PROVIDER_OPENAI, ascv1.AiKeyState_AI_KEY_STATE_ACTIVE)

	fs, err := s.ListFeatures(t.Context(), alice)
	require.NoError(t, err)
	require.Len(t, fs, 2)
	assert.Equal(t, ascbyo.FeatureReviewSummaries, ascbyo.FeatureViewOf(alice, fs[1]).Name, "in the order of the page")
	v := ascbyo.FeatureViewOf(alice, fs[0])
	assert.Equal(t, ascbyo.FeatureAgentRuns, v.Name)
	assert.Equal(t, "ascbyo.ai.feature.agent_runs", v.NameKey)
	assert.Equal(t, "ascbyo.ai.feature.agent_runs.hint", v.HintKey)
	assert.False(t, v.Own)
	assert.Equal(t, "instance", v.SetBy)
	assert.False(t, v.HasCap)

	t.Run("set with a key and a cap", func(t *testing.T) {
		got, fe, err := s.SetFeature(t.Context(), alice, "agent_runs", &ascbyo.FeatureInput{KeyID: k.GetId(), CapUSD: "200"}, false, ascbyo.NewOpID())
		require.NoError(t, err)
		require.Empty(t, fe)
		req := f.CallsTo("SetFeature")[0].Req.(*ascv1.SetAiFeatureRequest)
		assert.Equal(t, ascv1.AiFeature_AI_FEATURE_AGENT_RUNS, req.GetFeature())
		assert.Equal(t, uint64(200_000_000), req.GetMonthlyCapMicroUsd())
		assert.Equal(t, k.GetId(), req.GetKeyId())
		assert.False(t, req.GetClear())
		v := ascbyo.FeatureViewOf(alice, got)
		assert.True(t, v.Own)
		assert.Empty(t, v.SetBy)
		require.NotNil(t, v.Key)
		assert.True(t, v.KeyOwned)
		assert.Equal(t, "$200.00", v.Cap)
		assert.Equal(t, "200", v.CapInput)
	})

	t.Run("spend, meter and pause", func(t *testing.T) {
		f.SetFeatureFields(alice.Owner(), ascv1.AiFeature_AI_FEATURE_AGENT_RUNS, func(s *ascv1.AiFeatureSetting) {
			s.MonthSpentMicroUsd, s.Paused, s.ResumesAt = 200_000_000, true, 1_800_000_000
		})
		fs, err := s.ListFeatures(t.Context(), alice)
		require.NoError(t, err)
		v := ascbyo.FeatureViewOf(alice, fs[0])
		assert.Equal(t, 100, v.Percent)
		assert.Equal(t, "asc-meter-bad", v.MeterClass())
		assert.True(t, v.Paused)
		assert.EqualValues(t, 1_800_000_000, v.ResumesAt)
		assert.Equal(t, "$200.00", v.Spent)
		assert.Empty(t, v.PausedBy, "the scope's own cap paused it")
	})

	t.Run("form errors send nothing", func(t *testing.T) {
		before := len(f.CallsTo("SetFeature"))
		_, fe, err := s.SetFeature(t.Context(), alice, "agent_runs", &ascbyo.FeatureInput{KeyID: "not-a-key", CapUSD: "abc"}, false, ascbyo.NewOpID())
		require.NoError(t, err)
		assert.Equal(t, "ascbyo.ai.err.feature_key", fe["key_id"])
		assert.Equal(t, "ascbyo.ai.err.cap", fe["monthly_cap"])
		_, fe, err = s.SetFeature(t.Context(), alice, "agent_runs", &ascbyo.FeatureInput{KeyID: bobs.GetId(), CapUSD: ""}, false, ascbyo.NewOpID())
		require.NoError(t, err)
		assert.Equal(t, "ascbyo.ai.err.feature_key", fe["key_id"], "a key of another owner is not offered")
		assert.Len(t, f.CallsTo("SetFeature"), before)
	})

	t.Run("an unknown feature", func(t *testing.T) {
		_, _, err := s.SetFeature(t.Context(), alice, "mind_reading", &ascbyo.FeatureInput{}, false, ascbyo.NewOpID())
		assert.True(t, ascbyo.IsKind(err, ascbyo.FailNotFound))
	})

	t.Run("a setting from the owner above", func(t *testing.T) {
		f.SetFeatureFields(repo.Owner(), ascv1.AiFeature_AI_FEATURE_AGENT_RUNS, func(s *ascv1.AiFeatureSetting) {
			s.SetBy, s.KeyId, s.Key, s.MonthlyCapMicroUsd, s.MonthSpentMicroUsd = alice.Owner(), k.GetId(), k, 10_000_000, 8_500_000
		})
		fs, err := s.ListFeatures(t.Context(), repo)
		require.NoError(t, err)
		v := ascbyo.FeatureViewOf(repo, fs[0])
		assert.False(t, v.Own)
		assert.Equal(t, "alice", v.SetBy)
		require.NotNil(t, v.Key)
		assert.False(t, v.KeyOwned, "the key is the user's, not the repository's")
		assert.Equal(t, 85, v.Percent)
		assert.Equal(t, "asc-meter-warn", v.MeterClass())
		assert.Empty(t, v.Key.KeyHint, "a key of the owner above is only a reference")
		assert.Equal(t, "Team", v.Key.Label)
		assert.Empty(t, v.PausedBy)

		f.SetFeatureFields(repo.Owner(), ascv1.AiFeature_AI_FEATURE_AGENT_RUNS, func(s *ascv1.AiFeatureSetting) {
			s.MonthSpentMicroUsd, s.Paused, s.PausedBy = 10_000_000, true, alice.Owner()
		})
		fs, err = s.ListFeatures(t.Context(), repo)
		require.NoError(t, err)
		v = ascbyo.FeatureViewOf(repo, fs[0])
		assert.True(t, v.Paused)
		assert.Equal(t, "alice", v.PausedBy)
		v = ascbyo.FeatureViewOf(repo, &ascv1.AiFeatureSetting{
			Feature: ascv1.AiFeature_AI_FEATURE_AGENT_RUNS, Paused: true,
			PausedBy: &ascv1.OwnerRef{Kind: ascv1.OwnerKind_OWNER_KIND_INSTANCE},
		})
		assert.Equal(t, "instance", v.PausedBy)
	})

	t.Run("clear", func(t *testing.T) {
		got, fe, err := s.SetFeature(t.Context(), alice, "agent_runs", nil, true, ascbyo.NewOpID())
		require.NoError(t, err)
		require.Empty(t, fe)
		assert.True(t, f.CallsTo("SetFeature")[len(f.CallsTo("SetFeature"))-1].Req.(*ascv1.SetAiFeatureRequest).GetClear())
		assert.Equal(t, "instance", ascbyo.FeatureViewOf(alice, got).SetBy)
	})

	t.Run("features this hub does not know are dropped", func(t *testing.T) {
		f.ExtraFeatures = []*ascv1.AiFeatureSetting{
			{Feature: ascv1.AiFeature(99), SetBy: bob.Owner()},
			{Feature: ascv1.AiFeature_AI_FEATURE_AGENT_RUNS, SetBy: bob.Owner(), MonthlyCapMicroUsd: 1},
		}
		defer func() { f.ExtraFeatures = nil }()
		fs, err := s.ListFeatures(t.Context(), bob)
		require.NoError(t, err)
		require.Len(t, fs, 2, "the unknown one is dropped and the repeated one is kept once")
		assert.Zero(t, fs[0].GetMonthlyCapMicroUsd(), "the first answer for a feature is the one kept")
	})

	t.Run("asc failure", func(t *testing.T) {
		f.SetFail("/asc.v1.AiProviders/SetFeature", status.Error(codes.Internal, "boom"))
		defer f.SetFail("/asc.v1.AiProviders/SetFeature", nil)
		_, _, err := s.SetFeature(t.Context(), alice, "agent_runs", &ascbyo.FeatureInput{CapUSD: "1"}, false, ascbyo.NewOpID())
		require.Error(t, err)
	})
}

func TestAddedByOf(t *testing.T) {
	for in, want := range map[string]struct {
		id    int64
		login string
	}{
		"7:alice":                       {7, "alice"},
		"hub":                           {},
		"":                              {},
		"0:alice":                       {},
		"-1:alice":                      {},
		"7:":                            {},
		"x7:alice":                      {},
		"12345678901234567890:alice":    {},
		"7:al\x1bice":                   {},
		"9223372036854775807:max-login": {9223372036854775807, "max-login"},
		"9999999999999999999:alice":     {},
	} {
		id, login := ascbyo.AddedByOf(in)
		assert.Equal(t, want.id, id, "%q", in)
		assert.Equal(t, want.login, login, "%q", in)
	}
	v := ascbyo.KeyViewOf(&ascv1.AiKey{Id: "ak_abcdefghijklmnopqrst", CreatedBy: "42:maya"})
	assert.EqualValues(t, 42, v.AddedByID)
	assert.Equal(t, "maya", v.AddedBy)
}

func TestMeterClass(t *testing.T) {
	assert.Empty(t, ascbyo.MeterClass(0))
	assert.Empty(t, ascbyo.MeterClass(79))
	assert.Equal(t, "asc-meter-warn", ascbyo.MeterClass(80))
	assert.Equal(t, "asc-meter-warn", ascbyo.MeterClass(99))
	assert.Equal(t, "asc-meter-bad", ascbyo.MeterClass(100))
}

func TestFeatureForms(t *testing.T) {
	for _, f := range ascbyo.Features {
		w, ok := ascbyo.FeatureFromForm(string(f))
		require.True(t, ok, f)
		assert.Equal(t, f, ascbyo.FeatureToForm(w))
	}
	_, ok := ascbyo.FeatureFromForm("meaning_search")
	assert.False(t, ok, "a feature asc does not offer yet")
	assert.Empty(t, ascbyo.FeatureToForm(ascv1.AiFeature(99)))
}
