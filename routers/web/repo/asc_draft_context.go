// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"fmt"
	"net/http"
	"strings"

	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// "Draft from the diff" of a new change (board NewChange): the title and description are written from what asc knows of
// base..head (Review.DraftContext): the subjects of the changesets, the files with their size, the risk and its reasons. The
// draft is facts in plain words, not a model's prose; the person edits it. GET /{owner}/{repo}/asc/draft_context?base=&head=.

const (
	ascDraftMaxFiles = 12
	ascDraftTitleMax = 120
)

// AscDraft is the JSON the page script puts into the form.
type AscDraft struct {
	Title   string `json:"title"`
	Body    string `json:"body"`
	Nothing bool   `json:"nothing"`
}

// ascDraftOf writes the draft from the answer of asc. t translates the fixed words (keys under asc_review.draft_*).
func ascDraftOf(resp *ascv1.DraftContextResponse, t func(key string, args ...any) string) AscDraft {
	if resp.GetNothingToCompare() {
		return AscDraft{Nothing: true}
	}
	var msgs []string
	for _, m := range resp.GetMessages() {
		if s := asctrust.CleanText(m, 200); s != "" {
			msgs = append(msgs, s)
		}
	}
	d := AscDraft{}
	if len(msgs) > 0 {
		// newest first: the newest subject names the change when it is the only one, the oldest when there are several
		d.Title = msgs[len(msgs)-1]
		if len(msgs) == 1 {
			d.Title = msgs[0]
		}
		if r := []rune(d.Title); len(r) > ascDraftTitleMax {
			d.Title = string(r[:ascDraftTitleMax-3]) + "..."
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "### %s\n\n", t("asc_review.draft_what"))
	if len(msgs) == 0 {
		b.WriteString("-\n")
	}
	for _, m := range msgs {
		fmt.Fprintf(&b, "- %s\n", m)
	}
	fmt.Fprintf(&b, "\n### %s\n\n", t("asc_review.draft_size"))
	fmt.Fprintf(&b, "%s\n", t("asc_review.draft_size_line", resp.GetChangesets(), len(resp.GetFiles()), resp.GetAdded(), resp.GetRemoved()))
	for i, f := range resp.GetFiles() {
		if i >= ascDraftMaxFiles {
			fmt.Fprintf(&b, "- %s\n", t("asc_review.draft_more_files", len(resp.GetFiles())-ascDraftMaxFiles))
			break
		}
		p, ok := ascCleanPath(f.GetPath())
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "- `%s` (+%d -%d)\n", p, f.GetAdded(), f.GetRemoved())
	}
	if r := resp.GetRisk(); r != nil && r.GetLevel() != ascv1.RiskLevel_RISK_LEVEL_UNSPECIFIED {
		level := map[ascv1.RiskLevel]string{ascv1.RiskLevel_RISK_LEVEL_LOW: "low", ascv1.RiskLevel_RISK_LEVEL_MEDIUM: "medium", ascv1.RiskLevel_RISK_LEVEL_HIGH: "high"}[r.GetLevel()]
		fmt.Fprintf(&b, "\n### %s\n\n%s\n", t("asc_review.draft_risk"), t("asc_review.risk_"+level))
		for i, reason := range r.GetReasons() {
			if i >= 6 {
				break
			}
			if s := asctrust.CleanText(reason, 200); s != "" {
				fmt.Fprintf(&b, "- %s\n", s)
			}
		}
	}
	if resp.GetTruncated() {
		fmt.Fprintf(&b, "\n%s\n", t("asc_review.draft_truncated"))
	}
	d.Body = b.String()
	return d
}

// AscDraftContext handles GET /{owner}/{repo}/asc/draft_context.
func AscDraftContext(ctx *context.Context) {
	if !setting.Asc.Enabled || ctx.Repo.Repository.GetStorageBackend() != string(gitrepo.StorageBackendAsc) {
		ctx.NotFound("not an asc repository", nil)
		return
	}
	base, head := strings.TrimSpace(ctx.FormString("base")), strings.TrimSpace(ctx.FormString("head"))
	if len(base) > 255 || len(head) > 255 || strings.ContainsAny(base+head, "\x00\r\n") {
		ctx.Error(http.StatusBadRequest)
		return
	}
	name, err := ascGraphRepoName(ctx.Repo.Repository)
	if err != nil {
		ctx.NotFound("asc repository name", err)
		return
	}
	conn, err := ascRPCConn()
	if err != nil {
		ctx.JSON(http.StatusServiceUnavailable, map[string]string{"error": "unavailable"})
		return
	}
	// the reader has been checked by the route; the call is the hub's own, as for the brief
	callCtx, cancel := gocontext.WithTimeout(ascclient.WithSystem(ctx), ascBriefTimeout)
	defer cancel()
	resp, err := ascv1.NewReviewClient(conn).DraftContext(callCtx, &ascv1.DraftContextRequest{Repo: &ascv1.RepoRef{Name: name}, Base: base, Head: head})
	if err != nil {
		log.Warn("asc draft context: %v", err)
		ctx.JSON(http.StatusServiceUnavailable, map[string]string{"error": "unavailable"})
		return
	}
	ctx.Resp.Header().Set("Cache-Control", "private, no-store")
	ctx.JSON(http.StatusOK, ascDraftOf(resp, func(key string, args ...any) string { return ctx.Locale.TrString(key, args...) }))
}
