// Copyright 2020 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package stats

import (
	"errors"
	"sync/atomic"
	"time"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/git"
	"forgejo.org/modules/graceful"
	"forgejo.org/modules/log"
	"forgejo.org/modules/queue"
	"forgejo.org/modules/setting"
)

// statsQueue represents a queue to handle repository stats updates
var statsQueue *queue.WorkerPoolQueue[int64]

// waitNoticeEvery is how often, at most, the log says that repositories wait for the storage backend.
const waitNoticeEvery = time.Minute

// lastWaitNotice is the unix nano time of the last such notice.
var lastWaitNotice atomic.Int64

// handler indexes the repositories. One whose backend (asc serve) cannot be reached yet, as while the container
// starts the hub before asc serve, is handed back to the queue, which tries it again a few seconds later, so its
// language bar fills in once asc is up instead of staying empty until the next push.
func handler(items ...int64) []int64 {
	var waiting []int64
	for _, id := range items {
		err := indexer.Index(id)
		switch {
		case err == nil:
		case git.IsErrBackendUnavailable(err):
			waiting = append(waiting, id)
		case !setting.IsInTesting:
			log.Error("stats queue indexer.Index(%d) failed: %v", id, err)
		}
	}
	if len(waiting) > 0 {
		noteWaiting(len(waiting), time.Now())
	}
	return waiting
}

// noteWaiting logs, at most once every waitNoticeEvery, that n repositories wait for the storage backend.
func noteWaiting(n int, now time.Time) bool {
	last := lastWaitNotice.Load()
	if last != 0 && now.Sub(time.Unix(0, last)) < waitNoticeEvery {
		return false
	}
	if !lastWaitNotice.CompareAndSwap(last, now.UnixNano()) {
		return false
	}
	log.Info("asc serve cannot be reached yet; %d repositories wait for their language stats and are tried again", n)
	return true
}

func initStatsQueue() error {
	statsQueue = queue.CreateUniqueQueue(graceful.GetManager().ShutdownContext(), "repo_stats_update", handler)
	if statsQueue == nil {
		return errors.New("unable to create repo_stats_update queue")
	}
	go graceful.GetManager().RunWithCancel(statsQueue)
	return nil
}

// UpdateRepoIndexer update a repository's entries in the indexer
func UpdateRepoIndexer(repo *repo_model.Repository) error {
	if err := statsQueue.Push(repo.ID); err != nil {
		if err != queue.ErrAlreadyInQueue {
			return err
		}
		log.Debug("Repo ID: %d already queued", repo.ID)
	}
	return nil
}
