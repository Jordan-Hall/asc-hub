// Copyright 2026 The Forgejo Authors.
// SPDX-License-Identifier: GPLv3-or-later

package tests

// See README.md for a documentation of the test logic

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	actions_model "forgejo.org/models/actions"
	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	org_model "forgejo.org/models/organization"
	"forgejo.org/models/perm"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	unit_model "forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git"
	"forgejo.org/modules/optional"
	"forgejo.org/modules/structs"
	"forgejo.org/modules/util"
	"forgejo.org/modules/web/routing"
	apiv1_permissions "forgejo.org/routers/api/v1/permissions"
	apiv1_permissions_testhelpers "forgejo.org/routers/api/v1/permissions/testhelpers"
	"forgejo.org/services/auth"
	"forgejo.org/services/authz"
	issue_service "forgejo.org/services/issue"
	packages_service "forgejo.org/services/packages"
	pull_service "forgejo.org/services/pull"
	"forgejo.org/tests/forgery"

	"github.com/stretchr/testify/require"
)

func fixtureCreateToken(t *testing.T, user *user_model.User, scope auth_model.AccessTokenScope, repoIDs ...int64) (*auth_model.AccessToken, error) {
	t.Helper()
	scope, err := scope.Normalize()
	require.NoError(t, err)
	resourceAllRepos := len(repoIDs) == 0
	accessToken := &auth_model.AccessToken{
		UID:              user.ID,
		Name:             util.CryptoRandomString(10),
		Scope:            scope,
		ResourceAllRepos: resourceAllRepos,
	}
	require.NoError(t, auth_model.NewAccessToken(t.Context(), accessToken))
	if len(repoIDs) > 0 {
		var resourceRepos []*auth_model.AccessTokenResourceRepo
		for _, repoID := range repoIDs {
			resourceRepos = append(resourceRepos, &auth_model.AccessTokenResourceRepo{
				TokenID: accessToken.ID,
				RepoID:  repoID,
			})
		}
		require.NoError(t, auth_model.InsertAccessTokenResourceRepos(t.Context(), accessToken.ID, resourceRepos))
	}
	return accessToken, nil
}

func fixtureCreateIssue(t *testing.T, user *user_model.User, repo *repo_model.Repository, title, content string) *issues_model.Issue {
	t.Helper()
	issue := &issues_model.Issue{
		RepoID:   repo.ID,
		Title:    title,
		Content:  content,
		PosterID: user.ID,
		Poster:   user,
	}

	err := issue_service.NewIssue(t.Context(), repo, issue, nil, nil, nil)
	require.NoError(t, err)

	return issue
}

func fixtureGetUser(t *testing.T, name string) *user_model.User {
	t.Helper()
	existingUser, err := user_model.GetUserByName(t.Context(), name)
	if err == nil {
		return existingUser
	} else if !user_model.IsErrUserNotExist(err) {
		require.NoError(t, err)
	}
	return nil
}

func fixtureGetOrg(t *testing.T, name string) *org_model.Organization {
	t.Helper()
	return (*org_model.Organization)(fixtureGetUser(t, name))
}

func fixtureCreateUser(t *testing.T, user *user_model.User) *user_model.User {
	t.Helper()
	if existingUser := fixtureGetUser(t, user.Name); existingUser != nil {
		return existingUser
	}
	overwriteDefault := &user_model.CreateUserOverwriteOptions{}
	user.Email = user.Name + "@test.forgejo.org"
	user.Passwd = "password"
	if strings.Contains(user.Name, "private") {
		visibility := structs.VisibleTypePrivate
		overwriteDefault.Visibility = &visibility
	} else if strings.Contains(user.Name, "limited") {
		visibility := structs.VisibleTypeLimited
		overwriteDefault.Visibility = &visibility
	}
	require.NoError(t, user_model.CreateUser(t.Context(), user, overwriteDefault))
	return user
}

func fixtureCreateOrg(t *testing.T, org *org_model.Organization, owner *user_model.User) *org_model.Organization {
	t.Helper()
	if existing := fixtureGetOrg(t, org.Name); existing != nil {
		return existing
	}
	owner = fixtureCreateUser(t, owner)
	if strings.Contains(org.Name, "private") {
		org.Visibility = structs.VisibleTypePrivate
	}
	require.NoError(t, org_model.CreateOrganization(t.Context(), org, owner))
	return org
}

