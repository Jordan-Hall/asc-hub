// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package byoweb

import (
	gocontext "context"
	"errors"
	"net/http"

	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/routers/web/shared/trustweb"
	svc "forgejo.org/services/ascbyo"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"

	"google.golang.org/grpc"
)

// Sharing an AI key and the warning and stop rules of its cap (docs/BUILDOUT.md section 31), on the page of a key of
// an account or an organization, and the keys shared with the owner of the page on the list.
//
//	POST .../asc/ai/keys/{id}/shares                   share the key (fields kind, name, allow_use, see_spend)
//	POST .../asc/ai/keys/{id}/shares/{share}/revoke    take a share back
//	POST .../asc/ai/keys/{id}/caps                     the warning percentage and the stop rule (fields warn_percent, stop_at_cap)
//
// Unlike the calls of the rest of these pages, which are system calls checked by the scope, these are made as the person:
// the hub decides first (the account of the person, or an owner of the organization) and signs a claim, and asc decides
// again. A key of a repository cannot be shared (a repository key is the repository's), so a repository level has none
// of this.

// Sharing is the sharing section of the page of a key.
type Sharing struct {
	Data         *asctrust.KeySharing
	Problem      string
	ShareOp      string
	CapsOp       string
	RevokeOps    map[string]string
	GranteeKinds []string
}

// SharedList is the keys shared with the owner of the page.
type SharedList struct {
	Keys    []asctrust.SharedKeyRow
	Problem string
}

var errNoSharing = errors.New("byoweb: this level has no sharing")

// shareCall returns the connection and the context of a call made as the signed-in person for the owner of the level.
func shareCall(ctx *context.Context, lvl *Level) (grpc.ClientConnInterface, gocontext.Context, func(), error) {
	if ctx.Doer == nil {
		return nil, nil, nil, asctrust.ErrNoDoer
	}
	conn, err := asctrust.Conn()
	if err != nil {
		return nil, nil, nil, err
	}
	var (
		callCtx gocontext.Context
		release func()
	)
	switch lvl.Scope.Kind {
	case svc.KindUser:
		if lvl.Scope.Name != ctx.Doer.LowerName {
			return nil, nil, nil, asctrust.ErrForbidden
		}
		callCtx, release, err = asctrust.UserCall(ctx, ctx.Doer, asctrust.CallAccount, nil)
	case svc.KindOrg:
		org, gerr := user_model.GetUserByName(ctx, lvl.Scope.Name)
		if gerr != nil || !org.IsOrganization() {
			return nil, nil, nil, asctrust.ErrForbidden
		}
		callCtx, release, err = asctrust.UserCallOrg(ctx, ctx.Doer, org)
	case svc.KindRepo:
		return nil, nil, nil, errNoSharing
	default:
		return nil, nil, nil, errNoSharing
	}
	if err != nil {
		return nil, nil, nil, err
	}
	return conn, callCtx, release, nil
}

// loadSharing fills the sharing section of the page of key id. It is best effort: a server without the service, or
// a failure, leaves the section out or says why in it, and never fails the page of the key.
func loadSharing(ctx *context.Context, lvl *Level, p *Page, id string) {
	if lvl.Scope.Kind == svc.KindRepo || !asctrust.HasFeature(ctx, asctrust.FeatureAiKeyShares) {
		return
	}
	sh := &Sharing{ShareOp: svc.NewOpID(), CapsOp: svc.NewOpID(), RevokeOps: map[string]string{}, GranteeKinds: asctrust.GranteeKinds}
	p.Sharing = sh
	conn, callCtx, release, err := shareCall(ctx, lvl)
	if err != nil {
		sh.Problem = trustweb.FailureOf("ai shares credential", err).Localized(ctx)
		return
	}
	defer release()
	data, err := asctrust.LoadKeySharing(callCtx, conn, id)
	if err != nil {
		sh.Problem = trustweb.FailureOf("ai shares load", err).Localized(ctx)
		return
	}
	sh.Data = data
	for _, s := range data.Shares {
		sh.RevokeOps[s.ID] = svc.NewOpID()
	}
}

