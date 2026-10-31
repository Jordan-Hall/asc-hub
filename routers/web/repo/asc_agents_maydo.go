// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"net/http"
	"strings"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/log"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/context"
)

// The card "What this run may do" of the new-task page: Policy.Evaluate for the agent, with the write action for a sample
// path of each glob of the scope (the scope is a pattern, asc judges paths) and the run action for the limits of the form.
// It updates as the scope narrows (GET /asc/agents/may-do). The server enforces all of it again on every update; the card
// only shows what asc would say, and asc being silent leaves the card out.

const (
	// ascMayDoMaxPaths is how many sample paths are asked about (the globs of a form are at most 16).
	ascMayDoMaxPaths = 16
	ascMayDoReason   = 200
)

// ascAgentMayWrite is one sample path and what the policy says of writing it.
type ascAgentMayWrite struct {
	Glob    string `json:"glob"`
	Path    string `json:"path"`
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"`
}

// ascAgentMayDo is the card.
type ascAgentMayDo struct {
	Known      bool               `json:"known"`
	RunAllowed bool               `json:"run_allowed"`
	RunReason  string             `json:"run_reason,omitempty"`
	Writes     []ascAgentMayWrite `json:"writes"`
}

// ascAgentSamplePath makes a path that the glob matches, so asc can judge the write: "src/**" gives "src/file", "*.md"
// gives "file.md". A glob with a class or an alternative ([a-z], {a,b}) gives "" and is not sampled.
func ascAgentSamplePath(glob string) string {
	if strings.ContainsAny(glob, "[]{}") {
		return ""
	}
	segs := strings.Split(glob, "/")
	for i, s := range segs {
		switch {
		case s == "**" && i == len(segs)-1:
			segs[i] = "file"
		case s == "**":
			segs[i] = "dir"
		default:
			s = strings.ReplaceAll(s, "**", "file")
			s = strings.ReplaceAll(s, "*", "file")
			segs[i] = strings.ReplaceAll(s, "?", "a")
		}
	}
	p := strings.Join(segs, "/")
	if p == "" || strings.HasSuffix(p, "/") || !ascagentui.ValidGlob(p) {
		return ""
	}
	return p
}

// ascAgentMayDoOf asks asc what the agent may do. scope is the globs of the form (the default scope of the policy when
// empty); changes and paths are the limits of the form (0: none given). A failed call, or one asc answers with something
// that does not match the question, gives a card that is not Known.
func ascAgentMayDoOf(callCtx gocontext.Context, ref *ascv1.RepoRef, agent string, scope []string, changes, paths uint32) ascAgentMayDo {
	out := ascAgentMayDo{Writes: []ascAgentMayWrite{}}
	if !ascagentui.ValidAgentName(agent) {
		return out
	}
	conn, err := ascRPCConn()
	if err != nil {
		return out
	}
	api := ascv1.NewPolicyClient(conn)
	principal := &ascv1.PrincipalRef{Kind: "agent", Name: agent}

	var samples [][]byte
	var globText []string
	for _, g := range scope {
		if p := ascAgentSamplePath(g); p != "" && len(samples) < ascMayDoMaxPaths {
			samples, globText = append(samples, []byte(p)), append(globText, g)
		}
	}
	if len(samples) > 0 {
		resp, err := api.Evaluate(callCtx, &ascv1.EvaluatePolicyRequest{Repo: ref, Principal: principal, Action: ascv1.PolicyAction_POLICY_ACTION_WRITE, Paths: samples})
		if err != nil || len(resp.GetDecisions()) != len(samples) {
			if err != nil {
				log.Warn("asc agents: may-do write: %s", ascagentuiLogLine(ascagentui.Classify(err)))
			}
			return out
		}
		for i, d := range resp.GetDecisions() {
			if string(d.GetPath()) != string(samples[i]) {
				return out
			}
			out.Writes = append(out.Writes, ascAgentMayWrite{
				Glob: ascagentui.CleanLine(globText[i], ascagentui.MaxGlobBytes), Path: ascagentui.CleanLine(string(samples[i]), ascagentui.MaxGlobBytes),
				Allowed: d.GetAllowed(), Reason: ascagentui.CleanLine(d.GetReason(), ascMayDoReason),
			})
		}
	}
	resp, err := api.Evaluate(callCtx, &ascv1.EvaluatePolicyRequest{Repo: ref, Principal: principal, Action: ascv1.PolicyAction_POLICY_ACTION_RUN, RunChanges: changes, RunPaths: paths})
	if err != nil {
		log.Warn("asc agents: may-do run: %s", ascagentuiLogLine(ascagentui.Classify(err)))
		out.Writes = []ascAgentMayWrite{}
		return out
	}
	out.Known = true
	out.RunAllowed = resp.GetRunAllowed()
	out.RunReason = ascagentui.CleanLine(resp.GetRunReason(), ascMayDoReason)
	return out
}

// ascAgentMayDoScope is the scope the card judges: the globs typed, else the default scope of the policy. A malformed
// field gives nothing to judge.
func ascAgentMayDoScope(raw string, policy ascAgentPolicy) []string {
	globs, ok := ascagentui.ParseGlobs(raw)
	if !ok {
		return nil
	}
	if len(globs) == 0 {
		return policy.Scope
	}
	return globs
}

// AscAgentsMayDo serves `GET /{owner}/{repo}/asc/agents/may-do?agent=&paths=&max_changes=&max_paths=` for the form's script.
func AscAgentsMayDo(ctx *context.Context) {
	env, ok := ascAgentsGate(ctx, ascAgentsOpts{level: ascAgentsWrite, notArchived: true})
	if !ok {
		return
	}
	agent := ctx.FormTrim("agent")
	if agent == "" {
		agent = ascAgentsDefaultAgent
	}
	changes, okC := ascagentui.ParseCount(ctx.FormTrim(ascagentui.BudgetFields.Changes), 1<<20)
	paths, okP := ascagentui.ParseCount(ctx.FormTrim(ascagentui.BudgetFields.Paths), 1<<20)
	raw := ctx.FormString("paths")
	if !ascagentui.ValidAgentName(agent) || !okC || !okP {
		ctx.JSON(http.StatusBadRequest, map[string]string{"error": "bad query"})
		return
	}
	ascAgentsNoStore(ctx)
	callCtx, cancel := ascAgentsReadCtx(ctx)
	defer cancel()
	var policy ascAgentPolicy
	if strings.TrimSpace(raw) == "" {
		policy = ascAgentPolicyOf(callCtx, env.ref())
	}
	ctx.JSON(http.StatusOK, ascAgentMayDoOf(callCtx, env.ref(), agent, ascAgentMayDoScope(raw, policy), uint32(changes), uint32(paths)))
}
