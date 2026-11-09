// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	gocontext "context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	issues_model "forgejo.org/models/issues"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
	issue_service "forgejo.org/services/issue"
	pull_service "forgejo.org/services/pull"
)

// "Ask agent to change" of the inbox (board Inbox): on a pull request an agent made, the text of the reply box
// becomes a comment of the pull request and an instruction for that agent. The hub starts a run of the agent, as the
// person, on the branch of the pull request, to make the change and push the next version. Only a person who may
// change the pull request (its author, an owner or admin, or a writer when the stack is open to writers) is offered
// it, and asc checks their right to write the repository again.

// ascInboxAskMax is the longest instruction the hub sends; asc refuses a task over 64 KiB by default.
const ascInboxAskMax = 32 << 10

var (
	// ascInboxAskAgent returns the agent that made the pull request of e when doer may ask it to change the pull
	// request, else "". Tests replace it.
	ascInboxAskAgent = ascInboxAskAgentFromModels
	// ascInboxAskRun starts a run as the person. Tests replace it.
	ascInboxAskRun = ascagentui.StartRun
	// ascInboxAskComment posts the instruction as a comment of the pull request. Tests replace it.
	ascInboxAskComment = func(ctx gocontext.Context, doer *user_model.User, pr *issues_model.PullRequest, text string) error {
		_, err := issue_service.CreateIssueComment(ctx, doer, pr.BaseRepo, pr.Issue, text, nil)
		return err
	}
)

// ascInboxAskAgentOf returns the agent of the run that opened pr, or "" when no agent made it, its name is not one
// of asc, or its branch is in another repository (the run pushes to a branch of the repository).
func ascInboxAskAgentOf(ctx gocontext.Context, pr *issues_model.PullRequest) string {
	if pr.HeadRepoID != pr.BaseRepoID {
		return ""
	}
	link, err := issues_model.GetPullRequestAscRun(ctx, pr.ID)
	if err != nil || link == nil || !ascagentui.ValidAgentName(link.Agent) {
		return ""
	}
	return link.Agent
}

func ascInboxAskAgentFromModels(ctx gocontext.Context, doer *user_model.User, e *AscInboxEntry) string {
	if doer == nil || e == nil || e.PullID <= 0 {
		return ""
	}
	pr, err := issues_model.GetPullRequestByID(ctx, e.PullID)
	if err != nil {
		return ""
	}
	agent := ascInboxAskAgentOf(ctx, pr)
	if agent == "" {
		return ""
	}
	if open, err := pull_service.AscPullIsOpen(ctx, pr); err != nil || !open {
		return ""
	}
	if ok, err := pull_service.AscMayRestoreVersion(ctx, pr, doer); err != nil || !ok {
		return ""
	}
	return agent
}

// ascInboxAskTask is what the run is asked to do. The text is the instruction of the person who asked, who may
// change the pull request; its fence is longer than any run of backticks in it.
func ascInboxAskTask(login string, index int64, text string) string {
	fence := "```"
	for strings.Contains(text, fence) {
		fence += "`"
	}
	return fmt.Sprintf("A reviewer asks for a change to this pull request (#%d). Make the change the INSTRUCTION block "+
		"below asks for on the branch of the pull request, then push it as a new version. Change nothing the "+
		"instruction does not ask for. When the instruction cannot be done, push nothing and explain why.\n"+
		"INSTRUCTION (from %s, who may change this pull request):\n%s\n%s\n%s", index, login, fence, text, fence)
}

// ascInboxAnswer ends a post of the inbox: a redirect for a form the browser sent, a JSON redirect for one sent
// by the fetch of the page (form-fetch-action), which follows no redirect.
func ascInboxAnswer(ctx *context.Context, to string) {
	if strings.Contains(ctx.Req.Header.Get("Accept"), "text/html") {
		ctx.Redirect(to)
		return
	}
	ctx.JSONRedirect(to)
}

// AscInboxAsk serves `POST /-/asc/inbox/ask`: the reply text of the chosen item ("content") becomes a comment of its
// pull request ("pull") and the task of a run of the agent that made it.
func AscInboxAsk(ctx *context.Context) {
	if !setting.Asc.Enabled || ctx.Doer == nil {
		ctx.NotFound("AscInboxAsk", nil)
		return
	}
	pullID, err := strconv.ParseInt(ctx.FormString("pull"), 10, 64)
	if err != nil || pullID <= 0 {
		ctx.Error(http.StatusBadRequest)
		return
	}
	entries, err := ascInboxLoad(ctx, ctx.Doer, time.Now())
	if err != nil {
		ctx.ServerError("ascInboxLoad", err)
		return
	}
	var entry *AscInboxEntry
	for _, e := range entries {
		if e.PullID == pullID {
			entry = e
			break
		}
	}
	if entry == nil {
		ctx.NotFound("AscInboxAsk", nil)
		return
	}
	back := ascInboxURL(ascInboxTabOf(ctx.FormString("tab")), "", pullID)
	text := strings.TrimSpace(ctx.FormString("content"))
	switch {
	case text == "":
		ctx.Flash.Error(ctx.Tr("user.asc_inbox.ask_empty"))
		ascInboxAnswer(ctx, back)
		return
	case len(text) > ascInboxAskMax:
		ctx.Flash.Error(ctx.Tr("user.asc_inbox.ask_long"))
		ascInboxAnswer(ctx, back)
		return
	}
	agent := ascInboxAskAgent(ctx, ctx.Doer, entry)
	if agent == "" {
		ctx.Error(http.StatusForbidden)
		return
	}
	pr, err := issues_model.GetPullRequestByID(ctx, pullID)
	if err != nil {
		ctx.ServerError("GetPullRequestByID", err)
		return
	}
	if err := pr.LoadIssue(ctx); err != nil {
		ctx.ServerError("LoadIssue", err)
		return
	}
	if err := pr.LoadBaseRepo(ctx); err != nil {
		ctx.ServerError("LoadBaseRepo", err)
		return
	}
	req := &ascv1.CreateAgentRunRequest{
		Agent:  agent,
		Title:  fmt.Sprintf("Change #%d as asked in review", pr.Index),
		Task:   ascInboxAskTask(ctx.Doer.Name, pr.Index, text),
		Target: pr.HeadBranch,
	}
	if st, err := issues_model.GetPullRequestAscStack(ctx, pr.ID); err == nil && st != nil {
		req.ForChange = ascagentui.ChangeBytes(st.AscChange)
	}
	switch err := ascInboxAskRun(ctx, ctx.Doer, pr.BaseRepo, req); {
	case errors.Is(err, asctrust.ErrForbidden):
		ctx.Error(http.StatusForbidden)
		return
	case err != nil:
		log.Warn("asc inbox ask: run of %s for pull request %d: %v", agent, pr.ID, err)
		ctx.Flash.Error(ctx.Tr("user.asc_inbox.ask_failed", agent))
		ascInboxAnswer(ctx, back)
		return
	}
	log.Info("asc audit: inbox ask: repository %d, pull request %d: user %d asked %s to change it", pr.BaseRepoID, pr.ID, ctx.Doer.ID, agent)
	if err := ascInboxAskComment(ctx, ctx.Doer, pr, text); err != nil {
		// the run has the instruction; the thread misses it, which the person sees
		log.Warn("asc inbox ask: comment on pull request %d: %v", pr.ID, err)
		ctx.Flash.Warning(ctx.Tr("user.asc_inbox.ask_no_comment", agent))
		ascInboxAnswer(ctx, back)
		return
	}
	ctx.Flash.Success(ctx.Tr("user.asc_inbox.asked", agent))
	ascInboxAnswer(ctx, back)
}
