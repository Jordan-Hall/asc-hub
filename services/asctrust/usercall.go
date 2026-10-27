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
	"forgejo.org/modules/gitrepo"
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
	// CallRepoAdmin is a read or change that asc allows to a repository administrator only (the audit log of a
	// repository). asc asks the hub for write access to the repository to decide "administrator", so the token
	// must be one that may write.
	CallRepoAdmin
	// CallOrg is a read of an organisation (its audit log). asc lets the owner of every repository of the
	// organisation read it, which it decides repository by repository as for an administrator.
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

// scopeOf returns the access token scope a kind of call needs. It is the least that asc accepts for the call:
//
//   - a read of a repository is read:repository (and read:user, because asc asks the hub who the credential is);
//   - a change of a repository, and the calls that only a repository administrator may make, are
//     write:repository, because asc decides "administrator" by asking the hub for write access and reading the
//     permission in the answer. Such a token is always limited to the one repository (UserCall) or to the listed
//     repositories of an organisation (UserCallOrg), never to all repositories of the person;
//   - the audit log of the server is read:admin and read:user.
func scopeOf(kind CallKind) (auth_model.AccessTokenScope, error) {
	var s string
	switch kind {
	// Every call carries read:user: asc first asks the hub who the credential is (GET /api/v1/user) and refuses a
	// credential that cannot answer, whatever the rest of the call is.
	case CallRepoRead:
		s = string(auth_model.AccessTokenScopeReadRepository) + "," + string(auth_model.AccessTokenScopeReadUser)
	case CallRepoWrite, CallRepoAdmin:
		s = string(auth_model.AccessTokenScopeReadRepository) + "," + string(auth_model.AccessTokenScopeWriteRepository) + "," + string(auth_model.AccessTokenScopeReadUser)
	case CallOrg:
		// Only ever on the repositories of the organisation, listed one by one (UserCallOrg).
		s = string(auth_model.AccessTokenScopeWriteRepository) + "," + string(auth_model.AccessTokenScopeReadUser)
	case CallAccount:
		s = string(auth_model.AccessTokenScopeReadUser) + "," + string(auth_model.AccessTokenScopeWriteUser)
	case CallInstance:
		s = string(auth_model.AccessTokenScopeReadAdmin) + "," + string(auth_model.AccessTokenScopeReadUser)
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
	case CallRepoRead, CallRepoWrite, CallRepoAdmin:
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
	return withToken(ctx, doer, token, revoke)
}

// withToken puts the token and the signed identity of doer into the context of a call.
func withToken(ctx context.Context, doer *user_model.User, token string, revoke func()) (context.Context, func(), error) {
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

// MaxOrgRepos is the most repositories of an organisation a token is issued for: the most asc lets a log of an
// organisation cover.
const MaxOrgRepos = 200

// ErrNoOrgRepos is returned when an organisation has no repository in asc, or more than MaxOrgRepos: asc would
// refuse its log either way, so no token is issued.
var ErrNoOrgRepos = errors.New("asctrust: the organisation has no repository in asc, or too many")

// orgTokenIssuer issues a token limited to the listed repositories. Tests replace it.
type orgTokenIssuer func(ctx context.Context, doer *user_model.User, scope auth_model.AccessTokenScope, repoIDs []int64) (token string, revoke func(), err error)

var issueOrgToken orgTokenIssuer = issueAccessTokenRepos

// UseOrgTokenIssuer replaces the issuer of the tokens of UserCallOrg until the returned function is called. It is
// for tests.
func UseOrgTokenIssuer(fn func(ctx context.Context, doer *user_model.User, scope auth_model.AccessTokenScope, repoIDs []int64) (string, func(), error)) (restore func()) {
	prev := issueOrgToken
	issueOrgToken = fn
	return func() { issueOrgToken = prev }
}

// UserCallOrg is UserCall for the log of an organisation. asc lets the owner of every repository of the
// organisation read it and decides that repository by repository, so the token is made for exactly the repositories
// of the organisation that are stored in asc (at most MaxOrgRepos), as rows of the token and not as "all
// repositories". Like every call token it lives for the one request: it is deleted when release is called, also
// when the request was cancelled, and a crash leaves one for the sweep to delete after staleAfter.
func UserCallOrg(ctx context.Context, doer *user_model.User, orgID int64) (context.Context, func(), error) {
	if doer == nil || doer.ID <= 0 {
		return ctx, func() {}, ErrNoDoer
	}
	var ids []int64
	if err := db.GetEngine(ctx).Table("repository").Where("owner_id = ? AND storage_backend = ?", orgID, string(gitrepo.StorageBackendAsc)).Cols("id").Limit(MaxOrgRepos + 1).Find(&ids); err != nil {
		return ctx, func() {}, fmt.Errorf("asctrust: the repositories of the organisation: %w", err)
	}
	if len(ids) == 0 || len(ids) > MaxOrgRepos {
		return ctx, func() {}, ErrNoOrgRepos
	}
	scope, err := scopeOf(CallOrg)
	if err != nil {
		return ctx, func() {}, err
	}
	token, revoke, err := issueOrgToken(ctx, doer, scope, ids)
	if err != nil {
		return ctx, func() {}, fmt.Errorf("asctrust: issue the call credential: %w", err)
	}
	return withToken(ctx, doer, token, revoke)
}

var lastSweep atomic.Int64

// issueAccessToken is the default issuer: a Forgejo access token for doer, deleted by the returned function. A
// token for a repository is limited to that one repository; repoID 0 is for the calls about the person, which have no
// repository.
func issueAccessToken(ctx context.Context, doer *user_model.User, scope auth_model.AccessTokenScope, repoID int64) (string, func(), error) {
	if repoID == 0 {
		return issueTokenRows(ctx, doer, scope, nil, true)
	}
	return issueTokenRows(ctx, doer, scope, []int64{repoID}, false)
}

// issueAccessTokenRepos issues a token limited to the listed repositories.
func issueAccessTokenRepos(ctx context.Context, doer *user_model.User, scope auth_model.AccessTokenScope, repoIDs []int64) (string, func(), error) {
	if len(repoIDs) == 0 {
		return "", nil, errors.New("asctrust: a token for no repository")
	}
	return issueTokenRows(ctx, doer, scope, repoIDs, false)
}

func issueTokenRows(ctx context.Context, doer *user_model.User, scope auth_model.AccessTokenScope, repoIDs []int64, allRepos bool) (string, func(), error) {
	sweepStale(ctx)
	t := &auth_model.AccessToken{
		UID:              doer.ID,
		Name:             tokenPrefix + util.CryptoRandomString(util.RandomStringMedium),
		Scope:            scope,
		ResourceAllRepos: allRepos,
	}
	err := db.WithTx(ctx, func(ctx context.Context) error {
		if err := auth_model.NewAccessToken(ctx, t); err != nil {
			return err
		}
		rows := make([]*auth_model.AccessTokenResourceRepo, 0, len(repoIDs))
		for _, id := range repoIDs {
			rows = append(rows, &auth_model.AccessTokenResourceRepo{RepoID: id})
		}
		if len(rows) > 0 {
			return auth_model.InsertAccessTokenResourceRepos(ctx, t.ID, rows)
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
