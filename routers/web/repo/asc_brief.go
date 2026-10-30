// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"fmt"
	"forgejo.org/models/unit"
	"time"

	issues_model "forgejo.org/models/issues"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/util"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// The brief of the review page (board Review): what the change does, why, how to check it, what could go wrong and
// where to look first, next to the size, the risk class and "how it was made" when an agent run made the change.
// Everything comes from Review.GetBrief and is cleaned here; no link or markup comes from asc. When asc has no brief
// the card says so and still shows the size, the risk and the provenance it knows.

const (
	ascBriefTimeout      = 4 * time.Second
	ascBriefMaxText      = 8 << 10
	ascBriefMaxWhere     = 20
	ascBriefMaxFiles     = 500
	ascBriefMaxApprovals = 50
	ascBriefMaxLines     = 1 << 24
)

// AscBriefWhere is one pointer of "where to look": a path, a line (0 for the whole file) and a note.
type AscBriefWhere struct {
	Path string
	Line uint32
	Note string
	Link string // a link to the file at the head of the change, "" when it cannot be made
}

// AscBriefProvenance says how a change was made when an agent run made it.
type AscBriefProvenance struct {
	RunID     uint64
	RunLink   string
	Agent     string
	Title     string
	State     string
	ToolCalls uint32
	FilesRead uint32
	Versions  uint32
	Minutes   int64 // 0 when the run has not finished
}

// AscApproval is one verdict a person gave, as asc counts it for the newest version.
type AscApproval struct {
	Reviewer         string
	Version          uint32
	ChangesRequested bool
	Applies          bool   // still counts for the newest version
	Carried          bool   // counts although it was given on an earlier version
	UnchangedFiles   uint32 // files the newest version did not change since the verdict
}

// AscBrief is what the review page shows of Review.GetBrief.
type AscBrief struct {
	Written  bool // the author wrote a brief
	What     string
	Why      string
	How      string
	RiskNote string
	Where    []AscBriefWhere

	Files, Added, Removed uint32
	StatsTruncated        bool

	RiskLevel   string // "low", "medium", "high" or "" when asc did not classify the change
	RiskReasons []string

	Provenance *AscBriefProvenance

	Approvals                 []AscApproval
	OpenThreads, TotalThreads uint32
	ApprovalsCurrent          uint32
	ApprovalsCarried          uint32
	ChangesRequested          uint32
	PendingReviewers          uint32
}

// ascBriefOf checks and cleans the answer of Review.GetBrief. fileLink makes the link of a path and a line, or "".
func ascBriefOf(resp *ascv1.GetBriefResponse, runLink func(uint64) string, fileLink func(path string, line uint32) string) (*AscBrief, error) {
	if resp == nil {
		return nil, fmt.Errorf("asc brief: empty answer")
	}
	if len(resp.GetFileStats()) > ascBriefMaxFiles || resp.GetBrief() != nil && len(resp.GetBrief().GetWhereToLook()) > ascBriefMaxWhere {
		return nil, fmt.Errorf("asc brief: too many entries")
	}
	if resp.GetFiles() > ascBriefMaxLines || resp.GetAdded() > ascBriefMaxLines || resp.GetRemoved() > ascBriefMaxLines {
		return nil, fmt.Errorf("asc brief: sizes out of range")
	}
	out := &AscBrief{
		Files: resp.GetFiles(), Added: resp.GetAdded(), Removed: resp.GetRemoved(), StatsTruncated: resp.GetStatsTruncated(),
	}
	if b := resp.GetBrief(); b != nil {
		out.What = asctrust.CleanBlock(b.GetWhat(), ascBriefMaxText)
		out.Why = asctrust.CleanBlock(b.GetWhy(), ascBriefMaxText)
		out.How = asctrust.CleanBlock(b.GetHow(), ascBriefMaxText)
		out.RiskNote = asctrust.CleanBlock(b.GetRiskNote(), ascBriefMaxText)
		for _, w := range b.GetWhereToLook() {
			p, ok := ascCleanPath(w.GetPath())
			if !ok || w.GetLine() > ascSymbolsMaxLine {
				continue
			}
			out.Where = append(out.Where, AscBriefWhere{Path: p, Line: w.GetLine(), Note: asctrust.CleanText(w.GetNote(), 400), Link: fileLink(p, w.GetLine())})
		}
		out.Written = out.What != "" || out.Why != "" || out.How != "" || out.RiskNote != "" || len(out.Where) > 0
	}
	if r := resp.GetRisk(); r != nil {
		switch r.GetLevel() {
		case ascv1.RiskLevel_RISK_LEVEL_LOW:
			out.RiskLevel = "low"
		case ascv1.RiskLevel_RISK_LEVEL_MEDIUM:
			out.RiskLevel = "medium"
		case ascv1.RiskLevel_RISK_LEVEL_HIGH:
			out.RiskLevel = "high"
		}
		for i, reason := range r.GetReasons() {
			if i >= 8 {
				break
			}
			if s := asctrust.CleanText(reason, 300); s != "" {
				out.RiskReasons = append(out.RiskReasons, s)
			}
		}
	}
	if p := resp.GetProvenance(); p != nil && p.GetRunId() != 0 {
		prov := &AscBriefProvenance{
			RunID: p.GetRunId(), RunLink: runLink(p.GetRunId()), Agent: asctrust.CleanText(p.GetAgent(), 80),
			Title: asctrust.CleanText(p.GetTitle(), 200), State: asctrust.CleanText(p.GetRunState(), 40),
			ToolCalls: p.GetToolCalls(), FilesRead: p.GetFilesRead(), Versions: p.GetVersions(),
		}
		if p.GetFinished() > p.GetStarted() && p.GetStarted() > 0 {
			prov.Minutes = (p.GetFinished() - p.GetStarted() + 59) / 60
		}
		out.Provenance = prov
	}
	for i, a := range resp.GetApprovals() {
		if i >= ascBriefMaxApprovals {
			break
		}
		state := a.GetState()
		if state != ascv1.ApprovalState_APPROVAL_STATE_APPROVED && state != ascv1.ApprovalState_APPROVAL_STATE_CHANGES_REQUESTED {
			continue
		}
		out.Approvals = append(out.Approvals, AscApproval{
			Reviewer: asctrust.CleanText(a.GetReviewer(), 80), Version: a.GetVersion(), ChangesRequested: state == ascv1.ApprovalState_APPROVAL_STATE_CHANGES_REQUESTED,
			Applies: a.GetApplies(), Carried: a.GetCarried(), UnchangedFiles: a.GetUnchangedFiles(),
		})
	}
	if g := resp.GetGate(); g != nil {
		out.OpenThreads, out.TotalThreads = g.GetOpenThreads(), g.GetTotalThreads()
		out.ApprovalsCurrent, out.ApprovalsCarried = g.GetApprovalsCurrent(), g.GetApprovalsCarried()
		out.ChangesRequested, out.PendingReviewers = g.GetChangesRequested(), g.GetPendingReviewers()
	}
	return out, nil
}

