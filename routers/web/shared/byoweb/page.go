// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package byoweb serves the "Storage" and "AI keys" settings pages of a user, an organisation and a
// repository. The three levels differ only in who may open the page and which scope the page edits; the
// per-level packages decide that, build a Level, and call the handlers here.
//
// # Trust
//
// A Level is built by code that has already checked, from Forgejo's own permissions, that the signed-in user
// administers the scope. Nothing in a request (a form value, a query string) can change the scope: it comes
// from the route. Resources named by id in a form are checked against the scope by services/ascbyo before
// anything is sent on.
//
// # Secrets
//
// The credential fields are write-only. They are read from the form, passed to asc and dropped. A page that
// is shown again after a failed submit has them empty; a page never prints a secret, a sealed value or a
// full key id, only the hint asc returns. Every response of these pages is sent with Cache-Control: no-store.
package byoweb

import (
	"net/http"

	"forgejo.org/modules/base"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	svc "forgejo.org/services/ascbyo"
	"forgejo.org/services/context"
)

// Level is one place the pages are served from.
type Level struct {
	// Scope is the owner whose targets and keys the pages edit.
	Scope svc.Scope
	// Base is the link of the settings area of the page, without a trailing slash, for example
	// "/user/settings/asc" or "/alice/widgets/settings/asc".
	Base string
	// StorageTpl and AITpl are the templates that wrap the shared content in the settings layout.
	StorageTpl base.TplName
	AITpl      base.TplName
	// Allowed lists, for a repository, the scopes whose targets the repository may use: itself, and its
	// owner when the user administers the owner. It is empty for the other levels.
	Allowed []svc.Scope
	// OwnerStorageURL and OwnerAIURL are the pages of the repository's owner, or "" when the user may not
	// open them.
	OwnerStorageURL string
	OwnerAIURL      string
	// ScopeLabel is the name shown to people (the user name, the organisation name, "owner/repo").
	ScopeLabel string
}

// Problem is what a failed action shows at the top of the page. All three strings are escaped by the
// template; Detail is text asc wrote for the person, already cleaned.
type Problem struct {
	Message string
	Reason  string
	Detail  string
}

// Page is the template data under .Asc.
type Page struct {
	Kind       string // "user", "org" or "repo"
	ScopeLabel string
	Base       string
	StorageURL string
	AIURL      string
	// Unavailable is set when asc could not be asked; Unsupported when it answered UNIMPLEMENTED.
	Unavailable bool
	Unsupported bool
	Problem     *Problem
	Errors      svc.FieldErrors

	// Storage
	Targets         []svc.TargetView
	Target          *svc.TargetView
	Groups          []TargetGroup
	Usable          []UsableTarget
	Migrations      []*svc.Migration
	ActiveMigration *svc.Migration
	StorageForm     StorageForm
	Verify          *VerifyView
	OwnerStorageURL string

	// AI keys
	Keys         []svc.KeyView
	Key          *svc.KeyView
	AIForm       AIForm
	Providers    []ProviderOption
	Models       []svc.Model
	UsageByModel []svc.UsageRowView
	UsageByDay   []svc.UsageRowView
	UsageTotal   svc.UsageRowView
	UsageLoaded  bool
	Test         *TestView
	OwnerAIURL   string

	// Sharing of the key and the keys shared with the owner of the page (aishares.go); nil when the level or the
	// server has none.
	Sharing *Sharing
	Shared  *SharedList

	ops map[string]string
}

// Op returns the op id of the form called name, made on first use. Every form of a page gets its own, so
// two forms never share an answer; one form submitted twice is one change.
func (p *Page) Op(name string) string {
	if p.ops == nil {
		p.ops = map[string]string{}
	}
	if _, ok := p.ops[name]; !ok {
		p.ops[name] = svc.NewOpID()
	}
	return p.ops[name]
}

// StorageForm holds the non-secret values of the add and edit forms. Credentials are never in it.
type StorageForm struct {
	Name, Endpoint, Region, Bucket, Prefix, Addressing string
}

// AIForm holds the non-secret values of the add and edit forms. The key is never in it.
type AIForm struct {
	Label, Provider, BaseURL, DefaultModel, CapUSD string
}

// ProviderOption is an entry of the provider select.
type ProviderOption struct {
	Value string
	Label string
	Sel   bool
}

// TargetGroup is the targets of one scope in the repository's list.
type TargetGroup struct {
	Title   string
	Manage  bool // the targets are edited on this page
	URL     string
	Targets []svc.TargetView
}

// UsableTarget is an entry of the target select of a repository (an ENABLED target it may use).
type UsableTarget struct {
	ID    string
	Label string
}

// VerifyView is the result of a verification as shown.
type VerifyView struct {
	OK     bool
	Checks []VerifyStep
}

// VerifyStep is one line of a verification.
type VerifyStep struct {
	Name    string
	OK      bool
	Latency uint32
	Message string
}

// TestView is the result of a key test as shown.
type TestView struct {
	OK      bool
	Status  string
	Latency uint32
	Message string
}

