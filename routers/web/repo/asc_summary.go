// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// "Draft from the diff" with a model (feature review_summaries): asc reads the diff the person may read, asks the model
// with a key of the repository, its owner or the person (Review.SummarizeChange) and answers What, Why, How to check,
// Risk and up to five pointers. The call carries the claim of the person (asctrust.CallRepoWrite), so their path rules
// and their keys apply. The page puts the answer into the form; the person is the author and edits it.
//
// POST /{owner}/{repo}/asc/summarize (base, head, op): a new change, before it exists.
// POST /{owner}/{repo}/pulls/{index}/asc/brief/summarize (op): the brief of a stacked change.

const (
	// ascSummaryTimeout is long: the model writes the answer while the person waits.
	ascSummaryTimeout  = 90 * time.Second
	ascSummaryMaxText  = 2000
	ascSummaryMaxNote  = 300
	ascSummaryMaxWhere = 5
)

// AscSummaryPointer is one "where to look" pointer of a summary.
type AscSummaryPointer struct {
	Path string `json:"path"`
	Line uint32 `json:"line,omitempty"`
	Note string `json:"note,omitempty"`
}

// AscSummary is the JSON the page script puts into the form. Where is the text of the brief form's "where to look"
// field; Body is the description of a new change.
type AscSummary struct {
	Nothing   bool                `json:"nothing,omitempty"`
	What      string              `json:"what,omitempty"`
	Why       string              `json:"why,omitempty"`
	How       string              `json:"how,omitempty"`
	Risk      string              `json:"risk,omitempty"`
	Pointers  []AscSummaryPointer `json:"pointers,omitempty"`
	Where     string              `json:"where,omitempty"`
	Body      string              `json:"body,omitempty"`
	Truncated bool                `json:"truncated,omitempty"`
	Next      string              `json:"next"`
}

// ascSummaryRefusal is the JSON of a summary that was not written: a fixed reason, its sentence and the op id of the
// next click.
type ascSummaryRefusal struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Next    string `json:"next"`
}

// ascSummaryOf turns the answer of asc into the form's fields. Every text is cleaned; a pointer without a usable path
// is dropped. t translates the headings of the description.
func ascSummaryOf(resp *ascv1.SummarizeChangeResponse, t func(key string, args ...any) string) AscSummary {
	if resp.GetNothingToCompare() {
		return AscSummary{Nothing: true}
	}
	b := resp.GetBrief()
	s := AscSummary{
		What:      ascSummaryText(b.GetWhat(), ascSummaryMaxText),
		Why:       ascSummaryText(b.GetWhy(), ascSummaryMaxText),
		How:       ascSummaryText(b.GetHow(), ascSummaryMaxText),
		Risk:      ascSummaryText(b.GetRiskNote(), ascSummaryMaxText),
		Truncated: resp.GetTruncated(),
	}
	var where strings.Builder
	for _, w := range b.GetWhereToLook() {
		if len(s.Pointers) >= ascSummaryMaxWhere {
			break
		}
		path, ok := ascCleanPath(w.GetPath())
		if !ok || strings.ContainsAny(path, " \n") {
			continue
		}
		p := AscSummaryPointer{Path: path, Line: w.GetLine(), Note: asctrust.CleanText(w.GetNote(), ascSummaryMaxNote)}
		if p.Line > ascSymbolsMaxLine {
			p.Line = 0
		}
		s.Pointers = append(s.Pointers, p)
		where.WriteString(ascSummaryPointerText(p))
		where.WriteByte('\n')
	}
	s.Where = where.String()

	var body strings.Builder
	for _, sec := range []struct{ key, text string }{
		{"asc_rp.intent_what", s.What}, {"asc_rp.intent_why", s.Why}, {"asc_rp.intent_how", s.How}, {"asc_rp.intent_risk", s.Risk},
	} {
		if sec.text != "" {
			fmt.Fprintf(&body, "### %s\n\n%s\n\n", t(sec.key), sec.text)
		}
	}
	if len(s.Pointers) > 0 {
		fmt.Fprintf(&body, "### %s\n\n", t("asc_review.brief_where"))
		for _, p := range s.Pointers {
			loc := p.Path
			if p.Line > 0 {
				loc = fmt.Sprintf("%s:%d", p.Path, p.Line)
			}
			if p.Note != "" {
				fmt.Fprintf(&body, "- `%s` %s\n", loc, p.Note)
			} else {
				fmt.Fprintf(&body, "- `%s`\n", loc)
			}
		}
		body.WriteByte('\n')
	}
	if s.Truncated {
		fmt.Fprintf(&body, "%s\n", t("asc_review.summary_truncated"))
	}
	s.Body = strings.TrimRight(body.String(), "\n") + "\n"
	return s
}

