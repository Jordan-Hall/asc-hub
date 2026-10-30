// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"errors"

	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/services/pull"
)

// The verdicts a person gives on a pull request are told to asc, as that person, through the central gate. services/pull cannot
// import this package, so the gate is installed here.
func init() {
	pull.SetAscUserCall(ascWriteCall)
	pull.SetAscReadCall(ascReadCallFor)
}

// ascWriteCall is UserCall(CallRepoWrite) for services/pull: a person the hub does not allow is not an error, the report is skipped.
func ascWriteCall(ctx context.Context, doer *user_model.User, repo *repo_model.Repository) (context.Context, bool, error) {
	callCtx, release, err := UserCall(ctx, doer, CallRepoWrite, repo)
	if err != nil {
		if errors.Is(err, ErrForbidden) || errors.Is(err, ErrNoDoer) {
			return ctx, false, nil
		}
		return ctx, false, err
	}
	release()
	return callCtx, true, nil
}

// ascReadCallFor is UserCall(CallRepoRead) for services/pull: the comments of a person who may read are mirrored to asc as
// that person; a person the hub does not allow is not an error, the mirror is skipped.
func ascReadCallFor(ctx context.Context, doer *user_model.User, repo *repo_model.Repository) (context.Context, bool, error) {
	callCtx, release, err := UserCall(ctx, doer, CallRepoRead, repo)
	if err != nil {
		if errors.Is(err, ErrForbidden) || errors.Is(err, ErrNoDoer) {
			return ctx, false, nil
		}
		return ctx, false, err
	}
	release()
	return callCtx, true, nil
}
