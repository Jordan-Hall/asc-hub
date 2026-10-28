// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"time"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/log"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/context"
)

// Agent tokens of a repository (the credential of an agent that runs outside the server: a developer's own agent,
// CI). Repository admins only.
//
//	GET  /{owner}/{repo}/asc/agents/tokens                    the tokens, their scope, expiry and budget use
//	POST /{owner}/{repo}/asc/agents/tokens                    make one
//	POST /{owner}/{repo}/asc/agents/tokens/{id}/revoke        revoke one
//	POST /{owner}/{repo}/asc/agents/tokens/{id}/budget        set its budget
//
// A token always expires (an hour by default, 90 days at most), is scoped to path globs that the repository policy
// admits, may make a limited number of changes per run, and carries budgets (changes, tokens, cost) that asc
// enforces on every update and every report of use. The secret is shown once, in the answer to the POST that made
// it, with `Cache-Control: no-store`; asc keeps only its SHA-256 and the page keeps nothing. The secret is never
// logged, never put in a flash message or a redirect, and the list shows only the last four characters.

const (
	ascAgentsTokenMaxList = 50
	// ascAgentsTokenMaxChanges bounds the changes per run a form may ask for.
	ascAgentsTokenMaxChanges = 10_000
)

// ascAgentTokenTTLs are the lifetimes the form offers (seconds). asc accepts 1 second to 90 days; the form keeps
// to a short list so a typo cannot make a token that outlives its purpose.
var ascAgentTokenTTLs = []struct {
	Seconds uint32
	Label   string
}{
	{3600, "1 hour"},
	{86400, "1 day"},
	{7 * 86400, "7 days"},
	{30 * 86400, "30 days"},
	{90 * 86400, "90 days"},
}

