// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"forgejo.org/models/organization"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascclient"
	"forgejo.org/services/ascidentity"
)

// The credential of a person for one request.
//
// The hub makes no access tokens for its web pages. A call from a page is made with the hub's service credential and
// a signed claim about the person (services/ascidentity): who they are and what the hub decided they may do on the
// subject of the call. The decision is the hub's own permission model, evaluated here, again, at the moment of the
// call, whatever the handler checked before: repository units, collaborators, teams, organisation ownership,
// private and public visibility, restricted and blocked users, site administrators, an inactive account or one that
// may not sign in. asc serve does not decide again.
//
// The claim is short-lived (a minute of clock skew), made for one subject (one repository, one organisation, the
// account of the person, the server) and per call; nothing is stored. Platform access tokens are what the asc command
// line, agents and API clients present, and they are not involved here.

// CallKind says what a call is for, and so what the hub must decide before it signs the claim.
type CallKind int

const (
	// CallRepoRead is a read of one repository that the person may read.
	CallRepoRead CallKind = iota + 1
	// CallRepoWrite is a change of one repository: the person may write its code.
	CallRepoWrite
	// CallRepoAdmin is a read or change that only an administrator of the repository may make (the audit log of a
	// repository, a scan of its dependencies).
	CallRepoAdmin
	// CallOrg is a read of an organisation (its audit log): an owner of the organisation.
	CallOrg
	// CallAccount is a call about the account of the person (signing keys).
	CallAccount
	// CallInstance is a call about the whole server (the instance audit log): a site administrator.
	CallInstance
)

var (
	// ErrNoDoer is returned when a person's call is asked for nobody.
	ErrNoDoer = errors.New("asctrust: no signed-in user")
	// ErrForbidden is returned when the hub's permission model does not allow the person what the call is for. No
	// claim is signed and nothing is sent to asc.
	ErrForbidden = errors.New("asctrust: the hub does not allow this to the person")
)

// UserCall returns a context whose calls to asc are made for doer, and the function that ends the call (nothing to
// end: it exists so that callers keep their `defer release()`). repo is the repository of a repository call and is nil
// for the other kinds.
func UserCall(ctx context.Context, doer *user_model.User, kind CallKind, repo *repo_model.Repository) (context.Context, func(), error) {
	noop := func() {}
	if !usable(ctx, doer) {
		return ctx, noop, ErrNoDoer
	}
	switch kind {
	case CallRepoRead, CallRepoWrite, CallRepoAdmin:
		if repo == nil {
			return ctx, noop, errors.New("asctrust: a repository call needs a repository")
		}
		// The states ascAuthorize (routers/private/asc.go) refuses for a command line client: a repository that is
		// being created or is broken cannot be used, and a mirror or an archive is read only.
		if repo.IsBeingCreated() || repo.IsBroken() {
			return ctx, noop, ErrForbidden
		}
		if kind == CallRepoWrite && (repo.IsMirror || repo.IsArchived) {
			return ctx, noop, ErrForbidden
		}
		perm, err := access_model.GetUserRepoPermission(ctx, repo, doer)
		if err != nil {
			return ctx, noop, fmt.Errorf("asctrust: the permission of the person: %w", err)
		}
		role, ok := "", false
		switch kind {
		case CallRepoRead:
			role, ok = ascidentity.RoleRead, perm.CanRead(unit.TypeCode)
		case CallRepoWrite:
			role, ok = ascidentity.RoleWrite, perm.CanWrite(unit.TypeCode)
		default:
			role, ok = ascidentity.RoleAdmin, perm.IsAdmin()
		}
		if !ok {
			return ctx, noop, ErrForbidden
		}
		// A role a person has is the highest the hub gives them, so the claim says admin to an administrator even for
		// a read: the server sees what the hub decided, not what the call asked for.
		if perm.IsAdmin() {
			role = ascidentity.RoleAdmin
		} else if perm.CanWrite(unit.TypeCode) && role == ascidentity.RoleRead {
			role = ascidentity.RoleWrite
		}
		name, err := RepoName(repo)
		if err != nil {
			return ctx, noop, err
		}
		return claim(ctx, doer, role, name)
	case CallAccount:
		if repo != nil {
			return ctx, noop, errors.New("asctrust: only a repository call names a repository")
		}
		return claim(ctx, doer, ascidentity.RoleWrite, "user:"+strings.ToLower(doer.Name))
	case CallInstance:
		if repo != nil {
			return ctx, noop, errors.New("asctrust: only a repository call names a repository")
		}
		if !doer.IsAdmin {
			return ctx, noop, ErrForbidden
		}
		return claim(ctx, doer, ascidentity.RoleAdmin, "instance")
	}
	return ctx, noop, fmt.Errorf("asctrust: unknown call kind %d", kind)
}

// UserCallOrg is UserCall for the log of an organisation: the person must be an owner of the organisation, or a site
// administrator.
func UserCallOrg(ctx context.Context, doer *user_model.User, org *user_model.User) (context.Context, func(), error) {
	noop := func() {}
	if !usable(ctx, doer) {
		return ctx, noop, ErrNoDoer
	}
	if org == nil || !org.IsOrganization() {
		return ctx, noop, errors.New("asctrust: an organisation call needs an organisation")
	}
	if !doer.IsAdmin {
		owner, err := organization.IsOrganizationOwner(ctx, org.ID, doer.ID)
		if err != nil {
			return ctx, noop, fmt.Errorf("asctrust: the ownership of the organisation: %w", err)
		}
		if !owner {
			return ctx, noop, ErrForbidden
		}
	}
	return claim(ctx, doer, ascidentity.RoleAdmin, "org:"+strings.ToLower(org.Name))
}

// usable reports whether the person may use the hub at all, with the checks the authz endpoint makes for a command line
// credential (routers/private/asc.go, ascUserUsable): an account that exists, is active, may sign in, has no pending
// password change, and has two-factor authentication when the hub requires it.
func usable(ctx context.Context, doer *user_model.User) bool {
	return doer != nil && doer.ID > 0 && !doer.MustChangePassword && doer.IsAccessAllowed(ctx)
}

// claim puts the hub's service credential and the signed claim into the context of a call.
func claim(ctx context.Context, doer *user_model.User, role, subject string) (context.Context, func(), error) {
	callCtx, err := ascidentity.AttachDoer(ascclient.WithSystem(ctx), doer, role, subject)
	if err != nil {
		return ctx, func() {}, fmt.Errorf("asctrust: the claim about the person: %w", err)
	}
	return callCtx, func() {}, nil
}
