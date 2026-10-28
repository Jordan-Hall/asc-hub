// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"forgejo.org/models/db"
	git_model "forgejo.org/models/git"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/base"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/optional"
	"forgejo.org/modules/setting"
	"forgejo.org/services/context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Overlap and mergeability badges of a stacked pull request, and the stack compare view (SPEC 20.5, 20.6 and
// 24.4, PLAN unit C43).
//
// Badges. For an open pull request that is the pull request of an asc change, Changes.Overlaps names the other
// open changes that touch the same paths, and Changes.Mergeability says how the change meets the tip of its
// target. Both are advice. They are shown beside the merge box and never decide whether a merge is allowed
// (the merge rules are C36 and C38). When asc cannot answer, or an answer fails a check, the badges are left
// out and the page still renders: a badge that is wrong is worse than none.
//
//   - The change comes from the pull request's own pull_request_asc_stack row, never from the request.
//   - Every answer is checked before use: ids have the right length, a result names the change that was asked
//     for, an overlap never names the change itself, states are known, and every list and text is bounded.
//   - Text from asc is shown escaped by the template; nothing from asc is used as a URL. A link to another change
//     is built by Forgejo from its own open pull request of that change in the same repository.
//   - Errors and log lines carry the gRPC code only, never the server's message.
//
// Compare view. `GET /{owner}/{repo}/asc/compare?a=<rev>&b=<rev>[&three_dot=1]` shows Repo.Compare between two
// revisions: the merge base, the commits each side has that the other lacks, and the changed files. A "stack"
// is a rev (a branch or a change's head), so comparing two versions or two stacks is comparing two revs. The
// page needs the same permission as the code of the repository and reads with the internal token.

const (
	tplAscCompare base.TplName = "repo/asc_compare"

	// ascBadgeTimeout bounds the two calls of one page together.
	ascBadgeTimeout = 15 * time.Second
	// ascBadgeMaxOverlaps and ascBadgeMaxPaths bound what is shown; more is counted.
	ascBadgeMaxOverlaps = 20
	ascBadgeMaxPaths    = 10
	// ascBadgeMaxList bounds what asc may send before the answer is refused.
	ascBadgeMaxList = 10_000
	// ascBadgeMaxConflicts bounds the conflicting paths shown.
	ascBadgeMaxConflicts = 20
	// ascBadgeMaxPathBytes bounds one path.
	ascBadgeMaxPathBytes = 4096
	// ascBadgeMaxTextRunes bounds a review text.
	ascBadgeMaxTextRunes = 24

	// Compare view bounds.
	ascCompareLimit     = 50
	ascCompareMaxFiles  = 1000
	ascCompareMaxPathBy = 4096
	ascCompareTimeout   = 30 * time.Second
	ascCompareSubject   = 200
	ascCompareName      = 100
)

// The mergeability states the template knows by name.
const (
	AscMergeDisjoint     = "disjoint"
	AscMergeClean        = "clean"
	AscMergeConflicts    = "conflicts"
	AscMergeNeedsRestack = "needs_restack"
)

var errAscBadgeAnswer = errors.New("asc badges: bad answer")

func ascBadgeBad(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errAscBadgeAnswer, fmt.Sprintf(format, args...))
}

// AscOverlapBadge is one other open change that touches paths of this one.
type AscOverlapBadge struct {
	Change      string // short id: the first 8 letters
	PullIndex   int64  // the open pull request of that change in this repository, 0 if there is none
	PullLink    string
	ReviewState string // "" when asc has none
	Approvals   uint32
	Paths       []string
	MorePaths   int
}

// AscMergeBadge is how the change meets its target.
type AscMergeBadge struct {
	State         string // one of the AscMerge constants
	Conflicts     []string
	MoreConflicts int
}

// AscBadges is what the pull request page shows.
type AscBadges struct {
	Overlaps     []AscOverlapBadge
	MoreOverlaps int
	Merge        *AscMergeBadge
}

// ascBadgeAPI returns the Changes client. Tests replace the connection through ascRPCNewConn.
func ascBadgeAPI() (ascv1.ChangesClient, error) {
	conn, err := ascRPCConn()
	if err != nil {
		return nil, err
	}
	return ascv1.NewChangesClient(conn), nil
}

// ascChangeBytes turns the 32 letter text form of a change id into its 16 bytes (the letter z is the
// nibble 0 and k the nibble 15).
func ascChangeBytes(s string) []byte {
	if !issues_model.IsAscChange(s) {
		return nil
	}
	raw := make([]byte, 16)
	for i := range raw {
		raw[i] = (('z' - s[2*i]) << 4) | ('z' - s[2*i+1])
	}
	return raw
}

