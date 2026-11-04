// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package byoweb

import (
	gocontext "context"
	"net/http"

	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	svc "forgejo.org/services/ascbyo"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// The provider cards and the "What uses AI" table of the AI keys page, and the forms of their dialogs.
//
//	POST .../asc/ai/keys/{id}/replace     a new secret for the key, tried with the provider first (field api_key)
//	POST .../asc/ai/features/{feature}    the key and the monthly cap of a feature (fields key_id, monthly_cap, clear)

// KeyCard is a key of the scope as its card shows it.
type KeyCard struct {
	svc.KeyView
	// Name is the provider's name; ShowLabel is set when another key of the scope has the same provider, so the
	// label tells them apart.
	Name      string
	ShowLabel bool
	// AddedBy is the display name of the person who added the key, "" when it is not known.
	AddedBy string
	// UsedFor is the names of the features that use the key now.
	UsedFor []string
	// The replace dialog shown again after a refused replacement: Replace is set, ReplaceErr says why and
	// ReplaceDetail is the provider's own words.
	Replace       bool
	ReplaceErr    string
	ReplaceDetail string
}

// FeatureRow is a feature of the "What uses AI" table.
type FeatureRow struct {
	svc.FeatureView
	Title, Hint string
	// Options are the keys the feature may use, with the one that applies selected.
	Options []ProviderOption
	// SetByLabel and PausedByLabel name, for people, the owner above whose setting or cap applies.
	SetByLabel    string
	PausedByLabel string
	// The values of the edit dialog, and whether it is shown open (after a refused edit).
	EditCap string
	Open    bool
}

// FeatureEdit is a refused feature form, shown again.
type FeatureEdit struct {
	Name   svc.Feature
	KeyID  string
	CapUSD string
}

// keyInherit is the key_id of the option "as the owner above sets it": the scope's own setting is removed.
const keyInherit = "inherit"

// asPerson is ctx made into a call on behalf of the signed-in person, so asc records who added or changed a thing.
// The level already checked that the person administers the scope; asc checks the claim again. Without a key to sign
// claims, or when the claim cannot be made, the call is the hub's own.
func asPerson(ctx *context.Context, lvl *Level) gocontext.Context {
	if len(setting.Asc.ForwardedKey) == 0 || ctx.Doer == nil {
		return ctx
	}
	var (
		callCtx gocontext.Context
		err     error
	)
	switch lvl.Scope.Kind {
	case svc.KindUser:
		if lvl.Scope.Name != ctx.Doer.LowerName {
			return ctx
		}
		callCtx, _, err = asctrust.UserCall(ctx, ctx.Doer, asctrust.CallAccount, nil)
	case svc.KindOrg:
		org, gerr := user_model.GetUserByName(ctx, lvl.Scope.Name)
		if gerr != nil || !org.IsOrganization() {
			return ctx
		}
		callCtx, _, err = asctrust.UserCallOrg(ctx, ctx.Doer, org)
	case svc.KindRepo:
		if ctx.Repo == nil || ctx.Repo.Repository == nil {
			return ctx
		}
		callCtx, _, err = asctrust.UserCall(ctx, ctx.Doer, asctrust.CallRepoAdmin, ctx.Repo.Repository)
	default:
		return ctx
	}
	if err != nil {
		log.Debug("asc byo: a call as the person for %s: %v", lvl.Scope, err)
		return ctx
	}
	return callCtx
}

// keyCards builds the cards of the scope's keys, with the features that use each.
func keyCards(ctx *context.Context, keys []svc.KeyView, features []svc.FeatureView) []KeyCard {
	providers := map[svc.Provider]int{}
	for _, k := range keys {
		providers[k.Provider]++
	}
	names := map[int64]string{}
	out := make([]KeyCard, 0, len(keys))
	for _, k := range keys {
		c := KeyCard{KeyView: k, Name: ctx.Locale.TrString(k.ProviderKey), ShowLabel: providers[k.Provider] > 1}
		if k.AddedByID > 0 {
			name, ok := names[k.AddedByID]
			if !ok {
				name = k.AddedBy
				if u, err := user_model.GetUserByID(ctx, k.AddedByID); err == nil {
					name = u.DisplayName()
				}
				names[k.AddedByID] = name
			}
			c.AddedBy = name
		}
		for _, f := range features {
			if f.Key != nil && f.KeyOwned && f.Key.ID == k.ID {
				c.UsedFor = append(c.UsedFor, ctx.Locale.TrString(f.NameKey))
			}
		}
		out = append(out, c)
	}
	return out
}

// ownerName is how the page names an owner above: its name, or "this instance".
func ownerName(ctx *context.Context, label string) string {
	if label == "instance" {
		return ctx.Locale.TrString("ascbyo.ai.uses.instance")
	}
	return label
}

// featureRows builds the table, with edit, when set, being a refused form shown again.
func featureRows(ctx *context.Context, keys []svc.KeyView, features []svc.FeatureView, edit *FeatureEdit) []FeatureRow {
	out := make([]FeatureRow, 0, len(features))
	for _, f := range features {
		r := FeatureRow{
			FeatureView: f, Title: ctx.Locale.TrString(f.NameKey), Hint: ctx.Locale.TrString(f.HintKey),
			SetByLabel: ownerName(ctx, f.SetBy), PausedByLabel: ownerName(ctx, f.PausedBy),
		}
		sel := keyInherit
		if f.Own {
			sel, r.EditCap = f.KeyID, f.CapInput
		}
		if edit != nil && edit.Name == f.Name {
			sel, r.EditCap, r.Open = edit.KeyID, edit.CapUSD, true
		}
		inherit := ctx.Locale.TrString("ascbyo.ai.uses.instance_default")
		if f.SetBy != "" && f.SetBy != "instance" {
			inherit = ctx.Locale.TrString("ascbyo.ai.uses.inherit", f.SetBy)
		}
		if !f.Own && f.Key != nil && !f.KeyOwned {
			inherit = ctx.Locale.TrString("ascbyo.ai.uses.inherit_key", inherit, ctx.Locale.TrString(f.Key.ProviderKey))
		}
		r.Options = append(r.Options,
			ProviderOption{Value: keyInherit, Label: inherit, Sel: sel == keyInherit},
			ProviderOption{Value: "", Label: ctx.Locale.TrString("ascbyo.ai.uses.first_key"), Sel: sel == ""},
		)
		for _, k := range keys {
			label := ctx.Locale.TrString(k.ProviderKey)
			if label != k.Label {
				label = ctx.Locale.TrString("ascbyo.ai.uses.key_option", label, k.Label)
			}
			r.Options = append(r.Options, ProviderOption{Value: k.ID, Label: label, Sel: sel == k.ID})
		}
		out = append(out, r)
	}
	return out
}

// loadFeatures fills the cards and the table. The features are best effort: a server without them shows the cards alone.
func loadFeatures(ctx *context.Context, lvl *Level, s *svc.Service, p *Page) {
	var views []svc.FeatureView
	if fs, err := s.ListFeatures(ctx, lvl.Scope); err == nil {
		views = svc.FeatureViews(lvl.Scope, fs)
	} else {
		log.Debug("asc byo: features of %s: %v", lvl.Scope, err)
		p.FeaturesFailed = true
	}
	p.Cards = keyCards(ctx, p.Keys, views)
	p.Features = featureRows(ctx, p.Keys, views, p.FeatureEdit)
	if p.Replace != nil {
		for i := range p.Cards {
			if p.Cards[i].ID == p.Replace.ID {
				p.Cards[i].Replace, p.Cards[i].ReplaceErr, p.Cards[i].ReplaceDetail = true, p.Replace.Err, p.Replace.Detail
			}
		}
	}
}

// ReplaceView is a refused replacement, shown again in the dialog of its key.
type ReplaceView struct {
	ID     string
	Err    string
	Detail string
}

// AIReplace is POST .../asc/ai/keys/{id}/replace. The old secret stays until the provider accepts the new one.
func AIReplace(ctx *context.Context, lvl *Level) {
	s, p, ok := begin(ctx, lvl, dataAI)
	if !ok {
		return
	}
	id, ok := keyID(ctx)
	if !ok {
		return
	}
	p.AIForm = AIForm{Provider: string(svc.ProviderAnthropic)}
	if s == nil {
		renderAIIndex(ctx, lvl, nil, p, http.StatusServiceUnavailable)
		return
	}
	f := form{ctx}
	res, fe, err := s.ReplaceKey(asPerson(ctx, lvl), lvl.Scope, id, f.str("api_key", limAPIKey), f.op())
	if err != nil {
		if svc.IsKind(err, svc.FailNotFound) {
			ctx.NotFound("asc byo key", nil)
			return
		}
		p.Problem = problemOf(ctx, err)
		refuse(ctx, err, func(status int) { renderAIIndex(ctx, lvl, s, p, status) })
		return
	}
	if len(fe) > 0 {
		p.Replace = &ReplaceView{ID: id, Err: ctx.Locale.TrString(fe["api_key"])}
		renderAIIndex(ctx, lvl, s, p, http.StatusUnprocessableEntity)
		return
	}
	if !res.Replaced {
		record(ctx, lvl, "AI key replacement refused by the provider", id)
		p.Replace = &ReplaceView{ID: id, Err: ctx.Locale.TrString("ascbyo.ai.replace.rejected." + res.Status), Detail: res.Message}
		renderAIIndex(ctx, lvl, s, p, http.StatusUnprocessableEntity)
		return
	}
	record(ctx, lvl, "replaced AI key", id)
	ctx.Flash.Success(ctx.Locale.TrString("ascbyo.flash.ai_replaced"))
	ctx.Redirect(lvl.Base + "/ai")
}

// AIFeature is POST .../asc/ai/features/{feature}.
func AIFeature(ctx *context.Context, lvl *Level) {
	s, p, ok := begin(ctx, lvl, dataAI)
	if !ok {
		return
	}
	name := ctx.Params("feature")
	if _, ok := svc.FeatureFromForm(name); !ok {
		ctx.NotFound("asc byo feature", nil)
		return
	}
	p.AIForm = AIForm{Provider: string(svc.ProviderAnthropic)}
	f := form{ctx}
	keyID := f.str("key_id", limShort)
	clear := f.on("clear") || keyID == keyInherit
	in := &svc.FeatureInput{KeyID: keyID, CapUSD: f.str("monthly_cap", limCap)}
	edit := &FeatureEdit{Name: svc.Feature(name), KeyID: keyID, CapUSD: in.CapUSD}
	if s == nil {
		p.FeatureEdit = edit
		renderAIIndex(ctx, lvl, nil, p, http.StatusServiceUnavailable)
		return
	}
	_, fe, err := s.SetFeature(asPerson(ctx, lvl), lvl.Scope, name, in, clear, f.op())
	if err != nil {
		p.FeatureEdit = edit
		p.Problem = problemOf(ctx, err)
		refuse(ctx, err, func(status int) { renderAIIndex(ctx, lvl, s, p, status) })
		return
	}
	if len(fe) > 0 {
		p.FeatureEdit, p.Errors = edit, fe
		p.Problem = &Problem{Message: ctx.Locale.TrString("ascbyo.error.invalid")}
		renderAIIndex(ctx, lvl, s, p, http.StatusUnprocessableEntity)
		return
	}
	if clear {
		record(ctx, lvl, "cleared AI feature setting", name)
		ctx.Flash.Success(ctx.Locale.TrString("ascbyo.flash.feature_cleared"))
	} else {
		record(ctx, lvl, "set AI feature", name)
		ctx.Flash.Success(ctx.Locale.TrString("ascbyo.flash.feature_saved"))
	}
	ctx.Redirect(lvl.Base + "/ai")
}
