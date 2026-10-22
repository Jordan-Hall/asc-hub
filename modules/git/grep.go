// Copyright 2024 The Gitea Authors. All rights reserved.
// Copyright 2024 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package git

import (
	"cmp"
	"context"
)

type GrepResult struct {
	Filename          string
	LineNumbers       []int
	LineCodes         []string
	HighlightedRanges [][3]int
}

type GrepMode int

const (
	FixedGrepMode GrepMode = iota
	FixedAnyGrepMode
	RegExpGrepMode
)

// llu:TrKeysSuffix search.
var GrepSearchOptions = [3]string{"exact", "union", "regexp"}

func (mode GrepMode) String() string {
	return GrepSearchOptions[mode]
}

type GrepOptions struct {
	RefName           string
	MaxResultLimit    int
	MatchesPerFile    int // >= git 2.38
	ContextLineNumber int
	Mode              GrepMode
	Filename          string
}

func (opts *GrepOptions) ensureDefaults() {
	opts.RefName = cmp.Or(opts.RefName, "HEAD")
	opts.MaxResultLimit = cmp.Or(opts.MaxResultLimit, 50)
	opts.MatchesPerFile = cmp.Or(opts.MatchesPerFile, 20)
}

// GrepSearch searches the tree of opts.RefName (HEAD by default) for search.
func GrepSearch(ctx context.Context, repo *Repository, search string, opts GrepOptions) ([]*GrepResult, error) {
	opts.ensureDefaults()
	be, err := repo.Backend()
	if err != nil {
		return nil, err
	}
	at, err := repo.revToID(ctx, be, opts.RefName)
	if err != nil {
		return nil, err
	}
	return be.Grep(ctx, at, GrepQuery{Search: search, Options: opts})
}
