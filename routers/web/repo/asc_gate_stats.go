// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"net/http"
	"strconv"
	"time"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/log"
	"forgejo.org/routers/web/shared/trustweb"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"

	"google.golang.org/grpc"
)

// The statistics and the maintainer overrides of the policy gate page. asc keeps the history of what the gate looked at
// (the hub reports each evaluation) and answers counts per day and per rule. The numbers are drawn as asc sent them: a
// figure asc does not send is not drawn. An override is a signed, logged exception for one change and one rule; it is
// granted and revoked by a repository administrator as that person, with a reason, and listed for everyone who may read.

// ascGateDay is one bar of the chart "Checked and held, per day".
type ascGateDay struct {
	Day        string
	Checked    uint32
	Held       uint32
	CheckedPct int
	HeldPct    int
}

// ascGateRuleCount is "N held of M" for one rule.
type ascGateRuleCount struct {
	Rule    string
	Checked uint32
	Held    uint32
}

// ascGateOverride is one signed exception.
type ascGateOverride struct {
	ID        uint64
	Change    string
	Rule      string
	Reason    string
	GrantedBy string
	GrantedAt time.Time
	RevokedBy string
	RevokedAt time.Time
	Active    bool
	Proof     string
}

// ascGateStats is the template data under .AscGate.Stats.
type ascGateStats struct {
	Checked     uint32
	FirstTime   uint32
	FirstPct    int
	Held        uint32
	HeldOpen    uint32
	HeldCleared uint32
	Median      string
	Days        []ascGateDay
	// SparkChecked, SparkFirst and SparkHeld are the points of the small lines of the stat cards, one point to a day.
	SparkChecked, SparkFirst, SparkHeld string
	Rules                               []ascGateRuleCount
	Overrides                           []ascGateOverride
	Active                              uint32
	Truncated                           bool
	CanOverride                         bool
	OverrideURL                         string
}

func ascGateStatsOf(r *ascv1.GetGateStatsResponse) ascGateStats {
	s := ascGateStats{
		Checked: r.GetChecked(), FirstTime: r.GetPassedFirstTime(), Held: r.GetHeld(), HeldOpen: r.GetHeldOpen(),
		HeldCleared: r.GetHeldCleared(), Active: r.GetOverridesActive(), Truncated: r.GetTruncated(),
		FirstPct: ascagentui.Percent(uint64(r.GetPassedFirstTime()), uint64(r.GetChecked())),
	}
	if m := r.GetMedianClearSeconds(); m > 0 {
		s.Median = ascagentui.FormatDuration(uint64(m))
	}
	var peak uint32
	days := r.GetPerDay()
	if len(days) > 90 {
		days = days[len(days)-90:]
	}
	valid := func(d *ascv1.GateDay) bool {
		_, err := time.Parse(time.DateOnly, d.GetDay())
		return err == nil
	}
	for _, d := range days {
		if valid(d) {
			peak = max(peak, d.GetChecked())
		}
	}
	for _, d := range days {
		day := d.GetDay()
		if !valid(d) {
			continue
		}
		s.Days = append(s.Days, ascGateDay{Day: day, Checked: d.GetChecked(), Held: d.GetHeld(),
			CheckedPct: ascagentui.Percent(uint64(d.GetChecked()), uint64(peak)), HeldPct: ascagentui.Percent(uint64(d.GetHeld()), uint64(peak))})
	}
	var checked, first, held []uint32
	for _, d := range s.Days {
		checked = append(checked, d.Checked)
		first = append(first, d.Checked-min(d.Held, d.Checked))
		held = append(held, d.Held)
	}
	s.SparkChecked, s.SparkFirst, s.SparkHeld = asctrust.Sparkline(checked, 80, 28), asctrust.Sparkline(first, 80, 28), asctrust.Sparkline(held, 80, 28)
	for _, rc := range r.GetPerRule() {
		if rule := ascagentui.CleanLine(rc.GetRule(), 120); rule != "" && len(s.Rules) < 64 {
			s.Rules = append(s.Rules, ascGateRuleCount{Rule: rule, Checked: rc.GetChecked(), Held: rc.GetHeld()})
		}
	}
	return s
}

func ascGateOverridesOf(in []*ascv1.GateOverride) []ascGateOverride {
	var out []ascGateOverride
	for _, o := range in {
		change := ascagentui.ChangeText(o.GetChange())
		if o.GetId() == 0 || change == "" || len(out) >= 50 {
			continue
		}
		proof := ""
		if p := o.GetProof(); len(p) >= 6 {
			const hexd = "0123456789abcdef"
			for _, b := range p[:6] {
				proof += string([]byte{hexd[b>>4], hexd[b&15]})
			}
		}
		out = append(out, ascGateOverride{
			ID: o.GetId(), Change: change, Rule: ascagentui.CleanLine(o.GetRule(), 120), Reason: ascagentui.CleanLine(o.GetReason(), 500),
			GrantedBy: ascagentui.SafeName(o.GetGrantedBy()), GrantedAt: ascagentui.Time(o.GetGrantedAt()),
			RevokedBy: ascagentui.SafeName(o.GetRevokedBy()), RevokedAt: ascagentui.Time(o.GetRevokedAt()),
			Active: o.GetRevokedAt() == 0, Proof: proof,
		})
	}
	return out
}

