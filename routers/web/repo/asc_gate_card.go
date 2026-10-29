// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"slices"
	"strings"

	"forgejo.org/models/db"
	git_model "forgejo.org/models/git"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unit"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/log"
	api "forgejo.org/modules/structs"
	"forgejo.org/services/context"
	pull_service "forgejo.org/services/pull"
)

// The policy gate card of the review page (board Review): what the policy asks of a change before it can land, and which of
// those asks are met now. The hub evaluates what it knows (the approvals asc recorded against the approvals the policy
// needs, the required checks against the commit statuses of the head, the order of the stack) and reports the result to
// asc with Review.RecordGateDecision, which keeps the history that the gate statistics are made of. What only asc knows
// (signing, secret scan) is evaluated when the change lands and is not claimed here.

// AscGateRule is one line of the card. Kind is one of approvals, checks, order and threads; the template words it.
type AscGateRule struct {
	Kind    string
	Held    bool // the rule holds the change back
	Pending bool // held only because a check is still running
	Have    uint32
	Need    uint32
	Names   []string // the checks that are not green yet (checks)
}

// ascGateNames are the names the rules are reported to asc under.
var ascGateNames = map[string]string{"approvals": "Approvals", "checks": "Required checks", "order": "Stack order", "threads": "Open threads"}

// ascGateRules evaluates the rules from the land requirements, the approvals that apply to the newest version, the states of
// the commit statuses of the head (by context) and whether a parent change has not landed. A nil land (no policy, or the
// policy cannot be read) yields only the rules that need no policy.
func ascGateRules(land *ascv1.PolicyLandRequirements, approvals uint32, statuses map[string]api.CommitStatusState, parentWaits bool, openThreads uint32) []AscGateRule {
	var rules []AscGateRule
	if need := land.GetApprovals(); need > 0 {
		rules = append(rules, AscGateRule{Kind: "approvals", Held: approvals < need, Have: approvals, Need: need})
	}
	if req := land.GetChecks(); len(req) > 0 {
		r := AscGateRule{Kind: "checks", Need: uint32(len(req))}
		pending := false
		for _, name := range req {
			switch st, ok := statuses[name]; {
			case ok && st.IsSuccess():
				r.Have++
			case !ok || st.IsPending():
				pending = true
				r.Names = append(r.Names, name)
			default:
				r.Names = append(r.Names, name)
			}
		}
		slices.Sort(r.Names)
		r.Held = r.Have < r.Need
		r.Pending = r.Held && pending && !anyFailed(req, statuses)
		rules = append(rules, r)
	}
	rules = append(rules, AscGateRule{Kind: "order", Held: parentWaits})
	if openThreads > 0 {
		rules = append(rules, AscGateRule{Kind: "threads", Held: false, Have: openThreads})
	}
	return rules
}

func anyFailed(required []string, statuses map[string]api.CommitStatusState) bool {
	for _, name := range required {
		if st, ok := statuses[name]; ok && !st.IsSuccess() && !st.IsPending() {
			return true
		}
	}
	return false
}

// ascGateToClear counts the rules that hold the change.
func ascGateToClear(rules []AscGateRule) int {
	n := 0
	for _, r := range rules {
		if r.Held {
			n++
		}
	}
	return n
}

// ascGateReport turns the rules into the request of RecordGateDecision.
func ascGateReport(rules []AscGateRule) []*ascv1.GateRuleResult {
	out := make([]*ascv1.GateRuleResult, 0, len(rules))
	for _, r := range rules {
		if r.Kind == "threads" {
			continue // advice, not a rule of the policy
		}
		out = append(out, &ascv1.GateRuleResult{Rule: ascGateNames[r.Kind], Held: r.Held})
	}
	return out
}

// prepareAscGate evaluates the gate of a stacked change, puts the card into ctx.Data["AscGate"] and, when the viewer may
// write, reports the evaluation to asc. It never fails the page.
func prepareAscGate(ctx *context.Context, pull *issues_model.PullRequest, brief *AscBrief, name string, change []byte) {
	conn, err := ascRPCConn()
	if err != nil {
		return
	}
	callCtx, cancel := gocontext.WithTimeout(ascclient.WithSystem(ctx), ascBriefTimeout)
	defer cancel()
	repoRef := &ascv1.RepoRef{Name: name}
	var land *ascv1.PolicyLandRequirements
	if resp, err := ascv1.NewPolicyClient(conn).Evaluate(callCtx, &ascv1.EvaluatePolicyRequest{Repo: repoRef, Action: ascv1.PolicyAction_POLICY_ACTION_LAND}); err == nil {
		land = resp.GetLand()
	}
	statuses := map[string]api.CommitStatusState{}
	if ctx.Repo.GitRepo != nil {
		if sha, err := ctx.Repo.GitRepo.GetRefCommitID(pull.GetGitRefName()); err == nil {
			if list, _, err := git_model.GetLatestCommitStatus(ctx, pull.BaseRepoID, sha, db.ListOptionsAll); err == nil {
				for _, s := range list {
					statuses[strings.TrimSpace(s.Context)] = s.State
				}
			}
		}
	}
	blocking, err := pull_service.AscStackParentBlocking(ctx, pull)
	if err != nil {
		log.Warn("asc gate: parent: %v", err)
	}
	rules := ascGateRules(land, brief.ApprovalsCurrent, statuses, blocking != nil, brief.OpenThreads)
	ctx.Data["AscGate"] = rules
	ctx.Data["AscGateToClear"] = ascGateToClear(rules)
	ctx.Data["AscGateRules"] = land != nil
	if ctx.IsSigned && ctx.Repo.CanWrite(unit.TypeCode) {
		if _, err := ascv1.NewReviewClient(conn).RecordGateDecision(callCtx, &ascv1.RecordGateDecisionRequest{Repo: repoRef, Change: change, Rules: ascGateReport(rules)}); err != nil {
			log.Warn("asc gate: report: %v", err)
		}
	}
}
