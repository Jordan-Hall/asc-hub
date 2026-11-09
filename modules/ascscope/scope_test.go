// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascscope

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestScopeWordsAndList(t *testing.T) {
	assert.Equal(t, "checks", ScopeChecks.Word())
	assert.Equal(t, "agents", ScopeAgents.Word())
	assert.Equal(t, "releases", ScopeReleases.Word())
	assert.Equal(t, []Scope{ScopeChecks, ScopeAgents, ScopeReleases}, AllScopes.List(), "page order")
	assert.Equal(t, []Scope{ScopeChecks, ScopeReleases}, DefaultScope.List())
	assert.Empty(t, Scope(0).List())
}

func TestScopeHas(t *testing.T) {
	assert.True(t, DefaultScope.Has(ScopeChecks))
	assert.True(t, DefaultScope.Has(ScopeReleases))
	assert.False(t, DefaultScope.Has(ScopeAgents), "agents receive nothing until someone ticks Agent runs")
	assert.True(t, AllScopes.Has(DefaultScope))
	assert.False(t, AllScopes.Has(0), "no run has scope 0")
}

func TestScopeOfWordsAndValid(t *testing.T) {
	assert.Equal(t, ScopeAgents|ScopeReleases, ScopeOfWords([]string{"releases", "agents", "agents", "unknown"}))
	assert.Equal(t, Scope(0), ScopeOfWords(nil))
	assert.True(t, ScopeAgents.Valid())
	assert.True(t, AllScopes.Valid())
	assert.False(t, Scope(0).Valid())
	assert.False(t, Scope(8).Valid(), "an unknown bit")
	assert.False(t, (AllScopes | 16).Valid())
}

func TestOfForm(t *testing.T) {
	s, ok := OfForm(nil, false)
	assert.True(t, ok)
	assert.Equal(t, Scope(0), s, "a form without the boxes keeps the scopes")
	s, ok = OfForm([]string{"checks", "agents"}, true)
	assert.True(t, ok)
	assert.Equal(t, ScopeChecks|ScopeAgents, s)
	_, ok = OfForm(nil, true)
	assert.False(t, ok, "the boxes with none ticked")
	_, ok = OfForm([]string{"nonsense"}, true)
	assert.False(t, ok)
}
