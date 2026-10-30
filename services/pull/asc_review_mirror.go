// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/log"
	"forgejo.org/modules/optional"
	"forgejo.org/modules/setting"
	notify_service "forgejo.org/services/notify"
)

// The comments of a pull request that belongs to an asc stack are also the comment threads of the change in asc
// (Review.AddComment, ResolveThread, RequestReview), so `asc review` and every other client of asc sees what was said
// on the hub, and the hub is no island. The hub's comment stays the source: a comment is mirrored once, under the
// external id "hub:comment:<id>" (asc stores a comment mirrored twice under the same id once, so a repeated event does
// no harm), as the person who wrote it (the call carries a one-use claim of that person, never the hub's identity).
//
// Rules, as for the verdicts (asc_approval_report.go):
//   - Only a pull request that is linked to an asc change in an asc repository is ever mirrored.
//   - A person the hub does not allow to read the repository (or write, for a review request) is not mirrored.
//   - Nothing here fails the action of the person: a mirror that cannot be made is logged and dropped.
//   - The calls run off the request path, one after the other, so a reply finds the thread of its first comment.

const (
	ascMirrorTimeout = 5 * time.Second
	// AscExternalCommentPrefix starts the external id of a comment that the hub mirrored to asc.
	AscExternalCommentPrefix = "hub:comment:"
	ascMirrorMaxBody         = 64 << 10
	ascMirrorThreadPages     = 5
	ascMirrorQueue           = 1024
)

var (
	// ascReviewAPI is nil until InitAscStack installs it. Tests replace it with UseAscReviewAPI.
	ascReviewAPI ascv1.ReviewClient

	// ascReadCall gives the context of a call to asc made for doer on repo as a reader, or ok false when the hub does not allow
	// it. It is nil until services/asctrust installs it.
	ascReadCall func(ctx context.Context, doer *user_model.User, repo *repo_model.Repository) (callCtx context.Context, ok bool, err error)

	// ascMirrorInline makes the mirror run in the calling goroutine. Tests set it.
	ascMirrorInline bool

	ascMirrorOnce   sync.Once
	ascMirrorQueueC chan func()
)

// SetAscReadCall installs the gate of a person's read call to asc. It returns a function that puts the previous one back.
func SetAscReadCall(f func(ctx context.Context, doer *user_model.User, repo *repo_model.Repository) (context.Context, bool, error)) (restore func()) {
	prev := ascReadCall
	ascReadCall = f
	return func() { ascReadCall = prev }
}

// UseAscReviewAPI sets the client of asc's Review service until the returned function is called. It is for tests.
func UseAscReviewAPI(c ascv1.ReviewClient) (restore func()) {
	prev, prevInline := ascReviewAPI, ascMirrorInline
	ascReviewAPI, ascMirrorInline = c, true
	return func() { ascReviewAPI, ascMirrorInline = prev, prevInline }
}

func installAscReviewMirror() {
	if setting.InternalToken == "" {
		return
	}
	c, err := ascclient.New(ascclient.Config{Target: setting.Asc.ServerURL, InternalToken: setting.InternalToken, CallTimeout: ascMirrorTimeout})
	if err != nil {
		log.Error("asc review mirror: %v", err)
		return
	}
	ascReviewAPI = ascv1.NewReviewClient(c)
}

// ascMirrorRun runs job off the request path, in order.
func ascMirrorRun(job func()) {
	if ascMirrorInline {
		job()
		return
	}
	ascMirrorOnce.Do(func() {
		ascMirrorQueueC = make(chan func(), ascMirrorQueue)
		go func() {
			for j := range ascMirrorQueueC {
				j()
			}
		}()
	})
	select {
	case ascMirrorQueueC <- job:
	default:
		log.Warn("asc review mirror: the queue is full; a comment is not mirrored")
	}
}

// ascMirrorTarget is where a mirrored call goes: the call context of doer, the asc repository name and the change.
type ascMirrorTarget struct {
	callCtx context.Context
	name    string
	change  []byte
	pr      *issues_model.PullRequest
}

// ascReviewTarget returns the target for pr, or false when nothing is to be mirrored.
func ascReviewTarget(ctx context.Context, doer *user_model.User, pr *issues_model.PullRequest, write bool) (*ascMirrorTarget, bool) {
	if ascReviewAPI == nil || doer == nil || pr == nil {
		return nil, false
	}
	name, raw, err := ascChangeOf(ctx, pr)
	if err != nil {
		return nil, false // not a stack, or not on asc
	}
	if err := pr.LoadBaseRepo(ctx); err != nil {
		return nil, false
	}
	gate := ascReadCall
	if write {
		gate = ascUserCall
	}
	if gate == nil {
		return nil, false
	}
	callCtx, allowed, err := gate(ctx, doer, pr.BaseRepo)
	if err != nil {
		log.Warn("asc review mirror: no call for user %d: %v", doer.ID, err)
		return nil, false
	}
	if !allowed {
		return nil, false
	}
	return &ascMirrorTarget{callCtx: context.WithoutCancel(callCtx), name: name, change: raw, pr: pr}, true
}