func fixtureCreateTeams(t *testing.T, org *org_model.Organization, teams string) {
	t.Helper()

	for team := range strings.SplitSeq(teams, ",") {
		teamName, memberName, found := strings.Cut(team, ":")
		require.True(t, found)
		fixtureCreateTeam(t, org, memberName, &forgery.CreateTeamOptions{
			Name: teamName,
			Mode: perm.AccessModeWrite,
		})
	}
}

func fixtureCreateTeam(t *testing.T, org *org_model.Organization, memberName string, opts *forgery.CreateTeamOptions) *org_model.Team {
	t.Helper()

	member := fixtureCreateUser(t, &user_model.User{Name: memberName})
	opts.Members = []*user_model.User{member}
	team := forgery.CreateTeam(t, org, opts)
	require.NotNil(t, team)
	return team
}

func fixtureSetPackageOwner(t *testing.T, permissions *apiv1_permissions.Permissions, fixtureData *fixtureData) {
	t.Helper()
	if !fixtureData.Has("packageOwner") {
		return
	}
	owner := fixtureCreateUser(t, &user_model.User{Name: fixtureData.Get("packageOwner")})
	permissions.SetPackageOwner(owner)
	mode, err := packages_service.DeterminePackageAccessMode(permissions.GetContext(), permissions.GetPackageOwner(), permissions.GetDoer())
	require.NoError(t, err)
	permissions.SetPackageAccessMode(mode)
}

func fixtureSetDoer(t *testing.T, permissions *apiv1_permissions.Permissions, fixtureData *fixtureData) {
	t.Helper()
	if !fixtureData.Has("doer") {
		return
	}
	if doer := permissions.GetDoer(); doer != nil {
		if doer.Name != fixtureData.Get("doer") {
			panic(fmt.Sprintf("attempting to override already doer %s with %s", doer.Name, fixtureData.Get("doer")))
		}
		return
	}
	name := fixtureData.Get("doer")
	if name == user_model.ActionsUserName {
		fixtureSetDoerActionsUser(t, permissions, fixtureData)
	} else {
		fixtureSetDoerRegularUser(t, permissions, fixtureData)
	}
}

var _ auth.AuthenticationResult = &actionsTaskTokenAuthenticationResult{}

type actionsTaskTokenAuthenticationResult struct {
	*auth.BaseAuthenticationResult
	user   *user_model.User
	taskID int64
}

func (r *actionsTaskTokenAuthenticationResult) Scope() optional.Option[auth_model.AccessTokenScope] {
	return optional.None[auth_model.AccessTokenScope]()
}

func (r *actionsTaskTokenAuthenticationResult) User() *user_model.User {
	return r.user
}

func (r *actionsTaskTokenAuthenticationResult) ActionsTaskID() optional.Option[int64] {
	return optional.Some(r.taskID)
}

func fixtureSetDoerActionsUser(t *testing.T, permissions *apiv1_permissions.Permissions, fixtureData *fixtureData) {
	permissions.SetDoer(user_model.NewActionsUser())
	repository := permissions.GetRepository()
	require.NotNil(t, repository)
	repositoryID := repository.ID
	if fixtureData.Get("task.RepoID") == "unrelated" {
		repositoryID = 13245
	}
	task := &actions_model.ActionTask{
		RepoID: repositoryID,
	}
	if fixtureData.Get("task.IsForkPullRequest") == "true" {
		task.IsForkPullRequest = true
	}
	task.GenerateToken()
	{
		_, err := db.GetEngine(t.Context()).Insert(task)
		require.NoError(t, err)
		require.NotZero(t, task.ID)
	}

	permissions.SetAuthentication(&actionsTaskTokenAuthenticationResult{user: permissions.GetDoer(), taskID: task.ID})
	permissions.SetReducer(&authz.AllAccessAuthorizationReducer{})
	permission, err := access_model.GetUserRepoPermissionWithReducer(permissions.GetContext(), permissions.GetRepository(), permissions.GetDoer(), permissions.GetReducer())
	require.NoError(t, err)
	permissions.SetPermission(&permission)
}

var _ auth.AuthenticationResult = &basicPasswordAuthenticationResult{}

type basicPasswordAuthenticationResult struct {
	*auth.BaseAuthenticationResult
	user *user_model.User
}

func (*basicPasswordAuthenticationResult) IsPasswordAuthentication() bool {
	return true
}

func (r *basicPasswordAuthenticationResult) User() *user_model.User {
	return r.user
}

var _ auth.AuthenticationResult = &accessTokenAuthenticationResult{}

