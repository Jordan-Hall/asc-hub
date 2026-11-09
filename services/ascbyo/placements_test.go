// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascbyo_test

import (
	"fmt"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/services/ascbyo"
	"forgejo.org/services/ascbyo/ascbyotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRepoPlacements(t *testing.T) {
	s, f := newService(t)
	enabled := ascv1.StorageTargetState_STORAGE_TARGET_STATE_ENABLED
	f.Placements = map[string]*ascv1.RepoPlacement{
		"alice/widgets": {TargetId: "st_AAAAAAAAAAAAAAAAAAAA", TargetName: "garage-eu", TargetState: enabled},
		"alice/moving":  {TargetId: "st_BBBBBBBBBBBBBBBBBBBB", TargetName: "garage-us", TargetState: enabled, HasFallback: true, Migrating: true},
		"alice/split":   {HasFallback: true},
		"alice/off":     {TargetId: "st_CCCCCCCCCCCCCCCCCCCC", TargetName: "old", TargetState: ascv1.StorageTargetState_STORAGE_TARGET_STATE_DISABLED},
		"alice/gone":    {TargetId: "st_DDDDDDDDDDDDDDDDDDDD"},
		"alice/bad-id":  {TargetId: "not-a-target", TargetName: "evil"},
		"alice/default": {},
	}
	got, err := s.RepoPlacements(t.Context(), []string{
		"Alice/Widgets", "alice/moving", "alice/split", "alice/off", "alice/gone", "alice/bad-id", "alice/default",
		"alice/widgets", "alice/unknown", "not a repo", "",
	})
	require.NoError(t, err)

	w := got["alice/widgets"]
	assert.Equal(t, "garage-eu", w.TargetName, "keyed by the lower-case name")
	assert.False(t, w.Default())
	assert.False(t, w.Mismatch())

	m := got["alice/moving"]
	assert.True(t, m.Migrating)
	assert.False(t, m.Mismatch(), "a migration moving the objects is not a mismatch")

	assert.True(t, got["alice/split"].Default())
	assert.True(t, got["alice/split"].Split())
	assert.True(t, got["alice/split"].Mismatch(), "objects left behind with nothing moving them")
	assert.True(t, got["alice/off"].Off())
	assert.True(t, got["alice/off"].Mismatch())
	assert.True(t, got["alice/gone"].Gone())
	assert.True(t, got["alice/gone"].Mismatch())
	assert.True(t, got["alice/default"].Default())
	assert.False(t, got["alice/default"].Mismatch())
	assert.NotContains(t, got, "alice/bad-id", "a target id that is not one is dropped")
	assert.NotContains(t, got, "alice/unknown")

	calls := f.CallsTo("ListRepoPlacements")
	require.Len(t, calls, 1)
	req := calls[0].Req.(*ascv1.ListRepoPlacementsRequest)
	assert.Len(t, req.GetRepos(), 8, "duplicates and invalid names are not sent")
	assert.Equal(t, "Bearer "+ascbyotest.InternalToken, calls[0].Auth, "a system call")

	t.Run("more than a call holds goes in batches", func(t *testing.T) {
		names := make([]string, 0, 2*ascbyo.MaxPlacementsPerCall+1)
		for i := range cap(names) {
			names = append(names, fmt.Sprintf("bulk/r%d", i))
		}
		f.Placements["bulk/r200"] = &ascv1.RepoPlacement{TargetId: "st_EEEEEEEEEEEEEEEEEEEE", TargetName: "last", TargetState: enabled}
		before := len(f.CallsTo("ListRepoPlacements"))
		got, err := s.RepoPlacements(t.Context(), names)
		require.NoError(t, err)
		assert.Len(t, f.CallsTo("ListRepoPlacements"), before+3)
		assert.Equal(t, "last", got["bulk/r200"].TargetName, "the last batch is asked too")
	})

	t.Run("asc refusing is an error", func(t *testing.T) {
		f.SetFail("/asc.v1.Storage/ListRepoPlacements", status.Error(codes.PermissionDenied, "no"))
		_, err := s.RepoPlacements(t.Context(), []string{"alice/widgets"})
		require.Error(t, err)
	})

	t.Run("nothing to ask asks nothing", func(t *testing.T) {
		before := len(f.CallsTo("ListRepoPlacements"))
		got, err := s.RepoPlacements(t.Context(), []string{"no slash"})
		require.NoError(t, err)
		assert.Empty(t, got)
		assert.Len(t, f.CallsTo("ListRepoPlacements"), before)
	})
}
