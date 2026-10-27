// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package private

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"

	actions_model "forgejo.org/models/actions"
	asymkey_model "forgejo.org/models/asymkey"
	auth_model "forgejo.org/models/auth"
	git_model "forgejo.org/models/git"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/perm"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/private"
	"forgejo.org/modules/setting"
	auth_service "forgejo.org/services/auth"
	auth_method "forgejo.org/services/auth/method"
	"forgejo.org/services/authz"
	"forgejo.org/services/context"
)

// ascAuthGroup authenticates the bearer token that a client presented to asc
// serve. Only bearer credentials are accepted: no password, no session and no
// Basic header.
var ascAuthGroup = auth_method.NewGroup(
	&auth_method.OAuth2{},
	&auth_method.AccessToken{PermitBearer: true},
	&auth_method.ActionTaskToken{PermitBearer: true},
)

// ascBasicAuthGroup authenticates a user:token pair that a client presented to
// asc serve as an HTTP Basic login, with the methods of Forgejo's git routes: a
// token as the password first, then the account password last because it costs
// a password hash. There is no session and no reverse-proxy login, and the
// bearer forms are not read.
var ascBasicAuthGroup = auth_method.NewGroup(
	&auth_method.OAuth2{},
	&auth_method.AccessToken{PermitBasic: true},
	&auth_method.ActionTaskToken{PermitBasic: true},
	&auth_method.Basic{},
)

// AscAuthz answers `POST /api/internal/asc/authz`: may this principal read or
// write this repository through asc serve? A denial is always a 200 with
// `allowed: false`. When the credential did not authenticate, nothing else is
// set, so asc answers 401 as Forgejo's git routes do. When it did, only the
// user id and name are set, so asc answers 403, and a missing repository, an
// unreadable one and a missing permission look the same.
func AscAuthz(ctx *context.PrivateContext) {
	if !setting.Asc.Enabled {
		ctx.Error(http.StatusNotFound)
		return
	}

	authzReq, err := decodeAscAuthzRequest(ctx)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, private.Response{UserMsg: err.Error()})
		return
	}

	resp, err := ascAuthorize(ctx, authzReq)
	if err != nil {
		log.Error("asc authz: %v", err)
		ctx.JSON(http.StatusInternalServerError, private.Response{Err: "internal error"})
		return
	}
	ctx.JSON(http.StatusOK, resp)
}

func decodeAscAuthzRequest(ctx *context.PrivateContext) (*private.AscAuthzRequest, error) {
	req := &private.AscAuthzRequest{}
	if err := decodeAscStrict(ctx, req); err != nil {
		return nil, err
	}
	if err := validateAscAuthzRequest(req); err != nil {
		return nil, err
	}
	return req, nil
}

func validateAscAuthzRequest(req *private.AscAuthzRequest) error {
	if req.Mode != private.AscAuthzModeRead && req.Mode != private.AscAuthzModeWrite {
		return errors.New("mode must be read or write")
	}
	if req.Owner == "" || req.Repo == "" || len(req.Owner) > private.AscAuthzMaxName || len(req.Repo) > private.AscAuthzMaxName {
		return errors.New("invalid owner or repo")
	}
	if len(req.Ref) > private.AscAuthzMaxRef {
		return errors.New("ref is too long")
	}
	if req.SSHKey != nil {
		if req.Token != "" || req.User != "" || req.KeyID != 0 {
			return errors.New("ssh_key excludes token, user and key_id")
		}
		_, _, err := parseAscSSHKey(req.SSHKey)
		return err
	}
	if req.Token != "" && req.KeyID != 0 {
		return errors.New("token and key_id are mutually exclusive")
	}
	if req.KeyID < 0 {
		return errors.New("invalid key_id")
	}
	if len(req.Token) > private.AscAuthzMaxToken {
		return errors.New("token is too long")
	}
	if req.User != "" {
		return validateAscAuthzBasic(req)
	}
	for i := 0; i < len(req.Token); i++ {
		if c := req.Token[i]; c <= ' ' || c >= 0x7f {
			return errors.New("invalid token")
		}
	}
	return nil
}

// validateAscAuthzBasic checks the user:token pair of a Basic login. A password
// may hold spaces and non-ASCII text, so only control characters are refused;
// the user name must not hold the ':' that separates the two.
func validateAscAuthzBasic(req *private.AscAuthzRequest) error {
	if req.Token == "" {
		return errors.New("user needs a token")
	}
	if req.KeyID != 0 {
		return errors.New("user and key_id are mutually exclusive")
	}
	if len(req.User) > private.AscAuthzMaxName {
		return errors.New("user is too long")
	}
	if strings.ContainsRune(req.User, ':') || hasControlByte(req.User) {
		return errors.New("invalid user")
	}
	if hasControlByte(req.Token) {
		return errors.New("invalid token")
	}
	return nil
}

