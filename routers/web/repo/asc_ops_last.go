// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/http"

	"forgejo.org/modules/log"
	"forgejo.org/routers/web/shared/trustweb"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// ascUndoLastScan is how many of the person's newest operations are looked at to find one that can still be undone.
const ascUndoLastScan = 20

// AscOpsUndoLast serves GET /{owner}/{repo}/asc/ops/undo/last, the "Undo last operation" command of the palette: the
// newest operation of the person in this repository (on ref, when given) that can still be undone. It goes to the dry
// run and confirmation of that undo, so nothing is written here; with nothing to undo, to the person's operation log.
func AscOpsUndoLast(ctx *context.Context) {
	repo, name, ok := trustweb.CodeRepo(ctx, true)
	if !ok {
		return
	}
	trustweb.NoStore(ctx)
	mine := ascOpsURL(repo) + "?mine=1"
	if !asctrust.HasFeature(ctx, asctrust.FeatureStacks) {
		ctx.NotFound("asc undo last", nil)
		return
	}
	conn, err := asctrust.Conn()
	if err != nil {
		log.Error("asc undo last: no connection to asc serve: %v", err)
		ctx.Flash.Error(ctx.Locale.TrString("asc_trust.err.unavailable"))
		ctx.Redirect(mine)
		return
	}
	callCtx, release, err := trustweb.ReadCall(ctx, repo)
	if err != nil {
		ctx.Flash.Error(trustweb.FailureOf("undo last credential", err).Localized(ctx))
		ctx.Redirect(mine)
		return
	}
	defer release()
	page, err := asctrust.LoadOpLog(callCtx, conn, name, ascUndoWho(ctx), asctrust.OpLogQuery{
		Ref: ctx.FormString("ref"), Mine: true, Limit: ascUndoLastScan,
	})
	if err != nil {
		f := trustweb.FailureOf("undo last", err)
		if f.Status == http.StatusNotFound {
			ctx.NotFound("asc undo last", nil)
			return
		}
		ctx.Flash.Error(f.Localized(ctx))
		ctx.Redirect(mine)
		return
	}
	for i := range page.Rows {
		if r := &page.Rows[i]; r.Mine && r.Undoable {
			ctx.Redirect(ascUndoURL(repo, r))
			return
		}
	}
	ctx.Flash.Info(ctx.Locale.TrString("asc_trust.ops.nothing_mine"))
	ctx.Redirect(mine)
}
