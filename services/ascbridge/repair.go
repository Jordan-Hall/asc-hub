// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package ascbridge

import (
	"context"
	"errors"
	"sync"
	"time"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/setting"
)

// RepairInterval is the least time between two repairs of one bridge. A bridge that is still missing commits
// after a repair has a cause the repair cannot remove, and a page that is asked for again and again must not start
// an export for each ask.
const RepairInterval = 30 * time.Second

// ErrRepairTooSoon is returned by Repair when the bridge was repaired within RepairInterval.
var ErrRepairTooSoon = errors.New("ascbridge: the bridge was repaired a moment ago")

var (
	repairMu   sync.Mutex
	repairLast = map[string]time.Time{}
)

// Repair makes the bridge of an asc repository hold again what asc holds: every shared ref, with its commits. It is
// for the case that git finds a ref whose commit the directory lacks (a push that asc took and the host's git never
// finished, a restart in between, a volume restored from an older copy). asc is the source of truth, so the
// directory is rebuilt from it with the exporter, whatever the bridge mode, and a lazy bridge records what it was
// built from. It does nothing for a repository that is not in asc.
func Repair(ctx context.Context, repo *repo_model.Repository) error {
	isAsc, err := IsAsc(repo)
	if err != nil {
		return err
	}
	if !isAsc {
		return nil
	}
	if !setting.Asc.Enabled {
		return ErrDisabled
	}
	if Mode() == setting.AscBridgeOff {
		return ErrOff
	}
	path := Path(repo)
	name, err := ascName(path)
	if err != nil {
		return err
	}
	defer locks.lock(path)()

	repairMu.Lock()
	if at, ok := repairLast[path]; ok && time.Since(at) < RepairInterval {
		repairMu.Unlock()
		return ErrRepairTooSoon
	}
	repairLast[path] = time.Now()
	repairMu.Unlock()

	if err := ensureDir(ctx, path, repo.DefaultBranch); err != nil {
		return err
	}
	if err := exporter().Export(ctx, name, path); err != nil {
		return err
	}
	if Mode() != setting.AscBridgeLazy {
		return nil
	}
	// A lazy bridge records what it was built from, so that the next Ensure does not build it again.
	api, err := repoAPI()
	if err != nil {
		return err
	}
	fp, err := fingerprint(ctx, api, name)
	if err != nil {
		return err
	}
	return writeMarker(path, fp)
}

// ForgetRepairs forgets when bridges were repaired. It is for tests.
func ForgetRepairs() {
	repairMu.Lock()
	defer repairMu.Unlock()
	repairLast = map[string]time.Time{}
}
