// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package usageweb serves the usage and quota board of asc serve (docs/BUILDOUT.md section 27) at the three levels it
// has: the account of a person, an organisation (owners) and the whole server (site administrators, who may also look
// at any account or organisation and set the limits). The page of each level is a thin wrapper that builds a Level;
// everything else is here, and the body of the page is the template shared/asc_usage.
package usageweb

import (
	gocontext "context"
	"net/http"
	"net/url"
	"time"

	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/base"
	"forgejo.org/modules/log"
	"forgejo.org/routers/web/shared/trustweb"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// Data is the key of the View in the template data.
const Data = "AscUsage"

const maxForm = 8 << 10

// Level says whose usage a page shows and how the hub vouches for the person who asks.
type Level struct {
	// Owner is whose usage it is. The admin level starts as the instance and may be pointed at an account or an
	// organisation with the kind and name of the query.
	Owner *ascv1.OwnerRef
	// Call is what the hub decides before it signs the claim: CallAccount, CallOrg or CallInstance.
	Call asctrust.CallKind
	// Org is the organisation of CallOrg.
	Org *user_model.User
	// Base is the URL of the page.
	Base string
	// Label names the owner on the page.
	Label string
	Tpl   base.TplName
	// Admin is the site administrator's page: it can show another owner and set limits.
	Admin bool
}

// Link is a choice drawn as a link.
type Link struct {
	Name   string
	URL    string
	Active bool
}

// QuotaForm is the limits as the form fields show them.
type QuotaForm struct {
	StorageGiB   uint64
	Minutes      uint64
	SpendUSD     string
	BandwidthGiB uint64
}

func formOf(q asctrust.Quota) QuotaForm {
	return QuotaForm{StorageGiB: asctrust.GiB(q.StorageBytes), Minutes: q.AgentMinutes, SpendUSD: asctrust.USDField(q.AISpendMicro), BandwidthGiB: asctrust.GiB(q.BandwidthBytes)}
}

// View is what the template draws.
type View struct {
	Base        string
	Label       string
	Admin       bool
	Unsupported bool
	Error       string
	Period      string
	Periods     []Link
	Groups      []Link
	Usage       *asctrust.UsagePage
	Spend       *asctrust.SpendTable
	Quota       *asctrust.QuotaPage
	Override    QuotaForm
	Instance    bool
	OwnerKind   string
	OwnerName   string
	SetOp       string
	Now         time.Time
}

// call makes the context of a call for the signed-in person: the hub decides first, and signs a claim for the subject.
func (lvl *Level) call(ctx *context.Context) (gocontext.Context, func(), error) {
	if lvl.Call == asctrust.CallOrg {
		return asctrust.UserCallOrg(ctx, ctx.Doer, lvl.Org)
	}
	return asctrust.UserCall(ctx, ctx.Doer, lvl.Call, nil)
}

func (lvl *Level) link(q url.Values) string {
	if len(q) == 0 {
		return lvl.Base
	}
	return lvl.Base + "?" + q.Encode()
}

// monthLinks offers this month and the five before it.
func (lvl *Level) monthLinks(now time.Time, current string, keep url.Values) []Link {
	var out []Link
	m := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 6; i++ {
		name := m.AddDate(0, -i, 0).Format("2006-01")
		q := cloneValues(keep)
		q.Set("period", name)
		out = append(out, Link{Name: name, URL: lvl.link(q), Active: name == current})
	}
	return out
}

func cloneValues(v url.Values) url.Values {
	out := url.Values{}
	for k, vs := range v {
		out[k] = append([]string(nil), vs...)
	}
	return out
}

// owner works out whose usage the request is about.
func (lvl *Level) owner(ctx *context.Context) (*ascv1.OwnerRef, string, string, error) {
	if !lvl.Admin {
		return lvl.Owner, "", "", nil
	}
	kind, name := ctx.FormString("kind"), ctx.FormString("name")
	o, err := asctrust.OwnerOf(kind, name)
	if err != nil {
		return nil, "", "", err
	}
	return o, kind, o.GetName(), nil
}

// Serve serves GET <base>: the four measures, the repositories, the spend and (for the administrator) the limits.
func Serve(ctx *context.Context, lvl *Level) {
	trustweb.NoStore(ctx)
	ctx.Data["Title"] = ctx.Locale.TrString("asc_shell.usage_title")
	v := &View{Base: lvl.Base, Label: lvl.Label, Admin: lvl.Admin, Now: time.Now().UTC(), SetOp: asctrust.NewOpID()}
	ctx.Data[Data] = v
	status := http.StatusOK
	defer func() { ctx.HTML(status, lvl.Tpl) }()

	owner, kind, name, err := lvl.owner(ctx)
	if err != nil {
		f := trustweb.FailureOf("usage owner", err)
		v.Error, status = f.Localized(ctx), f.Status
		return
	}
	v.OwnerKind, v.OwnerName = kind, name
	v.Instance = owner.GetKind() == ascv1.OwnerKind_OWNER_KIND_INSTANCE
	keep := url.Values{}
	if kind != "" {
		keep.Set("kind", kind)
		keep.Set("name", name)
	}
	period, err := asctrust.CheckPeriod(ctx.FormString("period"))
	if err != nil {
		f := trustweb.FailureOf("usage period", err)
		v.Error, status = f.Localized(ctx), f.Status
		return
	}
	groupKey, _ := asctrust.SpendGroupOf(ctx.FormString("group"))

	if !asctrust.HasFeature(ctx, asctrust.FeatureUsage) {
		v.Unsupported = true
		return
	}
	conn, err := asctrust.Conn()
	if err != nil {
		log.Error("asc usage: no connection to asc serve: %v", err)
		v.Error, status = ctx.Locale.TrString("asc_trust.err.unavailable"), http.StatusBadGateway
		return
	}
	callCtx, release, err := lvl.call(ctx)
	if err != nil {
		f := trustweb.FailureOf("usage credential", err)
		v.Error, status = f.Localized(ctx), f.Status
		return
	}
	defer release()

	usage, err := asctrust.LoadUsage(callCtx, conn, owner, period)
	if err != nil {
		f := trustweb.FailureOf("usage load", err)
		v.Error, status = f.Localized(ctx), f.Status
		return
	}
	v.Usage, v.Period = usage, usage.Period
	v.Periods = lvl.monthLinks(v.Now, usage.Period, keep)
	for _, g := range asctrust.SpendGroups {
		q := cloneValues(keep)
		q.Set("period", usage.Period)
		q.Set("group", g)
		v.Groups = append(v.Groups, Link{Name: g, URL: lvl.link(q), Active: g == groupKey})
	}
	spend, err := asctrust.LoadSpend(callCtx, conn, owner, groupKey, usage.Start.Unix(), usage.ResetsAt.Unix())
	if err != nil {
		// The measures are worth showing without the breakdown.
		f := trustweb.FailureOf("usage spend", err)
		v.Error = f.Localized(ctx)
	} else {
		v.Spend = spend
	}
	if lvl.Admin {
		q, err := asctrust.LoadQuota(callCtx, conn, owner)
		if err != nil {
			f := trustweb.FailureOf("usage quota", err)
			v.Error = f.Localized(ctx)
			return
		}
		v.Quota, v.Override = q, formOf(q.Override)
	}
}

// ServeSetQuota serves POST <base>/quota with the fields kind, name, storage_gib, agent_minutes, ai_spend_usd,
// bandwidth_gib and op. Only the administrator's level has it. An empty field is no limit (an inherited one for an
// account or an organisation).
func ServeSetQuota(ctx *context.Context, lvl *Level) {
	if !lvl.Admin {
		ctx.Error(http.StatusForbidden)
		return
	}
	if !trustweb.ParseForm(ctx, maxForm) {
		return
	}
	back := lvl.Base
	owner, kind, name, err := lvl.owner(ctx)
	if err != nil {
		ctx.Flash.Error(trustweb.FailureOf("usage owner", err).Localized(ctx))
		ctx.Redirect(back)
		return
	}
	if kind != "" {
		back += "?" + url.Values{"kind": {kind}, "name": {name}}.Encode()
	}
	q, err := asctrust.QuotaFromForm(ctx.Req.PostFormValue)
	if err != nil {
		ctx.Flash.Error(trustweb.FailureOf("usage quota form", err).Localized(ctx))
		ctx.Redirect(back)
		return
	}
	conn, err := asctrust.Conn()
	if err != nil {
		ctx.Flash.Error(ctx.Locale.TrString("asc_trust.err.unavailable"))
		ctx.Redirect(back)
		return
	}
	callCtx, release, err := lvl.call(ctx)
	if err != nil {
		ctx.Flash.Error(trustweb.FailureOf("usage credential", err).Localized(ctx))
		ctx.Redirect(back)
		return
	}
	defer release()
	if _, err := asctrust.SetQuota(callCtx, conn, owner, q, ctx.Req.PostFormValue("op")); err != nil {
		ctx.Flash.Error(trustweb.FailureOf("usage set quota", err).Localized(ctx))
		ctx.Redirect(back)
		return
	}
	ctx.Flash.Success(ctx.Locale.TrString("asc_shell.usage_quota_saved"))
	ctx.Redirect(back)
}
