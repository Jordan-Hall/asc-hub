// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package ascsync

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	repo_model "forgejo.org/models/repo"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git"
)

const (
	// MaxHistoryRefs bounds the branches and tags one history import reads back from asc or sends.
	MaxHistoryRefs = 500_000
	// refPage is the page size asked of ListRefs; mapBatch the number of ids per GitMap call.
	refPage  = 1000
	mapBatch = 4096
)

// ImportHistory hands the history that a clone left in the bridge directory of an asc repository to asc: the
// branches and tags in want (PushRef.Name and PushRef.New; Old is ignored) are imported from <bridge>/objects
// with the normal import pipeline in fast-forward mode (the path rules, limits and gates of asc apply), a batch
// at a time, so a history of any size streams from disk and none of it is held in memory.
//
// It is safe to run again over a repository that holds part of the history: a ref that already points at its
// commit is left alone, a ref asc does not have is created, and a ref asc has at an ancestor is moved forward. It
// never forces a ref and never deletes one: a refusal (a path policy, a ref that is not a fast-forward) stops
// the import and is returned, as a *RejectError when the text is asc's own. Use WithBulkImport for the time a
// whole history needs and WithImporter to attribute the import to the user it is made for.
func ImportHistory(ctx context.Context, repo *repo_model.Repository, want []PushRef) error {
	if len(want) > MaxHistoryRefs {
		return fmt.Errorf("%w: %d refs (at most %d)", ErrBadRequest, len(want), MaxHistoryRefs)
	}
	h, err := open(repo)
	if err != nil {
		return err
	}
	have, err := h.gitRefs(ctx)
	if err != nil {
		return err
	}
	var todo []PushRef
	for _, w := range want {
		if isZeroID(w.New) {
			continue
		}
		cur, ok := have[w.Name]
		switch {
		case ok && cur == w.New:
			continue
		case ok:
			todo = append(todo, PushRef{Name: w.Name, Old: cur, New: w.New})
		default:
			todo = append(todo, PushRef{Name: w.Name, Old: strings.Repeat("0", 40), New: w.New})
		}
	}
	slices.SortFunc(todo, func(a, b PushRef) int { return strings.Compare(a.Name, b.Name) })
	dirs := PushDirs{ObjectDir: filepath.Join(repo.RepoPath(), "objects")}
	for start := 0; start < len(todo); start += MaxRefUpdates {
		batch := todo[start:min(start+MaxRefUpdates, len(todo))]
		if err := ImportPush(ctx, ImportRequest{Repo: repo, Dirs: dirs, Refs: batch}); err != nil {
			return err
		}
	}
	return nil
}

// gitRefs lists the branches and tags of the repository in asc with the git id each points at. A ref whose
// target has no git id yet (an annotated tag asc has not mapped) is left out, so an import brings it over.
func (h *ascRepo) gitRefs(ctx context.Context) (map[string]string, error) {
	type pending struct {
		name   string
		target []byte
	}
	var found []pending
	after := ""
	for {
		cctx, cancel := system(ctx, CallTimeout)
		resp, err := h.api.ListRefs(cctx, &ascv1.ListRefsRequest{
			Repo:   &ascv1.RepoRef{Name: h.name, IdMode: ascv1.IdMode_ID_MODE_ASC},
			Prefix: "refs/", After: after, Limit: refPage,
		})
		cancel()
		if err != nil {
			return nil, callError("list refs", err)
		}
		for _, r := range resp.GetRefs() {
			if !strings.HasPrefix(r.GetName(), git.BranchPrefix) && !strings.HasPrefix(r.GetName(), git.TagPrefix) {
				continue
			}
			if len(found) >= MaxHistoryRefs {
				return nil, fmt.Errorf("%w: more than %d refs", ErrBadAnswer, MaxHistoryRefs)
			}
			if t := r.GetTarget().GetChangeset(); len(t) == changesetIDBytes {
				found = append(found, pending{r.GetName(), t})
			}
		}
		if after = resp.GetNext(); after == "" {
			break
		}
	}
	out := make(map[string]string, len(found))
	for start := 0; start < len(found); start += mapBatch {
		chunk := found[start:min(start+mapBatch, len(found))]
		ids := make([][]byte, len(chunk))
		for i, p := range chunk {
			ids[i] = p.target
		}
		cctx, cancel := system(ctx, CallTimeout)
		resp, err := h.api.GitMap(cctx, &ascv1.GitMapRequest{
			Repo: &ascv1.RepoRef{Name: h.name, IdMode: ascv1.IdMode_ID_MODE_ASC}, Asc: ids,
		})
		cancel()
		if err != nil {
			return nil, callError("git map", err)
		}
		mapped := resp.GetAscToGit()
		if len(mapped) != len(chunk) {
			return nil, fmt.Errorf("%w: git map answered %d ids for %d", ErrBadAnswer, len(mapped), len(chunk))
		}
		for i, g := range mapped {
			switch len(g) {
			case 0:
			case gitIDBytes:
				out[chunk[i].name] = fmt.Sprintf("%x", g)
			default:
				return nil, fmt.Errorf("%w: git id of %d bytes", ErrBadAnswer, len(g))
			}
		}
	}
	return out, nil
}
