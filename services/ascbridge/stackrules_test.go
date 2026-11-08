// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascbridge

import (
	"context"
	"errors"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStackRulesOfPolicy(t *testing.T) {
	on := &ascv1.GetPolicyResponse{Present: true, Stacks: &ascv1.PolicyStacks{RestackOnMainMove: true, CollaboratorsMayPush: true}}
	assert.Equal(t, StackRules{RestackOnMainMove: true, CollaboratorsMayPush: true}, StackRulesOfPolicy(true, on))

	// A policy that is absent, has an error or says nothing gives the defaults: both off.
	assert.Equal(t, StackRules{}, StackRulesOfPolicy(false, on))
	assert.Equal(t, StackRules{}, StackRulesOfPolicy(true, nil))
	broken := &ascv1.GetPolicyResponse{Present: true, Error: "bad toml", Stacks: on.Stacks}
	assert.Equal(t, StackRules{}, StackRulesOfPolicy(true, broken))
	assert.Equal(t, StackRules{}, StackRulesOfPolicy(true, &ascv1.GetPolicyResponse{Present: true}))

	one := &ascv1.GetPolicyResponse{Present: true, Stacks: &ascv1.PolicyStacks{CollaboratorsMayPush: true}}
	assert.Equal(t, StackRules{CollaboratorsMayPush: true}, StackRulesOfPolicy(true, one))
}

func TestUseStackRules(t *testing.T) {
	var asked []string
	restore := UseStackRules(func(_ context.Context, repo string) (StackRules, error) {
		asked = append(asked, repo)
		if repo == "acme/broken" {
			return StackRules{}, errors.New("unavailable")
		}
		return StackRules{RestackOnMainMove: true}, nil
	})
	r, err := ReadStackRules(t.Context(), "acme/nova")
	require.NoError(t, err)
	assert.True(t, r.RestackOnMainMove)
	_, err = ReadStackRules(t.Context(), "acme/broken")
	require.Error(t, err)
	assert.Equal(t, []string{"acme/nova", "acme/broken"}, asked)

	restore()
	stackRulesMu.Lock()
	defer stackRulesMu.Unlock()
	assert.Nil(t, stackRulesVal, "restore puts back the reader that was there")
}
