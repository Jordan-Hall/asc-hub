// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"net/http"
	"time"

	"forgejo.org/modules/base"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/routers/web/shared/trustweb"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// The "Signing keys" page of a person (docs/BUILDOUT.md section 5): /user/settings/asc/keys.
//
// A signature is an SSH signature over a change, kept beside it, and it is Verified when a live key of the author
// made it. This page registers the public half of a key, lists the keys with their state, and revokes one. A key
// belongs to one person; the list is the signed-in person's own, and nothing in a request can name another owner.
// The calls are made as the person with a short-lived credential (asctrust.UserCall), so asc decides, and a key
// registered here is recorded under the person's name in the audit log.
//
// Only the public key is ever handled. The page asks for it as one authorized_keys line and checks it before
// sending (one line, no options, a kind asc accepts); a private key pasted by mistake is refused as "not a public
// key" and is neither echoed nor logged.
const (
	tplAscKeys base.TplName = "user/settings/asc_keys"

	maxAscKeysForm = 32 << 10
)

// ascKeysPage is the template data under .AscKeys.
type ascKeysPage struct {
	Page        *asctrust.KeysPage
	Title       string
	Expires     string
	AddOp       string
	RevokeOps   map[string]string
	Problem     string
	Unsupported bool
	KeysURL     string
	Namespace   string
}

func ascKeysPageFor(ctx *context.Context) *ascKeysPage {
	p := &ascKeysPage{AddOp: asctrust.NewOpID(), RevokeOps: map[string]string{}, KeysURL: setting.AppSubURL + "/user/settings/asc/keys", Namespace: asctrust.SigningNamespace}
	ctx.Data["Title"] = "Signing keys"
	ctx.Data["PageIsSettingsAscKeys"] = true
	ctx.Data["AscKeys"] = p
	trustweb.NoStore(ctx)
	return p
}

// ascKeysGuard fails closed: [asc] on, a signed-in person who is not a bot or a restricted placeholder.
func ascKeysGuard(ctx *context.Context) bool {
	if !setting.Asc.Enabled {
		ctx.NotFound("asc keys", nil)
		return false
	}
	if !ctx.IsSigned || ctx.Doer == nil {
		ctx.Error(http.StatusForbidden)
		return false
	}
	return true
}

// AscKeys serves GET /user/settings/asc/keys.
func AscKeys(ctx *context.Context) {
	if !ascKeysGuard(ctx) {
		return
	}
	p := ascKeysPageFor(ctx)
	ascKeysRender(ctx, p)
}

func ascKeysRender(ctx *context.Context, p *ascKeysPage) {
	status := http.StatusOK
	if p.Problem != "" {
		status = http.StatusBadRequest
	}
	if !asctrust.HasFeature(ctx, asctrust.FeatureSigning) {
		p.Unsupported = true
		ctx.HTML(http.StatusOK, tplAscKeys)
		return
	}
	conn, err := asctrust.Conn()
	if err != nil {
		log.Error("asc keys: no connection to asc serve: %v", err)
		p.Problem = "The ASC server is not reachable right now."
		ctx.HTML(http.StatusBadGateway, tplAscKeys)
		return
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallAccount, nil)
	if err != nil {
		f := trustweb.FailureOf("keys credential", err)
		p.Problem = f.Message
		ctx.HTML(f.Status, tplAscKeys)
		return
	}
	defer release()
	page, err := asctrust.LoadKeys(callCtx, conn, time.Now())
	if err != nil {
		f := trustweb.FailureOf("keys load", err)
		p.Problem = f.Message
		ctx.HTML(f.Status, tplAscKeys)
		return
	}
	p.Page = page
	for _, k := range page.Keys {
		if k.State == "live" {
			p.RevokeOps[k.ID] = asctrust.NewOpID()
		}
	}
	ctx.HTML(status, tplAscKeys)
}

// AscKeysAdd serves POST /user/settings/asc/keys with the fields title, public_key, expires and op.
func AscKeysAdd(ctx *context.Context) {
	if !ascKeysGuard(ctx) {
		return
	}
	if !trustweb.ParseForm(ctx, maxAscKeysForm) {
		return
	}
	p := ascKeysPageFor(ctx)
	p.Title = ctx.Req.PostFormValue("title")
	p.Expires = ctx.Req.PostFormValue("expires")
	publicKey := ctx.Req.PostFormValue("public_key")
	fail := func(problem string) {
		p.Problem = problem
		ascKeysRender(ctx, p)
	}
	if !asctrust.HasFeature(ctx, asctrust.FeatureSigning) {
		fail("This server does not have signed changes yet.")
		return
	}
	expires, err := asctrust.ExpiryOf(p.Expires, time.Now())
	if err != nil {
		fail(trustweb.FailureOf("keys add", err).Message)
		return
	}
	conn, err := asctrust.Conn()
	if err != nil {
		fail("The ASC server is not reachable right now.")
		return
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallAccount, nil)
	if err != nil {
		fail(trustweb.FailureOf("keys credential", err).Message)
		return
	}
	defer release()
	key, err := asctrust.AddKey(callCtx, conn, p.Title, publicKey, expires, ctx.Req.PostFormValue("op"))
	if err != nil {
		// The submitted key is not shown again: the person pastes it once more.
		fail(trustweb.FailureOf("keys add", err).Message)
		return
	}
	ctx.Flash.Success("The key " + key.Fingerprint + " was added.")
	ctx.Redirect(p.KeysURL)
}

// AscKeysRevoke serves POST /user/settings/asc/keys/revoke with the fields id, reason and op.
func AscKeysRevoke(ctx *context.Context) {
	if !ascKeysGuard(ctx) {
		return
	}
	if !trustweb.ParseForm(ctx, maxAscKeysForm) {
		return
	}
	p := ascKeysPageFor(ctx)
	conn, err := asctrust.Conn()
	if err != nil {
		ctx.Flash.Error("The ASC server is not reachable right now.")
		ctx.Redirect(p.KeysURL)
		return
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallAccount, nil)
	if err != nil {
		ctx.Flash.Error(trustweb.FailureOf("keys credential", err).Message)
		ctx.Redirect(p.KeysURL)
		return
	}
	defer release()
	key, err := asctrust.RevokeKey(callCtx, conn, ctx.Req.PostFormValue("id"), ctx.Req.PostFormValue("reason"), ctx.Req.PostFormValue("op"))
	if err != nil {
		ctx.Flash.Error(trustweb.FailureOf("keys revoke", err).Message)
		ctx.Redirect(p.KeysURL)
		return
	}
	ctx.Flash.Success("The key " + key.Fingerprint + " was revoked. Signatures sent after now with it are not Verified; earlier ones stay explainable.")
	ctx.Redirect(p.KeysURL)
}
