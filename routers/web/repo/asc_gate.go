// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/http"
	"time"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/base"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/routers/web/shared/trustweb"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
)

// The policy gate: what the rules of a repository are, what they refused lately and which tests do not hold up a
// landing (GET /{owner}/{repo}/asc/gate). The page reads the policy in force, the refusals in the audit log (only an
// administrator may read that log, so only an administrator sees the list) and the quarantined tests. It writes
// nothing: the rules change only through a reviewed change, on the settings page of the policy.
const (
	tplAscGate base.TplName = "repo/asc_gate"

	gateDaysShort = 7
	gateDaysLong  = 30
)

// ascGatePage is the template data under .AscGate.
type ascGatePage struct {
	View     *asctrust.PolicyView
	Days     int
	Held     []asctrust.AuditRow
	HeldMore bool
	// HeldAll is set when the list shows every refusal read; by default it shows the first few, as the board does.
	HeldAll     bool
	HeldShown   bool // the viewer may read the audit log, so the list (even an empty one) is the truth
	HeldProblem string
	Problem     string
	Unsupported bool
	Stats       *ascGateStats

	ProtectionsOn    int
	ProtectionsTotal int

	GateURL   string
	PolicyURL string // the settings page; empty for a person who may not open it
	AuditURL  string
	QueueURL  string
	ChecksURL string
	DaysShort int
	DaysLong  int
}

func ascGateURL(repo *repo_model.Repository) string {
	return setting.AppSubURL + "/" + repo.OwnerName + "/" + repo.Name + "/asc/gate"
}

// AscGate serves GET /{owner}/{repo}/asc/gate with ?days=7|30.
func AscGate(ctx *context.Context) {
	repo, name, ok := trustweb.CodeRepo(ctx, false)
	if !ok {
		return
	}
	trustweb.NoStore(ctx)
	ctx.Data["Title"] = ctx.Locale.TrString("asc_review.gate_title")
	ctx.Data["PageIsAscGate"] = true
	days := gateDaysShort
	if ctx.FormString("days") == "30" {
		days = gateDaysLong
	}
	root := setting.AppSubURL + "/" + repo.OwnerName + "/" + repo.Name
	p := &ascGatePage{
		Days: days, GateURL: ascGateURL(repo), AuditURL: root + "/settings/asc/audit", QueueURL: root + "/asc/queue",
		ChecksURL: ascChecksURL(repo), DaysShort: gateDaysShort, DaysLong: gateDaysLong,
	}
	if ctx.Repo.IsAdmin() {
		p.PolicyURL = root + "/settings/asc/policy"
	}
	ctx.Data["AscGate"] = p
	if !asctrust.HasFeature(ctx, asctrust.FeaturePolicy) {
		p.Unsupported = true
		ctx.HTML(http.StatusOK, tplAscGate)
		return
	}
	conn, err := asctrust.Conn()
	if err != nil {
		log.Error("asc gate: no connection to asc serve: %v", err)
		p.Problem = ctx.Locale.TrString("asc_trust.err.unavailable")
		ctx.HTML(http.StatusBadGateway, tplAscGate)
		return
	}
	callCtx, release, err := trustweb.ReadCall(ctx, repo)
	if err != nil {
		f := trustweb.FailureOf("gate credential", err)
		p.Problem = f.Localized(ctx)
		ctx.HTML(f.Status, tplAscGate)
		return
	}
	defer release()
	page, err := asctrust.LoadPolicyPage(callCtx, conn, name)
	if err != nil {
		f := trustweb.FailureOf("gate policy", err)
		if f.Status == http.StatusNotFound {
			ctx.NotFound("asc gate", nil)
			return
		}
		p.Problem = f.Localized(ctx)
		ctx.HTML(f.Status, tplAscGate)
		return
	}
	p.View = page.View
	for _, pr := range page.View.Protections {
		p.ProtectionsTotal++
		if pr.On {
			p.ProtectionsOn++
		}
	}
	ctx.Data["AscQuarantine"] = ascQueueQuarantine(ctx)
	ascGateLoadStats(ctx, p, callCtx, conn, name, days)

	if ctx.Repo.IsAdmin() && asctrust.HasFeature(ctx, asctrust.FeatureAudit) {
		p.HeldShown = true
		since := time.Now().UTC().AddDate(0, 0, -days).Format(time.DateOnly)
		get := func(k string) string {
			switch k {
			case "outcome":
				return "denied"
			case "since":
				return since
			}
			return ""
		}
		q, filter, _ := asctrust.AuditQueryOf(get)
		res, err := asctrust.ListAudit(callCtx, conn, &ascv1.OwnerRef{Kind: ascv1.OwnerKind_OWNER_KIND_REPO, Name: name}, q, filter)
		if err != nil {
			p.HeldProblem = trustweb.FailureOf("gate refusals", err).Localized(ctx)
		} else {
			p.Held, p.HeldMore = res.Page.Rows, res.Page.Next != ""
			p.HeldAll = ctx.FormString("held") == "all"
		}
	}
	ctx.HTML(http.StatusOK, tplAscGate)
}
