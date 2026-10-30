// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"errors"
	"net/http"

	"forgejo.org/modules/log"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// The proposed tag and the notes of the next release, written by asc from the changes that landed since the previous release
// (Releases.Next and Releases.DraftNotes). GET /{owner}/{repo}/releases/asc_draft?from=&to= answers JSON for the release form
// ("Write from the N landed changes"). The route is for people who may write releases; the form that uses it belongs to the
// release pages.

// AscReleaseDraft handles GET /{owner}/{repo}/releases/asc_draft.
func AscReleaseDraft(ctx *context.Context) {
	draft, err := asctrust.DraftRelease(ctx, ctx.Repo.Repository, ctx.FormTrim("from"), ctx.FormTrim("to"))
	switch {
	case err == nil:
		ctx.Resp.Header().Set("Cache-Control", "private, no-store")
		ctx.JSON(http.StatusOK, draft)
	case errors.Is(err, asctrust.ErrDisabled):
		ctx.NotFound("not an asc repository", nil)
	case errors.Is(err, asctrust.ErrBadInput):
		ctx.Error(http.StatusBadRequest)
	default:
		log.Warn("asc release draft: %v", err)
		ctx.JSON(http.StatusServiceUnavailable, map[string]string{"error": "unavailable"})
	}
}

// ascReleaseRecords reads the asc records of the releases on the page once, for the provenance and the checksums. Without them the
// page shows what the hub knows.
func ascReleaseRecords(ctx *context.Context, infos []*ReleaseInfo) {
	if len(infos) == 0 {
		return
	}
	records := asctrust.ListReleaseRecords(ctx, ctx.Repo.Repository)
	for _, info := range infos {
		info.Asc = records[info.Release.TagName]
	}
}
