// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	issues_model "forgejo.org/models/issues"
	"forgejo.org/modules/json"
	"forgejo.org/modules/log"
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
	//   Pull requests the user snoozed are left out unless include_snoozed is set. The response carries an
	//   ETag; a request with a matching If-None-Match gets 304 without a body.
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
	// - name: If-None-Match
	//   in: header
	//   description: an ETag from an earlier response; the answer is 304 when the page and the total are unchanged
	//   type: string
	// responses:
	//   "200":
	//     "$ref": "#/responses/AscInboxEntryList"
	//   "304":
	//     description: the page and the total are the same as the ETag in If-None-Match
	//   "401":
	//     "$ref": "#/responses/unauthorized"
	//   "403":
	//     "$ref": "#/responses/forbidden"
	//   "404":
	//     "$ref": "#/responses/notFound"
	//   "422":
	//     "$ref": "#/responses/validationError"

	entries, ok := loadAscInbox(ctx, "ListAscInbox")
	if !ok {
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
	serveAscInboxJSON(ctx, out, int64(len(entries)))
}

// CountAscInbox counts the inbox of the authenticated user, in total and by section.
func CountAscInbox(ctx *context.APIContext) {
	// swagger:operation GET /user/asc/inbox/count user userCurrentCountAscInbox
	// ---
	// summary: Count the pull requests in the inbox of the authenticated user, in total and by section
	// description: Counts the same pull requests as the inbox list, with the same section and include_snoozed
	//   filters; page and limit are ignored. The total is also sent in X-Total-Count. The response carries an
	//   ETag; a request with a matching If-None-Match gets 304 without a body, so a client can poll cheaply.
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
	//   description: also count the pull requests the user snoozed
	//   type: boolean
	// - name: If-None-Match
	//   in: header
	//   description: an ETag from an earlier response; the answer is 304 when the counts are unchanged
	//   type: string
	// responses:
	//   "200":
	//     "$ref": "#/responses/AscInboxCount"
	//   "304":
	//     description: the counts are the same as the ETag in If-None-Match
	//   "401":
	//     "$ref": "#/responses/unauthorized"
	//   "403":
	//     "$ref": "#/responses/forbidden"
	//   "404":
	//     "$ref": "#/responses/notFound"
	//   "422":
	//     "$ref": "#/responses/validationError"

	entries, ok := loadAscInbox(ctx, "CountAscInbox")
	if !ok {
		return
	}
	// loadAscInbox has already refused a bad section value, so this cannot fail.
	sections, _ := ascInboxSections(ctx.FormStrings("section"))

	out := &api.AscInboxCount{Total: int64(len(entries)), Sections: map[string]int64{}}
	for _, s := range ascinbox.SectionOrder {
		if len(sections) == 0 || slices.Contains(sections, s) {
			out.Sections[string(s)] = 0
		}
	}
	for _, e := range entries {
		out.Sections[string(e.Section)]++
	}
	ctx.SetTotalCountHeader(out.Total)
	serveAscInboxJSON(ctx, out, out.Total)
}

// loadAscInbox lists the inbox of the authenticated doer with the filters of the request, or answers the
// error and returns false. Every inbox endpoint goes through it, so none can skip the checks: the
// feature is on, the caller is signed in, the section values are valid, and the token's reducer decides
// read access per pull request.
func loadAscInbox(ctx *context.APIContext, title string) ([]*ascinbox.Entry, bool) {
	if !setting.Asc.Enabled {
		ctx.NotFound()
		return nil, false
	}
	doer := ctx.Doer()
	if doer == nil || !ctx.IsSigned() {
		ctx.Error(http.StatusUnauthorized, title, "token is required")
		return nil, false
	}
	sections, err := ascInboxSections(ctx.FormStrings("section"))
	if err != nil {
		ctx.Error(http.StatusUnprocessableEntity, "section", err)
		return nil, false
	}
	reducer := ctx.Reducer()
	if reducer == nil {
		ctx.Error(http.StatusInternalServerError, title, ascinbox.ErrNoReducer)
		return nil, false
	}

	entries, err := ascinbox.List(ctx, doer, reducer, time.Now(), ascinbox.Options{
		Sections:       sections,
		IncludeSnoozed: ctx.FormBool("include_snoozed"),
	})
	if err != nil {
		ctx.Error(http.StatusInternalServerError, "ascinbox.List", err)
		return nil, false
	}
	return entries, true
}

// serveAscInboxJSON answers with v as JSON and an ETag over the exact body and total, or with 304 and no
// body when If-None-Match already names that ETag. The body is the doer's own, so the response is private
// and varies with the credentials.
func serveAscInboxJSON(ctx *context.APIContext, v any, total int64) {
	body, err := json.Marshal(v)
	if err != nil {
		ctx.Error(http.StatusInternalServerError, "serveAscInboxJSON", err)
		return
	}
	body = append(body, '\n')
	etag := ascInboxETag(body, total)

	h := ctx.RespHeader()
	h.Set("ETag", etag)
	h.Add("Vary", "Authorization, Cookie")
	ctx.AppendAccessControlExposeHeaders("ETag")
	if ascInboxETagMatches(ctx.Req.Header.Values("If-None-Match"), etag) {
		ctx.Status(http.StatusNotModified)
		return
	}
	h.Set("Content-Type", "application/json;charset=utf-8")
	ctx.Status(http.StatusOK)
	if _, err := ctx.Write(body); err != nil {
		log.Error("asc inbox: write failed: %v", err)
	}
}

// ascInboxETag is the strong validator of a body and the total sent with it.
func ascInboxETag(body []byte, total int64) string {
	h := sha256.New()
	h.Write(body)
	h.Write([]byte{0})
	h.Write([]byte(strconv.FormatInt(total, 10)))
	return `"` + hex.EncodeToString(h.Sum(nil)[:16]) + `"`
}

// ascInboxETagMatches reports whether the If-None-Match values (one per header line, each a comma separated
// list) name etag: `*` matches, and a weak tag matches its strong form (weak comparison, RFC 9110 13.1.2).
func ascInboxETagMatches(values []string, etag string) bool {
	for _, value := range values {
		for item := range strings.SplitSeq(value, ",") {
			item = strings.TrimSpace(item)
			if item == "*" || strings.TrimPrefix(item, "W/") == etag {
				return true
			}
		}
	}
	return false
}
