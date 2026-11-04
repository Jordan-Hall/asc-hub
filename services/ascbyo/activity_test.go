// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascbyo_test

import (
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/services/ascbyo"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKeyActivity(t *testing.T) {
	s, f := newService(t)
	alice := scope(t, ascbyo.KindUser, "alice")
	bob := scope(t, ascbyo.KindUser, "bob")
	k := f.AddKey(alice.Owner(), "Team", ascv1.AiProvider_AI_PROVIDER_ANTHROPIC, ascv1.AiKeyState_AI_KEY_STATE_ACTIVE)
	for range ascbyo.MaxKeyActivity + 5 {
		f.AddEvent(alice.Owner(), &ascv1.AuditEvent{Action: "ai.key.tested", Target: k.GetId(), Actor: &ascv1.PrincipalRef{Name: "alice"}})
	}
	f.AddEvent(alice.Owner(), &ascv1.AuditEvent{Action: "storage.target.created", Target: k.GetId(), Actor: &ascv1.PrincipalRef{Name: "alice"}})

	resp, err := s.KeyActivity(t.Context(), alice, k.GetId())
	require.NoError(t, err)
	assert.Len(t, resp.GetEvents(), ascbyo.MaxKeyActivity, "the newest events only")
	for _, e := range resp.GetEvents() {
		assert.Equal(t, "ai.key.tested", e.GetAction(), "only events about AI keys")
	}
	calls := f.CallsTo("List")
	require.Len(t, calls, 1)
	assert.LessOrEqual(t, len(calls[0].Req.(*ascv1.ListAuditRequest).GetFilter().GetActions()), 32, "asc takes at most 32 actions")

	// another owner's key reads nothing, and the log is not asked
	_, err = s.KeyActivity(t.Context(), bob, k.GetId())
	assert.True(t, ascbyo.IsKind(err, ascbyo.FailNotFound), "%v", err)
	_, err = s.KeyActivity(t.Context(), alice, "not-a-key")
	assert.True(t, ascbyo.IsKind(err, ascbyo.FailNotFound), "%v", err)
	assert.Len(t, f.CallsTo("List"), 1)

	t.Cleanup(ascbyo.UseAPI(ascbyo.API{AI: ascv1.NewAiProvidersClient(nil)}))
	s2, err := ascbyo.New()
	require.NoError(t, err)
	_, err = s2.KeyActivity(t.Context(), alice, k.GetId())
	assert.True(t, ascbyo.IsKind(err, ascbyo.FailUnimplemented), "%v", err)
}
