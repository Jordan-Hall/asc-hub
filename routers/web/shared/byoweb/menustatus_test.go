// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package byoweb

import (
	"encoding/json"
	"net/http"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	svc "forgejo.org/services/ascbyo"
	"forgejo.org/services/ascbyo/ascbyotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func menuStatusOf(t *testing.T, lvl *Level) (int, map[string]string) {
	t.Helper()
	r := run(t, MenuStatus, lvl, http.MethodGet, nil, "")
	out := map[string]string{}
	if r.resp.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(r.resp.Body.Bytes(), &out), r.body())
		assert.Equal(t, "no-store", r.resp.Header().Get("Cache-Control"))
	}
	return r.resp.Code, out
}

func TestMenuStatus(t *testing.T) {
	t.Run("the repository's own enabled bucket and its usable providers", func(t *testing.T) {
		f := setup(t)
		repo := repoLevel(t, true)
		f.AddTarget(repo.Scope.Owner(), "spare", ascv1.StorageTargetState_STORAGE_TARGET_STATE_DISABLED)
		f.AddTarget(repo.Scope.Owner(), "primary", ascv1.StorageTargetState_STORAGE_TARGET_STATE_ENABLED)
		f.AddKey(repo.Scope.Owner(), "a", ascv1.AiProvider_AI_PROVIDER_OPENAI, ascv1.AiKeyState_AI_KEY_STATE_ACTIVE)
		f.AddKey(repo.Scope.Owner(), "b", ascv1.AiProvider_AI_PROVIDER_OPENAI, ascv1.AiKeyState_AI_KEY_STATE_UNTESTED)
		f.AddKey(repo.Scope.Owner(), "c", ascv1.AiProvider_AI_PROVIDER_GOOGLE, ascv1.AiKeyState_AI_KEY_STATE_ACTIVE)
		f.AddKey(repo.Scope.Owner(), "d", ascv1.AiProvider_AI_PROVIDER_ANTHROPIC, ascv1.AiKeyState_AI_KEY_STATE_DISABLED)
		f.AddKey(repo.Scope.Owner(), "e", ascv1.AiProvider_AI_PROVIDER_ANTHROPIC, ascv1.AiKeyState_AI_KEY_STATE_FAILED)
		// another owner's bucket and keys do not count
		bob := mustScope(t, svc.KindUser, "bob").Owner()
		f.AddKey(bob, "x", ascv1.AiProvider_AI_PROVIDER_GOOGLE, ascv1.AiKeyState_AI_KEY_STATE_ACTIVE)

		code, out := menuStatusOf(t, repo)
		require.Equal(t, http.StatusOK, code)
		assert.Equal(t, map[string]string{"storage": "Your bucket", "ai": "2 providers"}, out)
	})

	t.Run("no bucket in use and no usable key", func(t *testing.T) {
		f := setup(t)
		repo := repoLevel(t, false)
		f.AddTarget(repo.Scope.Owner(), "pending", ascv1.StorageTargetState_STORAGE_TARGET_STATE_PENDING)
		f.AddKey(repo.Scope.Owner(), "off", ascv1.AiProvider_AI_PROVIDER_OPENAI, ascv1.AiKeyState_AI_KEY_STATE_DISABLED)
		code, out := menuStatusOf(t, repo)
		require.Equal(t, http.StatusOK, code)
		assert.Equal(t, map[string]string{"storage": "Hub storage", "ai": "No keys"}, out)

		f.AddKey(repo.Scope.Owner(), "on", ascv1.AiProvider_AI_PROVIDER_OPENAI, ascv1.AiKeyState_AI_KEY_STATE_ACTIVE)
		_, out = menuStatusOf(t, repo)
		assert.Equal(t, "1 provider", out["ai"])
	})

	t.Run("a part asc cannot answer is left out", func(t *testing.T) {
		f := setup(t)
		f.SetFail("/asc.v1.Storage/ListTargets", ascbyotest.Detail(14, "down", "UNAVAILABLE"))
		code, out := menuStatusOf(t, repoLevel(t, false))
		require.Equal(t, http.StatusOK, code)
		assert.NotContains(t, out, "storage")
		assert.Equal(t, "No keys", out["ai"])
	})

	t.Run("asc is not configured: an empty answer", func(t *testing.T) {
		t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
		t.Cleanup(test.MockVariableValue(&setting.Asc.ServerURL, ""))
		code, out := menuStatusOf(t, repoLevel(t, false))
		require.Equal(t, http.StatusOK, code)
		assert.Empty(t, out)
	})

	t.Run("[asc] off or no level: a 404, and asc is not asked", func(t *testing.T) {
		f := ascbyotest.Start(t)
		t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, false))
		code, _ := menuStatusOf(t, repoLevel(t, false))
		assert.Equal(t, http.StatusNotFound, code)
		assert.Empty(t, f.Calls())
		t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
		code, _ = menuStatusOf(t, nil)
		assert.Equal(t, http.StatusNotFound, code)
	})
}
