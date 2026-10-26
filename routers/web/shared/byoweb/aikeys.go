// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package byoweb

import (
	"net/http"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/log"
	svc "forgejo.org/services/ascbyo"
	"forgejo.org/services/context"
)

func providerOptions(ctx *context.Context, selected string) []ProviderOption {
	out := make([]ProviderOption, 0, len(svc.Providers))
	for _, pr := range svc.Providers {
		out = append(out, ProviderOption{Value: string(pr), Label: ctx.Locale.TrString("ascbyo.ai.provider." + string(pr)), Sel: string(pr) == selected})
	}
	return out
}

func keyID(ctx *context.Context) (string, bool) {
	id := ctx.Params("id")
	if !svc.ValidKeyID(id) {
		ctx.NotFound("asc byo key id", nil)
		return "", false
	}
	return id, true
}

// renderAIIndex loads the keys and renders the list page with status.
func renderAIIndex(ctx *context.Context, lvl *Level, s *svc.Service, p *Page, status int) {
	ctx.Data["Title"] = ctx.Locale.TrString("ascbyo.ai.title")
	p.Providers = providerOptions(ctx, p.AIForm.Provider)
	if s == nil {
		p.Unavailable = true
	} else {
		ks, err := s.ListKeys(ctx, lvl.Scope)
		if err != nil {
			markAvailability(p, err)
			if p.Problem == nil {
				p.Problem = problemOf(ctx, err)
			}
		} else {
			p.Keys = svc.KeyViews(ks)
		}
	}
	if (p.Unavailable || p.Unsupported) && status < http.StatusBadRequest {
		status = http.StatusServiceUnavailable
	}
	ctx.HTML(status, lvl.AITpl)
}

// renderAIKey loads one key with its models and this month's usage and renders its page. A key that is not
// the scope's is the 404 page. Models and usage are best effort: a failure leaves that part out.
func renderAIKey(ctx *context.Context, lvl *Level, s *svc.Service, p *Page, id string, status int) {
	ctx.Data["Title"] = ctx.Locale.TrString("ascbyo.ai.key_title")
	if s == nil {
		p.Unavailable = true
		ctx.HTML(http.StatusServiceUnavailable, lvl.AITpl)
		return
	}
	k, err := s.GetKey(ctx, lvl.Scope, id)
	if err != nil {
		if svc.IsKind(err, svc.FailNotFound) {
			ctx.NotFound("asc byo key", nil)
			return
		}
		markAvailability(p, err)
		p.Problem = problemOf(ctx, err)
		ctx.HTML(statusOf(err), lvl.AITpl)
		return
	}
	v := svc.KeyViewOf(k)
	p.Key = &v
	if p.AIForm == (AIForm{}) {
		p.AIForm = AIForm{Label: v.Label, Provider: string(v.Provider), BaseURL: v.BaseURL, DefaultModel: v.DefaultModel, CapUSD: v.CapInput}
	}
	p.Providers = providerOptions(ctx, string(v.Provider))
	if k.GetState() == ascv1.AiKeyState_AI_KEY_STATE_ACTIVE || k.GetState() == ascv1.AiKeyState_AI_KEY_STATE_CAPPED {
		if models, err := s.ListModels(ctx, lvl.Scope, id); err == nil {
			p.Models = models
		} else {
			log.Debug("asc byo: models of a key in %s: %v", lvl.Scope, err)
		}
	}
	if byModel, err := s.Usage(ctx, lvl.Scope, id, ascv1.AiUsageGroup_AI_USAGE_GROUP_MODEL); err == nil {
		p.UsageByModel = svc.UsageViews(byModel.Rows)
		p.UsageTotal = svc.UsageRowViewOf(byModel.Total)
		p.UsageLoaded = true
		if byDay, err := s.Usage(ctx, lvl.Scope, id, ascv1.AiUsageGroup_AI_USAGE_GROUP_DAY); err == nil {
			p.UsageByDay = svc.UsageViews(byDay.Rows)
		}
	} else {
		log.Debug("asc byo: usage of a key in %s: %v", lvl.Scope, err)
	}
	ctx.HTML(status, lvl.AITpl)
}

