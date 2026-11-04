// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	"forgejo.org/modules/git"
	"forgejo.org/modules/setting"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// The Undo card of the change page (board Changeset). While the change is what the newest operation on the default
// branch put there, and that operation is the person's to undo, the card offers the undo from the operation log: the
// branch goes back to where it was and no new change is made. Once anything else lands, or asc says the operation
// cannot be undone, the card says so, and the revert (a new change that reverses this one) is what is left.
//
// "The newest operation put it there" is read as: the change is the head of the default branch now, and the newest
// operation of that branch is not an undo or a restack. The undo page reads the operation again and shows both heads
// before anything is written, so a branch that moves in between is caught there.

// changeUndoNewer is changeUndo.Late for a change that the default branch has moved past.
const changeUndoNewer = "newer"

// changeUndo is the operation log part of the Undo card.
type changeUndo struct {
	// URL is the confirmation page of the undo, "" when the operation log no longer undoes the change.
	URL string
	// Late says why not: changeUndoNewer, or the reason asc gave (a key of asc_trust.ops.blocked).
	Late string
}

// changeUndoOf decides the card from the head of the default branch and its newest operation (nil when it could not be
// read). It returns nil when there is nothing to say, so the card shows the revert alone.
func changeUndoOf(repo *repo_model.Repository, head, commitID string, row *asctrust.OpRow) *changeUndo {
	switch {
	case head != commitID:
		return &changeUndo{Late: changeUndoNewer}
	case row == nil || row.ViaKey == "undo" || row.ViaKey == "restack":
		// an undo of this operation would not take the change away
		return nil
	case row.Undoable:
		return &changeUndo{URL: ascUndoURL(repo, row)}
	}
	return &changeUndo{Late: row.BlockKey}
}

// prepareAscChangeUndo puts the operation log part of the Undo card into ctx.Data["ChangeUndo"] for a change on the
// default branch. It is for a signed-in person who may write the code; a read that fails leaves it out.
func prepareAscChangeUndo(ctx *context.Context, commitID string) {
	repo := ctx.Repo.Repository
	if !setting.Asc.Enabled || !ctx.IsSigned || ctx.Doer == nil || repo.IsArchived || ctx.Repo.GitRepo == nil ||
		!ctx.Repo.CanWrite(unit.TypeCode) || !asctrust.HasFeature(ctx, asctrust.FeatureStacks) {
		return
	}
	head, err := ctx.Repo.GitRepo.GetBranchCommitID(repo.DefaultBranch)
	if err != nil {
		return
	}
	var row *asctrust.OpRow
	if head == commitID {
		row = ascNewestOp(ctx, repo, git.BranchPrefix+repo.DefaultBranch)
	}
	if u := changeUndoOf(repo, head, commitID, row); u != nil {
		ctx.Data["ChangeUndo"] = u
	}
}