// ascChangeText is the inverse of ascChangeBytes.
func ascChangeText(raw []byte) string {
	if len(raw) != 16 {
		return ""
	}
	var b strings.Builder
	for _, v := range raw {
		b.WriteByte('z' - v>>4)
		b.WriteByte('z' - v&0x0f)
	}
	return b.String()
}

// prepareAscBadges puts the badges of an open stacked pull request into ctx.Data["AscBadges"]. It never fails
// the page: without asc, without a link, for a closed pull request or on any asc problem it sets nothing.
func prepareAscBadges(ctx *context.Context, pull *issues_model.PullRequest) {
	if pull == nil || pull.HasMerged || !setting.Asc.Enabled {
		return
	}
	if err := pull.LoadIssue(ctx); err != nil || pull.Issue.IsClosed {
		return
	}
	link, err := issues_model.GetPullRequestAscStack(ctx, pull.ID)
	if err != nil || link == nil {
		return
	}
	repo := pull.BaseRepo
	if repo == nil {
		if pull.LoadBaseRepo(ctx) != nil {
			return
		}
		repo = pull.BaseRepo
	}
	if repo.GetStorageBackend() != string(gitrepo.StorageBackendAsc) {
		return
	}
	name, err := ascGraphRepoName(repo)
	if err != nil {
		return
	}
	badges, err := ascBadgesOf(ctx, repo.ID, ctx.Repo.RepoLink, name, link.AscChange, pull.BaseBranch)
	if err != nil {
		ascBadgeLog(err)
		return
	}
	if badges != nil {
		ctx.Data["AscBadges"] = badges
	}
}

func ascBadgeLog(err error) {
	if errors.Is(err, errAscBadgeAnswer) {
		log.Error("asc badges: %v", err)
		return
	}
	if st, ok := status.FromError(err); ok {
		log.Warn("asc badges: asc answered %s", st.Code())
		return
	}
	log.Warn("asc badges: no answer")
}

// ascBadgesOf asks asc for the overlaps and the mergeability of change (text form) against target, and checks
// the answers. repoLink is the link of the repository, used for the links to other pull requests.
func ascBadgesOf(ctx gocontext.Context, repoID int64, repoLink, repoName, change, target string) (*AscBadges, error) {
	own := ascChangeBytes(change)
	if own == nil || target == "" {
		return nil, ascBadgeBad("no change or target")
	}
	api, err := ascBadgeAPI()
	if err != nil {
		return nil, err
	}
	callCtx, cancel := gocontext.WithTimeout(ascclient.WithSystem(ctx), ascBadgeTimeout)
	defer cancel()
	repo := &ascv1.RepoRef{Name: repoName}

	ov, err := api.Overlaps(callCtx, &ascv1.OverlapsRequest{Repo: repo, Target: target, Change: own})
	if err != nil {
		return nil, err
	}
	mg, err := api.Mergeability(callCtx, &ascv1.MergeabilityRequest{Repo: repo, Target: target, Changes: [][]byte{own}})
	if err != nil {
		return nil, err
	}
	badges := &AscBadges{}
	if err := ascVerifyOverlaps(ctx, repoID, repoLink, own, ov.GetOverlaps(), badges); err != nil {
		return nil, err
	}
	merge, err := ascVerifyMergeability(own, mg.GetResults())
	if err != nil {
		return nil, err
	}
	badges.Merge = merge
	return badges, nil
}

// ascCleanPath checks one path from asc and returns it as text.
func ascCleanPath(p []byte) (string, bool) {
	if len(p) == 0 || len(p) > ascBadgeMaxPathBytes || !utf8.Valid(p) || slices.Contains(p, 0) {
		return "", false
	}
	return ascGraphText(string(p), ascBadgeMaxPathBytes), true
}