type accessTokenAuthenticationResult struct {
	*auth.BaseAuthenticationResult
	user    *user_model.User
	scope   auth_model.AccessTokenScope
	reducer authz.AuthorizationReducer
}

func (r *accessTokenAuthenticationResult) User() *user_model.User {
	return r.user
}

func (r *accessTokenAuthenticationResult) Scope() optional.Option[auth_model.AccessTokenScope] {
	return optional.Some(r.scope)
}

func (r *accessTokenAuthenticationResult) Reducer() authz.AuthorizationReducer {
	return r.reducer
}

var _ auth.AuthenticationResult = &reverseProxyAuthenticationResult{}

type reverseProxyAuthenticationResult struct {
	*auth.BaseAuthenticationResult
	user *user_model.User
}

func (r *reverseProxyAuthenticationResult) User() *user_model.User {
	return r.user
}

func (*reverseProxyAuthenticationResult) IsReverseProxyAuthentication() bool {
	return true
}

func fixtureSetDoerRegularUser(t *testing.T, permissions *apiv1_permissions.Permissions, fixtureData *fixtureData) {
	var scope auth_model.AccessTokenScope
	if fixtureData.Has("scope") {
		scope = auth_model.AccessTokenScope(fixtureData.Get("scope"))
	} else {
		scope = auth_model.AccessTokenScopeAll
	}
	if fixtureData.Has("doer") {
		doer := fixtureData.Get("doer")
		if doer != "anonymous" {
			isAdmin := strings.Contains(doer, "admin")
			user := &user_model.User{
				Name:    doer,
				IsAdmin: isAdmin,
			}
			fixtureCreateUser(t, user)
			permissions.SetDoer(user)
		}
	} else {
		panic(fmt.Errorf("attempting to set doer with no name"))
	}

	if permissions.GetDoer() == nil {
		permissions.SetAuthentication(&auth.UnauthenticatedResult{})
	} else {
		token, err := fixtureCreateToken(t, permissions.GetDoer(), scope)
		require.NoError(t, err)
		tokenReducer, err := authz.GetAuthorizationReducerForAccessToken(t.Context(), token)
		require.NoError(t, err)
		permissions.SetIsSigned(true)
		switch fixtureData.Get("authentication") {
		case "basic":
			permissions.SetAuthentication(&basicPasswordAuthenticationResult{user: permissions.GetDoer()})
		case "proxy":
			permissions.SetAuthentication(&reverseProxyAuthenticationResult{user: permissions.GetDoer()})
		default:
			permissions.SetToken(token)
			permissions.SetAuthentication(&accessTokenAuthenticationResult{user: permissions.GetDoer(), scope: token.Scope, reducer: tokenReducer})
		}
	}
}

func fixtureCreateBranch(t *testing.T, permissions *apiv1_permissions.Permissions, branch string) {
	t.Helper()
	repository := permissions.GetRepository()
	require.NotNil(t, repository)

	gitRepo, err := git.OpenRepository(t.Context(), repository.RepoPath())
	require.NoError(t, err)
	defaultBranch, err := git.GetDefaultBranch(t.Context(), repository.RepoPath())
	require.NoError(t, err)
	require.NoError(t, gitRepo.CreateBranch(branch, defaultBranch))
}

func fixtureCreatePullRequest(t *testing.T, permissions *apiv1_permissions.Permissions, fixtureData *fixtureData) {
	t.Helper()
	if !fixtureData.Has("pullRequest") {
		return
	}

	repository := permissions.GetRepository()
	require.NotNil(t, repository)

	poster := fixtureGetUser(t, fixtureData.Get("pullRequestAuthor"))
	require.NotNil(t, poster)

	ctx, committer, err := db.TxContext(t.Context())
	require.NoError(t, err)
	defer committer.Close()

	idx, err := db.GetNextResourceIndex(ctx, "issue_index", repository.ID)
	if err != nil {
		panic(fmt.Errorf("generate issue index failed: %w", err))
	}
	issue := &issues_model.Issue{
		Index:    idx,
		RepoID:   repository.ID,
		IsPull:   true,
		Title:    fixtureData.Get("pullRequest"),
		PosterID: poster.ID,
		Poster:   poster,
	}

	sess := db.GetEngine(ctx)

	if _, err = sess.NoAutoTime().Insert(issue); err != nil {
		panic(err)
	}
	issue.PullRequest = &issues_model.PullRequest{}

	pr := issue.PullRequest
	pr.Index = issue.Index
	pr.IssueID = issue.ID
	pr.HeadRepoID = repository.ID
	pr.BaseRepoID = repository.ID
	pr.HeadBranch = fixtureData.Get("pullRequestBranch")
	_, err = sess.NoAutoTime().Insert(pr)
	require.NoError(t, err)
	require.NoError(t, committer.Commit())
	require.NoError(t, pr.LoadBaseRepo(ctx))
	require.NoError(t, pr.LoadHeadRepo(ctx))
	require.NoError(t, pull_service.PushToBaseRepo(ctx, pr))
}