func (t *ascMirrorTarget) repo() *ascv1.RepoRef { return &ascv1.RepoRef{Name: t.name} }

func ascExternalID(commentID int64) string {
	return fmt.Sprintf("%s%d", AscExternalCommentPrefix, commentID)
}

var ascSuggestionFence = regexp.MustCompile("(?s)```suggestion[ \t]*\r?\n(.*?)\r?\n?```")

// ascSuggestionOf is the replacement text of a ```suggestion block in a comment, or "".
func ascSuggestionOf(body string) string {
	m := ascSuggestionFence.FindStringSubmatch(body)
	if m == nil || len(m[1]) > 64<<10 {
		return ""
	}
	return m[1]
}

// ascConversationRoot is the first comment of the conversation that c belongs to: the code comments of the same pull
// request on the same file and line that were not invalidated. It is c itself when it starts the conversation.
func ascConversationRoot(ctx context.Context, c *issues_model.Comment) *issues_model.Comment {
	if c.Type != issues_model.CommentTypeCode || c.TreePath == "" {
		return c
	}
	list, err := issues_model.FindComments(ctx, &issues_model.FindCommentsOptions{
		IssueID: c.IssueID, TreePath: c.TreePath, Line: c.Line, Type: issues_model.CommentTypeCode, Invalidated: optional.Some(false),
	})
	if err != nil || len(list) == 0 {
		return c
	}
	root := list[0]
	for _, o := range list {
		if o.ID < root.ID {
			root = o
		}
	}
	return root
}

// ascFindThread finds the thread of asc whose comments hold the external id, or 0.
func ascFindThread(t *ascMirrorTarget, external string) uint64 {
	token := ""
	for range ascMirrorThreadPages {
		cctx, cancel := context.WithTimeout(t.callCtx, ascMirrorTimeout)
		resp, err := ascReviewAPI.ListThreads(cctx, &ascv1.ListThreadsRequest{Repo: t.repo(), Change: t.change, Limit: 200, PageToken: token})
		cancel()
		if err != nil {
			log.Warn("asc review mirror: ListThreads for %s: %v", t.name, err)
			return 0
		}
		for _, th := range resp.GetThreads() {
			for _, c := range th.GetComments() {
				if c.GetExternalId() == external {
					return th.GetId()
				}
			}
		}
		token = resp.GetNextPageToken()
		if token == "" {
			return 0
		}
	}
	return 0
}

// ascVersionLookup returns the verified version of a commit of pr, 0 when there is none; the list is read once.
func ascVersionLookup(ctx context.Context, pr *issues_model.PullRequest) func(commit string) uint32 {
	var list *AscVersionList
	var done bool
	return func(commit string) uint32 {
		if !done {
			done = true
			l, err := AscVersions(ctx, pr)
			if err != nil {
				log.Warn("asc review mirror: the versions of pull request %d: %v", pr.ID, err)
			} else {
				list = l
			}
		}
		if list == nil || commit == "" {
			return 0
		}
		return list.ByGitCommit(commit)
	}
}

// ascMirrorComment mirrors one comment of the pull request. It never fails.
func ascMirrorComment(ctx context.Context, doer *user_model.User, pr *issues_model.PullRequest, c *issues_model.Comment, versionOf func(string) uint32) {
	if c == nil || c.ID == 0 || strings.TrimSpace(c.Content) == "" {
		return
	}
	t, ok := ascReviewTarget(ctx, doer, pr, false)
	if !ok {
		return
	}
	body := c.Content
	if len(body) > ascMirrorMaxBody {
		body = body[:ascMirrorMaxBody]
	}
	req := &ascv1.AddCommentRequest{Repo: t.repo(), Change: t.change, Body: body, ExternalId: ascExternalID(c.ID)}
	if c.Type == issues_model.CommentTypeCode && c.TreePath != "" {
		side := ascv1.DiffSide_DIFF_SIDE_NEW
		if c.Line < 0 {
			side = ascv1.DiffSide_DIFF_SIDE_OLD
		}
		start := uint32(c.UnsignedLine())
		end := uint32(c.UnsignedDisplayLine())
		var version uint32
		if versionOf != nil {
			version = versionOf(c.CommitSHA)
		}
		req.Anchor = &ascv1.ThreadAnchor{Path: []byte(c.TreePath), Version: version, Side: side, StartLine: start, EndLine: end}
		if s := ascSuggestionOf(c.Content); s != "" && side == ascv1.DiffSide_DIFF_SIDE_NEW {
			req.Suggestion = s
		}
		if root := ascConversationRoot(ctx, c); root.ID != c.ID {
			req.ThreadId = ascFindThread(t, ascExternalID(root.ID))
		}
	}
	cctx, cancel := context.WithTimeout(t.callCtx, ascMirrorTimeout)
	defer cancel()
	if _, err := ascReviewAPI.AddComment(cctx, req); err != nil {
		log.Warn("asc review mirror: AddComment for comment %d on %s: %v", c.ID, t.name, err)
	}
}

