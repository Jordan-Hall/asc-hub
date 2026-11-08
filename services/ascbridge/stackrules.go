// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascbridge

import (
	"context"
	"errors"
	"sync"

	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
)

// StackRules are the `[stacks]` switches of a repository policy that the hub acts on (docs/BUILDOUT.md section 25).
// asc records and returns them; it does not restack on its own, because a restack rewrites a person's changes.
type StackRules struct {
	// RestackOnMainMove: the hub restacks the open stacks when the default branch moves.
	RestackOnMainMove bool
	// CollaboratorsMayPush: people with write access who did not start a stack may update it.
	CollaboratorsMayPush bool
}

// StackRulesReader reads the stack rules of a repository by its asc name.
type StackRulesReader func(ctx context.Context, repo string) (StackRules, error)

var (
	stackRulesMu  sync.Mutex
	stackRulesVal StackRulesReader
)

// newStackRules builds the reader on the internal token. A policy that is missing or that asc refused gives the
// defaults (both off): a broken policy never opens a stack to more people or starts restacks.
var newStackRules = func() (StackRulesReader, error) {
	if setting.InternalToken == "" {
		return nil, errors.New("ascbridge: the internal token is not set")
	}
	c, err := ascclient.New(ascclient.Config{Target: setting.Asc.ServerURL, InternalToken: setting.InternalToken, CallTimeout: StackCallTimeout})
	if err != nil {
		return nil, err
	}
	policy := ascv1.NewPolicyClient(c)
	return func(ctx context.Context, repo string) (StackRules, error) {
		cctx, cancel := context.WithTimeout(ascclient.WithSystem(ctx), StackCallTimeout)
		defer cancel()
		src, err := policy.GetSource(cctx, &ascv1.GetPolicySourceRequest{Repo: &ascv1.RepoRef{Name: repo}})
		if err != nil {
			return StackRules{}, mapStackError("policy", err)
		}
		return StackRulesOfPolicy(src.GetPresent(), src.GetParsed()), nil
	}, nil
}

// StackRulesOfPolicy reads the switches from what asc parsed. A policy that is absent or has an error gives the
// defaults.
func StackRulesOfPolicy(present bool, parsed *ascv1.GetPolicyResponse) StackRules {
	if !present || parsed == nil || parsed.GetError() != "" {
		return StackRules{}
	}
	s := parsed.GetStacks()
	return StackRules{RestackOnMainMove: s.GetRestackOnMainMove(), CollaboratorsMayPush: s.GetCollaboratorsMayPush()}
}

// UseStackRules replaces the reader until the returned function is called. It is for tests.
func UseStackRules(r StackRulesReader) (restore func()) {
	stackRulesMu.Lock()
	defer stackRulesMu.Unlock()
	prev := stackRulesVal
	stackRulesVal = r
	return func() {
		stackRulesMu.Lock()
		defer stackRulesMu.Unlock()
		stackRulesVal = prev
	}
}

// ReadStackRules returns the stack rules of repo, creating the reader on first use.
func ReadStackRules(ctx context.Context, repo string) (StackRules, error) {
	stackRulesMu.Lock()
	if stackRulesVal == nil {
		r, err := newStackRules()
		if err != nil {
			stackRulesMu.Unlock()
			return StackRules{}, err
		}
		stackRulesVal = r
	}
	r := stackRulesVal
	stackRulesMu.Unlock()
	return r(ctx, repo)
}