func hasControlByte(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c < ' ' || c == 0x7f {
			return true
		}
	}
	return false
}

// ascPrincipal is the asker after authentication.
type ascPrincipal struct {
	user      *user_model.User // nil for an anonymous asker and for a deploy key
	deployKey *asymkey_model.DeployKey
	task      *actions_model.ActionTask
	reducer   authz.AuthorizationReducer
	scope     string // the scope of the token, "" for any other credential
	// What the credential itself rested on, for ascPermVersion: the stored token (id and last change), the expiry
	// and the grant scopes of a token that carries them, and whether a password was used.
	tokenID, tokenUpdated int64
	expires               int64
	grantScopes           string
	passwordAuth          bool
	userID                int64
	username              string
}

// ascDenied is the answer for a credential that did not authenticate.
func ascDenied() *private.AscAuthzResponse { return &private.AscAuthzResponse{} }

// ascDeniedFor is the answer for a refusal. An asker who authenticated as a user
// learns that user's own identity, which tells asc the credential was good.
func ascDeniedFor(p *ascPrincipal) *private.AscAuthzResponse {
	if p == nil || p.user == nil {
		return ascDenied()
	}
	return &private.AscAuthzResponse{UserID: p.userID, Username: p.username}
}

// ascAuthorize authenticates before it looks at the repository. A principal
// that is returned with ok false authenticated but may not use the repository.
func ascAuthorize(ctx *context.PrivateContext, req *private.AscAuthzRequest) (*private.AscAuthzResponse, error) {
	mode := perm.AccessModeRead
	if req.Mode == private.AscAuthzModeWrite {
		mode = perm.AccessModeWrite
	}

	// repo is nil when the repository does not exist.
	repo, err := ascLoadRepo(ctx, req.Owner, req.Repo)
	if err != nil {
		return ascDenied(), err
	}

	var (
		principal *ascPrincipal
		ok        bool
	)
	switch {
	case req.SSHKey != nil:
		principal, ok, err = ascPrincipalFromSSHKey(ctx, repo, req.SSHKey, mode)
	case req.KeyID != 0:
		principal, ok, err = ascPrincipalFromKey(ctx, repo, req.KeyID, mode)
	case req.Token != "":
		principal, ok, err = ascPrincipalFromToken(ctx, repo, req, mode)
	default:
		// Anonymous: only read access to a public repository can follow.
		principal, ok = &ascPrincipal{}, mode == perm.AccessModeRead && !setting.Service.RequireSignInView
	}
	if err != nil {
		return ascDenied(), err
	}
	if principal == nil || !ok || repo == nil {
		return ascDeniedFor(principal), nil
	}
	// A repository that is being created or is broken cannot be used, and a
	// mirror or an archive is read-only.
	if repo.IsBeingCreated() || repo.IsBroken() {
		return ascDeniedFor(principal), nil
	}
	if mode > perm.AccessModeRead && (repo.IsMirror || repo.IsArchived) {
		return ascDeniedFor(principal), nil
	}

	access, err := ascPermission(ctx, repo, principal)
	if err != nil {
		return ascDenied(), err
	}
	// A job token can always read the code of its own repository, which is what checking it out does, even when the
	// `permissions:` of its workflow give no `contents` access.
	readsOwnCode := principal.task != nil && principal.task.RepoID == repo.ID && mode == perm.AccessModeRead
	if !access.CanAccess(mode, unit.TypeCode) && !readsOwnCode {
		return ascDeniedFor(principal), nil
	}

	resp := &private.AscAuthzResponse{
		Allowed:  true,
		RepoID:   repo.ID,
		OwnerID:  repo.OwnerID,
		UserID:   principal.userID,
		Username: principal.username,
		Perm:     max(access.UnitAccessMode(unit.TypeCode), mode).String(),
	}
	if principal.deployKey != nil {
		resp.DeployKeyID = principal.deployKey.ID
	}
	if principal.task != nil {
		resp.IsActionsTask = true
		if resp.Task, err = ascTaskInfo(ctx, principal.task); err != nil {
			return ascDenied(), err
		}
	}
	if resp.BranchRules.RequireSigned, err = ascRequireSigned(ctx, repo, req.Ref); err != nil {
		return ascDenied(), err
	}
	if resp.PermVersion, err = ascPermVersion(ctx, repo, principal, access, req.Ref); err != nil {
		return ascDenied(), err
	}
	return resp, nil
}

