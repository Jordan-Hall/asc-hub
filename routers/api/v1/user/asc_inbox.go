// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"errors"
	"net/http"
	"strings"
	"time"

	issues_model "forgejo.org/models/issues"
	"forgejo.org/modules/setting"
	api "forgejo.org/modules/structs"
	"forgejo.org/routers/api/v1/utils"
	"forgejo.org/services/ascinbox"
	"forgejo.org/services/context"
)

// The inbox API (SPEC 20.8, PLAN unit C41b): `GET /api/v1/user/asc/inbox` lists the pull requests that need
// the attention of the authenticated user, each with the section that says whose turn it is and the user's
// own pin and snooze marks. It is the API twin of the inbox page (C41c) and uses the same service.
//
// Trust: the user is always the authenticated doer; no parameter names a user. Read access to the pull
// requests of each repository is checked through the token's authorization reducer, so a token limited to
// public repositories (or to some repositories) never lists a pull request outside them. Marks are
// the doer's own.

const (
	// ascInboxMaxSectionValues bounds the section values of one request.
	ascInboxMaxSectionValues = 16
	// ascInboxMaxSectionBytes bounds the size of one section value.
	ascInboxMaxSectionBytes = 32
)

var errAscInboxBadSection = errors.New("section must be one of needs_review, returned, ready, blocked, waiting, drafts, queued, merged")

// ascInboxSections reads the section filter: the `section` parameter, repeated or comma separated. An
// unknown value is an error; no value means every section.
func ascInboxSections(values []string) ([]issues_model.AscInboxSection, error) {
	if len(values) > ascInboxMaxSectionValues {
		return nil, errAscInboxBadSection
	}
	var out []issues_model.AscInboxSection
	seen := map[issues_model.AscInboxSection]bool{}
	for _, v := range values {
		if len(v) > ascInboxMaxSectionValues*ascInboxMaxSectionBytes {
			return nil, errAscInboxBadSection
		}
		for part := range strings.SplitSeq(v, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			s := issues_model.AscInboxSection(part)
			if len(part) > ascInboxMaxSectionBytes || !s.IsValid() {
				return nil, errAscInboxBadSection
			}
			if !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	return out, nil
}

// ascInboxPage returns the entries of the page of size pageSize (both already normalised, page from 1).
func ascInboxPage(entries []*ascinbox.Entry, page, pageSize int) []*ascinbox.Entry {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		return nil
	}
	skip := (page - 1) * pageSize
	if skip < 0 || skip >= len(entries) {
		return nil
	}
	end := min(skip+pageSize, len(entries))
	return entries[skip:end]
}

func toAscInboxEntry(e *ascinbox.Entry) *api.AscInboxEntry {
	out := &api.AscInboxEntry{
		PullRequestID: e.PullID,
		Number:        e.Index,
		Title:         e.Title,
		HTMLURL:       e.HTMLURL,
		Repository:    e.RepoFullName,
		Section:       string(e.Section),
		Pinned:        e.Pinned,
		Updated:       e.Updated.UTC(),
	}
	if !e.SnoozedUntil.IsZero() {
		t := e.SnoozedUntil.UTC()
		out.SnoozedUntil = &t
	}
	return out
}

// ListAscInbox lists the inbox of the authenticated user.
func ListAscInbox(ctx *context.APIContext) {
	// swagger:operation GET /user/asc/inbox user userCurrentListAscInbox
	// ---
	// summary: List the pull requests that need the attention of the authenticated user, by whose turn it is
	// description: The most recently updated 200 pull requests the user wrote or was asked to review are
	//   examined. Entries are ordered by section, then pinned first, then most recently updated first.
	//   Pull requests the user snoozed are left out unless include_snoozed is set.
	// produces:
	// - application/json
	// parameters:
	// - name: section
	//   in: query
	//   description: only these sections (needs_review, returned, ready, blocked, waiting, drafts, queued,
	//     merged); repeat the parameter or separate the values with commas
	//   type: string
	// - name: include_snoozed
	//   in: query
	//   description: also list the pull requests the user snoozed
	//   type: boolean
	// - name: page
	//   in: query
	//   description: page number of results to return (1-based)
	//   type: integer
	// - name: limit
	//   in: query
	//   description: page size of results
	//   type: integer
	// responses:
	//   "200":
	//     "$ref": "#/responses/AscInboxEntryList"
	//   "401":
	//     "$ref": "#/responses/unauthorized"
	//   "403":
	//     "$ref": "#/responses/forbidden"
	//   "404":
	//     "$ref": "#/responses/notFound"
	//   "422":
	//     "$ref": "#/responses/validationError"

	if !setting.Asc.Enabled {
		ctx.NotFound()
		return
	}
	doer := ctx.Doer()
	if doer == nil || !ctx.IsSigned() {
		ctx.Error(http.StatusUnauthorized, "ListAscInbox", "token is required")
		return
	}
	sections, err := ascInboxSections(ctx.FormStrings("section"))
	if err != nil {
		ctx.Error(http.StatusUnprocessableEntity, "section", err)
		return
	}
	reducer := ctx.Reducer()
	if reducer == nil {
		ctx.Error(http.StatusInternalServerError, "ListAscInbox", ascinbox.ErrNoReducer)
		return
	}

	entries, err := ascinbox.List(ctx, doer, reducer, time.Now(), ascinbox.Options{
		Sections:       sections,
		IncludeSnoozed: ctx.FormBool("include_snoozed"),
	})
	if err != nil {
		ctx.Error(http.StatusInternalServerError, "ascinbox.List", err)
		return
	}

	listOptions := utils.GetListOptions(ctx)
	listOptions.SetDefaultValues()
	pageEntries := ascInboxPage(entries, listOptions.Page, listOptions.PageSize)
	out := make([]*api.AscInboxEntry, 0, len(pageEntries))
	for _, e := range pageEntries {
		out = append(out, toAscInboxEntry(e))
	}

	ctx.SetLinkHeader(len(entries), listOptions.PageSize)
	ctx.SetTotalCountHeader(int64(len(entries)))
	ctx.JSON(http.StatusOK, out)
}
