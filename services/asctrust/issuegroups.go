// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"html/template"
	"net/url"
	"slices"
	"strconv"
	"strings"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"

	"google.golang.org/grpc"
)

// Issues and failure groups. "Open an issue" on a failure group fills in the body of the new issue with the address of
// the group; an issue whose text holds the address of a group of its own repository is about that group. The issue list
// shows such an issue with the chip "Quarantined" while the group is quarantined, so the issue that explains why a test
// does not hold up landing is easy to find. The link is the text of the issue: editing the address out removes it.

const (
	// MaxGroupRefsPerIssue bounds the group addresses read from one issue.
	MaxGroupRefsPerIssue = 5
	// quarantinePageSize and maxQuarantinePages bound the reading of the quarantined groups for one issue list.
	quarantinePageSize = 100
	maxQuarantinePages = 5
)

// GroupURL is the full address of a failure group of a repository.
func GroupURL(owner, repo string, id uint64) string {
	return setting.AppURL + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/asc/checks/groups/" + strconv.FormatUint(id, 10)
}

// GroupIssueQuery is the query string of the new-issue link of a failure group: the title and a body that names the
// group by its address. Every value is query-escaped, so the query is safe to put after "?" in a link as it is.
func GroupIssueQuery(title, body string) template.URL {
	return template.URL("title=" + url.QueryEscape(title) + "&body=" + url.QueryEscape(body))
}

// GroupRefs reads the ids of the failure groups of owner/repo that the text names by address, in order, without
// repeats, at most MaxGroupRefsPerIssue. The address is either the full address on this hub or its path on its own
// (starting the text or after a space or an opening bracket or quote); an address on another host is not a reference.
// Owner and repo match without regard to case, like the routes.
func GroupRefs(text, owner, repo string) []uint64 {
	if text == "" {
		return nil
	}
	lower := strings.ToLower(text)
	marker := strings.ToLower(setting.AppSubURL + "/" + owner + "/" + repo + "/asc/checks/groups/")
	host := strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(setting.AppURL, "/"), setting.AppSubURL))
	var out []uint64
	for at := 0; len(out) < MaxGroupRefsPerIssue; {
		i := strings.Index(lower[at:], marker)
		if i < 0 {
			break
		}
		start := at + i
		at = start + len(marker)
		n := 0
		for at+n < len(lower) && lower[at+n] >= '0' && lower[at+n] <= '9' {
			n++
		}
		if n == 0 || n > 19 || !refStart(lower[:start], host) || (at+n < len(lower) && isNameByte(lower[at+n])) {
			continue
		}
		id, err := strconv.ParseUint(lower[at:at+n], 10, 64)
		if err != nil || id == 0 || slices.Contains(out, id) {
			continue
		}
		out = append(out, id)
	}
	return out
}

// refStart tells whether a group path may start after before: the host of this hub, or no address at all.
func refStart(before, host string) bool {
	if before == "" || (host != "" && strings.HasSuffix(before, host)) {
		return true
	}
	return strings.IndexByte(" \t\r\n(<[\"'", before[len(before)-1]) >= 0
}

// isNameByte tells whether c continues a word (the text is in lower case), so "3abc" is no id; a full stop or a
// bracket after the id ends it.
func isNameByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_'
}

// LoadQuarantinedOf reads which of the wanted groups are quarantined. It pages through the quarantined groups, most
// recently seen first, until every wanted group is found or maxQuarantinePages pages were read; a group past that is
// not shown as quarantined. An answer with a group in another state cannot be trusted and is an error.
func LoadQuarantinedOf(ctx context.Context, conn grpc.ClientConnInterface, repoName string, want map[uint64]bool) (map[uint64]GroupRow, error) {
	out := map[uint64]GroupRow{}
	if len(want) == 0 {
		return out, nil
	}
	token := ""
	for range maxQuarantinePages {
		cctx, cancel := context.WithTimeout(ctx, CallTimeout)
		resp, err := ascv1.NewCiInsightsClient(conn).ListFailureGroups(cctx, &ascv1.ListFailureGroupsRequest{
			Repo:      RepoRef(repoName),
			States:    []ascv1.FailureGroupState{ascv1.FailureGroupState_FAILURE_GROUP_STATE_QUARANTINED},
			Limit:     quarantinePageSize,
			PageToken: token,
		})
		cancel()
		if err != nil {
			return nil, err
		}
		page, err := GroupsPageOf(resp)
		if err != nil {
			return nil, err
		}
		if len(page.Rows) > quarantinePageSize {
			return nil, ErrBadAnswer
		}
		for _, r := range page.Rows {
			if r.StateKey != "quarantined" {
				return nil, ErrBadAnswer
			}
			if want[r.ID] {
				out[r.ID] = r
			}
		}
		if len(out) == len(want) || page.Next == "" || page.Next == token {
			break
		}
		token = page.Next
	}
	return out, nil
}
