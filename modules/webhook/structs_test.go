// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package webhook

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestChooseAscEvents(t *testing.T) {
	t.Run("push only keeps push", func(t *testing.T) {
		e := &HookEvent{PushOnly: true}
		e.ChooseAscEvents([]string{"change.landed", "nope"})
		assert.Equal(t, HookEvent{ChooseEvents: true, HookEvents: HookEvents{Push: true, ChangeLanded: true}}, *e)
	})
	t.Run("everything keeps every other event", func(t *testing.T) {
		e := &HookEvent{SendEverything: true, BranchFilter: "main"}
		e.ChooseAscEvents([]string{"policy.changed"})
		assert.True(t, e.ChooseEvents)
		assert.False(t, e.SendEverything)
		assert.Equal(t, "main", e.BranchFilter)
		assert.True(t, e.Push && e.Issues && e.Release && e.ActionRunSuccess && e.PolicyChanged)
		for _, ev := range AscHookEvents {
			assert.Equal(t, ev == HookEventPolicyChanged, e.AscEvent(ev), ev)
		}
	})
	t.Run("chosen keeps the others and replaces the ASC ones", func(t *testing.T) {
		e := &HookEvent{ChooseEvents: true, HookEvents: HookEvents{Issues: true, ChangeOpened: true}}
		e.ChooseAscEvents([]string{"agent_run.finished", "release.published"})
		assert.Equal(t, HookEvent{ChooseEvents: true, HookEvents: HookEvents{Issues: true, AgentRunFinished: true, ReleasePublished: true}}, *e)
	})
}
