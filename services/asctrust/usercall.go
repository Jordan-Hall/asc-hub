// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
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

// The credential of a person for one request.
//
// A web session holds no asc token, and asc must decide for the person, not for the hub (docs/BUILDOUT.md, "Who is
// calling"). UserCall therefore issues an ordinary Forgejo access token for the doer, limited in scope (and, for a
// repository call, to that one repository), hands it to the asc client for the calls of one request, and deletes it
// when the request is done. asc asks Forgejo to authorise it as it does for any bearer token.
//
//   - The token is never shown, logged or stored anywhere but the database row, which holds only its salted hash.
//   - It is deleted by a context that survives the cancellation of the request, so a browser that goes away
//     cannot leave one behind. A token that outlives a crash is deleted by the next sweep (staleAfter).
//   - The scopes are the least that the call needs: read:repository for a read, write:repository for a change,
//     read:organization for the audit log of an organisation, read:user and write:user for signing keys.
const (
	// tokenPrefix names every token this package issues, so a sweep finds only its own.
	tokenPrefix = "asc-hub-call-"
	// staleAfter is the age after which a token of this package is deleted by a sweep. A request lives seconds.
	staleAfter = 15 * time.Minute
	// sweepEvery bounds how often a sweep may run.
	sweepEvery = time.Minute
	// revokeTimeout bounds the deletion of the token.
	revokeTimeout = 10 * time.Second
)

// CallKind says what a call is for, and so what scope its token gets.
type CallKind int

const (
	// CallRepoRead is a read of one repository that only administrators or writers may do.
	CallRepoRead CallKind = iota + 1
	// CallRepoWrite is a change of one repository.
	CallRepoWrite
	// CallOrg is a read of an organisation (its audit log).
	CallOrg
	// CallAccount is a call about the account of the doer (signing keys).
	CallAccount
	// CallInstance is a call about the whole server (the instance audit log). It needs a site administrator.
	CallInstance
)

// ErrNoDoer is returned when a person's call is asked for nobody.
var ErrNoDoer = errors.New("asctrust: no signed-in user")

// tokenIssuer issues a token. Tests replace it.
type tokenIssuer func(ctx context.Context, doer *user_model.User, scope auth_model.AccessTokenScope, repoID int64) (token string, revoke func(), err error)

var issueToken tokenIssuer = issueAccessToken

// UseTokenIssuer replaces the issuer until the returned function is called. It is for tests of the handlers, which
// have no asc server that checks a Forgejo token.
func UseTokenIssuer(fn func(ctx context.Context, doer *user_model.User, scope auth_model.AccessTokenScope, repoID int64) (string, func(), error)) (restore func()) {
	prev := issueToken
	issueToken = fn
	return func() { issueToken = prev }
}

// scopeOf returns the access token scope a kind of call needs.
func scopeOf(kind CallKind) (auth_model.AccessTokenScope, error) {
	var s string
	switch kind {
	case CallRepoRead:
		s = string(auth_model.AccessTokenScopeReadRepository)
	case CallRepoWrite:
		s = string(auth_model.AccessTokenScopeReadRepository) + "," + string(auth_model.AccessTokenScopeWriteRepository)
	case CallOrg:
		s = string(auth_model.AccessTokenScopeReadOrganization)
	case CallAccount:
		s = string(auth_model.AccessTokenScopeReadUser) + "," + string(auth_model.AccessTokenScopeWriteUser)
	case CallInstance:
		s = string(auth_model.AccessTokenScopeReadAdmin)
	default:
		return "", fmt.Errorf("asctrust: unknown call kind %d", kind)
	}
	return auth_model.AccessTokenScope(s).Normalize()
}