func ascVerifyOverlaps(ctx gocontext.Context, repoID int64, repoLink string, own []byte, in []*ascv1.Overlap, out *AscBadges) error {
	if len(in) > ascBadgeMaxList {
		return ascBadgeBad("%d overlaps", len(in))
	}
	seen := map[string]bool{}
	for _, o := range in {
		if o == nil || len(o.GetChange()) != 16 {
			return ascBadgeBad("overlap with a bad change id")
		}
		text := ascChangeText(o.GetChange())
		if text == ascChangeText(own) {
			// A change never overlaps itself. Skip it rather than show the page a contradiction.
			continue
		}
		if seen[text] {
			continue
		}
		seen[text] = true
		if len(o.GetPaths()) > ascBadgeMaxList {
			return ascBadgeBad("too many paths")
		}
		if len(out.Overlaps) >= ascBadgeMaxOverlaps {
			out.MoreOverlaps++
			continue
		}
		b := AscOverlapBadge{Change: text[:8]}
		for _, p := range o.GetPaths() {
			s, ok := ascCleanPath(p)
			if !ok {
				return ascBadgeBad("overlap path is invalid")
			}
			if len(b.Paths) < ascBadgeMaxPaths {
				b.Paths = append(b.Paths, s)
			} else {
				b.MorePaths++
			}
		}
		if rs := o.GetReview(); rs != nil {
			b.ReviewState = ascGraphText(rs.GetState(), ascBadgeMaxTextRunes)
			b.Approvals = rs.GetApprovals()
		}
		// The link is Forgejo's own: the open pull request of that change in this repository.
		if pr, err := issues_model.GetOpenPullRequestByAscChange(ctx, repoID, text); err == nil && pr != nil {
			if err := pr.LoadIssue(ctx); err == nil {
				b.PullIndex = pr.Index
				b.PullLink = fmt.Sprintf("%s/pulls/%d", repoLink, pr.Index)
			}
		}
		out.Overlaps = append(out.Overlaps, b)
	}
	return nil
}

func ascVerifyMergeability(own []byte, in []*ascv1.Mergeable) (*AscMergeBadge, error) {
	if len(in) != 1 || in[0] == nil || !slices.Equal(in[0].GetChange(), own) {
		return nil, ascBadgeBad("mergeability does not answer for the change")
	}
	m := in[0]
	b := &AscMergeBadge{}
	switch m.GetState() {
	case ascv1.Mergeable_STATE_DISJOINT:
		b.State = AscMergeDisjoint
	case ascv1.Mergeable_STATE_CLEAN_MERGE:
		b.State = AscMergeClean
	case ascv1.Mergeable_STATE_CONFLICTS:
		b.State = AscMergeConflicts
	case ascv1.Mergeable_STATE_NEEDS_RESTACK:
		b.State = AscMergeNeedsRestack
	default:
		return nil, ascBadgeBad("unknown mergeability state")
	}
	if len(m.GetConflicts()) > ascBadgeMaxList {
		return nil, ascBadgeBad("too many conflicts")
	}
	if b.State == AscMergeConflicts && len(m.GetConflicts()) == 0 {
		return nil, ascBadgeBad("conflicts without a path")
	}
	for _, c := range m.GetConflicts() {
		s, ok := ascCleanPath(c.GetPath())
		if !ok {
			return nil, ascBadgeBad("conflict path is invalid")
		}
		if len(b.Conflicts) < ascBadgeMaxConflicts {
			b.Conflicts = append(b.Conflicts, s)
		} else {
			b.MoreConflicts++
		}
	}
	return b, nil
}

// --- compare view ---------------------------------------------------------------------------------------

// AscCompareCommit is one commit of the compare view.
type AscCompareCommit struct {
	SHA     string
	Subject string
	Author  string
}

// AscCompareFile is one changed file.
type AscCompareFile struct {
	Path   string
	Status string // one letter from the fixed set, or "?"
}

// AscCompareResult is the checked answer of Repo.Compare.
type AscCompareResult struct {
	MergeBase  string
	Ahead      uint64
	Behind     uint64
	AheadList  []AscCompareCommit
	BehindList []AscCompareCommit
	AheadMore  bool
	BehindMore bool
	Files      []AscCompareFile
	FilesMore  bool
}

var ascCompareStatuses = map[string]bool{"A": true, "M": true, "D": true, "R": true, "C": true, "T": true}

func ascCompareEntries(in []*ascv1.LogEntry) ([]AscCompareCommit, error) {
	if len(in) > ascCompareLimit {
		return nil, ascBadgeBad("more than %d commits", ascCompareLimit)
	}
	out := make([]AscCompareCommit, 0, len(in))
	for _, e := range in {
		if e == nil || len(e.GetGit()) != 20 {
			return nil, ascBadgeBad("commit with a bad id")
		}
		subject, _, _ := strings.Cut(string(e.GetMessage()), "\n")
		c := AscCompareCommit{SHA: fmt.Sprintf("%x", e.GetGit()), Subject: ascGraphText(subject, ascCompareSubject)}
		if a := e.GetAuthor(); a != nil {
			c.Author = ascGraphText(string(a.GetName()), ascCompareName)
		}
		out = append(out, c)
	}
	return out, nil
}

