// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascagentui

import (
	"context"
	"errors"
	"fmt"
	"time"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascclient"
	"forgejo.org/modules/log"
	"forgejo.org/modules/timeutil"
	"forgejo.org/modules/util"
	"forgejo.org/services/ascidentity"
)

// A browser session holds no asc credential. A call that changes something (start or stop a run, make or revoke
// an agent token, trigger a workflow, decide an approval) must still reach asc as the signed-in person, so asc
// authorises and audits it as that person and never as the hub (docs/BUILDOUT.md, "Who is calling"). For such a
// call the hub makes a one-use access token for the doer: scope write:repository, limited to the one repository,
// deleted when the call returns. A crash between the two leaves a token that names this prefix, is limited to one
// repository and is removed by the next call of the same user (staleCallTokens).
const (
	callTokenPrefix = "asc-hub-call-"
	callTokenMaxAge = 10 * time.Minute
)

// ErrNoDoer is returned when a user call is asked for without a signed-in user.
var ErrNoDoer = errors.New("ascagentui: a user call needs a signed-in user")

// UserCall returns a context whose asc calls carry a one-use token of doer for repo, and the function that
// deletes the token. The caller must have checked the doer's Forgejo permission first: asc checks it again, but
// the hub is the first gate. Always call the cleanup, also on failure.
func UserCall(ctx context.Context, doer *user_model.User, repo *repo_model.Repository) (context.Context, func(), error) {
	if doer == nil || doer.ID <= 0 || repo == nil {
		return ctx, func() {}, ErrNoDoer
	}
	staleCallTokens(ctx, doer.ID)

	tok := &auth_model.AccessToken{
		UID:              doer.ID,
		Name:             callTokenPrefix + util.CryptoRandomString(util.RandomStringMedium),
		Scope:            auth_model.AccessTokenScopeWriteRepository,
		ResourceAllRepos: false,
	}
	err := db.WithTx(ctx, func(ctx context.Context) error {
		if err := auth_model.NewAccessToken(ctx, tok); err != nil {
			return err
		}
		return auth_model.InsertAccessTokenResourceRepos(ctx, tok.ID, []*auth_model.AccessTokenResourceRepo{{TokenID: tok.ID, RepoID: repo.ID}})
	})
	if err != nil {
		return ctx, func() {}, fmt.Errorf("ascagentui: making the call token: %w", err)
	}
	cleanup := func() {
		// The request context may be cancelled already (the browser left); the token must still go.
		if err := auth_model.DeleteAccessTokenByID(context.WithoutCancel(ctx), tok.ID, doer.ID); err != nil {
			log.Error("asc agents: deleting the call token %d of user %d failed: %v", tok.ID, doer.ID, err)
		}
	}
	callCtx, err := ascclient.WithUserToken(ctx, tok.Token)
	if err != nil {
		cleanup()
		return ctx, func() {}, fmt.Errorf("ascagentui: %w", err)
	}
	// The identity the hub signed in goes with the token, signed with the service secret (ascidentity).
	if callCtx, err = ascidentity.AttachDoer(callCtx, doer); err != nil {
		cleanup()
		return ctx, func() {}, fmt.Errorf("ascagentui: the forwarded identity: %w", err)
	}
	return callCtx, cleanup, nil
}

// staleCallTokens deletes call tokens of the user that a crash left behind.
func staleCallTokens(ctx context.Context, uid int64) {
	var old []*auth_model.AccessToken
	cutoff := timeutil.TimeStamp(time.Now().Add(-callTokenMaxAge).Unix())
	err := db.GetEngine(ctx).
		Where("uid = ? AND name LIKE ? AND created_unix < ?", uid, callTokenPrefix+"%", cutoff).
		Limit(50).Find(&old)
	if err != nil {
		log.Warn("asc agents: looking for stale call tokens of user %d failed: %v", uid, err)
		return
	}
	for _, t := range old {
		if err := auth_model.DeleteAccessTokenByID(ctx, t.ID, uid); err != nil {
			log.Warn("asc agents: deleting stale call token %d failed: %v", t.ID, err)
		}
	}
}
