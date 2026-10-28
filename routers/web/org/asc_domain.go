// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package org

import (
	"errors"
	"net/http"

	user_model "forgejo.org/models/user"
	"forgejo.org/modules/base"
	"forgejo.org/modules/log"
	"forgejo.org/services/ascorgverify"
	"forgejo.org/services/context"
)

const tplSettingsDomain base.TplName = "org/settings/asc_domain"

// SettingsDomain shows the page where an owner claims a domain for the organization and proves it with a DNS record.
func SettingsDomain(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("asc_work.org_domain")
	ctx.Data["PageIsOrgSettings"] = true
	ctx.Data["PageIsSettingsDomain"] = true
	d, err := user_model.GetAscOrgDomain(ctx, ctx.Org.Organization.ID)
	if err != nil {
		ctx.ServerError("GetAscOrgDomain", err)
		return
	}
	if d != nil {
		ctx.Data["Domain"] = d
		ctx.Data["DomainVerified"] = d.VerifiedUnix > 0
		ctx.Data["RecordName"] = ascorgverify.RecordName(d.Domain)
		ctx.Data["RecordValue"] = ascorgverify.RecordValue(d)
	}
	ctx.HTML(http.StatusOK, tplSettingsDomain)
}

// SettingsDomainPost claims a domain, checks the record, or removes the claim. Owners only (the route says so).
func SettingsDomainPost(ctx *context.Context) {
	orgID := ctx.Org.Organization.ID
	link := ctx.Org.OrgLink + "/settings/domain"
	switch ctx.FormString("do") {
	case "claim":
		if _, err := user_model.SetAscOrgDomain(ctx, orgID, ctx.FormString("domain")); err != nil {
			ctx.Flash.Error(err.Error())
		}
	case "verify":
		d, err := user_model.GetAscOrgDomain(ctx, orgID)
		if err != nil || d == nil {
			ctx.Redirect(link)
			return
		}
		ok, err := ascorgverify.Check(ctx, d)
		switch {
		case ok:
			ctx.Flash.Success(ctx.Tr("asc_work.org_domain_verified"))
		case errors.Is(err, ascorgverify.ErrNotFound):
			ctx.Flash.Error(ctx.Tr("asc_work.org_domain_not_found"))
		case errors.Is(err, user_model.ErrAscDomainTaken):
			ctx.Flash.Error(ctx.Tr("asc_work.org_domain_taken"))
		default:
			log.Warn("asc org domain: %s of organization %d: %v", d.Domain, orgID, err)
			ctx.Flash.Error(ctx.Tr("asc_work.org_domain_lookup_failed"))
		}
	case "remove":
		if err := user_model.ClearAscOrgDomain(ctx, orgID); err != nil {
			ctx.ServerError("ClearAscOrgDomain", err)
			return
		}
	default:
		ctx.Error(http.StatusBadRequest)
		return
	}
	ctx.Redirect(link)
}
