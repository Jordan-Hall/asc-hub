// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	issues_model "forgejo.org/models/issues"
	user_model "forgejo.org/models/user"
	"forgejo.org/services/ascbridge"
)

// Conflicts of a stacked pull request with its target, and their resolution on the web.
//
// AscPullConflicts asks asc for a dry run of the restack of the stack (Stacks.Restack; nothing is written) and
// returns the conflicts of the pull request's own change: for each file, the text asc marked with both sides
// (the target first, the change last) and the conflicting hunks parsed from it. It reads only, so anyone who
// may read the pull request may see it.
//
// ResolveAscConflicts restacks the stack with the content the doer wrote for the conflicting files of the
// change (RestackAscStack with resolutions): the same checks and the same moves of heads as the Restack button.
// A resolution is accepted only for a text file that conflicts in the change now, and never with a conflict
// marker left in it.

// Bounds of the conflicts page.
const (
	// AscConflictMaxFiles bounds the files shown; more are counted.
	AscConflictMaxFiles = 20
	// AscConflictMaxBytes bounds the marked text of one file read for the page and the editor.
	AscConflictMaxBytes = 256 << 10
	// ascConflictMaxHunks bounds the hunks parsed from one file.
	ascConflictMaxHunks = 50
)

// Errors of the conflicts page.
var (
	// ErrAscNoConflict is returned when the change has no conflict to show or to settle.
	ErrAscNoConflict = errors.New("the change has no conflict with its target")
	// ErrAscResolutionPath is returned for a resolution of a file that does not conflict, or cannot be edited here.
	ErrAscResolutionPath = errors.New("a resolution names a file that cannot be resolved here")
	// ErrAscResolutionMarkers is returned for a resolution that still has a conflict marker.
	ErrAscResolutionMarkers = errors.New("a resolution still has conflict markers")
)

// AscConflictHunk is one conflicting region of a marked file. Line is the line of its opening marker, from 1.
type AscConflictHunk struct {
	Line  int
	Main  []string // the target's side
	Base  []string // what both started from; empty when the marked text has no base section
	Yours []string // the change's side
}

// AscConflictFile is one conflicting file of the change.
type AscConflictFile struct {
	Path string
	Kind string // ascbridge.ConflictFile.Kind
	// Marked is the text with conflict markers; "" when asc has none for the file or it is not text. Editable is
	// true when it is complete text the editor can hold.
	Marked   string
	Editable bool
	Hunks    []AscConflictHunk
	file     ascbridge.ConflictFile
}

// AscConflicts is what the conflicts page shows for one pull request.
type AscConflicts struct {
	Change    string
	State     ascbridge.RestackState
	Files     []AscConflictFile
	MoreFiles int
	// Below is the open pull request of a lower change of the stack whose conflict blocks this one; nil when
	// none, or when that change has no open pull request here.
	Below *issues_model.PullRequest
}

// ascReadConflictFile reads a marked file. Tests replace it.
var ascReadConflictFile = ascbridge.ReadFileByID

// UseAscConflictReader replaces how the marked files of a conflict are read, for tests.
func UseAscConflictReader(f func(ctx context.Context, repo, id string, limit int) ([]byte, bool, error)) (restore func()) {
	prev := ascReadConflictFile
	ascReadConflictFile = f
	return func() { ascReadConflictFile = prev }
}

// AscPullConflicts returns the conflicts of the change of pr with the target of its stack.
func AscPullConflicts(ctx context.Context, pr *issues_model.PullRequest) (*AscConflicts, error) {
	if pr == nil || pr.ID == 0 {
		return nil, ErrAscStackNotStacked
	}
	row, err := issues_model.GetPullRequestAscStack(ctx, pr.ID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, ErrAscStackNotStacked
	}
	if err := pr.LoadBaseRepo(ctx); err != nil {
		return nil, err
	}
	if backed, err := ascStackBacked(pr.BaseRepo); err != nil {
		return nil, err
	} else if !backed {
		return nil, ErrAscStackDisabled
	}
	ops, err := ascbridge.StackClient()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAscStackDisabled, err)
	}
	repo := pr.BaseRepo.FullName()
	preview, err := ops.RestackStack(ctx, ascbridge.RestackStackRequest{Repo: repo, Change: row.AscChange, DryRun: true})
	if err != nil {
		return nil, err
	}
	out := &AscConflicts{Change: row.AscChange}
	var below string
	for _, it := range preview.Items {
		if it.Change != row.AscChange {
			if it.State == ascbridge.RestackConflict && below == "" {
				below = it.Change
			}
			continue
		}
		out.State = it.State
		for _, f := range it.Files {
			if len(out.Files) >= AscConflictMaxFiles {
				out.MoreFiles++
				continue
			}
			out.Files = append(out.Files, readAscConflictFile(ctx, repo, f))
		}
		break
	}
	if out.State == ascbridge.RestackBlocked && below != "" {
		if p, err := issues_model.GetOpenPullRequestByAscChange(ctx, pr.BaseRepoID, below); err == nil {
			out.Below = p
		} else if !issues_model.IsErrPullRequestNotExist(err) {
			return nil, err
		}
	}
	return out, nil
}