// ascLoadRepo returns nil, nil when the repository cannot be used: the owner or the repository does not exist, the
// owner is inactive, or the repository is not stored in asc.
func ascLoadRepo(ctx *context.PrivateContext, ownerName, repoName string) (*repo_model.Repository, error) {
	owner, err := user_model.GetUserByName(ctx, ownerName)
	if err != nil {
		if user_model.IsErrUserNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("GetUserByName: %w", err)
	}
	if !owner.IsOrganization() && !owner.IsActive {
		return nil, nil
	}
	repo, err := repo_model.GetRepositoryByName(ctx, owner.ID, repoName)
	if err != nil {
		if repo_model.IsErrRepoNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("GetRepositoryByName: %w", err)
	}
	// asc authorises by name, so only a repository that Forgejo stores in asc may be answered for. A repository on
	// git storage that took the name of one that was renamed or transferred away looks the same as a missing one.
	if repo.GetStorageBackend() != string(gitrepo.StorageBackendAsc) {
		return nil, nil
	}
	repo.Owner = owner
	repo.OwnerName = owner.Name
	return repo, nil
}

// ascUserUsable reports whether a user may sign in, with the same checks as the
// API middleware and /serv/command.
func ascUserUsable(ctx *context.PrivateContext, user *user_model.User) (bool, error) {
	if !user.IsActive || user.ProhibitLogin || user.MustChangePassword {
		return false, nil
	}
	if user.MustHaveTwoFactor() {
		has, err := auth_model.HasTwoFactorByUID(ctx, user.ID)
		if err != nil {
			return false, fmt.Errorf("HasTwoFactorByUID: %w", err)
		}
		return has, nil
	}
	return true, nil
}

func ascPrincipalFromKey(ctx *context.PrivateContext, repo *repo_model.Repository, keyID int64, mode perm.AccessMode) (*ascPrincipal, bool, error) {
	key, err := asymkey_model.GetPublicKeyByID(ctx, keyID)
	if err != nil {
		if asymkey_model.IsErrKeyNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("GetPublicKeyByID: %w", err)
	}
	return ascPrincipalFromPublicKey(ctx, repo, key, mode)
}

// ascPrincipalFromPublicKey is the asker behind a stored user key or deploy key.
// repo is nil when the repository does not exist.
func ascPrincipalFromPublicKey(ctx *context.PrivateContext, repo *repo_model.Repository, key *asymkey_model.PublicKey, mode perm.AccessMode) (*ascPrincipal, bool, error) {
	if key.Type == asymkey_model.KeyTypeDeploy {
		if repo == nil {
			return nil, false, nil
		}
		deployKey, err := asymkey_model.GetDeployKeyByRepo(ctx, key.ID, repo.ID)
		if err != nil {
			if asymkey_model.IsErrDeployKeyNotExist(err) {
				return nil, false, nil
			}
			return nil, false, fmt.Errorf("GetDeployKeyByRepo: %w", err)
		}
		if deployKey.Mode < mode {
			return nil, false, nil
		}
		// As in /serv/command, a deploy key acts as the repository owner.
		return &ascPrincipal{deployKey: deployKey, userID: repo.OwnerID, username: repo.OwnerName}, true, nil
	}

	user, err := user_model.GetUserByID(ctx, key.OwnerID)
	if err != nil {
		if user_model.IsErrUserNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("GetUserByID: %w", err)
	}
	if usable, err := ascUserUsable(ctx, user); err != nil || !usable {
		return nil, false, err
	}
	return &ascPrincipal{user: user, userID: user.ID, username: user.Name}, true, nil
}

func ascPrincipalFromToken(ctx *context.PrivateContext, repo *repo_model.Repository, req *private.AscAuthzRequest, mode perm.AccessMode) (*ascPrincipal, bool, error) {
	// The auth methods read the credential from a request, so build a minimal
	// one that carries only the Authorization header. It has no one-time
	// password header, so an account password never passes for a user who has
	// two-factor authentication.
	authReq, err := http.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
	if err != nil {
		return nil, false, err
	}
	group := ascAuthGroup
	if req.User != "" {
		group = ascBasicAuthGroup
		authReq.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(req.User+":"+req.Token)))
	} else {
		authReq.Header.Set("Authorization", "Bearer "+req.Token)
	}

	switch out := group.Verify(authReq, nil, nil).(type) {
	case *auth_service.AuthenticationSuccess:
		p, ok, err := ascPrincipalFromAuthResult(ctx, repo, out.Result, mode)
		if p != nil {
			// A personal access token is a row: which one, and when it last changed, is part of the decision.
			if tok, terr := auth_model.GetAccessTokenBySHA(ctx, req.Token); terr == nil {
				p.tokenID, p.tokenUpdated = tok.ID, int64(tok.UpdatedUnix)
			}
		}
		return p, ok, err
	case *auth_service.AuthenticationAttemptedIncorrectCredential, *auth_service.AuthenticationNotAttempted:
		return nil, false, nil
	case *auth_service.AuthenticationError:
		return nil, false, fmt.Errorf("authentication: %w", out.Error)
	default:
		return nil, false, errors.New("unexpected authentication output")
	}
}

