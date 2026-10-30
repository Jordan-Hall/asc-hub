// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package automerge

import (
	"context"
	"encoding/hex"
	"fmt"
	"time"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git/ascbackend"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/asctrust"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The hub keeps the merge queue (the entries, the order, the worker that lands them). asc's MergeQueue service keeps a
// mirror of it so the queue page can show where each entry is, how long until it starts and lands, and what the checks are
// doing, and so a client of asc sees the same queue. The mirror is told what the hub does: an entry is queued, it starts to
// land, it landed, it is blocked, it is taken out. It never decides anything and never delays the hub: a call that fails is
// logged and the hub's queue goes on as it was.

const ascQueueMirrorTimeout = 5 * time.Second

// ascQueueMirror is nil until InitAscQueueMirror sets it. Tests replace it with UseAscQueueMirror.
var ascQueueMirror func(ctx context.Context, repo *repo_model.Repository, e *AscStackQueueEntry, state string)

// UseAscQueueMirror sets the mirror until the returned function is called. It is for tests.
func UseAscQueueMirror(f func(ctx context.Context, repo *repo_model.Repository, e *AscStackQueueEntry, state string)) (restore func()) {
	prev := ascQueueMirror
	ascQueueMirror = f
	return func() { ascQueueMirror = prev }
}

// InitAscQueueMirror makes the mirror that calls asc with the hub's own credential. It does nothing when asc is off.
func InitAscQueueMirror() {
	if !setting.Asc.Enabled || setting.InternalToken == "" {
		return
	}
	c, err := ascclient.New(ascclient.Config{Target: setting.Asc.ServerURL, InternalToken: setting.InternalToken, CallTimeout: ascQueueMirrorTimeout})
	if err != nil {
		log.Error("asc queue mirror: %v", err)
		return
	}
	api := ascv1.NewMergeQueueClient(c)
	asc := ascQueueClients{queue: api, repo: ascv1.NewRepoClient(c)}
	ascQueueMirror = func(ctx context.Context, repo *repo_model.Repository, e *AscStackQueueEntry, state string) {
		change := ascChangeRaw(e.AscChange)
		if change == nil {
			return
		}
		name, err := ascbackend.RepoName(setting.RepoRootPath, repo_model.RepoPath(repo.OwnerName, repo.Name))
		if err != nil {
			return
		}
		ref := &ascv1.RepoRef{Name: name}
		cctx, cancel := context.WithTimeout(ascclient.WithSystem(context.WithoutCancel(ctx)), ascQueueMirrorTimeout)
		defer cancel()
		target := "refs/heads/" + e.Branch
		if state == AscQueueQueued {
			// An entry has a person who queued it: asc records that person's immutable id, so this call is made as them
			// (a one-use claim for the repository), never as the hub. Forgejo has authorised them already.
			doer, err := user_model.GetUserByID(ctx, e.DoerID)
			if err != nil {
				log.Warn("asc queue mirror: the person who queued entry %d is gone: %v", e.ID, err)
				return
			}
			uctx, release, err := asctrust.UserCall(ctx, doer, asctrust.CallRepoWrite, repo)
			if err != nil {
				log.Warn("asc queue mirror: no credential for user %d: %v", doer.ID, err)
				return
			}
			defer release()
			opID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("asc-queue-mirror|"+name+"|"+e.AscChange+"|"+time.Unix(int64(e.CreatedUnix), 0).UTC().Format(time.RFC3339Nano)))
			uctx, ucancel := context.WithTimeout(ascclient.WithOpID(uctx, opID), ascQueueMirrorTimeout)
			defer ucancel()
			// The hub queues every pull request of a stack; asc has one entry for the stack, so the later ones are "queued already".
			if _, err := api.Enqueue(uctx, &ascv1.EnqueueRequest{Repo: ref, Change: change, Target: target}); err != nil && status.Code(err) != codes.AlreadyExists {
				log.Warn("asc queue mirror: Enqueue failed: %v", err)
			}
			return
		}
		list, err := api.List(cctx, &ascv1.ListQueueRequest{Repo: ref, Target: target, Limit: 200})
		if err != nil {
			log.Warn("asc queue mirror: List failed: %v", err)
			return
		}
		var id uint64
		for _, q := range list.GetEntries() {
			if string(q.GetChange()) == string(change) {
				id = q.GetId()
				break
			}
		}
		if id == 0 {
			return
		}
		switch state {
		case AscQueueCancelled:
			_, err = api.Dequeue(cctx, &ascv1.DequeueRequest{Repo: ref, Id: id, Reason: "removed in the hub"})
		case AscQueueLanding:
			err = asc.landing(cctx, ref, id, e.Branch)
		case AscQueueBlocked:
			_, err = api.ReportEntry(cctx, &ascv1.ReportQueueEntryRequest{Repo: ref, Id: id, State: ascv1.QueueEntryState_QUEUE_ENTRY_STATE_BLOCKED, Detail: "waits for a new version"})
		case AscQueueMerged:
			var mergedGit string
			if pr, perr := issues_model.GetPullRequestByID(ctx, e.PullID); perr == nil {
				mergedGit = pr.MergedCommitID
			}
			err = asc.landed(cctx, ref, id, mergedGit)
		}
		if err != nil {
			log.Warn("asc queue mirror: %s failed: %v", state, err)
		}
	}
}