// ascGateLoadStats reads the statistics and the overrides after the page's own checks. A failure leaves them out.
func ascGateLoadStats(ctx *context.Context, p *ascGatePage, callCtx gocontext.Context, conn grpc.ClientConnInterface, name string, days int) {
	api := ascv1.NewReviewClient(conn)
	ref := &ascv1.RepoRef{Name: name}
	stats := ascGateStats{}
	got := false
	if resp, err := api.GetGateStats(callCtx, &ascv1.GetGateStatsRequest{Repo: ref, Days: uint32(days)}); err == nil {
		stats = ascGateStatsOf(resp)
		got = true
	} else {
		log.Warn("asc gate: GetGateStats failed: %s", ascagentuiLogLine(ascagentui.Classify(err)))
	}
	if resp, err := api.ListOverrides(callCtx, &ascv1.ListOverridesRequest{Repo: ref, Limit: 50}); err == nil {
		stats.Overrides = ascGateOverridesOf(resp.GetOverrides())
	} else {
		log.Warn("asc gate: ListOverrides failed: %s", ascagentuiLogLine(ascagentui.Classify(err)))
	}
	stats.CanOverride = ctx.Repo.IsAdmin() && !ctx.Repo.Repository.IsArchived
	stats.OverrideURL = p.GateURL + "/overrides"
	if got || len(stats.Overrides) > 0 {
		p.Stats = &stats
	}
}

// AscGateOverrideGrant serves `POST .../asc/gate/overrides`: a maintainer lets one change pass one rule, with a reason.
func AscGateOverrideGrant(ctx *context.Context) {
	repo, name, ok := trustweb.AdminRepo(ctx)
	if !ok {
		return
	}
	trustweb.NoStore(ctx)
	back := ascGateURL(repo)
	if repo.IsArchived {
		ctx.Error(http.StatusForbidden)
		return
	}
	change := ascagentui.ChangeBytes(ctx.FormTrim("change"))
	rule, okRule := ascagentui.CheckLine(ctx.FormTrim("rule"), 120, false)
	reason, okReason := ascagentui.CheckLine(ctx.FormTrim("reason"), 500, false)
	if change == nil || !okRule || !okReason {
		ctx.Flash.Error(ctx.Locale.TrString("asc_review.gate_override_invalid"))
		ctx.Redirect(back)
		return
	}
	conn, err := asctrust.Conn()
	if err != nil {
		ctx.Flash.Error(ctx.Locale.TrString("asc_trust.err.unavailable"))
		ctx.Redirect(back)
		return
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallRepoAdmin, repo)
	if err != nil {
		ctx.Flash.Error(trustweb.FailureOf("override credential", err).Localized(ctx))
		ctx.Redirect(back)
		return
	}
	defer release()
	if _, err := ascv1.NewReviewClient(conn).GrantOverride(callCtx, &ascv1.GrantOverrideRequest{Repo: &ascv1.RepoRef{Name: name}, Change: change, Rule: rule, Reason: reason}); err != nil {
		ctx.Flash.Error(trustweb.FailureOf("grant override", err).Localized(ctx))
		ctx.Redirect(back)
		return
	}
	ctx.Flash.Success(ctx.Locale.TrString("asc_review.gate_override_granted"))
	ctx.Redirect(back)
}

// AscGateOverrideRevoke serves `POST .../asc/gate/overrides/{id}/revoke`.
func AscGateOverrideRevoke(ctx *context.Context) {
	repo, name, ok := trustweb.AdminRepo(ctx)
	if !ok {
		return
	}
	trustweb.NoStore(ctx)
	back := ascGateURL(repo)
	id, err := strconv.ParseUint(ctx.Params("id"), 10, 53)
	if err != nil || id == 0 || repo.IsArchived {
		ctx.NotFound("asc gate override", nil)
		return
	}
	conn, err := asctrust.Conn()
	if err != nil {
		ctx.Flash.Error(ctx.Locale.TrString("asc_trust.err.unavailable"))
		ctx.Redirect(back)
		return
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallRepoAdmin, repo)
	if err != nil {
		ctx.Flash.Error(trustweb.FailureOf("override credential", err).Localized(ctx))
		ctx.Redirect(back)
		return
	}
	defer release()
	if _, err := ascv1.NewReviewClient(conn).RevokeOverride(callCtx, &ascv1.RevokeOverrideRequest{Repo: &ascv1.RepoRef{Name: name}, Id: id}); err != nil {
		ctx.Flash.Error(trustweb.FailureOf("revoke override", err).Localized(ctx))
		ctx.Redirect(back)
		return
	}
	ctx.Flash.Success(ctx.Locale.TrString("asc_review.gate_override_revoked"))
	ctx.Redirect(back)
}
