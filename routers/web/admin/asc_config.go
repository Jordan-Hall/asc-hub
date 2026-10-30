// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	"strconv"
	"strings"

	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/routers/web/shared/trustweb"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// The runtime settings that live in asc serve rather than in the hub's database. They are saved from the configuration page
// with the same call as /admin/asc/agents, so the two pages cannot disagree.
const (
	ascAllowAgentsKey   = "asc.allow_agents"
	ascDefaultBudgetKey = "asc.default_budget"
)

// ascRuntime is what the configuration page shows of those settings.
type ascRuntime struct {
	// Available is false when the hub has no asc serve (or one without the OrgSettings service): the rows are left out.
	Available bool
	// Problem is the sentence for a failed load; the rows then show nothing to change.
	Problem  string
	Settings asctrust.InstanceAgents
}

func isAscConfigKey(key string) bool { return strings.HasPrefix(key, "asc.") }

// loadAscRuntime reads the agent switch and the default budget of the server for the configuration page.
func loadAscRuntime(ctx *context.Context) *ascRuntime {
	r := &ascRuntime{}
	if !setting.Asc.Enabled || !asctrust.HasFeature(ctx, asctrust.FeatureOrgSettings) {
		return r
	}
	conn, err := asctrust.Conn()
	if err != nil {
		log.Error("admin config: no connection to asc serve: %v", err)
		r.Available, r.Problem = true, ctx.Locale.TrString("asc_trust.err.unavailable")
		return r
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallInstance, nil)
	if err != nil {
		r.Available, r.Problem = true, trustweb.FailureOf("config credential", err).Localized(ctx)
		return r
	}
	defer release()
	s, err := asctrust.LoadInstanceAgents(callCtx, conn)
	if err != nil {
		r.Available, r.Problem = true, trustweb.FailureOf("config load", err).Localized(ctx)
		return r
	}
	r.Available, r.Settings = true, *s
	return r
}

// changeAscConfig saves one of the asc settings. It changes only the field the key names: the other keeps the value asc has.
func changeAscConfig(ctx *context.Context, key, value string) {
	fail := func(msg string) { ctx.JSONError(msg) }
	if !setting.Asc.Enabled || !asctrust.HasFeature(ctx, asctrust.FeatureOrgSettings) {
		fail(ctx.Locale.TrString("admin.config.set_setting_failed", key))
		return
	}
	conn, err := asctrust.Conn()
	if err != nil {
		fail(ctx.Locale.TrString("asc_trust.err.unavailable"))
		return
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallInstance, nil)
	if err != nil {
		fail(trustweb.FailureOf("config credential", err).Localized(ctx))
		return
	}
	defer release()
	cur, err := asctrust.LoadInstanceAgents(callCtx, conn)
	if err != nil {
		fail(trustweb.FailureOf("config load", err).Localized(ctx))
		return
	}
	next := *cur
	switch key {
	case ascAllowAgentsKey:
		on, _ := strconv.ParseBool(value)
		next.AllowAgents = on
	case ascDefaultBudgetKey:
		micro, err := asctrust.ParseUSD(value)
		if err != nil {
			fail(trustweb.FailureOf("config budget", err).Localized(ctx))
			return
		}
		next.BudgetMicro = micro
	default:
		fail(ctx.Locale.TrString("admin.config.set_setting_failed", key))
		return
	}
	if _, err := asctrust.SetInstanceAgents(callCtx, conn, next, asctrust.NewOpID()); err != nil {
		fail(trustweb.FailureOf("config save", err).Localized(ctx))
		return
	}
	log.Info("admin %s set %s to %q", ctx.Doer.Name, key, value)
	ctx.JSONOK()
}

// configRow is one read-only setting of the configuration page: its key, what it is for and its value.
type configRow struct {
	Key, Label, Value string
}

// configCard is a group of read-only settings.
type configCard struct {
	ID, Title, Note string
	Rows            []configRow
}

// curatedConfig is the short list of server settings an administrator looks for first. Every value is read from the running
// configuration, never made up; the full list is behind "Show every setting".
func curatedConfig(ctx *context.Context) []configCard {
	tr := ctx.Locale.TrString
	ssh := tr("asc_admin.config_off")
	if !setting.SSH.Disabled {
		ssh = tr("asc_admin.config_on") + ", " + tr("asc_admin.config_port", setting.SSH.Port)
	}
	attach := string(setting.Attachment.Storage.Type)
	byo := tr("asc_admin.config_off")
	if setting.Asc.Enabled {
		byo = tr("asc_admin.config_on")
	}
	from, host := tr("asc_admin.config_not_set"), tr("asc_admin.config_not_set")
	if m := setting.MailService; m != nil {
		from = m.From
		switch {
		case m.Protocol == "sendmail" || m.Protocol == "dummy":
			host = m.Protocol
		case m.SMTPPort != "":
			host = m.SMTPAddr + ":" + m.SMTPPort
		default:
			host = m.SMTPAddr
		}
	}
	return []configCard{
		{ID: "server", Title: tr("asc_admin.config_card_server"), Note: tr("asc_admin.config_server_note"), Rows: []configRow{
			{"server.domain", tr("asc_admin.config_domain"), setting.Domain},
			{"server.protocol", tr("asc_admin.config_protocol"), string(setting.Protocol)},
			{"server.ssh", tr("asc_admin.config_ssh"), ssh},
		}},
		{ID: "storage", Title: tr("asc_admin.config_card_storage"), Note: tr("asc_admin.config_storage_note"), Rows: []configRow{
			{"storage.attachments", tr("asc_admin.config_storage_files"), attach},
			{"storage.byo", tr("asc_admin.config_storage_byo"), byo},
		}},
		{ID: "mail", Title: tr("asc_admin.config_card_mail"), Note: tr("asc_admin.config_mail_note"), Rows: []configRow{
			{"mail.from", tr("asc_admin.config_mail_from"), from},
			{"mail.host", tr("asc_admin.config_mail_host"), host},
		}},
	}
}
