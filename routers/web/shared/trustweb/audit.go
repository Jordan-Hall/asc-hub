// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package trustweb

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/base"
	"forgejo.org/modules/log"
	"forgejo.org/services/asctrust"
	app_context "forgejo.org/services/context"
)

// AuditLevel is one place the audit log is served from: a repository, an organisation or the instance.
type AuditLevel struct {
	// Scope is what the log is of. It comes from the route, never from the request.
	Scope *ascv1.OwnerRef
	// Call is the kind of credential the call needs, and Repo the repository of a repository level.
	Call asctrust.CallKind
	// Org is the organisation of a CallOrg level.
	Org  *user_model.User
	Repo *repo_model.Repository
	// Base is the link of the audit page without a query string, for the pager, the export and the form.
	Base string
	// Label names the scope for the person ("owner/repo", an organisation, "this server").
	Label string
	// Tpl wraps the shared content in the layout of the level.
	Tpl base.TplName
	// Slug names the scope in a file name.
	Slug string
}

// AuditView is the template data under .AscAudit.
type AuditView struct {
	Base        string
	Label       string
	Query       asctrust.AuditQuery
	Rows        []asctrust.AuditRow
	Actions     []asctrust.ActionInfo
	NextURL     string
	FirstURL    string
	ExportJSONL string
	ExportCSV   string
	Problems    []string
	Error       string
	Unsupported bool
	Chain       *asctrust.ChainResult
	Searched    bool
	// RetentionDays is how long events are kept, as asc says (0: not said, or kept for ever).
	RetentionDays uint32
}

// AuditData is the key of the view in the template data.
const AuditData = "AscAudit"

var slugPattern = regexp.MustCompile(`[^a-z0-9._-]+`)

func slugOf(s string) string {
	s = slugPattern.ReplaceAllString(strings.ToLower(s), "-")
	s = strings.Trim(s, "-.")
	if s == "" {
		return "audit"
	}
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}

func (lvl *AuditLevel) link(q asctrust.AuditQuery, page, suffix string, extra map[string]string) string {
	v := url.Values{}
	for k, val := range q.Values() {
		v.Set(k, val)
	}
	if page != "" {
		v.Set("page", page)
	}
	for k, val := range extra {
		v.Set(k, val)
	}
	out := lvl.Base + suffix
	if len(v) > 0 {
		out += "?" + v.Encode()
	}
	return out
}

// EventURL is the audit page narrowed to the one event with the sequence number seq (the list shows only it).
func (v *AuditView) EventURL(seq uint64) string {
	return v.Base + "?seq=" + strconv.FormatUint(seq, 10)
}

func (lvl *AuditLevel) view(q asctrust.AuditQuery) *AuditView {
	return &AuditView{
		Base: lvl.Base, Label: lvl.Label, Query: q,
		FirstURL:    lvl.link(q, "", "", nil),
		ExportJSONL: lvl.link(q, "", "/export", map[string]string{"format": "jsonl"}),
		ExportCSV:   lvl.link(q, "", "/export", map[string]string{"format": "csv"}),
	}
}

// ServeAudit serves GET <base>: the filter form and one page of the log.
func ServeAudit(ctx *app_context.Context, lvl *AuditLevel) {
	NoStore(ctx)
	ctx.Data["Title"] = ctx.Locale.TrString("asc_trust.audit.title")
	q, filter, problems := asctrust.AuditQueryOf(ctx.FormString)
	v := lvl.view(q)
	ctx.Data[AuditData] = v

	if !asctrust.HasFeature(ctx, asctrust.FeatureAudit) {
		v.Unsupported = true
		ctx.HTML(http.StatusOK, lvl.Tpl)
		return
	}
	if len(problems) > 0 {
		v.Problems = problems
		ctx.HTML(http.StatusBadRequest, lvl.Tpl)
		return
	}
	conn, err := asctrust.Conn()
	if err != nil {
		log.Error("asc audit: no connection to asc serve: %v", err)
		v.Error = ctx.Locale.TrString("asc_trust.err.unavailable")
		ctx.HTML(http.StatusBadGateway, lvl.Tpl)
		return
	}
	callCtx, release, err := lvl.userCall(ctx)
	if err != nil {
		f := FailureOf("audit credential", err)
		v.Error = f.Localized(ctx)
		ctx.HTML(f.Status, lvl.Tpl)
		return
	}
	defer release()

	res, err := asctrust.ListAudit(callCtx, conn, lvl.Scope, q, filter)
	if err != nil {
		f := FailureOf("audit list", err)
		v.Error = f.Localized(ctx)
		if f.Status == http.StatusNotFound {
			// A scope the person may not see looks like a scope that does not exist.
			ctx.NotFound("asc audit", nil)
			return
		}
		ctx.HTML(f.Status, lvl.Tpl)
		return
	}
	v.Searched = true
	v.Rows = res.Page.Rows
	v.Actions = res.Actions
	v.RetentionDays = res.RetentionDays
	if res.Page.Next != "" {
		v.NextURL = lvl.link(q, res.Page.Next, "", nil)
	}
	ctx.HTML(http.StatusOK, lvl.Tpl)
}

