// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascbyo_test

import (
	"strconv"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/services/ascbyo"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const aChange = "kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkz"

func TestKeyUses(t *testing.T) {
	s, f := newService(t)
	alice := scope(t, ascbyo.KindUser, "alice")
	bob := scope(t, ascbyo.KindUser, "bob")
	k := f.AddKey(alice.Owner(), "Team", ascv1.AiProvider_AI_PROVIDER_ANTHROPIC, ascv1.AiKeyState_AI_KEY_STATE_ACTIVE)
	f.Uses = map[string][]*ascv1.AiActivity{k.GetId(): {
		{Kind: ascv1.AiActivityKind_AI_ACTIVITY_KIND_AGENT_RUN, Subject: "42", Actor: "carol", Repo: "acme/app", Agent: "fixer",
			Model: "claude-x", FirstAt: 1_790_000_000, LastAt: 1_790_000_600, Requests: 3, InputTokens: 300, OutputTokens: 120, CostMicroUsd: 4_500},
		{Kind: ascv1.AiActivityKind_AI_ACTIVITY_KIND_REVIEW_SUMMARY, Subject: aChange, Actor: "dave", Repo: "acme/app", Requests: 1},
		{Kind: ascv1.AiActivityKind_AI_ACTIVITY_KIND_KEY_TEST, Subject: "2026-11-09", Requests: 1},
		{Kind: ascv1.AiActivityKind_AI_ACTIVITY_KIND_AGENT_RUN, Subject: "../../admin", Repo: "not a repo"},
		{Kind: ascv1.AiActivityKind_AI_ACTIVITY_KIND_REVIEW_SUMMARY, Subject: "javascript:alert(1)"},
		{Kind: ascv1.AiActivityKind(99), Subject: "something new", Actor: "erin\x00‮", UnpricedRequests: 2},
	}}

	u, err := s.KeyUses(t.Context(), alice, k.GetId(), "")
	require.NoError(t, err)
	require.Len(t, u.Rows, 6)
	assert.Empty(t, u.Next)
	run := u.Rows[0]
	assert.Equal(t, ascv1.AiActivityKind_AI_ACTIVITY_KIND_AGENT_RUN, run.Kind)
	assert.Equal(t, "42", run.Subject)
	assert.Equal(t, "carol", run.Actor)
	assert.Equal(t, "acme/app", run.Repo)
	assert.Equal(t, int64(1_790_000_600), run.LastAt.Unix())
	assert.Equal(t, [4]uint64{3, 300, 120, 4_500}, [4]uint64{run.Requests, run.InputTokens, run.OutputTokens, run.CostMicro})
	assert.Equal(t, aChange, u.Rows[1].Subject)
	assert.Equal(t, "2026-11-09", u.Rows[2].Subject)
	// A subject that does not fit its kind, or a repository name that is not one, is dropped.
	assert.Empty(t, u.Rows[3].Subject)
	assert.Empty(t, u.Rows[3].Repo)
	assert.Empty(t, u.Rows[4].Subject)
	// A kind the hub does not know is shown as other calls, without its subject.
	assert.Equal(t, ascv1.AiActivityKind_AI_ACTIVITY_KIND_OTHER, u.Rows[5].Kind)
	assert.Empty(t, u.Rows[5].Subject)
	assert.Empty(t, u.Rows[5].Actor, "an actor that is not a handle is dropped")
	assert.Equal(t, uint64(2), u.Rows[5].Unpriced)

	lr := f.CallsTo("ListActivity")[0].Req.(*ascv1.ListAiActivityRequest)
	assert.Equal(t, uint32(ascbyo.KeyUsesPage), lr.GetPageSize())
	assert.Equal(t, "alice", lr.GetOwner().GetName())

	// Paging.
	var many []*ascv1.AiActivity
	for i := range ascbyo.KeyUsesPage + 7 {
		many = append(many, &ascv1.AiActivity{Kind: ascv1.AiActivityKind_AI_ACTIVITY_KIND_AGENT_RUN, Subject: strconv.Itoa(i + 1)})
	}
	f.Uses[k.GetId()] = many
	u, err = s.KeyUses(t.Context(), alice, k.GetId(), "")
	require.NoError(t, err)
	assert.Len(t, u.Rows, ascbyo.KeyUsesPage)
	assert.Equal(t, "o"+strconv.Itoa(ascbyo.KeyUsesPage), u.Next)
	u, err = s.KeyUses(t.Context(), alice, k.GetId(), u.Next)
	require.NoError(t, err)
	assert.Len(t, u.Rows, 7)
	assert.Empty(t, u.Next)

	// A next token the hub would not send back is dropped.
	f.NextTokenOverride = "o1&tab=limits"
	u, err = s.KeyUses(t.Context(), alice, k.GetId(), "")
	require.NoError(t, err)
	assert.Empty(t, u.Next)
	f.NextTokenOverride = ""

	// Bad tokens, another owner's key and a bad id never reach asc.
	calls := len(f.CallsTo("ListActivity"))
	for _, bad := range []string{"x", "o0", "o-1", "o1234567", "o1 ", "o01"} {
		_, err = s.KeyUses(t.Context(), alice, k.GetId(), bad)
		assert.True(t, ascbyo.IsKind(err, ascbyo.FailInvalid), "%q: %v", bad, err)
	}
	_, err = s.KeyUses(t.Context(), bob, k.GetId(), "")
	assert.True(t, ascbyo.IsKind(err, ascbyo.FailNotFound), "%v", err)
	_, err = s.KeyUses(t.Context(), alice, "not-a-key", "")
	assert.True(t, ascbyo.IsKind(err, ascbyo.FailNotFound), "%v", err)
	assert.Len(t, f.CallsTo("ListActivity"), calls)

	// An asc without the call.
	f.SetFail("/asc.v1.AiProviders/ListActivity", status.Error(codes.Unimplemented, "no ListActivity"))
	_, err = s.KeyUses(t.Context(), alice, k.GetId(), "")
	assert.True(t, ascbyo.IsKind(err, ascbyo.FailUnimplemented), "%v", err)
}