var (
	ascAgentSecretRe = regexp.MustCompile(`^ascat_[A-Za-z0-9_-]{43}$`)
	ascAgentHintRe   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,8}$`)
)

// ascAgentTokenRow is one token as drawn.
type ascAgentTokenRow struct {
	ID         string
	Name       string
	Agent      string
	Paths      []string
	MaxChanges uint32
	State      ascagentui.State
	CreatedBy  string
	Created    time.Time
	Expires    time.Time
	Revoked    time.Time
	LastUsed   time.Time
	Hint       string
	Meters     []ascagentui.Meter
	CanRevoke  bool
	Initials   string
}

func ascAgentTokenOf(t *ascv1.AgentToken, name string) (ascAgentTokenRow, bool) {
	if t == nil || !ascagentui.ValidTokenID(t.GetId()) || t.GetRepo() != name {
		return ascAgentTokenRow{}, false
	}
	row := ascAgentTokenRow{
		ID:         t.GetId(),
		Name:       ascagentui.CleanLine(t.GetName(), ascagentui.MaxTokenName),
		Agent:      ascagentui.SafeName(t.GetAgent()),
		Initials:   ascAgentInitials(ascagentui.SafeName(t.GetAgent())),
		MaxChanges: t.GetMaxChangesPerRun(),
		State:      ascagentui.TokenState(t.GetState()),
		CreatedBy:  ascagentui.SafeName(t.GetCreatedBy()),
		Created:    ascagentui.Time(t.GetCreated()),
		Expires:    ascagentui.Time(t.GetExpiresAt()),
		Revoked:    ascagentui.Time(t.GetRevokedAt()),
		LastUsed:   ascagentui.Time(t.GetLastUsed()),
		Meters:     ascagentui.Meters(t.GetLimits(), t.GetUsage()),
	}
	for i, p := range t.GetPaths() {
		if i >= 32 {
			break
		}
		if c := ascagentui.CleanLine(p, ascagentui.MaxGlobBytes); c != "" {
			row.Paths = append(row.Paths, c)
		}
	}
	if h := t.GetSecretHint(); ascAgentHintRe.MatchString(h) {
		row.Hint = h
	}
	// Meters that have no limit and no use are noise on a list.
	kept := row.Meters[:0]
	for _, m := range row.Meters {
		if !m.Idle {
			kept = append(kept, m)
		}
	}
	row.Meters = kept
	row.CanRevoke = t.GetState() == ascv1.AgentTokenState_AGENT_TOKEN_STATE_ACTIVE
	return row, true
}

// ascAgentTokenForm is the make-a-token form as typed.
type ascAgentTokenForm struct {
	Name       string
	Agent      string
	Paths      string
	MaxChanges string
	TTL        uint32
	Budget     ascagentui.BudgetInput
	OpID       string
	Errors     map[string]string
	Summary    string
}

// ascAgentsTokensPage draws the page. newSecret is non-empty only for the answer to the POST that made a token.
func ascAgentsTokensPage(ctx *context.Context, env *ascAgentsEnv, form *ascAgentTokenForm, status int, made *ascAgentTokenRow, secret string) {
	ctx.Data["Title"] = "Agent tokens"
	ascAgentsNav(ctx, "tokens")
	ctx.Data["AscAgentTokenForm"] = form
	ctx.Data["AscBudgetFields"] = ascagentui.BudgetFields
	ctx.Data["AscAgentTokenTTLs"] = ascAgentTokenTTLs
	ascAgentsNoStore(ctx)
	if secret != "" && made != nil {
		ctx.Data["AscAgentTokenMade"] = made
		ctx.Data["AscAgentTokenSecret"] = secret
	}

	conn, err := ascRPCConn()
	if err != nil {
		log.Error("asc agents: no connection to asc serve: %v", err)
		ascAgentsFail(ctx, tplAscAgentsTokens, "connection", err)
		return
	}
	callCtx, done, ok := ascAgentsWriteCtx(ctx, "")
	if !ok {
		return
	}
	defer done()
	resp, err := ascv1.NewAgentTokensClient(conn).ListTokens(callCtx, &ascv1.ListAgentTokensRequest{Repo: env.ref(), Limit: ascAgentsTokenMaxList})
	if err != nil {
		ascAgentsFail(ctx, tplAscAgentsTokens, "ListTokens", err)
		return
	}
	rows := make([]ascAgentTokenRow, 0, len(resp.GetTokens()))
	for _, t := range resp.GetTokens() {
		row, ok := ascAgentTokenOf(t, env.name)
		if !ok {
			log.Error("asc agents: ListTokens answered a token that is not of this repository")
			ascAgentsFail(ctx, tplAscAgentsTokens, "ListTokens", errAscAgentsBadAnswer)
			return
		}
		rows = append(rows, row)
	}
	// Active tokens first, newest first within a state.
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].CanRevoke != rows[j].CanRevoke {
			return rows[i].CanRevoke
		}
		return rows[i].Created.After(rows[j].Created)
	})
	ctx.Data["AscAgentTokens"] = rows
	readCtx, cancelRead := ascAgentsReadCtx(ctx)
	defer cancelRead()
	ctx.Data["AscAgentPolicy"] = ascAgentPolicyOf(readCtx, env.ref())
	ctx.HTML(status, tplAscAgentsTokens)
}

// AscAgentsTokens serves `GET /{owner}/{repo}/asc/agents/tokens`.
func AscAgentsTokens(ctx *context.Context) {
	env, ok := ascAgentsGate(ctx, ascAgentsOpts{level: ascAgentsAdmin})
	if !ok {
		return
	}
	form := &ascAgentTokenForm{TTL: ascAgentTokenTTLs[0].Seconds, OpID: ascAgentsNewOpID(), Agent: ascAgentsDefaultAgent}
	ascAgentsTokensPage(ctx, env, form, http.StatusOK, nil, "")
}

// AscAgentsTokenCreate serves `POST /{owner}/{repo}/asc/agents/tokens`.
func AscAgentsTokenCreate(ctx *context.Context) {
	env, ok := ascAgentsGate(ctx, ascAgentsOpts{level: ascAgentsAdmin, notArchived: true})
	if !ok {
		return
	}
	ascAgentsLimitBody(ctx)
	ttl, _ := strconv.ParseUint(ctx.FormTrim("ttl"), 10, 32)
	form := &ascAgentTokenForm{
		Name:       ctx.FormString("name"),
		Agent:      ctx.FormTrim("agent"),
		Paths:      ctx.FormString("paths"),
		MaxChanges: ctx.FormTrim("max_changes_per_run"),
		TTL:        uint32(ttl),
		OpID:       ctx.FormTrim("op"),
		Errors:     map[string]string{},
		Budget: ascagentui.BudgetInput{
			Changes: ctx.FormTrim(ascagentui.BudgetFields.Changes),
			Tokens:  ctx.FormTrim(ascagentui.BudgetFields.Tokens),
			CostUSD: ctx.FormTrim(ascagentui.BudgetFields.CostUSD),
			Paths:   ctx.FormTrim(ascagentui.BudgetFields.Paths),
			Minutes: ctx.FormTrim(ascagentui.BudgetFields.Minutes),
		},
	}
	if form.OpID == "" {
		form.OpID = ascAgentsNewOpID()
	}
	req := &ascv1.CreateAgentTokenRequest{Repo: env.ref(), Agent: form.Agent}
	if n, ok := ascagentui.CheckLine(form.Name, ascagentui.MaxTokenName, false); ok {
		req.Name = n
	} else {
		form.Errors["name"] = "Name the token in 1 to 64 characters on one line."
	}
	if !ascagentui.ValidAgentName(form.Agent) {
		form.Errors["agent"] = "An agent name is 1 to 40 characters: a to z, 0 to 9 and hyphens."
	}
	if globs, ok := ascagentui.ParseGlobs(form.Paths); ok {
		req.Paths = globs
	} else {
		form.Errors["paths"] = "Give up to 16 relative path patterns, one per line, with no \"..\"."
	}
	if n, ok := ascagentui.ParseCount(form.MaxChanges, ascAgentsTokenMaxChanges); ok {
		req.MaxChangesPerRun = uint32(n)
	} else {
		form.Errors["max_changes_per_run"] = "Enter a whole number, or leave it empty for the policy default."
	}
	ttlOK := false
	for _, t := range ascAgentTokenTTLs {
		if t.Seconds == form.TTL {
			ttlOK = true
		}
	}
	if ttlOK {
		req.TtlSeconds = form.TTL
	} else {
		form.Errors["ttl"] = "Choose how long the token lasts."
		form.TTL = ascAgentTokenTTLs[0].Seconds
	}
	if limits, err := ascagentui.ParseBudget(form.Budget); err == nil {
		req.Limits = limits
	} else if be, ok := err.(*ascagentui.BudgetError); ok {
		form.Errors[be.Field] = "Enter a whole number, or leave it empty for the policy default."
		if be.Field == ascagentui.BudgetFields.CostUSD {
			form.Errors[be.Field] = "Enter dollars such as 2.50, or leave it empty for the policy default."
		}
	}
	if len(form.Errors) > 0 {
		form.Summary = "Some fields need another look."
		ascAgentsTokensPage(ctx, env, form, http.StatusBadRequest, nil, "")
		return
	}

	conn, err := ascRPCConn()
	if err != nil {
		log.Error("asc agents: no connection to asc serve: %v", err)
		form.Summary = ascagentui.Classify(err).Message
		ascAgentsTokensPage(ctx, env, form, http.StatusBadGateway, nil, "")
		return
	}
	callCtx, done, ok := ascAgentsWriteCtx(ctx, form.OpID)
	if !ok {
		return
	}
	resp, err := ascv1.NewAgentTokensClient(conn).CreateToken(callCtx, req)
	done()
	if err != nil {
		f := ascagentui.Classify(err)
		if f == nil || ctx.Req.Context().Err() != nil {
			return
		}
		if f.Unavailable {
			log.Error("asc agents: CreateToken failed: %s", ascagentuiLogLine(f))
		}
		form.Summary = f.Message
		for _, d := range f.Details {
			form.Summary += " " + d + "."
		}
		ascAgentsTokensPage(ctx, env, form, f.HTTP, nil, "")
		return
	}
	row, ok := ascAgentTokenOf(resp.GetToken(), env.name)
	secret := resp.GetSecret()
	if !ok || !ascAgentSecretRe.MatchString(secret) {
		// The token exists on the server but its secret cannot be shown safely. Say so; the admin revokes it from
		// the list. Nothing of the answer is logged.
		log.Error("asc agents: CreateToken answered something the hub cannot show")
		form.Summary = "The token was made but its answer was not usable, so the secret cannot be shown. Revoke it below and make another."
		ascAgentsTokensPage(ctx, env, form, http.StatusBadGateway, nil, "")
		return
	}
	blank := &ascAgentTokenForm{TTL: ascAgentTokenTTLs[0].Seconds, OpID: ascAgentsNewOpID(), Agent: ascAgentsDefaultAgent}
	ascAgentsTokensPage(ctx, env, blank, http.StatusOK, &row, secret)
}

// AscAgentsTokenRevoke serves `POST /{owner}/{repo}/asc/agents/tokens/{id}/revoke`.
func AscAgentsTokenRevoke(ctx *context.Context) {
	env, ok := ascAgentsGate(ctx, ascAgentsOpts{level: ascAgentsAdmin})
	if !ok {
		return
	}
	id := ctx.Params("id")
	if !ascagentui.ValidTokenID(id) {
		ctx.NotFound("asc agent token", nil)
		return
	}
	ascAgentsLimitBody(ctx)
	back := env.runsLink() + "/tokens"
	reason, ok := ascagentui.CheckLine(ctx.FormString("reason"), ascagentui.MaxReasonBytes, true)
	if !ok {
		ctx.Flash.Error("The reason is at most 256 characters on one line.")
		ctx.Redirect(back)
		return
	}
	conn, err := ascRPCConn()
	if err != nil {
		ascAgentsFlashFail(ctx, back, "connection", err)
		return
	}
	callCtx, done, ok := ascAgentsWriteCtx(ctx, ctx.FormTrim("op"))
	if !ok {
		return
	}
	defer done()
	resp, err := ascv1.NewAgentTokensClient(conn).RevokeToken(callCtx, &ascv1.RevokeAgentTokenRequest{Repo: env.ref(), Id: id, Reason: reason})
	if err != nil {
		ascAgentsFlashFail(ctx, back, "RevokeToken", err)
		return
	}
	if row, ok := ascAgentTokenOf(resp.GetToken(), env.name); !ok || row.ID != id {
		ascAgentsFlashFail(ctx, back, "RevokeToken", errAscAgentsBadAnswer)
		return
	}
	ctx.Flash.Success("The token was revoked. It stops working on its next call.")
	ctx.Redirect(back)
}

// AscAgentsTokenBudget serves `POST /{owner}/{repo}/asc/agents/tokens/{id}/budget`.
func AscAgentsTokenBudget(ctx *context.Context) {
	env, ok := ascAgentsGate(ctx, ascAgentsOpts{level: ascAgentsAdmin, notArchived: true})
	if !ok {
		return
	}
	id := ctx.Params("id")
	if !ascagentui.ValidTokenID(id) {
		ctx.NotFound("asc agent token", nil)
		return
	}
	ascAgentsLimitBody(ctx)
	back := env.runsLink() + "/tokens"
	limits, err := ascagentui.ParseBudget(ascagentui.BudgetInput{
		Changes: ctx.FormTrim(ascagentui.BudgetFields.Changes),
		Tokens:  ctx.FormTrim(ascagentui.BudgetFields.Tokens),
		CostUSD: ctx.FormTrim(ascagentui.BudgetFields.CostUSD),
		Paths:   ctx.FormTrim(ascagentui.BudgetFields.Paths),
		Minutes: ctx.FormTrim(ascagentui.BudgetFields.Minutes),
	})
	if err != nil {
		ctx.Flash.Error("A budget value is not a valid number. Use whole numbers, and dollars such as 2.50 for the cost.")
		ctx.Redirect(back)
		return
	}
	conn, err := ascRPCConn()
	if err != nil {
		ascAgentsFlashFail(ctx, back, "connection", err)
		return
	}
	callCtx, done, ok := ascAgentsWriteCtx(ctx, ctx.FormTrim("op"))
	if !ok {
		return
	}
	defer done()
	resp, err := ascv1.NewAgentTokensClient(conn).SetBudget(callCtx, &ascv1.SetAgentTokenBudgetRequest{Repo: env.ref(), Id: id, Limits: limits})
	if err != nil {
		ascAgentsFlashFail(ctx, back, "SetBudget", err)
		return
	}
	if row, ok := ascAgentTokenOf(resp.GetToken(), env.name); !ok || row.ID != id {
		ascAgentsFlashFail(ctx, back, "SetBudget", errAscAgentsBadAnswer)
		return
	}
	ctx.Flash.Success("The budget of the token was set.")
	ctx.Redirect(back)
}