// prepareAscBrief puts the brief of an open stacked pull request into ctx.Data["AscBrief"]. It never fails the page:
// without asc, without a link or on any asc problem it sets nothing, and the page shows no brief card.
func prepareAscBrief(ctx *context.Context, pull *issues_model.PullRequest) {
	if pull == nil || !setting.Asc.Enabled {
		return
	}
	link, err := issues_model.GetPullRequestAscStack(ctx, pull.ID)
	if err != nil || link == nil {
		return
	}
	if pull.BaseRepo == nil && pull.LoadBaseRepo(ctx) != nil {
		return
	}
	repo := pull.BaseRepo
	if repo.GetStorageBackend() != string(gitrepo.StorageBackendAsc) {
		return
	}
	name, err := ascGraphRepoName(repo)
	if err != nil {
		return
	}
	change := ascChangeBytes(link.AscChange)
	if change == nil {
		return
	}
	conn, err := ascRPCConn()
	if err != nil {
		return
	}
	callCtx, cancel := gocontext.WithTimeout(ascclient.WithSystem(ctx), ascBriefTimeout)
	defer cancel()
	resp, err := ascv1.NewReviewClient(conn).GetBrief(callCtx, &ascv1.GetBriefRequest{Repo: &ascv1.RepoRef{Name: name}, Change: change})
	if err != nil {
		log.Warn("asc brief: %v", err)
		return
	}
	repoLink := ctx.Repo.RepoLink
	headSame := pull.HeadRepoID == pull.BaseRepoID && pull.HeadBranch != ""
	brief, err := ascBriefOf(resp,
		func(id uint64) string { return fmt.Sprintf("%s/asc/agents/runs/%d", repoLink, id) },
		func(path string, line uint32) string {
			if !headSame {
				return ""
			}
			l := fmt.Sprintf("%s/src/branch/%s/%s", repoLink, util.PathEscapeSegments(pull.HeadBranch), util.PathEscapeSegments(path))
			if line > 0 {
				l += fmt.Sprintf("#L%d", line)
			}
			return l
		})
	if err != nil {
		log.Error("asc brief: %v", err)
		return
	}
	if !ascAgentsCanReadRuns(ctx) {
		// the agent run behind a change (its title, its agent, its counts) is data of the agents area
		brief.Provenance = nil
	}
	ctx.Data["AscBrief"] = brief
	ctx.Data["AscBriefCanEdit"] = ctx.IsSigned && ctx.Repo.CanWrite(unit.TypeCode) && !pull.HasMerged
	prepareAscGate(ctx, pull, brief, name, change)
	prepareAscThreads(ctx, conn, name, change)
}