// loadShared fills the list of keys shared with the owner of the page, best effort.
func loadShared(ctx *context.Context, lvl *Level, p *Page) {
	if lvl.Scope.Kind == svc.KindRepo || !asctrust.HasFeature(ctx, asctrust.FeatureAiKeyShares) {
		return
	}
	list := &SharedList{}
	p.Shared = list
	conn, callCtx, release, err := shareCall(ctx, lvl)
	if err != nil {
		list.Problem = trustweb.FailureOf("ai shared credential", err).Localized(ctx)
		return
	}
	defer release()
	kind := "user"
	if lvl.Scope.Kind == svc.KindOrg {
		kind = "org"
	}
	keys, err := asctrust.ListSharedWithMe(callCtx, conn, kind, lvl.Scope.Name)
	if err != nil {
		list.Problem = trustweb.FailureOf("ai shared load", err).Localized(ctx)
		return
	}
	list.Keys = keys
}

// shareAction runs a change of the sharing of key id and goes back to the page of the key with a message.
func shareAction(ctx *context.Context, lvl *Level, run func(callCtx gocontext.Context, conn grpc.ClientConnInterface, id string) (string, error)) {
	if _, p, ok := begin(ctx, lvl, dataAI); !ok || p == nil {
		return
	}
	id, ok := keyID(ctx)
	if !ok {
		return
	}
	back := lvl.Base + "/ai/keys/" + id
	conn, callCtx, release, err := shareCall(ctx, lvl)
	if err != nil {
		if errors.Is(err, errNoSharing) {
			ctx.NotFound("asc ai shares", nil)
			return
		}
		ctx.Flash.Error(trustweb.FailureOf("ai shares credential", err).Localized(ctx))
		ctx.Redirect(back)
		return
	}
	defer release()
	done, err := run(callCtx, conn, id)
	if err != nil {
		f := trustweb.FailureOf("ai shares change", err)
		if f.Status == http.StatusNotFound {
			log.Debug("asc byo: shares of a key that is not the scope's: %v", lvl.Scope)
		}
		ctx.Flash.Error(f.Localized(ctx))
		ctx.Redirect(back)
		return
	}
	ctx.Flash.Success(done)
	ctx.Redirect(back)
}

// AIShare is POST .../asc/ai/keys/{id}/shares.
func AIShare(ctx *context.Context, lvl *Level) {
	shareAction(ctx, lvl, func(callCtx gocontext.Context, conn grpc.ClientConnInterface, id string) (string, error) {
		f := form{ctx}
		_, err := asctrust.ShareKey(callCtx, conn, id, f.str("kind", 8), f.str("name", 200), f.on("allow_use"), f.on("see_spend"), f.op())
		return ctx.Locale.TrString("asc_shell.ks_shared"), err
	})
}

// AIShareRevoke is POST .../asc/ai/keys/{id}/shares/{share}/revoke.
func AIShareRevoke(ctx *context.Context, lvl *Level) {
	shareAction(ctx, lvl, func(callCtx gocontext.Context, conn grpc.ClientConnInterface, id string) (string, error) {
		return ctx.Locale.TrString("asc_shell.ks_revoked"), asctrust.RevokeShare(callCtx, conn, id, ctx.Params(":share"), form{ctx}.op())
	})
}

// AICaps is POST .../asc/ai/keys/{id}/caps.
func AICaps(ctx *context.Context, lvl *Level) {
	shareAction(ctx, lvl, func(callCtx gocontext.Context, conn grpc.ClientConnInterface, id string) (string, error) {
		f := form{ctx}
		_, err := asctrust.SetKeyCaps(callCtx, conn, id, f.str("warn_percent", 8), f.on("stop_at_cap"), f.op())
		return ctx.Locale.TrString("asc_shell.ks_caps_saved"), err
	})
}