// ascQueueClients are the two services of asc that the mirror speaks to.
type ascQueueClients struct {
	queue ascv1.MergeQueueClient
	repo  ascv1.RepoClient
}

// landing tells asc that the entry starts to land. asc lands only an entry that is READY, so the entry is revalidated first: the
// approvals and checks that were recorded since it was queued are what make it READY. asc decides again when it is told, and an
// entry that no longer passes is refused there (and the hub's own land is refused by the same policy).
func (c ascQueueClients) landing(ctx context.Context, ref *ascv1.RepoRef, id uint64, branch string) error {
	if _, err := c.queue.Revalidate(ctx, &ascv1.RevalidateRequest{Repo: ref, Id: id}); err != nil {
		return fmt.Errorf("revalidate: %w", err)
	}
	_, err := c.queue.ReportEntry(ctx, &ascv1.ReportQueueEntryRequest{Repo: ref, Id: id, State: ascv1.QueueEntryState_QUEUE_ENTRY_STATE_LANDING, Stage: "landing", Detail: "landing on " + branch})
	return err
}

// landed tells asc that the entry landed, with the changeset that landed: the changeset of the git commit that the stack's top
// pull request was merged as.
func (c ascQueueClients) landed(ctx context.Context, ref *ascv1.RepoRef, id uint64, mergedGit string) error {
	raw, err := hex.DecodeString(mergedGit)
	if err != nil || len(raw) == 0 {
		return fmt.Errorf("the merged commit %q is no git id", mergedGit)
	}
	m, err := c.repo.GitMap(ctx, &ascv1.GitMapRequest{Repo: ref, Git: [][]byte{raw}})
	if err != nil {
		return fmt.Errorf("git map: %w", err)
	}
	if len(m.GetGitToAsc()) != 1 || len(m.GetGitToAsc()[0]) != 32 {
		return fmt.Errorf("asc has no changeset for the merged commit %s", mergedGit)
	}
	_, err = c.queue.ReportEntry(ctx, &ascv1.ReportQueueEntryRequest{Repo: ref, Id: id, State: ascv1.QueueEntryState_QUEUE_ENTRY_STATE_LANDED, LandedAs: m.GetGitToAsc()[0], Stage: "done"})
	return err
}

// ascQueueMirrorState tells the mirror that the entries of these pull requests are now in state.
func ascQueueMirrorState(ctx context.Context, state string, pullIDs []int64) {
	if ascQueueMirror == nil || len(pullIDs) == 0 {
		return
	}
	var entries []*AscStackQueueEntry
	if err := db.GetEngine(ctx).In("pull_id", pullIDs).And("state = ?", state).Find(&entries); err != nil {
		return // only the entries that are in the state now: the update may have skipped some
	}
	ascQueueMirrorEntries(ctx, state, entries)
}

func ascQueueMirrorEntries(ctx context.Context, state string, entries []*AscStackQueueEntry) {
	if ascQueueMirror == nil {
		return
	}
	repos := map[int64]*repo_model.Repository{}
	for _, e := range entries {
		repo, ok := repos[e.RepoID]
		if !ok {
			var err error
			if repo, err = repo_model.GetRepositoryByID(ctx, e.RepoID); err != nil {
				continue
			}
			repos[e.RepoID] = repo
		}
		ascQueueMirror(ctx, repo, e, state)
	}
}

// ascChangeRaw turns the 32 letters k-z of a change id into the 16 bytes asc uses; nil for anything else.
func ascChangeRaw(text string) []byte {
	if len(text) != 32 {
		return nil
	}
	raw := make([]byte, 16)
	for i := range raw {
		hi, lo := text[2*i], text[2*i+1]
		if hi < 'k' || hi > 'z' || lo < 'k' || lo > 'z' {
			return nil
		}
		raw[i] = ('z'-hi)<<4 | ('z' - lo)
	}
	return raw
}