func ascPrincipalFromAuthResult(ctx *context.PrivateContext, repo *repo_model.Repository, ar auth_service.AuthenticationResult, mode perm.AccessMode) (*ascPrincipal, bool, error) {
	user := ar.User()
	if user == nil {
		return nil, false, nil
	}
	p := &ascPrincipal{user: user, userID: user.ID, username: user.Name}

	if hasTask, taskID := ar.ActionsTaskID().Get(); hasTask {
		task, err := actions_model.GetTaskByID(ctx, taskID)
		if err != nil {
			return nil, false, fmt.Errorf("GetTaskByID: %w", err)
		}
		p.task = task
		return p, true, nil
	}

	if usable, err := ascUserUsable(ctx, user); err != nil || !usable {
		return nil, false, err
	}

	// The same rules as services/context.CheckRepoScopedToken: a public-only
	// token never reaches a private repository, and the token needs the
	// repository scope of the level it asks for.
	if has, scope := ar.Scope().Get(); has {
		publicOnly, err := scope.PublicOnly()
		if err != nil {
			return nil, false, fmt.Errorf("PublicOnly: %w", err)
		}
		if publicOnly && repo != nil && repo.IsPrivate {
			return p, false, nil
		}
		level := auth_model.Read
		if mode > perm.AccessModeRead {
			level = auth_model.Write
		}
		required := auth_model.GetRequiredScopes(level, auth_model.AccessTokenScopeCategoryRepository)
		matched, err := scope.HasScope(required...)
		if err != nil {
			return nil, false, fmt.Errorf("HasScope: %w", err)
		}
		if !matched {
			return p, false, nil
		}
	}
	p.reducer = ar.Reducer()
	if has, scope := ar.Scope().Get(); has {
		p.scope = string(scope)
	}
	if has, at := ar.ExpiresAt().Get(); has {
		p.expires = int64(at)
	}
	p.grantScopes = ar.OAuth2GrantScopes().ValueOrDefault("")
	p.passwordAuth = ar.IsPasswordAuthentication()
	return p, true, nil
}

// ascPermission computes what the principal may do in the repository.
func ascPermission(ctx *context.PrivateContext, repo *repo_model.Repository, p *ascPrincipal) (access_model.Permission, error) {
	switch {
	case p.deployKey != nil:
		// A deploy key has no user, and its own mode was checked already.
		if err := repo.LoadUnits(ctx); err != nil {
			return access_model.Permission{}, err
		}
		mode := perm.AccessModeRead
		if p.deployKey.Mode >= perm.AccessModeWrite {
			mode = perm.AccessModeWrite
		}
		return access_model.Permission{AccessMode: mode, Units: repo.Units}, nil
	case p.task != nil:
		return access_model.GetActionRepoPermission(ctx, repo, p.task)
	case p.reducer != nil:
		return access_model.GetUserRepoPermissionWithReducer(ctx, repo, p.user, p.reducer)
	default:
		return access_model.GetUserRepoPermission(ctx, repo, p.user)
	}
}

func ascTaskInfo(ctx *context.PrivateContext, task *actions_model.ActionTask) (*private.AscAuthzTask, error) {
	if err := task.LoadAttributes(ctx); err != nil {
		return nil, fmt.Errorf("task LoadAttributes: %w", err)
	}
	run := task.Job.Run
	info := &private.AscAuthzTask{
		RunID:    run.ID,
		Repo:     run.Repo.FullName(),
		Event:    string(run.Event),
		Ref:      run.Ref,
		IsForkPR: run.IsForkPullRequest || task.IsForkPullRequest,
	}
	if run.PullRequestID > 0 {
		pr, err := issues_model.GetPullRequestByID(ctx, run.PullRequestID)
		if err != nil {
			if issues_model.IsErrPullRequestNotExist(err) {
				return info, nil
			}
			return nil, fmt.Errorf("GetPullRequestByID: %w", err)
		}
		if err := pr.LoadIssue(ctx); err != nil {
			return nil, fmt.Errorf("pr LoadIssue: %w", err)
		}
		info.PRIndex = pr.Issue.Index
	}
	return info, nil
}

func ascRequireSigned(ctx *context.PrivateContext, repo *repo_model.Repository, ref string) (bool, error) {
	branch := strings.TrimPrefix(ref, "refs/heads/")
	if branch == "" {
		return false, nil
	}
	rule, err := git_model.GetFirstMatchProtectedBranchRule(ctx, repo.ID, branch)
	if err != nil {
		return false, fmt.Errorf("GetFirstMatchProtectedBranchRule: %w", err)
	}
	return rule != nil && rule.RequireSignedCommits, nil
}
