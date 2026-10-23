// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package ascbackend

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
)

// maxClassifiedFiles bounds the files one Classify answer may list (the server's classify MAX_FILES).
const maxClassifiedFiles = 100_000

// FileClass is the change class asc assigned to one changed path.
type FileClass struct {
	Path  string
	Class ascv1.ChangeClass
}

// Classification is a verified Classify answer.
type Classification struct {
	Files   []FileClass
	Overall ascv1.ChangeClass
}

// knownChangeClass reports whether c is a class this client understands. The enum's zero value is the
// most relaxed class, so a value outside the list is refused rather than read as a class.
func knownChangeClass(c ascv1.ChangeClass) bool {
	return c >= ascv1.ChangeClass_CHANGE_CLASS_RENAME_ONLY && c <= ascv1.ChangeClass_CHANGE_CLASS_LOGIC
}

// Classify asks the server for the change class of every file changed between base and head
// (revision strings; with mergeBase, base is replaced by the merge base of base and head). It checks
// the answer before returning it: every class is known, paths are valid and unique, the count is
// within bounds and overall is the maximum of the file classes. A failed check is ErrBadAnswer.
func (b *Backend) Classify(ctx context.Context, base, head string, mergeBase bool) (*Classification, error) {
	if !validRev(base) || !validRev(head) {
		return nil, errors.New("ascbackend: invalid revision")
	}
	ctx, cancel, err := b.unary(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	resp, err := b.api.Classify(ctx, &ascv1.ClassifyRequest{Repo: b.gitRef(), Base: base, Head: head, MergeBase: mergeBase})
	if err != nil {
		return nil, callErr("Classify", head, err)
	}
	return verifyClassification(resp)
}

func verifyClassification(resp *ascv1.ClassifyResponse) (*Classification, error) {
	files := resp.GetFiles()
	if len(files) > maxClassifiedFiles {
		return nil, fmt.Errorf("%w: more than %d classified files", ErrTooLarge, maxClassifiedFiles)
	}
	if !knownChangeClass(resp.GetOverall()) {
		return nil, badAnswer("unknown overall change class")
	}
	out := &Classification{Files: make([]FileClass, 0, len(files)), Overall: resp.GetOverall()}
	seen := make(map[string]struct{}, len(files))
	highest := ascv1.ChangeClass_CHANGE_CLASS_RENAME_ONLY
	for _, f := range files {
		p := f.GetPath()
		if len(p) == 0 || len(p) > maxPathBytes || bytes.IndexByte(p, 0) >= 0 {
			return nil, badAnswer("classified path is invalid")
		}
		if !knownChangeClass(f.GetClass()) {
			return nil, badAnswer("unknown file change class")
		}
		if _, dup := seen[string(p)]; dup {
			return nil, badAnswer("classified path is listed twice")
		}
		seen[string(p)] = struct{}{}
		highest = max(highest, f.GetClass())
		out.Files = append(out.Files, FileClass{Path: string(p), Class: f.GetClass()})
	}
	if len(files) > 0 && highest != resp.GetOverall() {
		return nil, badAnswer("overall change class is not the maximum of the file classes")
	}
	return out, nil
}

// ClassifyRepository classifies the changes of the asc repository stored at repoPath with the
// process-wide client. It fails with ErrNotConfigured while no client is set up.
func ClassifyRepository(ctx context.Context, repoPath, base, head string, mergeBase bool) (*Classification, error) {
	shared.mu.RLock()
	api, opts := shared.api, shared.opts
	shared.mu.RUnlock()
	if api == nil {
		return nil, ErrNotConfigured
	}
	name, err := RepoName(setting.RepoRootPath, repoPath)
	if err != nil {
		return nil, err
	}
	b, err := New(api, name, nil, opts)
	if err != nil {
		return nil, err
	}
	defer func() { _ = b.Close() }()
	return b.Classify(ctx, base, head, mergeBase)
}