func fixtureSetRepository(t *testing.T, permissions *apiv1_permissions.Permissions, fixtureData *fixtureData) {
	t.Helper()
	if !fixtureData.Has("repository") {
		return
	}
	if repository := permissions.GetRepository(); repository != nil {
		if repository.FullName() != fixtureData.Get("repository") {
			panic(fmt.Sprintf("attempting to override already repository %s with %s", repository.FullName(), fixtureData.Get("repository")))
		}
		return
	}
	ownerName, repoName, found := strings.Cut(fixtureData.Get("repository"), "/")
	require.True(t, found)
	owner := fixtureCreateUser(t, &user_model.User{Name: ownerName})
	opts := &forgery.CreateRepositoryOptions{
		Name:      repoName,
		IsPrivate: strings.Contains(repoName, "private"),
	}
	if fixtureData.Get("repository-init") == "true" {
		opts.Files = forgery.FilesInit{}
	}
	repository := forgery.CreateRepository(t, owner, opts)
	// some of it is redundant with the config but that makes
	// the tests immune to changes in the defaults
	for _, unitType := range unit_model.DefaultRepoUnits {
		forgery.EnableRepoUnit(t, repository, unitType, nil)
	}
	if strings.Contains(repoName, "archived") {
		require.NoError(t, repo_model.SetArchiveRepoState(t.Context(), repository, true))
	}
	permissions.SetRepository(repository)
}

func dataToString(t *testing.T, fixtureData *fixtureData, key string) string {
	t.Helper()
	require.True(t, fixtureData.Has(key))
	return fixtureData.Get(key)
}

func fixtureGetIssue(t *testing.T, fixtureData *fixtureData) *issues_model.Issue {
	t.Helper()
	var issue issues_model.Issue
	found, err := db.GetEngine(t.Context()).Where("name = ?", dataToString(t, fixtureData, "issue")).Get(&issue)
	require.NoError(t, err)
	if !found {
		return nil
	}
	return &issue
}

func fixtureSetIssue(t *testing.T, permissions *apiv1_permissions.Permissions, fixtureData *fixtureData) {
	t.Helper()
	if fixtureGetIssue(t, fixtureData) == nil {
		authorName := fixtureData.Get("issueAuthor")
		author := fixtureCreateUser(t, &user_model.User{Name: authorName})
		_ = fixtureCreateIssue(t, author, permissions.GetRepository(), dataToString(t, fixtureData, "issue"), "issue description")
	}
}

func fixtureGetComment(t *testing.T, fixtureData *fixtureData) *issues_model.Comment {
	var comment issues_model.Comment
	found, err := db.GetEngine(t.Context()).Where("content = ?", dataToString(t, fixtureData, "comment")).Get(&comment)
	require.NoError(t, err)
	if !found {
		return nil
	}
	_ = comment.LoadIssue(t.Context())
	return &comment
}

func fixtureCreateComment(t *testing.T, permissions *apiv1_permissions.Permissions, fixtureData *fixtureData) {
	t.Helper()
	if fixtureGetComment(t, fixtureData) == nil {
		authorName := fixtureData.Get("issueAuthor")
		author := fixtureCreateUser(t, &user_model.User{Name: authorName})
		_, err := issues_model.CreateComment(t.Context(), &issues_model.CreateCommentOptions{
			Type:    issues_model.CommentTypeComment,
			Doer:    author,
			Issue:   fixtureGetIssue(t, fixtureData),
			Repo:    permissions.GetRepository(),
			Content: dataToString(t, fixtureData, "comment"),
		})
		require.NoError(t, err)
	}
}

func fixtureDisableRepoUnit(t *testing.T, permissions *apiv1_permissions.Permissions, unitType unit_model.Type) {
	t.Helper()
	repo := permissions.GetRepository()
	require.NotNil(t, repo)
	forgery.DisableRepoUnits(t, repo, unitType)
}

