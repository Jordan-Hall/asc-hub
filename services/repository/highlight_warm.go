// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repository

import (
	"context"
	"io"
	"path"
	"strings"
	"time"

	"forgejo.org/modules/git"
	"forgejo.org/modules/highlight"
	"forgejo.org/modules/log"
	"forgejo.org/modules/typesniffer"
)

// Highlighting a file is the slowest step of showing it, so the files a push changed are highlighted right after the
// push, in the push queue and under a budget, and the result lands in the highlight cache. The first person to open
// the file then finds it ready. Nothing here is needed for correctness: a file that is skipped, too big or too slow is
// highlighted when someone first views it.
const (
	// warmMaxFiles is how many changed files one push warms.
	warmMaxFiles = 40
	// warmMaxBytes is the largest blob that is warmed; bigger files are highlighted on view, under the view's own budget.
	warmMaxBytes = 128 * 1024
	// warmBudget is the time one push may spend warming.
	warmBudget = 5 * time.Second
)

// selectWarmPaths keeps the paths worth warming: no more than max, none that look like generated or vendored
// content by name, none twice.
func selectWarmPaths(paths []string, max int) []string {
	seen := make(map[string]bool, len(paths))
	var out []string
	for _, p := range paths {
		if len(out) >= max {
			break
		}
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		base := strings.ToLower(path.Base(p))
		if strings.HasSuffix(base, ".min.js") || strings.HasSuffix(base, ".min.css") || strings.HasSuffix(base, ".lock") || strings.HasSuffix(base, ".map") {
			continue
		}
		out = append(out, p)
	}
	return out
}

// warmHighlights highlights the text files that changed between base and head. It never fails the push.
func warmHighlights(ctx context.Context, gitRepo *git.Repository, head *git.Commit, base string) {
	if head == nil || base == "" {
		return
	}
	changed, err := gitRepo.GetFilesChangedBetween(base, head.ID.String())
	if err != nil {
		log.Trace("warm highlights: changed files: %v", err)
		return
	}
	deadline := time.Now().Add(warmBudget)
	for _, p := range selectWarmPaths(changed, warmMaxFiles) {
		if ctx.Err() != nil || time.Now().After(deadline) {
			return
		}
		blob, err := head.GetBlobByPath(p)
		if err != nil || blob.Size() > warmMaxBytes {
			continue
		}
		rc, err := blob.DataAsync()
		if err != nil {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(rc, warmMaxBytes+1))
		_ = rc.Close()
		if err != nil || len(data) > warmMaxBytes || !typesniffer.DetectContentType(data, blob.Name()).IsText() {
			continue
		}
		highlight.Warm(blob.Name(), data)
	}
}