// ascCompareVerify checks Compare's answer and turns it into what the template shows.
func ascCompareVerify(r *ascv1.CompareResponse) (*AscCompareResult, error) {
	if r == nil {
		return nil, ascBadgeBad("empty answer")
	}
	res := &AscCompareResult{Ahead: r.GetAhead(), Behind: r.GetBehind()}
	if mb := r.GetMergeBase(); len(mb) != 0 {
		if len(mb) != 20 && len(mb) != 32 {
			return nil, ascBadgeBad("merge base with a bad id")
		}
		res.MergeBase = fmt.Sprintf("%x", mb)
	}
	var err error
	if res.AheadList, err = ascCompareEntries(r.GetAheadEntries()); err != nil {
		return nil, err
	}
	if res.BehindList, err = ascCompareEntries(r.GetBehindEntries()); err != nil {
		return nil, err
	}
	res.AheadMore = r.GetAheadNext() != "" || res.Ahead > uint64(len(res.AheadList))
	res.BehindMore = r.GetBehindNext() != "" || res.Behind > uint64(len(res.BehindList))
	if len(r.GetFiles()) > ascCompareMaxFiles {
		return nil, ascBadgeBad("more than %d files", ascCompareMaxFiles)
	}
	for _, f := range r.GetFiles() {
		p := f.GetPath()
		if len(p) == 0 || len(p) > ascCompareMaxPathBy || !utf8.Valid(p) || slices.Contains(p, 0) {
			return nil, ascBadgeBad("file with a bad path")
		}
		st := f.GetStatus()
		if !ascCompareStatuses[st] {
			st = "?"
		}
		res.Files = append(res.Files, AscCompareFile{Path: ascGraphText(string(p), ascCompareMaxPathBy), Status: st})
	}
	return res, nil
}

// AscCompare serves `GET /{owner}/{repo}/asc/compare`.
func AscCompare(ctx *context.Context) {
	repo := ctx.Repo.Repository
	if !setting.Asc.Enabled || repo == nil || repo.GetStorageBackend() != string(gitrepo.StorageBackendAsc) {
		ctx.NotFound("AscCompare", nil)
		return
	}
	ctx.Data["Title"] = ctx.Tr("repo.asc_compare")
	ctx.Data["PageIsCommits"] = true
	ctx.Data["PageIsViewCode"] = true

	a, okA := ascCompareRev(ctx.FormString("a"))
	b, okB := ascCompareRev(ctx.FormString("b"))
	three := ctx.FormBool("three_dot")
	ctx.Data["AscCompareA"] = a
	ctx.Data["AscCompareB"] = b
	ctx.Data["AscCompareThreeDot"] = three
	prepareAscCompareRefs(ctx, repo, a, b)
	if okA && okB && a != "" && a == b {
		// the same version on both sides: say so, the server has nothing to compare
		ctx.Data["AscCompareSame"] = true
		ctx.HTML(http.StatusOK, tplAscCompare)
		return
	}
	if !okA || !okB {
		ascCompareFail(ctx, http.StatusBadRequest, "repo.asc_compare.error.bad_request")
		return
	}
	if a == "" || b == "" {
		ctx.HTML(http.StatusOK, tplAscCompare) // the empty form
		return
	}
	name, err := ascGraphRepoName(repo)
	if err != nil {
		ctx.NotFound("AscCompare", nil)
		return
	}
	conn, err := ascRPCConn()
	if err != nil {
		log.Error("asc compare: no connection to asc serve")
		ascCompareFail(ctx, http.StatusBadGateway, "repo.asc_compare.error.unavailable")
		return
	}
	callCtx, cancel := gocontext.WithTimeout(ascclient.WithSystem(ctx.Req.Context()), ascCompareTimeout)
	defer cancel()
	resp, err := ascv1.NewRepoClient(conn).Compare(callCtx, &ascv1.CompareRequest{
		Repo: &ascv1.RepoRef{Name: name, IdMode: ascv1.IdMode_ID_MODE_GIT},
		A:    a, B: b, ThreeDot: three, Limit: ascCompareLimit,
	})
	if err != nil {
		ascCompareMapError(ctx, err)
		return
	}
	res, err := ascCompareVerify(resp)
	if err != nil {
		log.Error("asc compare: %v", err)
		ascCompareFail(ctx, http.StatusBadGateway, "repo.asc_compare.error.bad_answer")
		return
	}
	ctx.Data["AscCompare"] = res
	ctx.HTML(http.StatusOK, tplAscCompare)
}

// ascRefGroup is one group of the From and To selects of the compare page.
type ascRefGroup struct {
	Label string // a locale key
	Names []string
}