// AIIndex is GET .../asc/ai.
func AIIndex(ctx *context.Context, lvl *Level) {
	s, p, ok := begin(ctx, lvl, dataAI)
	if !ok {
		return
	}
	p.AIForm = AIForm{Provider: string(svc.ProviderAnthropic)}
	renderAIIndex(ctx, lvl, s, p, http.StatusOK)
}

// AIKey is GET .../asc/ai/keys/{id}.
func AIKey(ctx *context.Context, lvl *Level) {
	s, p, ok := begin(ctx, lvl, dataAI)
	if !ok {
		return
	}
	id, ok := keyID(ctx)
	if !ok {
		return
	}
	renderAIKey(ctx, lvl, s, p, id, http.StatusOK)
}

func aiInputOf(f form) *svc.AIKeyInput {
	return &svc.AIKeyInput{
		Label: f.str("label", limName*2), Provider: f.str("provider", 32), APIKey: f.str("api_key", limAPIKey),
		BaseURL: f.str("base_url", limURL), DefaultModel: f.str("default_model", limShort+16), CapUSD: f.str("monthly_cap", limCap),
	}
}

func aiFormOf(in *svc.AIKeyInput) AIForm {
	return AIForm{Label: in.Label, Provider: in.Provider, BaseURL: in.BaseURL, DefaultModel: in.DefaultModel, CapUSD: in.CapUSD}
}

// AICreate is POST .../asc/ai/keys.
func AICreate(ctx *context.Context, lvl *Level) {
	s, p, ok := begin(ctx, lvl, dataAI)
	if !ok {
		return
	}
	f := form{ctx}
	in := aiInputOf(f)
	fe := in.Validate(true)
	p.AIForm = aiFormOf(in)
	if len(fe) > 0 {
		p.Errors = fe
		p.Problem = &Problem{Message: ctx.Locale.TrString("ascbyo.error.invalid")}
		renderAIIndex(ctx, lvl, s, p, http.StatusUnprocessableEntity)
		return
	}
	if s == nil {
		renderAIIndex(ctx, lvl, nil, p, http.StatusServiceUnavailable)
		return
	}
	k, _, err := s.CreateKey(ctx, lvl.Scope, in, f.op())
	if err != nil {
		p.Problem = problemOf(ctx, err)
		refuse(ctx, err, func(status int) { renderAIIndex(ctx, lvl, s, p, status) })
		return
	}
	record(ctx, lvl, "added AI key", k.GetId())
	ctx.Flash.Success(ctx.Locale.TrString("ascbyo.flash.ai_created"))
	ctx.Redirect(lvl.Base + "/ai/keys/" + k.GetId())
}

// AIUpdate is POST .../asc/ai/keys/{id}.
func AIUpdate(ctx *context.Context, lvl *Level) {
	s, p, ok := begin(ctx, lvl, dataAI)
	if !ok {
		return
	}
	id, ok := keyID(ctx)
	if !ok {
		return
	}
	f := form{ctx}
	in := aiInputOf(f)
	if s == nil {
		p.AIForm = aiFormOf(in)
		renderAIKey(ctx, lvl, nil, p, id, http.StatusServiceUnavailable)
		return
	}
	_, changed, fe, err := s.UpdateKey(ctx, lvl.Scope, id, in, f.op())
	p.AIForm = aiFormOf(in)
	if err != nil {
		p.Problem = problemOf(ctx, err)
		refuse(ctx, err, func(status int) { renderAIKey(ctx, lvl, s, p, id, status) })
		return
	}
	if len(fe) > 0 {
		p.Errors = fe
		p.Problem = &Problem{Message: ctx.Locale.TrString("ascbyo.error.invalid")}
		renderAIKey(ctx, lvl, s, p, id, http.StatusUnprocessableEntity)
		return
	}
	if changed {
		record(ctx, lvl, "updated AI key", id)
		ctx.Flash.Success(ctx.Locale.TrString("ascbyo.flash.ai_updated"))
	} else {
		ctx.Flash.Info(ctx.Locale.TrString("ascbyo.flash.nothing_changed"))
	}
	ctx.Redirect(lvl.Base + "/ai/keys/" + id)
}