// UserCall returns a context whose calls to asc are made as doer, and the function that ends the credential. The
// caller defers release immediately. repo is the repository of a CallRepoRead or CallRepoWrite call and is nil for
// the other kinds.
func UserCall(ctx context.Context, doer *user_model.User, kind CallKind, repo *repo_model.Repository) (context.Context, func(), error) {
	if doer == nil || doer.ID <= 0 {
		return ctx, func() {}, ErrNoDoer
	}
	var repoID int64
	switch kind {
	case CallRepoRead, CallRepoWrite:
		if repo == nil {
			return ctx, func() {}, errors.New("asctrust: a repository call needs a repository")
		}
		repoID = repo.ID
	default:
		if repo != nil {
			return ctx, func() {}, errors.New("asctrust: only a repository call names a repository")
		}
	}
	scope, err := scopeOf(kind)
	if err != nil {
		return ctx, func() {}, err
	}
	token, revoke, err := issueToken(ctx, doer, scope, repoID)
	if err != nil {
		return ctx, func() {}, fmt.Errorf("asctrust: issue the call credential: %w", err)
	}
	callCtx, err := ascclient.WithUserToken(ctx, token)
	if err != nil {
		revoke()
		return ctx, func() {}, fmt.Errorf("asctrust: the call credential: %w", err)
	}
	// The identity the hub signed in goes with the token, signed with the service secret (ascidentity).
	if callCtx, err = ascidentity.AttachDoer(callCtx, doer); err != nil {
		revoke()
		return ctx, func() {}, fmt.Errorf("asctrust: the forwarded identity: %w", err)
	}
	return callCtx, revoke, nil
}

var lastSweep atomic.Int64

// issueAccessToken is the default issuer: a Forgejo access token for doer, deleted by the returned function.
func issueAccessToken(ctx context.Context, doer *user_model.User, scope auth_model.AccessTokenScope, repoID int64) (string, func(), error) {
	sweepStale(ctx)
	t := &auth_model.AccessToken{
		UID:              doer.ID,
		Name:             tokenPrefix + util.CryptoRandomString(util.RandomStringMedium),
		Scope:            scope,
		ResourceAllRepos: repoID == 0,
	}
	err := db.WithTx(ctx, func(ctx context.Context) error {
		if err := auth_model.NewAccessToken(ctx, t); err != nil {
			return err
		}
		if repoID != 0 {
			return auth_model.InsertAccessTokenResourceRepos(ctx, t.ID, []*auth_model.AccessTokenResourceRepo{{RepoID: repoID}})
		}
		return nil
	})
	if err != nil {
		return "", nil, err
	}
	id, uid := t.ID, doer.ID
	revoke := func() {
		dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), revokeTimeout)
		defer cancel()
		if err := auth_model.DeleteAccessTokenByID(dctx, id, uid); err != nil && !auth_model.IsErrAccessTokenNotExist(err) {
			// The sweep removes it later. The log names the row, never the token.
			log.Error("asctrust: delete call credential %d: %v", id, err)
		}
	}
	return t.Token, revoke, nil
}

// sweepStale deletes the call tokens that a crash left behind. It runs at most once a minute.
func sweepStale(ctx context.Context) {
	now := time.Now().Unix()
	last := lastSweep.Load()
	if now-last < int64(sweepEvery/time.Second) || !lastSweep.CompareAndSwap(last, now) {
		return
	}
	cutoff := timeutil.TimeStamp(now - int64(staleAfter/time.Second))
	var rows []struct {
		ID  int64
		UID int64 `xorm:"uid"`
	}
	err := db.GetEngine(ctx).Table("access_token").
		Where("name LIKE ? AND created_unix < ?", tokenPrefix+"%", cutoff).
		Cols("id", "uid").Limit(100).Find(&rows)
	if err != nil {
		log.Error("asctrust: sweep call credentials: %v", err)
		return
	}
	for _, r := range rows {
		if err := auth_model.DeleteAccessTokenByID(ctx, r.ID, r.UID); err != nil && !auth_model.IsErrAccessTokenNotExist(err) {
			log.Error("asctrust: sweep call credential %d: %v", r.ID, err)
		}
	}
}
