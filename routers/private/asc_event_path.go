// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package private

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/setting"
)

// The pages asc serve links to, and the pages of the hub they are. asc composes links from what it knows (the repository,
// the change id, the run id) and the hub gives them their real address, so a link in a notification never leads to a
// 404 and never leaves the repository. The address is hub-relative: without the sub-path of the site, which is added
// where the link is used.
//
//	asc writes                          the hub page
//	/{repo}                             /{repo}
//	/{repo}/changes/{change}            /{repo}/pulls/{index}    (the pull request of the change; /{repo}/pulls when it has none)
//	/{repo}/pulls/{index}               kept
//	/{repo}/issues/{index}              kept
//	/{repo}/agents                      /{repo}/asc/agents
//	/{repo}/agents/runs/{id}            /{repo}/asc/agents/runs/{id}
//	/{repo}/merge-queue                 /{repo}/asc/queue
//	/{repo}/settings/policy             /{repo}/settings/asc/policy
//	/{repo}/asc/...                     kept (agents, workflows, queue)
//	/{repo}/settings/asc/...            kept (policy, security, audit)
//	anything else                       /{repo}
var (
	ascChangeRe    = regexp.MustCompile(`^[k-z]{32}$`)
	ascRunIDRe     = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	ascKeptRe      = regexp.MustCompile(`^/(?:pulls(?:/[0-9]{1,12})?|issues(?:/[0-9]{1,12})?|asc/(?:agents|workflows|queue)(?:/[A-Za-z0-9_./-]{0,120})?|settings/asc/(?:policy|security|audit))$`)
	ascSuffixOkRe  = regexp.MustCompile(`^[?#][A-Za-z0-9_=&.%-]{0,60}$`)
	ascPathCharsRe = regexp.MustCompile(`^/[A-Za-z0-9_./-]*$`)
)

// ascChangePullIndex finds the pull request of a change of the repository, an open one first and else the newest.
var ascChangePullIndex = func(ctx context.Context, repoID int64, change string) (int64, bool) {
	var idx int64
	has, err := db.GetEngine(ctx).Table("pull_request").
		Select("issue.`index`").
		Join("INNER", "pull_request_asc_stack", "pull_request_asc_stack.pull_id = pull_request.id").
		Join("INNER", "issue", "issue.id = pull_request.issue_id").
		Where("pull_request_asc_stack.repo_id = ? AND pull_request_asc_stack.asc_change = ?", repoID, change).
		OrderBy("issue.is_closed ASC, pull_request.id DESC").
		Get(&idx)
	return idx, err == nil && has && idx > 0
}

// ascCanonicalPath turns the path asc sent into the page of the hub, always inside the repository.
func ascCanonicalPath(ctx context.Context, repo *repo_model.Repository, p string) string {
	base := "/" + repo.FullName()
	if p == "" {
		return base
	}
	suffix := ""
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p, suffix = p[:i], p[i:]
		if !ascSuffixOkRe.MatchString(suffix) {
			suffix = ""
		}
	}
	if sub := setting.AppSubURL; sub != "" && strings.HasPrefix(p, sub+"/") {
		p = strings.TrimPrefix(p, sub)
	}
	p = strings.TrimRight(p, "/")
	if !ascPathCharsRe.MatchString(p) || strings.Contains(p, "..") || strings.Contains(p, "//") {
		return base
	}
	if len(p) < len(base) || !strings.EqualFold(p[:len(base)], base) {
		return base // another repository, or none
	}
	rest := p[len(base):]
	if rest != "" && rest[0] != '/' {
		return base // /owner/repo-other
	}
	switch {
	case rest == "":
		return base
	case strings.HasPrefix(rest, "/changes/"):
		change := strings.TrimPrefix(rest, "/changes/")
		if !ascChangeRe.MatchString(change) {
			return base
		}
		if idx, ok := ascChangePullIndex(ctx, repo.ID, change); ok {
			return base + "/pulls/" + strconv.FormatInt(idx, 10) + suffix
		}
		return base + "/pulls"
	case rest == "/agents", rest == "/agents/runs":
		return base + "/asc/agents" + suffix
	case strings.HasPrefix(rest, "/agents/runs/"):
		id := strings.TrimPrefix(rest, "/agents/runs/")
		if !ascRunIDRe.MatchString(id) {
			return base + "/asc/agents"
		}
		return base + "/asc/agents/runs/" + id + suffix
	case rest == "/merge-queue":
		return base + "/asc/queue" + suffix
	case rest == "/settings/policy":
		return base + "/settings/asc/policy" + suffix
	case ascKeptRe.MatchString(rest):
		return base + rest + suffix
	}
	return base
}
