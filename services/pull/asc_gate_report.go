// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"context"
	"time"

	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
)

// The rules of the gate the hub applies to a change before it lands (checkAscStackItem), in the order they are looked at.
// The names are what the policy page shows ("N held of M" per rule).
const (
	ascGateRuleOpen        = "change is open"
	ascGateRuleReady       = "not a draft"
	ascGateRuleBranch      = "same target branch as the stack"
	ascGateRuleMayMerge    = "person may merge"
	ascGateRuleProtections = "approvals, checks and branch protections"
	ascGateRuleHead        = "head has not moved"
)

// AscGateRule is one rule the gate looked at and whether it held the change.
type AscGateRule struct {
	Rule string
	Held bool
}

// ascGateReporter tells asc what the gate decided (Review.RecordGateDecision). It is nil until InitAscStack sets it, and a
// test may replace it. It never changes a decision: a failure to report is logged and the gate stands as it was.
var ascGateReporter func(ctx context.Context, repo, change string, rules []AscGateRule)

// UseAscGateReporter sets the reporter until the returned function is called. It is for tests.
func UseAscGateReporter(f func(ctx context.Context, repo, change string, rules []AscGateRule)) (restore func()) {
	prev := ascGateReporter
	ascGateReporter = f
	return func() { ascGateReporter = prev }
}

// ascGateRun collects what the gate decides for one change.
type ascGateRun struct {
	item  *AscStackItem
	repo  string
	rules []AscGateRule
}

func (g *ascGateRun) pass(rule string) { g.rules = append(g.rules, AscGateRule{Rule: rule}) }

// hold records the rule that held the change, reports, and returns err for the caller to return.
func (g *ascGateRun) hold(ctx context.Context, rule string, err error) error {
	g.rules = append(g.rules, AscGateRule{Rule: rule, Held: true})
	g.report(ctx)
	return err
}

func (g *ascGateRun) report(ctx context.Context) {
	if ascGateReporter == nil || g.item == nil || g.item.Change == "" || g.repo == "" {
		return
	}
	ascGateReporter(ctx, g.repo, g.item.Change, g.rules)
}

const ascGateReportTimeout = 5 * time.Second

// installAscGateReporter makes the reporter that calls asc with the hub's own credential: the hub is the host that
// evaluates, and Forgejo has already authorised whoever asked for the check. The answer is not used for anything.
func installAscGateReporter() {
	if setting.InternalToken == "" {
		return
	}
	c, err := ascclient.New(ascclient.Config{Target: setting.Asc.ServerURL, InternalToken: setting.InternalToken, CallTimeout: ascGateReportTimeout})
	if err != nil {
		log.Error("asc gate reporter: %v", err)
		return
	}
	api := ascv1.NewReviewClient(c)
	ascGateReporter = func(ctx context.Context, repo, change string, rules []AscGateRule) {
		raw := ascChangeRaw(change)
		if raw == nil || len(rules) == 0 {
			return
		}
		req := &ascv1.RecordGateDecisionRequest{Repo: &ascv1.RepoRef{Name: repo}, Change: raw}
		for _, r := range rules {
			if len(req.Rules) >= 64 {
				break
			}
			req.Rules = append(req.Rules, &ascv1.GateRuleResult{Rule: r.Rule, Held: r.Held})
		}
		cctx, cancel := context.WithTimeout(ascclient.WithSystem(context.WithoutCancel(ctx)), ascGateReportTimeout)
		defer cancel()
		if _, err := api.RecordGateDecision(cctx, req); err != nil {
			log.Warn("asc gate: RecordGateDecision failed: %v", err)
		}
	}
}

// ascChangeRaw turns the 32 letters k-z of a change id into the 16 bytes asc uses. It returns nil for anything else.
func ascChangeRaw(text string) []byte {
	if len(text) != 32 {
		return nil
	}
	raw := make([]byte, 16)
	for i := range raw {
		hi, lo := text[2*i], text[2*i+1]
		if hi < 'k' || hi > 'z' || lo < 'k' || lo > 'z' {
			return nil
		}
		raw[i] = ('z'-hi)<<4 | ('z' - lo)
	}
	return raw
}