// AscMirrorResolve tells asc that the conversation that comment belongs to was resolved or reopened on the hub.
func AscMirrorResolve(ctx context.Context, doer *user_model.User, pr *issues_model.PullRequest, comment *issues_model.Comment, resolved bool) {
	if ascReviewAPI == nil || comment == nil {
		return
	}
	root := ascConversationRoot(ctx, comment)
	ascMirrorRun(func() {
		t, ok := ascReviewTarget(ctx, doer, pr, false)
		if !ok {
			return
		}
		id := ascFindThread(t, ascExternalID(root.ID))
		if id == 0 {
			return // the conversation was never mirrored (it is older than the mirror): nothing to resolve
		}
		cctx, cancel := context.WithTimeout(t.callCtx, ascMirrorTimeout)
		defer cancel()
		if _, err := ascReviewAPI.ResolveThread(cctx, &ascv1.ResolveThreadRequest{Repo: t.repo(), ThreadId: id, Resolved: resolved}); err != nil {
			log.Warn("asc review mirror: ResolveThread %d on %s: %v", id, t.name, err)
		}
	})
}

// ascMirrorReviewRequest tells asc that reviewer was asked (or no longer asked) to review pr.
func ascMirrorReviewRequest(ctx context.Context, doer *user_model.User, pr *issues_model.PullRequest, reviewer *user_model.User, isRequest bool) {
	if ascReviewAPI == nil || reviewer == nil || reviewer.IsOrganization() {
		return
	}
	ascMirrorRun(func() {
		t, ok := ascReviewTarget(ctx, doer, pr, true)
		if !ok {
			return
		}
		req := &ascv1.RequestReviewRequest{Repo: t.repo(), Change: t.change}
		if isRequest {
			req.Add = []string{reviewer.Name}
		} else {
			req.Remove = []string{reviewer.Name}
		}
		cctx, cancel := context.WithTimeout(t.callCtx, ascMirrorTimeout)
		defer cancel()
		if _, err := ascReviewAPI.RequestReview(cctx, req); err != nil {
			log.Warn("asc review mirror: RequestReview for %s on %s: %v", reviewer.Name, t.name, err)
		}
	})
}

// ascReviewMirrorNotifier turns the events of the hub into the mirror calls above.
type ascReviewMirrorNotifier struct {
	notify_service.NullNotifier
}

var _ notify_service.Notifier = (*ascReviewMirrorNotifier)(nil)

func init() {
	notify_service.RegisterNotifier(&ascReviewMirrorNotifier{})
}

func (*ascReviewMirrorNotifier) mirror(ctx context.Context, pr *issues_model.PullRequest, comments ...*issues_model.Comment) {
	if ascReviewAPI == nil || len(comments) == 0 {
		return
	}
	ctx = context.WithoutCancel(ctx)
	ascMirrorRun(func() {
		versionOf := ascVersionLookup(ctx, pr)
		for _, c := range comments {
			if err := c.LoadPoster(ctx); err != nil || c.Poster == nil {
				continue
			}
			ascMirrorComment(ctx, c.Poster, pr, c, versionOf)
		}
	})
}

// A comment on a pull request: a plain comment is a thread on the change as a whole, a code comment is a thread on lines.
func (n *ascReviewMirrorNotifier) CreateIssueComment(ctx context.Context, _ *user_model.User, _ *repo_model.Repository, issue *issues_model.Issue, comment *issues_model.Comment, _ []*user_model.User) {
	if ascReviewAPI == nil || issue == nil || !issue.IsPull || comment == nil {
		return
	}
	if comment.Type != issues_model.CommentTypeComment && comment.Type != issues_model.CommentTypeCode {
		return
	}
	if err := issue.LoadPullRequest(ctx); err != nil {
		return
	}
	n.mirror(ctx, issue.PullRequest, comment)
}

// The code comments of a review that was submitted.
func (n *ascReviewMirrorNotifier) PullRequestCodeComment(ctx context.Context, pr *issues_model.PullRequest, comment *issues_model.Comment, _ []*user_model.User) {
	if pr == nil || comment == nil {
		return
	}
	n.mirror(ctx, pr, comment)
}

func (*ascReviewMirrorNotifier) PullRequestReviewRequest(ctx context.Context, doer *user_model.User, issue *issues_model.Issue, reviewer *user_model.User, isRequest bool, _ *issues_model.Comment) {
	if ascReviewAPI == nil || issue == nil || !issue.IsPull {
		return
	}
	if err := issue.LoadPullRequest(ctx); err != nil {
		return
	}
	ascMirrorReviewRequest(context.WithoutCancel(ctx), doer, issue.PullRequest, reviewer, isRequest)
}