const (
	// The settings highlighted in the navigation.
	dataStorage = "PageIsSettingsAscStorage"
	dataAI      = "PageIsSettingsAscAI"
)

// begin prepares a response: it refuses when [asc] is off, keeps the page out of every cache and returns the
// service. A nil service with ok=true means asc cannot be reached; the caller shows that.
func begin(ctx *context.Context, lvl *Level, nav string) (s *svc.Service, p *Page, ok bool) {
	if !setting.Asc.Enabled || ctx.Doer == nil || lvl == nil || !lvl.Scope.Valid() {
		ctx.NotFound("asc byo", nil)
		return nil, nil, false
	}
	h := ctx.Resp.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("Pragma", "no-cache")
	ctx.Data[nav] = true
	ctx.Data["PageIsSettingsAsc"] = true
	p = &Page{
		Kind:            lvl.Scope.Kind.String(),
		ScopeLabel:      lvl.ScopeLabel,
		Base:            lvl.Base,
		StorageURL:      lvl.Base + "/storage",
		AIURL:           lvl.Base + "/ai",
		OwnerStorageURL: lvl.OwnerStorageURL,
		OwnerAIURL:      lvl.OwnerAIURL,
		Errors:          svc.FieldErrors{},
	}
	ctx.Data["Asc"] = p
	s, err := svc.New()
	if err != nil {
		p.Unavailable = true
		return nil, p, true
	}
	// The service is bound to the signed-in person: op ids are derived from the user, so one person cannot
	// replay the answer of another.
	return s.ForUser(ctx.Doer.ID), p, true
}

// problemOf turns a failure into the text of the box at the top of the page.
func problemOf(ctx *context.Context, err error) *Problem {
	f, ok := svc.AsFailure(err)
	if !ok {
		f = &svc.Failure{Kind: svc.FailInternal}
	}
	pr := &Problem{Message: ctx.Locale.TrString(f.LocaleKey())}
	if k := f.ReasonLocaleKey(); k != "" {
		pr.Reason = ctx.Locale.TrString(k)
	}
	pr.Detail = f.Detail
	return pr
}

// statusOf is the HTTP status of a failed action.
func statusOf(err error) int {
	f, ok := svc.AsFailure(err)
	if !ok {
		return http.StatusInternalServerError
	}
	switch f.Kind {
	case svc.FailNotFound:
		return http.StatusNotFound
	case svc.FailDenied:
		return http.StatusForbidden
	case svc.FailUnavailable, svc.FailInternal, svc.FailUnimplemented:
		return http.StatusServiceUnavailable
	case svc.FailRateLimited:
		return http.StatusTooManyRequests
	case svc.FailExists, svc.FailInUse, svc.FailMigrationActive:
		return http.StatusConflict
	}
	return http.StatusUnprocessableEntity
}

// markAvailability records on the page why asc could not answer a read.
func markAvailability(p *Page, err error) {
	if svc.IsKind(err, svc.FailUnimplemented) {
		p.Unsupported = true
		return
	}
	p.Unavailable = true
}

// refuse answers a failed action. A missing resource is the plain 404 page (it looks the same as another
// owner's); everything else is shown on the page again by render.
func refuse(ctx *context.Context, err error, render func(status int)) {
	if svc.IsKind(err, svc.FailNotFound) {
		ctx.NotFound("asc byo", nil)
		return
	}
	if svc.IsKind(err, svc.FailDenied) {
		ctx.Error(http.StatusForbidden)
		return
	}
	render(statusOf(err))
}

// record logs who changed what. It names ids and the scope and never a value. asc writes the audit entry of
// the change itself; this line says which hub user asked for it.
func record(ctx *context.Context, lvl *Level, action, id string) {
	if ctx.Doer == nil {
		return
	}
	log.Info("asc byo: user %d (%s) %s %s in %s", ctx.Doer.ID, ctx.Doer.LowerName, action, id, lvl.Scope)
}

// form reads limited form values. A value over its limit is cut to one byte past it, which every
// validator then refuses on length.
type form struct{ ctx *context.Context }

func (f form) str(name string, limit int) string {
	v := f.ctx.Req.PostFormValue(name)
	if len(v) > limit {
		v = v[:limit+1]
	}
	return v
}

func (f form) on(name string) bool { return f.ctx.Req.PostFormValue(name) == "on" }

func (f form) op() string { return f.str("op_id", 64) }

// Field limits of the raw form values (a little above the validated limits, so a paste with whitespace
// around it still validates).
const (
	limName   = 80
	limURL    = svc.MaxEndpointBytes + 16
	limShort  = 128
	limPrefix = svc.MaxPrefixBytes + 16
	limKeyID  = svc.MaxAccessKeyIDBytes + 16
	limSecret = svc.MaxSecretKeyBytes + 16
	limToken  = svc.MaxSessionTokenBytes + 16
	limAPIKey = svc.MaxAPIKeyBytes + 16
	limCap    = 32
)