func fixtureDisableUnits(t *testing.T, permissions *apiv1_permissions.Permissions, fixtureData *fixtureData) {
	t.Helper()
	if !fixtureData.Has("disable-units") {
		return
	}
	for unit := range strings.SplitSeq(fixtureData.Get("disable-units"), ",") {
		unitType := unit_model.TypeFromKey(unit)
		if unitType == unit_model.TypeInvalid {
			panic(fmt.Errorf("unable to find a unit matching '%s'", unit))
		}
		fixtureDisableRepoUnit(t, permissions, unitType)
	}
}

type fixtureData struct {
	entries map[string]string
}

func (o *fixtureData) Set(key, value string) {
	o.entries[key] = value
}

func (o *fixtureData) SetDefault(key, value string) {
	if !o.Has(key) {
		o.Set(key, value)
	}
}

func (o *fixtureData) Get(key string) string {
	return o.entries[key]
}

func (o *fixtureData) Has(key string) bool {
	_, has := o.entries[key]
	return has
}

func (o *fixtureData) String() string {
	var s []string
	for k, e := range o.entries {
		s = append(s, fmt.Sprintf("%s:%s", k, e))
	}
	slices.Sort(s)
	return strings.Join(s, ",")
}

func newFixtureData(data map[string]string) *fixtureData {
	fixtureData := &fixtureData{
		entries: make(map[string]string, 10),
	}
	for key, value := range data {
		fixtureData.Set(key, value)
	}
	return fixtureData
}

func (o *fixtureData) Clone() *fixtureData {
	return &fixtureData{entries: maps.Clone(o.entries)}
}

type fixtureType struct {
	data  *fixtureData
	error string

	used bool
}

func (o *fixtureType) Clone() *fixtureType {
	f := *o
	f.data = o.data.Clone()
	return &f
}

// See README.md for a documentation of the test logic that uses
// this test description.
type functionTest struct {
	// The fixture will be constructed, when this function is the last
	// one of the chain.  It will go through the fulfillNeeds and
	// interpret of the previous functions in the chain, as well as its
	// own interpret.
	fixtures []*fixtureType

	// List the settings which might be updated while interpreting the fixtureData
	// so that they are restored upon test completion.
	protectSettingsBool []*bool

	// number of static arguments to pass to call's last argument
	staticArgs int
	// call the middleware (set automatically by [registerFunctionTest])
	call func(t *testing.T, ctx apiv1_permissions.Context, data *fixtureData, staticArgs []any)

	sequenceFilter []string
	fulfillNeeds   func(t *testing.T, data *fixtureData)
	interpret      func(t *testing.T, permissions *apiv1_permissions.Permissions, data *fixtureData)
}

func buildSignatureStringToFunctionTest(t *testing.T) {
	for signatureString, signature := range apiv1_permissions_testhelpers.GetSignatureStringToSignature() {
		for prefix, builder := range prefixToFunctionTestBuilder {
			if strings.HasPrefix(signatureString, prefix) {
				builder(t, signatureString, signature)
			}
		}
	}
}

func registerFunctionTest(fun func(apiv1_permissions.Context), test functionTest) bool {
	shortName := routing.GetFuncShortName(fun)
	test.call = func(t *testing.T, ctx apiv1_permissions.Context, _ *fixtureData, _ []any) {
		t.Logf("calling %s(ctx)", shortName)
		fun(ctx)
	}
	return registerFunctionTestWithCall(fun, test)
}

func registerFunctionTestWithCall(fun any, test functionTest) bool {
	signatureString := apiv1_permissions_testhelpers.SignatureToString([]any{fun})
	if _, has := signatureStringToFunctionTest[signatureString]; has {
		panic(fmt.Errorf("attempt to register %s twice", signatureString))
	}
	if test.call == nil {
		panic("'call' field is required")
	}
	signatureStringToFunctionTest[signatureString] = test
	return true
}

var signatureStringToFunctionTest = map[string]functionTest{}

type functionTestBuilder func(t *testing.T, signatureString string, signature []any)

func registerFunctionTestBuilder(prefixes []string, builder functionTestBuilder) bool {
	for _, prefix := range prefixes {
		if _, has := prefixToFunctionTestBuilder[prefix]; has {
			panic(fmt.Errorf("attempt to register %s twice", prefix))
		}
		prefixToFunctionTestBuilder[prefix] = builder
	}
	return true
}

var prefixToFunctionTestBuilder = map[string]functionTestBuilder{}
