// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package files

import (
	"context"
	"fmt"

	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
)

// AscPolicyEdit is set by services/asctrust. It is asked about a change of files that is about to be committed straight
// onto the default branch of a repository, before anything is written, and may take it over.
//
// The policy file of a repository stored in asc (.asc/policy.toml) is not committed by the hub's service credential:
// asc decides every write of that credential as the hub itself, which is no maintainer, so the commit of a person who
// is one would be refused. The hook turns the edit into what the policy page does, a proposal (Policy.Propose, made as
// the person with their claim, so asc decides whether they may touch the file) that becomes an ordinary change under
// review with a pull request, never a write to trunk. It returns
//
//   - nil, when the change is none of its business and the commit goes on as before;
//   - *ErrAscPolicyProposed, when the edit was proposed instead of committed;
//   - *ErrAscPolicyRefused, when the person may not edit the policy (asc said so) or the change cannot be proposed;
//   - another error, when asc could not be asked: the edit is then not made.
var AscPolicyEdit func(ctx context.Context, repo *repo_model.Repository, doer *user_model.User, opts *ChangeRepoFilesOptions) error

// ErrAscPolicyProposed says that an edit of the policy file was proposed as a change and nothing was committed.
type ErrAscPolicyProposed struct {
	// Change is the short id of the change under review.
	Change string
	// Link is the address of its pull request, or "" when the git view of the repository does not have the change
	// yet (the pull request then opens when it does).
	Link string
}

func (e *ErrAscPolicyProposed) Error() string {
	return "the policy edit was proposed as the change " + e.Change
}

// ErrAscPolicyRefused says that an edit of the policy file was not made, with a sentence for the person.
type ErrAscPolicyRefused struct {
	Reason string
}

func (e *ErrAscPolicyRefused) Error() string {
	return fmt.Sprintf("the policy file was not changed: %s", e.Reason)
}
