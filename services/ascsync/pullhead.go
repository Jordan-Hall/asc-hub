// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascsync

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	repo_model "forgejo.org/models/repo"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git"
)

// pullHeadRef matches refs/pull/<n>/head with a positive index of at most 18 digits.
var pullHeadRef = regexp.MustCompile(`^refs/pull/[1-9][0-9]{0,17}/head$`)

// SetPullHead points ref (refs/pull/<n>/head) of repo at commit, a 40-digit git id, and returns once the
// git bridge shows it.
//
//  1. asc moves the ref first: UpdateRefs with HOOK_MODE_SKIP_PRE and UPDATE_MODE_FORCE (a pull request
//     head may be rewritten), as the internal token. A commit asc does not know yet is imported from
//     the repository's objects directory: git has migrated an AGit push there before it runs
//     proc-receive, and PushToBaseRepo fetches into it.
//  2. The bridge is not written here. asc's bridge_promote effect sets it to asc's value, and SetPullHead
//     waits up to BridgeWait for that, because Forgejo still reads pull heads from the bridge at Stage 2.
//
// Any failure leaves the caller's operation failed. A ref asc already took is harmless: the next write of
// the same pull request replaces it.
func SetPullHead(ctx context.Context, repo *repo_model.Repository, ref, commit string) error {
	h, err := open(repo)
	if err != nil {
		return err
	}
	if !pullHeadRef.MatchString(ref) {
		return fmt.Errorf("%w: pull head ref", ErrBadRequest)
	}
	if _, err := gitID(commit); err != nil || strings.Trim(commit, "0") == "" {
		return fmt.Errorf("%w: pull head commit", ErrBadRequest)
	}

	id, err := h.changesetOf(ctx, commit)
	if err != nil {
		return err
	}
	if id == nil {
		// asc has never seen the commit: import it, moving the ref when that is a fast-forward.
		moved, err := h.importHead(ctx, ref, commit)
		if err != nil {
			return err
		}
		if !moved {
			if id, err = h.changesetOf(ctx, commit); err != nil {
				return err
			}
			if id == nil {
				return fmt.Errorf("%w: the commit is not in asc after its import", ErrBadAnswer)
			}
		}
	}
	if id != nil {
		if err := h.forceRef(ctx, ref, id); err != nil {
			return err
		}
	}
	return h.waitBridge(ctx, ref, commit)
}

// DeletePullHead removes ref (refs/pull/<n>/head) from asc. The bridge follows through bridge_promote.
// It is for the cleanup of a pull request that failed to be created, so it runs even when ctx is done.
func DeletePullHead(ctx context.Context, repo *repo_model.Repository, ref string) error {
	h, err := open(repo)
	if err != nil {
		return err
	}
	if !pullHeadRef.MatchString(ref) {
		return fmt.Errorf("%w: pull head ref", ErrBadRequest)
	}
	cctx, cancel := system(context.WithoutCancel(ctx), CallTimeout)
	defer cancel()
	resp, err := h.api.UpdateRefs(cctx, &ascv1.UpdateRefsRequest{
		Repo:    &ascv1.RepoRef{Name: h.name, IdMode: ascv1.IdMode_ID_MODE_ASC},
		Updates: []*ascv1.RefUpdate{{Name: ref, Expect: &ascv1.RefUpdate_Any{Any: true}, Delete: true}},
		Mode:    ascv1.UpdateMode_UPDATE_MODE_FORCE,
		Hooks:   ascv1.HookMode_HOOK_MODE_SKIP_PRE,
	})
	if err != nil {
		return callError("delete pull head", err)
	}
	if len(resp.GetResults()) != 1 {
		return fmt.Errorf("%w: %d results for 1 update", ErrBadAnswer, len(resp.GetResults()))
	}
	return nil
}

// forceRef moves ref to the changeset id with the internal token, skipping asc's pre-receive.
func (h *ascRepo) forceRef(ctx context.Context, ref string, id []byte) error {
	cctx, cancel := system(ctx, CallTimeout)
	defer cancel()
	resp, err := h.api.UpdateRefs(cctx, &ascv1.UpdateRefsRequest{
		Repo: &ascv1.RepoRef{Name: h.name, IdMode: ascv1.IdMode_ID_MODE_ASC},
		Updates: []*ascv1.RefUpdate{{
			Name:   ref,
			Expect: &ascv1.RefUpdate_Any{Any: true},
			New:    &ascv1.RefTarget{T: &ascv1.RefTarget_Changeset{Changeset: id}},
		}},
		Mode:  ascv1.UpdateMode_UPDATE_MODE_FORCE,
		Hooks: ascv1.HookMode_HOOK_MODE_SKIP_PRE,
	})
	if err != nil {
		return callError("update pull head", err)
	}
	if len(resp.GetResults()) != 1 {
		return fmt.Errorf("%w: %d results for 1 update", ErrBadAnswer, len(resp.GetResults()))
	}
	return nil
}