func aiReturnTo(ctx *context.Context, lvl *Level, id string) string {
	if (form{ctx}).str("return", 8) == "list" {
		return lvl.Base + "/ai"
	}
	return lvl.Base + "/ai/keys/" + id
}

// AITest is POST .../asc/ai/keys/{id}/test. The result is shown in the response.
func AITest(ctx *context.Context, lvl *Level) {
	s, p, ok := begin(ctx, lvl, dataAI)
	if !ok {
		return
	}
	id, ok := keyID(ctx)
	if !ok {
		return
	}
	if s == nil {
		renderAIKey(ctx, lvl, nil, p, id, http.StatusServiceUnavailable)
		return
	}
	res, err := s.TestKey(ctx, lvl.Scope, id, form{ctx}.op())
	if err != nil {
		p.Problem = problemOf(ctx, err)
		refuse(ctx, err, func(status int) { renderAIKey(ctx, lvl, s, p, id, status) })
		return
	}
	record(ctx, lvl, "tested AI key", id)
	p.Test = &TestView{OK: res.OK, Status: res.Status, Latency: res.LatencyMS, Message: res.Message}
	renderAIKey(ctx, lvl, s, p, id, http.StatusOK)
}

// AIDisable is POST .../asc/ai/keys/{id}/disable and /enable.
func AIDisable(ctx *context.Context, lvl *Level, disabled bool) {
	s, p, ok := begin(ctx, lvl, dataAI)
	if !ok {
		return
	}
	id, ok := keyID(ctx)
	if !ok {
		return
	}
	if s == nil {
		renderAIKey(ctx, lvl, nil, p, id, http.StatusServiceUnavailable)
		return
	}
	if _, err := s.SetKeyDisabled(ctx, lvl.Scope, id, disabled, form{ctx}.op()); err != nil {
		p.Problem = problemOf(ctx, err)
		refuse(ctx, err, func(status int) { renderAIKey(ctx, lvl, s, p, id, status) })
		return
	}
	if disabled {
		record(ctx, lvl, "disabled AI key", id)
		ctx.Flash.Success(ctx.Locale.TrString("ascbyo.flash.ai_disabled"))
	} else {
		record(ctx, lvl, "enabled AI key", id)
		ctx.Flash.Success(ctx.Locale.TrString("ascbyo.flash.ai_enabled"))
	}
	ctx.Redirect(aiReturnTo(ctx, lvl, id))
}

// AIDelete is POST .../asc/ai/keys/{id}/delete.
func AIDelete(ctx *context.Context, lvl *Level) {
	s, p, ok := begin(ctx, lvl, dataAI)
	if !ok {
		return
	}
	id, ok := keyID(ctx)
	if !ok {
		return
	}
	if s == nil {
		renderAIKey(ctx, lvl, nil, p, id, http.StatusServiceUnavailable)
		return
	}
	if !(form{ctx}.on("confirm")) {
		p.Problem = &Problem{Message: ctx.Locale.TrString("ascbyo.error.confirm")}
		renderAIKey(ctx, lvl, s, p, id, http.StatusUnprocessableEntity)
		return
	}
	if err := s.DeleteKey(ctx, lvl.Scope, id, form{ctx}.op()); err != nil {
		p.Problem = problemOf(ctx, err)
		refuse(ctx, err, func(status int) { renderAIKey(ctx, lvl, s, p, id, status) })
		return
	}
	record(ctx, lvl, "deleted AI key", id)
	ctx.Flash.Success(ctx.Locale.TrString("ascbyo.flash.ai_deleted"))
	ctx.Redirect(lvl.Base + "/ai")
}
