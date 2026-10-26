// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascagentui

import (
	"context"
	"strconv"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
)

// MaxChangeLinks bounds the changes a run page resolves to pull requests.
const MaxChangeLinks = 64

// ChangeLink is one change of a run, bottom to top, and where to review it.
type ChangeLink struct {
	Text     string // the 32 letter change id
	Short    string
	Position int // 1 is the bottom of the stack
	URL      string
	HasPull  bool
	PullID   int64 // the pull request index, 0 without one
	Merged   bool
	Closed   bool
}

type stackPull struct {
	AscChange string
	Index     int64
	HasMerged bool
	IsClosed  bool
}

// ChangeLinks resolves the changes of a run to their review pages. A change that was never submitted for review
// has no pull request and is shown without a link. Only the repository's own stack rows are read, found by
// (repository, change id), never by anything a request names. Pass canReadPulls false when the viewer may not
// read pull requests: no links are made then. Ids of the wrong length are skipped.
func ChangeLinks(ctx context.Context, repo *repo_model.Repository, repoLink string, changes [][]byte, canReadPulls bool) ([]ChangeLink, error) {
	out := make([]ChangeLink, 0, min(len(changes), MaxChangeLinks))
	texts := make([]string, 0, cap(out))
	for _, raw := range changes {
		if len(out) >= MaxChangeLinks {
			break
		}
		text := ChangeText(raw)
		if text == "" {
			continue
		}
		out = append(out, ChangeLink{Text: text, Short: ShortChange(text), Position: len(out) + 1})
		texts = append(texts, text)
	}
	if len(out) == 0 || !canReadPulls || repo == nil {
		return out, nil
	}

	var rows []stackPull
	err := db.GetEngine(ctx).
		Table("pull_request_asc_stack").
		Select("pull_request_asc_stack.asc_change AS asc_change, issue.`index` AS `index`, pull_request.has_merged AS has_merged, issue.is_closed AS is_closed").
		Join("INNER", "pull_request", "pull_request.id = pull_request_asc_stack.pull_id").
		Join("INNER", "issue", "issue.id = pull_request.issue_id").
		Where("pull_request_asc_stack.repo_id = ?", repo.ID).
		In("pull_request_asc_stack.asc_change", texts).
		OrderBy("pull_request.id ASC").
		Find(&rows)
	if err != nil {
		return nil, err
	}
	// Later rows (newer pull requests of the same change) win, so a re-submitted change links to its current page.
	byChange := make(map[string]stackPull, len(rows))
	for _, r := range rows {
		byChange[r.AscChange] = r
	}
	for i := range out {
		if r, ok := byChange[out[i].Text]; ok {
			out[i].HasPull = true
			out[i].PullID = r.Index
			out[i].Merged = r.HasMerged
			out[i].Closed = r.IsClosed && !r.HasMerged
			out[i].URL = repoLink + "/pulls/" + strconv.FormatInt(r.Index, 10)
		}
	}
	return out, nil
}