// ServeAuditVerify serves POST <base>/verify: asc verifies the hash chain of the scope and the page shows the
// result. It changes nothing.
func ServeAuditVerify(ctx *app_context.Context, lvl *AuditLevel) {
	NoStore(ctx)
	ctx.Data["Title"] = ctx.Locale.TrString("asc_trust.audit.title")
	q := asctrust.AuditQuery{Limit: asctrust.AuditPageSizeDefault}
	v := lvl.view(q)
	ctx.Data[AuditData] = v
	if !asctrust.HasFeature(ctx, asctrust.FeatureAudit) {
		v.Unsupported = true
		ctx.HTML(http.StatusOK, lvl.Tpl)
		return
	}
	conn, err := asctrust.Conn()
	if err != nil {
		v.Error = ctx.Locale.TrString("asc_trust.err.unavailable")
		ctx.HTML(http.StatusBadGateway, lvl.Tpl)
		return
	}
	callCtx, release, err := lvl.userCall(ctx)
	if err != nil {
		f := FailureOf("audit credential", err)
		v.Error = f.Localized(ctx)
		ctx.HTML(f.Status, lvl.Tpl)
		return
	}
	defer release()
	chain, err := asctrust.VerifyAuditChain(callCtx, conn, lvl.Scope)
	if err != nil {
		f := FailureOf("audit verify", err)
		if f.Status == http.StatusNotFound {
			ctx.NotFound("asc audit verify", nil)
			return
		}
		v.Error = f.Localized(ctx)
		ctx.HTML(f.Status, lvl.Tpl)
		return
	}
	v.Chain = chain
	// The page below the result is the first page of the log.
	if res, err := asctrust.ListAudit(callCtx, conn, lvl.Scope, q, &ascv1.AuditFilter{}); err == nil {
		v.Searched = true
		v.Rows = res.Page.Rows
		v.Actions = res.Actions
		if res.Page.Next != "" {
			v.NextURL = lvl.link(q, res.Page.Next, "", nil)
		}
	}
	ctx.HTML(http.StatusOK, lvl.Tpl)
}

// ServeAuditExport serves GET <base>/export?format=jsonl|csv with the filters of the page. The export is
// received into a scratch file and checked against its digest before a byte goes to the browser, so a download
// that arrives is a download that matched; a mismatch is an error page.
func ServeAuditExport(ctx *app_context.Context, lvl *AuditLevel) {
	NoStore(ctx)
	_, filter, problems := asctrust.AuditQueryOf(ctx.FormString)
	if len(problems) > 0 {
		ctx.Error(http.StatusBadRequest, problems[0])
		return
	}
	var csv bool
	switch ctx.FormString("format") {
	case "", "jsonl":
	case "csv":
		csv = true
	default:
		ctx.Error(http.StatusBadRequest, ctx.Locale.TrString("asc_trust.audit.err_format"))
		return
	}
	if !asctrust.HasFeature(ctx, asctrust.FeatureAudit) {
		ctx.Error(http.StatusNotImplemented, ctx.Locale.TrString("asc_trust.audit.unsupported"))
		return
	}
	conn, err := asctrust.Conn()
	if err != nil {
		ctx.Error(http.StatusBadGateway, ctx.Locale.TrString("asc_trust.err.unavailable"))
		return
	}
	callCtx, release, err := lvl.userCall(ctx)
	if err != nil {
		f := FailureOf("audit export credential", err)
		ctx.Error(f.Status, f.Localized(ctx))
		return
	}
	defer release()
	streamCtx, cancel := context.WithTimeout(callCtx, asctrust.ExportTimeout)
	defer cancel()

	stream, err := asctrust.StartAuditExport(streamCtx, conn, lvl.Scope, filter, csv)
	if err != nil {
		f := FailureOf("audit export", err)
		ctx.Error(f.Status, f.Localized(ctx))
		return
	}
	tmp, err := os.CreateTemp("", "asc-audit-*")
	if err != nil {
		log.Error("asc audit export: scratch file: %v", err)
		ctx.Error(http.StatusInternalServerError, ctx.Locale.TrString("asc_trust.audit.err_export"))
		return
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()
	if _, err := asctrust.CopyExport(stream, tmp); err != nil {
		f := FailureOf("audit export copy", err)
		ctx.Error(f.Status, f.Localized(ctx))
		return
	}
	size, err := tmp.Seek(0, io.SeekCurrent)
	if err != nil {
		ctx.Error(http.StatusInternalServerError, ctx.Locale.TrString("asc_trust.audit.err_export"))
		return
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		ctx.Error(http.StatusInternalServerError, ctx.Locale.TrString("asc_trust.audit.err_export"))
		return
	}
	ext, ctype := "jsonl", "application/x-ndjson"
	if csv {
		ext, ctype = "csv", "text/csv"
	}
	name := "asc-audit-" + slugOf(lvl.Slug) + "-" + time.Now().UTC().Format("20060102") + "." + ext
	opts := &app_context.ServeHeaderOptions{ContentType: ctype, ContentTypeCharset: "utf-8", Filename: name, Disposition: "attachment", ContentLength: &size}
	ctx.SetServeHeaders(opts)
	ctx.Resp.Header().Set("Cache-Control", "no-store")
	ctx.Resp.Header().Set("X-Content-Type-Options", "nosniff")
	ctx.Resp.WriteHeader(http.StatusOK)
	if ctx.Req.Method != http.MethodHead {
		if _, err := io.Copy(ctx.Resp, tmp); err != nil {
			log.Warn("asc audit export: the browser went away: %v", err)
		}
	}
}

// userCall makes the credential of the person for the level.
func (lvl *AuditLevel) userCall(ctx *app_context.Context) (context.Context, func(), error) {
	if lvl.Call == asctrust.CallOrg {
		return asctrust.UserCallOrg(ctx, ctx.Doer, lvl.Org)
	}
	return asctrust.UserCall(ctx, ctx.Doer, lvl.Call, lvl.Repo)
}
