// SPDX-License-Identifier: MIT

package webhook

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	webhook_model "forgejo.org/models/webhook"
	"forgejo.org/modules/util"
	webhook_module "forgejo.org/modules/webhook"
)

// listEvents is how many event names a list row spells out before it says "+N more".
const listEvents = 2

// streakLimit is how many of a hook's latest deliveries a list reads to say how long it has been failing.
const streakLimit = 10

// ListRow is one webhook as a list of webhooks shows it: what it sends and how its last delivery went.
type ListRow struct {
	*webhook_model.Webhook
	// Last is the latest delivered task, nil when the hook never delivered.
	Last *webhook_model.HookTask
	// AllEvents is set when the hook sends everything; Events then is empty.
	AllEvents bool
	// Events holds the locale keys of the first event groups the hook sends, MoreEvents counts the rest.
	Events     []string
	MoreEvents int
	// Scope names where the hook applies, for a list that mixes scopes (System, Default); empty otherwise.
	Scope string
	// FailStreak is how many of the latest deliveries failed in a row (at most streakLimit), 0 when the last one succeeded.
	FailStreak int
}

// Kind is the locale key of what the hook is for: a generic receiver signed with a secret, a chat service, or a build or package service.
func (r *ListRow) Kind() string {
	switch r.Type {
	case webhook_module.FORGEJO, webhook_module.GITEA, webhook_module.GOGS:
		return "asc_hooks.kind_generic"
	case webhook_module.SLACK, webhook_module.DISCORD, webhook_module.DINGTALK, webhook_module.TELEGRAM, webhook_module.MSTEAMS,
		webhook_module.FEISHU, webhook_module.MATRIX, webhook_module.WECHATWORK:
		return "asc_hooks.kind_chat"
	}
	return "asc_hooks.kind_service"
}

// Failing reports whether the hook failed more than once in a row, which the list says in a sentence.
func (r *ListRow) Failing() bool {
	return r.FailStreak > 1
}

// Host is the host the hook delivers to, the row's title; the whole URL when it has none.
func (r *ListRow) Host() string {
	if u, err := url.Parse(r.URL); err == nil && u.Host != "" {
		return u.Host
	}
	return r.URL
}

// URLScheme is the scheme of the hook's address with its "://", which a list prints dimmed; empty when the address has none.
func (r *ListRow) URLScheme() string {
	if scheme, _, ok := strings.Cut(r.URL, "://"); ok {
		return scheme + "://"
	}
	return ""
}

// URLRest is the hook's address after its scheme.
func (r *ListRow) URLRest() string {
	return strings.TrimPrefix(r.URL, r.URLScheme())
}

// Status is the HTTP status of the last delivery, 0 when there was none or no response came back.
func (r *ListRow) Status() int {
	if r.Last == nil || r.Last.ResponseInfo == nil {
		return 0
	}
	return r.Last.ResponseInfo.Status
}

// StatusLine is the status of the last delivery with its reason phrase, as "502 Bad Gateway".
func (r *ListRow) StatusLine() string {
	return strings.TrimSpace(fmt.Sprintf("%d %s", r.Status(), http.StatusText(r.Status())))
}

// Failed reports whether the last delivery failed, which is when the list offers Replay.
func (r *ListRow) Failed() bool {
	return r.Last != nil && !r.Last.IsSucceed
}

// ListRows pairs each webhook with its last delivery, how long it has been failing and its event summary.
func ListRows(ctx context.Context, hooks []*webhook_model.Webhook) ([]*ListRow, error) {
	ids := make([]int64, len(hooks))
	for i, w := range hooks {
		ids[i] = w.ID
	}
	last, err := webhook_model.LastDeliveries(ctx, ids)
	if err != nil {
		return nil, err
	}
	streaks, err := webhook_model.FailingStreaks(ctx, ids, streakLimit)
	if err != nil {
		return nil, err
	}
	rows := make([]*ListRow, len(hooks))
	for i, w := range hooks {
		r := &ListRow{Webhook: w, Last: last[w.ID], AllEvents: w.SendEverything, FailStreak: streaks[w.ID]}
		if !r.AllEvents {
			groups := EventGroups(w)
			r.Events = groups[:min(len(groups), listEvents)]
			r.MoreEvents = len(groups) - len(r.Events)
		}
		rows[i] = r
	}
	return rows, nil
}

// EventGroups names the groups of events a hook that does not send everything sends, as locale keys,
// in the order of the webhook form. A push-only hook sends the push group.
func EventGroups(w *webhook_model.Webhook) []string {
	if w.PushOnly {
		return []string{"repo.settings.event_push"}
	}
	if !w.ChooseEvents {
		return nil
	}
	e := w.HookEvents
	groups := []struct {
		on  bool
		key string
	}{
		{e.Create, "repo.settings.event_create"},
		{e.Delete, "repo.settings.event_delete"},
		{e.Fork, "repo.settings.event_fork"},
		{e.Push, "repo.settings.event_push"},
		{e.Repository, "repo.settings.event_repository"},
		{e.Release, "repo.settings.event_release"},
		{e.Package, "repo.settings.event_package"},
		{e.Wiki, "repo.settings.event_wiki"},
		{e.Issues || e.IssueAssign || e.IssueLabel || e.IssueMilestone, "asc_hooks.ev_issues"},
		{e.IssueComment, "asc_hooks.ev_issue_comments"},
		{e.PullRequest || e.PullRequestAssign || e.PullRequestLabel || e.PullRequestMilestone || e.PullRequestSync, "asc_hooks.ev_pull_requests"},
		{e.PullRequestComment, "asc_hooks.ev_pull_request_comments"},
		{e.PullRequestReview || e.PullRequestReviewRequest, "asc_hooks.ev_reviews"},
		{e.ActionRunFailure || e.ActionRunRecover || e.ActionRunSuccess, "asc_hooks.ev_action_runs"},
	}
	keys := make([]string, 0, len(groups))
	for _, g := range groups {
		if g.on {
			keys = append(keys, g.key)
		}
	}
	return keys
}

// HasSecret reports whether a hook of this type signs its deliveries with a secret the receiver checks.
func HasSecret(t webhook_module.HookType) bool {
	return t == webhook_module.FORGEJO || t == webhook_module.GITEA || t == webhook_module.GOGS
}

// NewSecret makes a webhook secret.
func NewSecret() string {
	return util.CryptoRandomString(util.RandomStringHigh)
}