// ascSummaryText cleans one section: the paragraphs are kept, each line is cleaned like any untrusted text.
func ascSummaryText(s string, max int) string {
	var out []string
	n := 0
	for _, line := range strings.Split(s, "\n") {
		c := asctrust.CleanText(line, max)
		if n+len(c) > max {
			break
		}
		n += len(c) + 1
		out = append(out, c)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// ascSummaryPointerText is one line of the brief form's "where to look" field: path, then :line, then the note.
func ascSummaryPointerText(p AscSummaryPointer) string {
	s := p.Path
	if p.Line > 0 {
		s = fmt.Sprintf("%s:%d", s, p.Line)
	}
	if p.Note != "" {
		s += " " + p.Note
	}
	return s
}

// ascSummaryRefuse answers a summary that was not written. The reason is fixed; asc's own words never reach the page.
func ascSummaryRefuse(ctx *context.Context, err error) {
	code, reason, key := http.StatusServiceUnavailable, "unavailable", "asc_review.summary_failed"
	switch {
	case errors.Is(err, asctrust.ErrForbidden), errors.Is(err, asctrust.ErrNoDoer):
		code, reason, key = http.StatusForbidden, "denied", "asc_review.summary_denied"
	default:
		switch status.Code(err) {
		case codes.FailedPrecondition:
			code, reason, key = http.StatusConflict, "no_key", "asc_review.summary_no_key"
		case codes.Unimplemented:
			code, reason, key = http.StatusNotImplemented, "off", "asc_review.summary_off"
		case codes.ResourceExhausted:
			code, reason, key = http.StatusTooManyRequests, "paused", "asc_review.summary_paused"
		case codes.PermissionDenied, codes.Unauthenticated:
			code, reason, key = http.StatusForbidden, "denied", "asc_review.summary_denied"
		case codes.NotFound:
			code, reason, key = http.StatusNotFound, "unknown", "asc_review.summary_unknown"
		}
	}
	ctx.Resp.Header().Set("Cache-Control", "private, no-store")
	ctx.JSON(code, ascSummaryRefusal{Error: reason, Message: ctx.Locale.TrString(key), Next: ascAgentsNewOpID()})
}

// ascSummarize makes the call for the person and answers the page.
func ascSummarize(ctx *context.Context, req *ascv1.SummarizeChangeRequest) {
	op, err := uuid.Parse(ctx.FormString("op"))
	if err != nil || op.Version() != 7 {
		ctx.Error(http.StatusBadRequest)
		return
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallRepoWrite, ctx.Repo.Repository)
	if err != nil {
		if !errors.Is(err, asctrust.ErrForbidden) && !errors.Is(err, asctrust.ErrNoDoer) {
			log.Error("asc summary: the claim about the person: %v", err)
		}
		ascSummaryRefuse(ctx, err)
		return
	}
	defer release()
	conn, err := ascRPCConn()
	if err != nil {
		ascSummaryRefuse(ctx, err)
		return
	}
	cctx, cancel := gocontext.WithTimeout(ascclient.WithOpID(gocontext.WithoutCancel(callCtx), op), ascSummaryTimeout)
	defer cancel()
	resp, err := ascv1.NewReviewClient(conn).SummarizeChange(cctx, req)
	if err != nil {
		if c := status.Code(err); c != codes.FailedPrecondition && c != codes.ResourceExhausted && c != codes.NotFound && c != codes.Unimplemented {
			log.Warn("asc summary: SummarizeChange for %s: %v", req.GetRepo().GetName(), err)
		}
		ascSummaryRefuse(ctx, err)
		return
	}
	s := ascSummaryOf(resp, func(key string, args ...any) string { return ctx.Locale.TrString(key, args...) })
	s.Next = ascAgentsNewOpID()
	ctx.Resp.Header().Set("Cache-Control", "private, no-store")
	ctx.JSON(http.StatusOK, s)
}

// AscSummarizeRange handles POST /{owner}/{repo}/asc/summarize: base..head of a new change.
func AscSummarizeRange(ctx *context.Context) {
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
	ascSummarize(ctx, &ascv1.SummarizeChangeRequest{Repo: &ascv1.RepoRef{Name: name}, Base: base, Head: head})
}

// AscSummarizeBrief handles POST /{owner}/{repo}/pulls/{index}/asc/brief/summarize: the stacked change against its parent.
func AscSummarizeBrief(ctx *context.Context) {
	_, name, change, ok := ascBriefTarget(ctx)
	if !ok {
		return
	}
	ascSummarize(ctx, &ascv1.SummarizeChangeRequest{Repo: &ascv1.RepoRef{Name: name}, Change: change})
}
