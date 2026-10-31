// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"math"
	"strconv"
	"strings"

	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/services/context"
)

// The search box of the issue and pull request lists accepts `label:name` and `author:login` next to the words
// (`label:"good first issue" author:maya retry`). They are the same filters as the label menu and the author menu: the list
// takes them out of the words, applies them, and the menus show them chosen. A name that matches nothing matches nothing.

// issueQuery is a search box text taken apart.
type issueQuery struct {
	Keyword string   // the words and every other token (is:open ...), as typed
	Labels  []string // label:name, in the order typed
	Authors []string // author:login
}

const maxQueryFilters = 10

// splitQuery cuts q at spaces, keeping a quoted run together (label:"two words" is one token).
func splitQuery(q string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	for _, r := range q {
		switch {
		case r == '"':
			inQuote = !inQuote
			cur.WriteRune(r)
		case (r == ' ' || r == '\t') && !inQuote:
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// parseIssueQuery takes `label:` and `author:` tokens out of q.
func parseIssueQuery(q string) issueQuery {
	var out issueQuery
	var rest []string
	for _, tok := range splitQuery(q) {
		key, val, ok := strings.Cut(tok, ":")
		val = strings.Trim(val, `"`)
		switch {
		case ok && strings.EqualFold(key, "label") && val != "" && len(out.Labels) < maxQueryFilters:
			out.Labels = append(out.Labels, val)
		case ok && strings.EqualFold(key, "author") && val != "" && len(out.Authors) < maxQueryFilters:
			out.Authors = append(out.Authors, val)
		default:
			rest = append(rest, tok)
		}
	}
	out.Keyword = strings.Join(rest, " ")
	return out
}

// nothingID is an id that no label or person has: a filter by it matches nothing.
const nothingID = int64(math.MaxInt64)

// queryLabelIDs turns label names into the ids of the labels of the repository (and of its organization); a name that is none
// of them becomes nothingID, so the list is empty rather than unfiltered.
func queryLabelIDs(ctx *context.Context, repo *repo_model.Repository, names []string) []int64 {
	var ids []int64
	for _, name := range names {
		found := int64(0)
		if l, err := issues_model.GetLabelInRepoByName(ctx, repo.ID, name); err == nil {
			found = l.ID
		} else if !issues_model.IsErrRepoLabelNotExist(err) {
			log.Warn("issue search: label %q: %v", name, err)
		}
		if found == 0 && repo.Owner != nil && repo.Owner.IsOrganization() {
			if l, err := issues_model.GetLabelInOrgByName(ctx, repo.OwnerID, name); err == nil {
				found = l.ID
			}
		}
		if found == 0 {
			found = nothingID
		}
		ids = append(ids, found)
	}
	return ids
}

// queryPosterID is the id of the person an author: filter names (the first one when several are typed), or nothingID.
func queryPosterID(ctx *context.Context, authors []string) int64 {
	if len(authors) == 0 {
		return 0
	}
	u, err := user_model.GetUserByName(ctx, authors[0])
	if err != nil {
		return nothingID
	}
	return u.ID
}

// mergeIDs appends the ids that are not in the list yet.
func mergeIDs(list, add []int64) []int64 {
	for _, id := range add {
		dup := false
		for _, have := range list {
			if have == id {
				dup = true
				break
			}
		}
		if !dup {
			list = append(list, id)
		}
	}
	return list
}

// joinIDs is the `labels=1,2` form of the label menu.
func joinIDs(ids []int64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(parts, ",")
}