// readAscConflictFile reads the marked text of f. A file asc has no marked text for, one that is not text, or one
// that cannot be read is shown by its path and kind only.
func readAscConflictFile(ctx context.Context, repo string, f ascbridge.ConflictFile) AscConflictFile {
	out := AscConflictFile{Path: f.Path, Kind: f.Kind, file: f}
	if f.MarkedID == "" {
		return out
	}
	data, truncated, err := ascReadConflictFile(ctx, repo, f.MarkedID, AscConflictMaxBytes)
	if err != nil || !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return out
	}
	out.Marked = string(data)
	out.Editable = !truncated
	out.Hunks = ParseAscConflictHunks(out.Marked)
	return out
}

// ascMarker reports whether line is a conflict marker of kind c: seven of c, then the end of the line or a space.
func ascMarker(line string, c byte) bool {
	if len(line) < 7 || strings.Count(line[:7], string(c)) != 7 {
		return false
	}
	return len(line) == 7 || line[7] == ' '
}

// ParseAscConflictHunks returns the conflicting hunks of a marked text: the first section is the target's, a
// "|||||||" section the base, the last section the change's. An unclosed hunk is left out.
func ParseAscConflictHunks(marked string) []AscConflictHunk {
	var out []AscConflictHunk
	var cur *AscConflictHunk
	section := 0 // 1 main, 2 base, 3 yours
	for i, line := range strings.Split(strings.TrimSuffix(marked, "\n"), "\n") {
		line = strings.TrimSuffix(line, "\r")
		switch {
		case ascMarker(line, '<'):
			cur, section = &AscConflictHunk{Line: i + 1}, 1
		case cur == nil:
		case ascMarker(line, '|') && section == 1:
			section = 2
		case line == "=======" && (section == 1 || section == 2):
			section = 3
		case ascMarker(line, '>') && section == 3:
			if len(out) < ascConflictMaxHunks {
				out = append(out, *cur)
			}
			cur, section = nil, 0
		case section == 1:
			cur.Main = append(cur.Main, line)
		case section == 2:
			cur.Base = append(cur.Base, line)
		case section == 3:
			cur.Yours = append(cur.Yours, line)
		}
	}
	return out
}

// HasAscConflictMarkers reports whether text still has a line that opens, splits or closes a conflict.
func HasAscConflictMarkers(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if ascMarker(line, '<') || ascMarker(line, '>') || ascMarker(line, '|') {
			return true
		}
	}
	return false
}

// ResolveAscConflicts restacks the stack of pr with contents (path to the new text of the file) settling the
// conflicts of its change, as doer.
func ResolveAscConflicts(ctx context.Context, doer *user_model.User, pr *issues_model.PullRequest, contents map[string]string) (*AscRestackStackResult, error) {
	if doer == nil || !doer.IsActive || doer.ProhibitLogin {
		return nil, ErrAscRestackNotAllowed
	}
	if pr == nil || pr.ID == 0 {
		return nil, ErrAscStackNotStacked
	}
	// The doer must be allowed to rewrite pr before asc is asked anything.
	if err := pr.LoadBaseRepo(ctx); err != nil {
		return nil, err
	}
	if ok, err := AscMayRewrite(ctx, doer, pr); err != nil {
		return nil, err
	} else if !ok {
		return nil, fmt.Errorf("%w: #%d", ErrAscRestackNotAllowed, pr.Index)
	}
	if len(contents) == 0 {
		return nil, ErrAscNoConflict
	}
	now, err := AscPullConflicts(ctx, pr)
	if err != nil {
		return nil, err
	}
	if now.State != ascbridge.RestackConflict || len(now.Files) == 0 {
		return nil, ErrAscNoConflict
	}
	files := make(map[string]AscConflictFile, len(now.Files))
	for _, f := range now.Files {
		files[f.Path] = f
	}
	total := 0
	resolutions := make([]ascbridge.Resolution, 0, len(contents))
	for path, text := range contents {
		f, ok := files[path]
		if !ok || !f.Editable {
			return nil, fmt.Errorf("%w: %s", ErrAscResolutionPath, path)
		}
		// A browser sends the text of a form with CRLF line ends; a file that had LF keeps LF.
		if !strings.Contains(f.Marked, "\r\n") {
			text = strings.ReplaceAll(text, "\r\n", "\n")
		}
		if HasAscConflictMarkers(text) {
			return nil, fmt.Errorf("%w: %s", ErrAscResolutionMarkers, path)
		}
		total += len(text)
		if total > ascbridge.MaxResolutionBytes || !utf8.ValidString(text) {
			return nil, fmt.Errorf("%w: %s", ErrAscResolutionPath, path)
		}
		resolutions = append(resolutions, ascbridge.Resolution{ChangesetID: f.file.ChangesetID, Path: f.file.Path, Content: []byte(text)})
	}
	return RestackAscStack(ctx, doer, pr, AscRestackOptions{Resolutions: resolutions})
}
