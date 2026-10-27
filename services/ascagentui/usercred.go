// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascagentui

import (
	"context"
	"errors"

	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/services/asctrust"
)

// A browser session holds no asc credential, and the hub makes no access token for it. A call that changes something
// (start or stop a run, make or revoke an agent token, trigger a workflow, decide an approval) is made with the hub's
// service credential and a signed claim about the person: who they are, and that the hub's permission model allows
// them to write the repository (services/asctrust, services/ascidentity). The hub decides; asc serve does not decide
// again, and records the person in the audit log from the claim (docs/BUILDOUT.md, "Who is calling").

// ErrNoDoer is returned when a user call is asked for without a signed-in user.
var ErrNoDoer = errors.New("ascagentui: a user call needs a signed-in user")

// UserCall returns a context whose asc calls are made for doer on repo, and the function that ends the call. The
// hub's permission model is evaluated at the moment of the call: a person who may not write the repository gets
// asctrust.ErrForbidden and nothing is sent. Always call the cleanup.
func UserCall(ctx context.Context, doer *user_model.User, repo *repo_model.Repository) (context.Context, func(), error) {
	if doer == nil || doer.ID <= 0 || repo == nil {
		return ctx, func() {}, ErrNoDoer
	}
	return asctrust.UserCall(ctx, doer, asctrust.CallRepoWrite, repo)
}
