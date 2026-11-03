// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package issue

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"

	issues_model "forgejo.org/models/issues"
	access_model "forgejo.org/models/perm/access"
	project_model "forgejo.org/models/project"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/util"
)

// ProjectCardError says why a card could not be added; Key is a locale key of asc_work.
type ProjectCardError struct {
	Key string
}

func (e ProjectCardError) Error() string { return "project card: " + e.Key }

var cardRefName = regexp.MustCompile(`^[\w.-]+$`)

// ParseProjectCardRef reads "#12", "12", "repo#12" or "owner/repo#12".
func ParseProjectCardRef(ref string) (owner, repo string, index int64, ok bool) {
	ref = strings.TrimSpace(ref)
	head, num := "", ref
	if i := strings.LastIndex(ref, "#"); i >= 0 {
		head, num = ref[:i], ref[i+1:]
	}
	index, err := strconv.ParseInt(num, 10, 64)
	if err != nil || index <= 0 || strings.HasPrefix(num, "+") {
		return "", "", 0, false
	}
	if head == "" {
		return "", "", index, true
	}
	owner, repo, hasOwner := strings.Cut(head, "/")
	if !hasOwner {
		owner, repo = "", head
	}
	if !cardRefName.MatchString(repo) || (hasOwner && !cardRefName.MatchString(owner)) {
		return "", "", 0, false
	}
	return owner, repo, index, true
}

// projectCardRepo finds the repository a reference names on the board of the project: a repository project takes only
// its own repository, a project of a user or an organization takes any repository of that owner.
func projectCardRepo(ctx context.Context, project *project_model.Project, owner, name string) (*repo_model.Repository, error) {
	notFound := ProjectCardError{Key: "asc_work.pj_card_not_found"}
	if project.RepoID > 0 {
		repo, err := repo_model.GetRepositoryByID(ctx, project.RepoID)
		if err != nil {
			return nil, err
		}
		if err := repo.LoadOwner(ctx); err != nil {
			return nil, err
		}
		if (name != "" && !strings.EqualFold(name, repo.Name)) || (owner != "" && !strings.EqualFold(owner, repo.OwnerName)) {
			return nil, ProjectCardError{Key: "asc_work.pj_card_other_repo"}
		}
		return repo, nil
	}
	if name == "" {
		return nil, ProjectCardError{Key: "asc_work.pj_card_need_repo"}
	}
	projectOwner, err := user_model.GetUserByID(ctx, project.OwnerID)
	if err != nil {
		return nil, err
	}
	if owner != "" && !strings.EqualFold(owner, projectOwner.Name) {
		return nil, ProjectCardError{Key: "asc_work.pj_card_other_owner"}
	}
	repo, err := repo_model.GetRepositoryByName(ctx, projectOwner.ID, name)
	if repo_model.IsErrRepoNotExist(err) {
		return nil, notFound
	}
	return repo, err
}

// AddProjectCard puts the issue or pull request that ref names on the board of the project, in its default column.
// The doer must be able to change that issue's project; an issue the doer cannot read is reported as not found.
func AddProjectCard(ctx context.Context, doer *user_model.User, project *project_model.Project, ref string) (*issues_model.Issue, error) {
	owner, name, index, ok := ParseProjectCardRef(ref)
	if !ok {
		return nil, ProjectCardError{Key: "asc_work.pj_card_bad_ref"}
	}
	repo, err := projectCardRepo(ctx, project, owner, name)
	if err != nil {
		return nil, err
	}
	notFound := ProjectCardError{Key: "asc_work.pj_card_not_found"}
	issue, err := issues_model.GetIssueByIndex(ctx, repo.ID, index)
	if issues_model.IsErrIssueNotExist(err) {
		return nil, notFound
	} else if err != nil {
		return nil, err
	}
	perm, err := access_model.GetUserRepoPermission(ctx, repo, doer)
	if err != nil {
		return nil, err
	}
	if !perm.CanReadIssuesOrPulls(issue.IsPull) {
		return nil, notFound
	}
	if !perm.CanWriteIssuesOrPulls(issue.IsPull) {
		return nil, ProjectCardError{Key: "asc_work.pj_card_no_permission"}
	}
	if err := issue.LoadProject(ctx); err != nil {
		return nil, err
	}
	if issue.Project != nil && issue.Project.ID == project.ID {
		return nil, ProjectCardError{Key: "asc_work.pj_card_already"}
	}
	if err := issues_model.IssueAssignOrRemoveProject(ctx, issue, doer, project.ID, 0); err != nil {
		if errors.Is(err, util.ErrPermissionDenied) {
			return nil, ProjectCardError{Key: "asc_work.pj_card_other_owner"}
		}
		return nil, err
	}
	return issue, nil
}