// changesetOf maps a git commit id to asc's changeset id. It returns nil when asc does not know the commit.
func (h *ascRepo) changesetOf(ctx context.Context, commit string) ([]byte, error) {
	raw, err := gitID(commit)
	if err != nil {
		return nil, err
	}
	cctx, cancel := system(ctx, CallTimeout)
	defer cancel()
	resp, err := h.api.GitMap(cctx, &ascv1.GitMapRequest{
		Repo: &ascv1.RepoRef{Name: h.name, IdMode: ascv1.IdMode_ID_MODE_ASC},
		Git:  [][]byte{raw},
	})
	if err != nil {
		return nil, callError("git map", err)
	}
	got := resp.GetGitToAsc()
	if len(got) != 1 || (len(got[0]) != 0 && len(got[0]) != changesetIDBytes) {
		return nil, fmt.Errorf("%w: git map", ErrBadAnswer)
	}
	if len(got[0]) == 0 {
		return nil, nil
	}
	return got[0], nil
}

// changesetIDBytes and gitIDBytes are the sizes of an asc changeset id and of a git id.
const (
	changesetIDBytes = 32
	gitIDBytes       = 20
)

// currentGit is the git id of the commit ref points at in asc, or the all-zero id when ref is absent.
func (h *ascRepo) currentGit(ctx context.Context, ref string) (string, error) {
	const zero = "0000000000000000000000000000000000000000"
	cctx, cancel := system(ctx, CallTimeout)
	defer cancel()
	list, err := h.api.ListRefs(cctx, &ascv1.ListRefsRequest{
		Repo:   &ascv1.RepoRef{Name: h.name, IdMode: ascv1.IdMode_ID_MODE_ASC},
		Prefix: ref,
		Limit:  8,
	})
	if err != nil {
		return "", callError("list refs", err)
	}
	var target []byte
	for _, r := range list.GetRefs() {
		if r.GetName() != ref {
			continue
		}
		target = r.GetTarget().GetChangeset()
		if len(target) != changesetIDBytes {
			return "", fmt.Errorf("%w: pull head is not a changeset", ErrBadAnswer)
		}
		break
	}
	if target == nil {
		return zero, nil
	}
	m, err := h.api.GitMap(cctx, &ascv1.GitMapRequest{
		Repo: &ascv1.RepoRef{Name: h.name, IdMode: ascv1.IdMode_ID_MODE_ASC},
		Asc:  [][]byte{target},
	})
	if err != nil {
		return "", callError("git map", err)
	}
	got := m.GetAscToGit()
	if len(got) != 1 || len(got[0]) != gitIDBytes {
		return "", fmt.Errorf("%w: git map", ErrBadAnswer)
	}
	return fmt.Sprintf("%x", got[0]), nil
}

// importHead has asc import commit and move ref to it with ImportGitPush (fast-forward). It reports
// whether the ref moved. A refusal because the move is not a fast-forward (git's "fetch first") is not
// an error: the import itself is complete before the ref is judged, so the caller then forces the ref.
func (h *ascRepo) importHead(ctx context.Context, ref, commit string) (bool, error) {
	oldID, err := h.currentGit(ctx, ref)
	if err != nil {
		return false, err
	}
	objectDir, alternates, err := checkDirs(h.bridge, PushDirs{})
	if err != nil {
		return false, err
	}
	updates, err := gitRefUpdates([]PushRef{{Name: ref, Old: oldID, New: commit}})
	if err != nil {
		return false, err
	}
	cctx, cancel := system(ctx, ImportTimeout)
	defer cancel()
	resp, err := h.api.ImportGitPush(cctx, &ascv1.ImportGitPushRequest{
		Repo:          &ascv1.RepoRef{Name: h.name, IdMode: ascv1.IdMode_ID_MODE_ASC},
		GitDir:        h.bridge,
		ObjectDir:     objectDir,
		AlternateDirs: alternates,
		Updates:       updates,
	})
	if err != nil {
		if rej, ok := RejectMessage(callError("import pull head", err)); ok && isFetchFirst(rej) {
			return false, nil
		}
		return false, callError("import pull head", err)
	}
	if len(resp.GetResults()) != 1 {
		return false, fmt.Errorf("%w: %d results for 1 command", ErrBadAnswer, len(resp.GetResults()))
	}
	return true, nil
}

// isFetchFirst reports whether msg is git's rejection of a non-fast-forward, which asc writes as
// "! [rejected] <ref> (fetch first)".
func isFetchFirst(msg string) bool {
	return strings.HasPrefix(msg, "! [rejected]") && strings.HasSuffix(msg, "(fetch first)")
}

// waitBridge polls the bridge until ref there equals commit, for at most BridgeWait.
func (h *ascRepo) waitBridge(ctx context.Context, ref, commit string) error {
	deadline := time.NewTimer(bridgeWait)
	defer deadline.Stop()
	delay := bridgePollMin
	for {
		got, err := bridgeRef(ctx, h.bridge, ref)
		if err == nil && got == commit {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("ascsync: waiting for the bridge: %w", ctx.Err())
		case <-deadline.C:
			return ErrBridgeBehind
		case <-time.After(delay):
		}
		delay = min(delay*2, bridgePollMax)
	}
}

// bridgeRef reads ref in the bridge directory. A missing ref is the empty string.
func bridgeRef(ctx context.Context, bridge, ref string) (string, error) {
	id, err := git.GetFullCommitID(ctx, bridge, ref)
	if err != nil {
		if git.IsErrNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return id, nil
}