// ascCompareMaxRefs bounds each group of the selects.
const ascCompareMaxRefs = 100

// ascCompareGroups builds the groups of the selects: shared refs, stacks and tags, without empty groups, without
// repeated names inside a group, and a group of "current" for a value of the query that none of them holds (a
// change id or a commit), so the select can show what was asked.
func ascCompareGroups(shared, stacks, tags []string, current ...string) []ascRefGroup {
	var groups []ascRefGroup
	seen := map[string]bool{}
	add := func(label string, names []string) {
		var out []string
		for _, n := range names {
			if n == "" || seen[n] || len(out) >= ascCompareMaxRefs {
				continue
			}
			seen[n] = true
			out = append(out, n)
		}
		if len(out) > 0 {
			groups = append(groups, ascRefGroup{Label: label, Names: out})
		}
	}
	add("asc_rp.cmp_shared", shared)
	add("asc_rp.cmp_stacks", stacks)
	add("asc_rp.cmp_tags", tags)
	var extra []string
	for _, c := range current {
		if c != "" && !seen[c] {
			extra = append(extra, c)
		}
	}
	add("asc_rp.cmp_current", extra)
	return groups
}

// prepareAscCompareRefs lists what the From and To selects offer. A failed lookup leaves its group out; the
// selects still show the value of the query.
func prepareAscCompareRefs(ctx *context.Context, repo *repo_model.Repository, a, b string) {
	var shared, stacks, tags []string
	if names, err := git_model.FindBranchNames(ctx, git_model.FindBranchOptions{
		ListOptions: db.ListOptions{PageSize: ascCompareMaxRefs, Page: 1}, RepoID: repo.ID, IsDeletedBranch: optional.Some(false),
	}); err != nil {
		log.Error("asc compare: branches of %s: %v", repo.FullName(), err)
	} else {
		shared = names
	}
	if repo.CanEnablePulls() && ctx.Repo.CanRead(unit.TypePullRequests) {
		if pulls, err := issues_model.Issues(ctx, &issues_model.IssuesOptions{
			Paginator: &db.ListOptions{Page: 1, PageSize: ascCompareMaxRefs}, RepoIDs: []int64{repo.ID},
			IsPull: optional.Some(true), IsClosed: optional.Some(false), SortType: "recentupdate",
		}); err != nil {
			log.Error("asc compare: stacks of %s: %v", repo.FullName(), err)
		} else {
			for _, p := range pulls {
				if err := p.LoadPullRequest(ctx); err == nil && p.PullRequest != nil && p.PullRequest.BaseRepoID == repo.ID {
					stacks = append(stacks, p.PullRequest.HeadBranch)
				}
			}
		}
	}
	if names, err := repo_model.GetTagNamesByRepoID(ctx, repo.ID); err != nil {
		log.Error("asc compare: tags of %s: %v", repo.FullName(), err)
	} else {
		tags = names
	}
	// a stack is also a branch of the repository: list it once, under Stacks
	isStack := map[string]bool{}
	for _, s := range stacks {
		isStack[s] = true
	}
	shared = slices.DeleteFunc(slices.Clone(shared), func(n string) bool { return isStack[n] })
	ctx.Data["AscCompareGroups"] = ascCompareGroups(shared, stacks, tags, a, b)
}

// ascCompareRev checks one revision of the query. An empty value is allowed (the form is shown empty).
func ascCompareRev(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", true
	}
	revs, ok := ascGraphRevs(s)
	if !ok || len(revs) != 1 {
		return s[:min(len(s), ascGraphMaxRevBytes)], false
	}
	return revs[0], true
}

func ascCompareFail(ctx *context.Context, code int, key string) {
	ctx.Data["AscCompareError"] = key
	ctx.HTML(code, tplAscCompare)
}

// ascCompareMapError turns a failed call into a page. Only the status code of an asc error is used.
func ascCompareMapError(ctx *context.Context, err error) {
	if ctx.Req.Context().Err() != nil {
		return
	}
	st, ok := status.FromError(err)
	if !ok {
		ascCompareFail(ctx, http.StatusBadGateway, "repo.asc_compare.error.unavailable")
		return
	}
	switch st.Code() {
	case codes.NotFound:
		ascCompareFail(ctx, http.StatusNotFound, "repo.asc_compare.error.not_found")
	case codes.InvalidArgument:
		ascCompareFail(ctx, http.StatusBadRequest, "repo.asc_compare.error.bad_request")
	default:
		log.Error("asc compare: asc answered %s", st.Code())
		ascCompareFail(ctx, http.StatusBadGateway, "repo.asc_compare.error.unavailable")
	}
}
