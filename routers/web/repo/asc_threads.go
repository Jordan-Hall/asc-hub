// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"fmt"
	"strings"

	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/log"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
	pull_service "forgejo.org/services/pull"

	"google.golang.org/grpc"
)

// The comments made on a change with asc (not on the hub) are threads of asc (Review.ListThreads). The hub shows them on the
// review page, in their own card, because they have no place in the conversation of the pull request: the hub did not
// store them. What the hub itself wrote is mirrored to asc (services/pull/asc_review_mirror.go) and is already in the
// conversation, so it is not listed again.

const (
	ascThreadsMax         = 30
	ascThreadsMaxComments = 12
	ascThreadsMaxBody     = 2000
	ascThreadsPageLimit   = 100
)

// AscThread is a thread of asc that did not start on the hub.
type AscThread struct {
	ID       uint64
	Path     string // empty: the change as a whole
	Lines    string // "14" or "14-16"; empty without lines
	Version  uint32
	Resolved bool
	OpenedBy string
	Comments []AscThreadComment
}

// AscThreadComment is one comment of a thread.
type AscThreadComment struct {
	Author     string
	Body       string
	Suggestion string
	Applied    uint32 // the version that applied the suggestion, 0 while it is not applied
}

// ascThreadsOf keeps the threads that started in asc and cleans their texts. Nothing from asc is trusted as markup.
func ascThreadsOf(threads []*ascv1.ReviewThread) []AscThread {
	var out []AscThread
	for _, th := range threads {
		cs := th.GetComments()
		if len(cs) == 0 || strings.HasPrefix(cs[0].GetExternalId(), pull_service.AscExternalCommentPrefix) {
			continue
		}
		if len(out) >= ascThreadsMax {
			break
		}
		t := AscThread{ID: th.GetId(), Resolved: th.GetStatus() == ascv1.ThreadStatus_THREAD_STATUS_RESOLVED, OpenedBy: asctrust.CleanText(th.GetOpenedBy(), 80)}
		if a := th.GetAnchor(); a != nil {
			if p, ok := ascCleanPath(a.GetPath()); ok {
				t.Path = p
			}
			t.Version = a.GetVersion()
			if t.Path != "" && a.GetStartLine() > 0 {
				t.Lines = fmt.Sprint(a.GetStartLine())
				if a.GetEndLine() > a.GetStartLine() {
					t.Lines = fmt.Sprintf("%d-%d", a.GetStartLine(), a.GetEndLine())
				}
			}
		}
		for i, c := range cs {
			if i >= ascThreadsMaxComments {
				break
			}
			t.Comments = append(t.Comments, AscThreadComment{
				Author:     asctrust.CleanText(c.GetAuthor(), 80),
				Body:       asctrust.CleanBlock(c.GetBody(), ascThreadsMaxBody),
				Suggestion: asctrust.CleanBlock(c.GetSuggestion(), ascThreadsMaxBody),
				Applied:    c.GetAppliedVersion(),
			})
		}
		out = append(out, t)
	}
	return out
}

// prepareAscThreads puts the threads that started in asc into ctx.Data["AscThreads"]. It never fails the page.
func prepareAscThreads(ctx *context.Context, conn grpc.ClientConnInterface, name string, change []byte) {
	callCtx, cancel := gocontext.WithTimeout(ascclient.WithSystem(ctx), ascBriefTimeout)
	defer cancel()
	resp, err := ascv1.NewReviewClient(conn).ListThreads(callCtx, &ascv1.ListThreadsRequest{Repo: &ascv1.RepoRef{Name: name}, Change: change, Limit: ascThreadsPageLimit})
	if err != nil {
		log.Warn("asc threads: %v", err)
		return
	}
	if threads := ascThreadsOf(resp.GetThreads()); len(threads) > 0 {
		ctx.Data["AscThreads"] = threads
	}
}
