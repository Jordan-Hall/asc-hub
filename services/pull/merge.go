// Copyright 2019 The Gitea Authors.
// All rights reserved.
// SPDX-License-Identifier: MIT

package pull

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"forgejo.org/models"
	git_model "forgejo.org/models/git"
	issues_model "forgejo.org/models/issues"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/cache"
	"forgejo.org/modules/git"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/references"
	repo_module "forgejo.org/modules/repository"
	"forgejo.org/modules/repostore"
	"forgejo.org/modules/setting"
	issue_service "forgejo.org/services/issue"
	notify_service "forgejo.org/services/notify"
)

var mergeMessageTemplates = make(map[repo_model.MergeStyle]string, len(repo_model.MergeStyles))

func LoadMergeMessageTemplates() error {
	// Load templates for all known merge styles
	for _, mergeStyle := range repo_model.MergeStyles {
		templateFilename := filepath.Join(
			setting.CustomPath,
			"default_merge_message",
			fmt.Sprintf("%s_TEMPLATE.md", strings.ToUpper(string(mergeStyle))),
		)

		content, err := os.ReadFile(templateFilename)
		if err == nil {
			mergeMessageTemplates[mergeStyle] = string(content)
		} else if os.IsNotExist(err) {
			// The file no longer exists, so delete any previous content
			delete(mergeMessageTemplates, mergeStyle)
		} else {
			return err
		}
	}
	return nil
}

// getMergeMessage composes the message used when merging a pull request.
func getMergeMessage(ctx context.Context, baseGitRepo *git.Repository, pr *issues_model.PullRequest, mergeStyle repo_model.MergeStyle, extraVars map[string]string) (message, body string, err error) {
	if err := pr.LoadBaseRepo(ctx); err != nil {
		return "", "", err
	}
	if err := pr.LoadHeadRepo(ctx); err != nil {
		return "", "", err
	}
	if err := pr.LoadIssue(ctx); err != nil {
		return "", "", err
	}
	if err := pr.Issue.LoadPoster(ctx); err != nil {
		return "", "", err
	}
	if err := pr.Issue.LoadRepo(ctx); err != nil {
		return "", "", err
	}

	isExternalTracker := pr.BaseRepo.UnitEnabled(ctx, unit.TypeExternalTracker)
	issueReference := "#"
	if isExternalTracker {
		issueReference = "!"
	}

	reviewedOn := fmt.Sprintf("Reviewed-on: %s", pr.Issue.HTMLURL())
	reviewedBy := pr.GetApprovers(ctx)

	body = fmt.Sprintf("%s\n%s", reviewedOn, reviewedBy)

	// Squash merge has a different from other styles.
	if mergeStyle == repo_model.MergeStyleSquash {
		message = fmt.Sprintf("%s (%s%d)", pr.Issue.Title, issueReference, pr.Issue.Index)
	} else if pr.BaseRepoID == pr.HeadRepoID {
		message = fmt.Sprintf("Merge pull request '%s' (%s%d) from %s into %s", pr.Issue.Title, issueReference, pr.Issue.Index, pr.HeadBranch, pr.BaseBranch)
	} else if pr.HeadRepo == nil {
		message = fmt.Sprintf("Merge pull request '%s' (%s%d) from <deleted>:%s into %s", pr.Issue.Title, issueReference, pr.Issue.Index, pr.HeadBranch, pr.BaseBranch)
	} else {
		message = fmt.Sprintf("Merge pull request '%s' (%s%d) from %s:%s into %s", pr.Issue.Title, issueReference, pr.Issue.Index, pr.HeadRepo.FullName(), pr.HeadBranch, pr.BaseBranch)
	}

	if mergeStyle != "" {
		commit, err := baseGitRepo.GetBranchCommit(pr.BaseRepo.DefaultBranch)
		if err != nil {
			return "", "", err
		}

		templateFilepathForgejo := fmt.Sprintf(".forgejo/default_merge_message/%s_TEMPLATE.md", strings.ToUpper(string(mergeStyle)))
		templateFilepathGitea := fmt.Sprintf(".gitea/default_merge_message/%s_TEMPLATE.md", strings.ToUpper(string(mergeStyle)))

		templateContent, err := commit.GetFileContent(templateFilepathForgejo, setting.Repository.PullRequest.DefaultMergeMessageSize)
		if _, ok := err.(git.ErrNotExist); ok {
			templateContent, err = commit.GetFileContent(templateFilepathGitea, setting.Repository.PullRequest.DefaultMergeMessageSize)
		}

		if _, ok := err.(git.ErrNotExist); ok {
			if preloadedContent, ok := mergeMessageTemplates[mergeStyle]; ok {
				templateContent, err = preloadedContent, nil
			}
		}

		if err != nil {
			if !git.IsErrNotExist(err) {
				return "", "", err
			}
		} else {
			vars := map[string]string{
				"BaseRepoOwnerName":      pr.BaseRepo.OwnerName,
				"BaseRepoName":           pr.BaseRepo.Name,
				"BaseBranch":             pr.BaseBranch,
				"HeadRepoOwnerName":      "",
				"HeadRepoName":           "",
				"HeadBranch":             pr.HeadBranch,
				"PullRequestTitle":       pr.Issue.Title,
				"PullRequestDescription": pr.Issue.Content,
				"PullRequestPosterName":  pr.Issue.Poster.Name,
				"PullRequestIndex":       strconv.FormatInt(pr.Index, 10),
				"PullRequestReference":   fmt.Sprintf("%s%d", issueReference, pr.Index),
				"ReviewedOn":             reviewedOn,
				"ReviewedBy":             reviewedBy,
			}
			if pr.HeadRepo != nil {
				vars["HeadRepoOwnerName"] = pr.HeadRepo.OwnerName
				vars["HeadRepoName"] = pr.HeadRepo.Name
			}
			maps.Copy(vars, extraVars)
			refs, err := pr.ResolveCrossReferences(ctx)
			if err == nil {
				closeIssueIndexes := make([]string, 0, len(refs))
				closeWord := "close"
				if len(setting.Repository.PullRequest.CloseKeywords) > 0 {
					closeWord = setting.Repository.PullRequest.CloseKeywords[0]
				}
				for _, ref := range refs {
					if ref.RefAction == references.XRefActionCloses {
						if err := ref.LoadIssue(ctx); err != nil {
							return "", "", err
						}
						closeIssueIndexes = append(closeIssueIndexes, fmt.Sprintf("%s %s%d", closeWord, issueReference, ref.Issue.Index))
					}
				}
				if len(closeIssueIndexes) > 0 {
					vars["ClosingIssues"] = strings.Join(closeIssueIndexes, ", ")
				} else {
					vars["ClosingIssues"] = ""
				}
			}
			return expandDefaultMergeMessage(templateContent, vars, message, body)
		}
	}

	if mergeStyle == repo_model.MergeStyleRebase {
		// for fast-forward rebase, do not amend the last commit if there is no template
		return "", "", nil
	}

	return message, body, nil
}

func expandDefaultMergeMessage(template string, vars map[string]string, message, body string) (finalMessage, finalBody string, err error) {
	if template == "" {
		return message, body, nil
	}
	mapping := func(s string) string { return vars[s] }
	if splits := strings.SplitN(template, "\n", 2); len(splits) == 2 {
		var templateTitle string
		var templateBody string
		if len(splits[0]) == 0 {
			templateTitle = message
		} else {
			templateTitle = os.Expand(strings.TrimSpace(splits[0]), mapping)
		}
		if len(splits[1]) == 0 {
			templateBody = body
		} else {
			templateBody = os.Expand(strings.TrimRightFunc(splits[1], unicode.IsSpace), mapping)
		}
		return templateTitle, templateBody, nil
	}
	return os.Expand(strings.TrimSpace(template), mapping), body, nil
}

// GetDefaultMergeMessage returns default message used when merging pull request
func GetDefaultMergeMessage(ctx context.Context, baseGitRepo *git.Repository, pr *issues_model.PullRequest, mergeStyle repo_model.MergeStyle) (message, body string, err error) {
	return getMergeMessage(ctx, baseGitRepo, pr, mergeStyle, nil)
}

// Merge merges pull request to base repository.
// Caller should check PR is ready to be merged (review and status checks)
func Merge(ctx context.Context, pr *issues_model.PullRequest, doer *user_model.User, baseGitRepo *git.Repository, mergeStyle repo_model.MergeStyle, expectedHeadCommitID, message string, wasAutoMerged bool) error {
	pullWorkingPool.CheckIn(fmt.Sprint(pr.ID))
	defer pullWorkingPool.CheckOut(fmt.Sprint(pr.ID))

	pr, err := issues_model.GetPullRequestByID(ctx, pr.ID)
	if err != nil {
		log.Error("Unable to load pull request itself: %v", err)
		return fmt.Errorf("unable to load pull request itself: %w", err)
	}

	if pr.HasMerged {
		return models.ErrPullRequestHasMerged{
			ID:         pr.ID,
			IssueID:    pr.IssueID,
			HeadRepoID: pr.HeadRepoID,
			BaseRepoID: pr.BaseRepoID,
			HeadBranch: pr.HeadBranch,
			BaseBranch: pr.BaseBranch,
		}
	}

	if err := pr.LoadIssue(ctx); err != nil {
		log.Error("Unable to load issue: %v", err)
		return fmt.Errorf("unable to load issue: %w", err)
	} else if err := pr.LoadBaseRepo(ctx); err != nil {
		log.Error("Unable to load base repo: %v", err)
		return fmt.Errorf("unable to load base repo: %w", err)
	} else if err := pr.LoadHeadRepo(ctx); err != nil {
		log.Error("Unable to load head repo: %v", err)
		return fmt.Errorf("unable to load head repo: %w", err)
	}

	prUnit, err := pr.BaseRepo.GetUnit(ctx, unit.TypePullRequests)
	if err != nil {
		log.Error("pr.BaseRepo.GetUnit(unit.TypePullRequests): %v", err)
		return err
	}
	prConfig := prUnit.PullRequestsConfig()

	// Check if merge style is correct and allowed
	if !prConfig.IsMergeStyleAllowed(mergeStyle) {
		return models.ErrInvalidMergeStyle{ID: pr.BaseRepo.ID, Style: mergeStyle}
	}

	defer func() {
		AddTestPullRequestTask(ctx, doer, pr.BaseRepo.ID, pr.BaseBranch, false, "", "", 0)
	}()

	_, err = doMergeAndPush(ctx, pr, doer, mergeStyle, expectedHeadCommitID, message, repo_module.PushTriggerPRMergeToBase)
	if err != nil {
		return err
	}

	// reload pull request because it has been updated by post receive hook
	pr, err = issues_model.GetPullRequestByID(ctx, pr.ID)
	if err != nil {
		return err
	}

	if err := pr.LoadIssue(ctx); err != nil {
		log.Error("LoadIssue %-v: %v", pr, err)
	}

	if err := pr.Issue.LoadRepo(ctx); err != nil {
		log.Error("pr.Issue.LoadRepo %-v: %v", pr, err)
	}
	if err := pr.Issue.Repo.LoadOwner(ctx); err != nil {
		log.Error("LoadOwner for %-v: %v", pr, err)
	}

	if wasAutoMerged {
		notify_service.AutoMergePullRequest(ctx, doer, pr)
	} else {
		notify_service.MergePullRequest(ctx, doer, pr)
	}

	// Reset cached commit count
	cache.Remove(pr.Issue.Repo.GetCommitsCountCacheKey(pr.BaseBranch, true))

	// Branch-chain pull requests based on this one move to its base branch (SPEC 20.5 flow 6, C46).
	if _, err := RetargetAscBranchChain(ctx, pr, doer); err != nil {
		log.Error("RetargetAscBranchChain %-v: %v", pr, err)
	}

	return handleCloseCrossReferences(ctx, pr, doer)
}

func handleCloseCrossReferences(ctx context.Context, pr *issues_model.PullRequest, doer *user_model.User) error {
	// Resolve cross references
	refs, err := pr.ResolveCrossReferences(ctx)
	if err != nil {
		log.Error("ResolveCrossReferences: %v", err)
		return nil
	}

	for _, ref := range refs {
		if err = ref.LoadIssue(ctx); err != nil {
			return err
		}
		if err = ref.Issue.LoadRepo(ctx); err != nil {
			return err
		}
		isClosed := ref.RefAction == references.XRefActionCloses
		if isClosed != ref.Issue.IsClosed {
			if err = issue_service.ChangeStatus(ctx, ref.Issue, doer, pr.MergedCommitID, isClosed); err != nil {
				// Allow ErrDependenciesLeft
				if !issues_model.IsErrDependenciesLeft(err) {
					return err
				}
			}
		}
	}
	return nil
}

// doMergeAndPush performs the merge operation without changing any pull information in database and pushes it up to the base repository
func doMergeAndPush(ctx context.Context, pr *issues_model.PullRequest, doer *user_model.User, mergeStyle repo_model.MergeStyle, expectedHeadCommitID, message string, pushTrigger repo_module.PushTrigger) (string, error) { //nolint:unparam
	writer, err := repostore.Default()
	if err != nil {
		return "", err
	}
	req, err := newMergeRequest(ctx, pr)
	if err != nil {
		return "", err
	}

	switch mergeStyle {
	case repo_model.MergeStyleMerge, repo_model.MergeStyleRebase, repo_model.MergeStyleRebaseMerge, repo_model.MergeStyleSquash, repo_model.MergeStyleFastForwardOnly:
		req.Style = repostore.MergeStyle(mergeStyle)
	default:
		return "", models.ErrInvalidMergeStyle{ID: pr.BaseRepo.ID, Style: mergeStyle}
	}

	if expectedHeadCommitID != "" {
		if req.ExpectHead, err = git.NewIDFromString(expectedHeadCommitID); err != nil {
			return "", models.ErrSHADoesNotMatch{GivenSHA: expectedHeadCommitID}
		}
	}

	sig := doer.NewGitSig()
	req.Meta = repostore.CommitMeta{Author: sig, Committer: sig, Message: message, Sign: mergeSigner(pr, doer)}

	req.SquashAuthor = func(ctx context.Context, authors []*git.Signature) (*git.Signature, error) {
		return getAuthorSignatureSquash(ctx, pr, authors)
	}
	req.AmendMessage = func(ctx context.Context, last string) (string, error) {
		// Original repo to read template from.
		baseGitRepo, err := gitrepo.OpenRepository(ctx, pr.BaseRepo)
		if err != nil {
			log.Error("Unable to get Git repo for rebase: %v", err)
			return "", err
		}
		defer baseGitRepo.Close()
		return getRebaseAmendMessage(ctx, baseGitRepo, pr, last)
	}

	// Now it's questionable about where this should go - either after or before the push
	// I think in the interests of data safety - failures to push to the lfs should prevent
	// the merge as you can always remerge.
	if pr.BaseRepo.LFSAllowed() && pr.HeadRepo.LFSAllowed() {
		req.BeforePublish = func(ctx context.Context, in repostore.PublishInput) error {
			return LFSPush(ctx, in.GitDir, in.Commit.String(), in.Base.String(), pr)
		}
	}

	var headUser *user_model.User
	err = pr.HeadRepo.LoadOwner(ctx)
	if err != nil {
		if !user_model.IsErrUserNotExist(err) {
			log.Error("Can't find user: %d for head repository in %-v: %v", pr.HeadRepo.OwnerID, pr, err)
			return "", err
		}
		log.Warn("Can't find user: %d for head repository in %-v - defaulting to doer: %s - %v", pr.HeadRepo.OwnerID, pr, doer.Name, err)
		headUser = doer
	} else {
		headUser = pr.HeadRepo.Owner
	}

	req.HookEnv = append(repo_module.FullPushingEnvironment(
		headUser,
		doer,
		pr.BaseRepo,
		pr.BaseRepo.Name,
		pr.ID,
	), repo_module.EnvPushTrigger+"="+string(pushTrigger))
	req.Publish = &repostore.RefUpdate{Repo: req.Base, Ref: req.BaseRef}

	// Merge, and push back to upstream.
	// This cause an api call to "/api/internal/hook/post-receive/...",
	// If it's merge, all db transaction and operations should be there but not here to prevent deadlock.
	result, err := writer.Merge(ctx, *req)
	if err != nil {
		return "", mapMergeError(err, pr)
	}
	if writtenThroughAsc(pr.BaseRepo) {
		// No git hook ran, and asc's own post-receive call does not carry the pull request: mark it merged here.
		if err := markMergedThroughAsc(ctx, pr, doer, result.Commit.String()); err != nil {
			return "", err
		}
	}
	return result.Commit.String(), nil
}

// getAuthorSignatureSquash gets a commit author signature for squash commits
func getAuthorSignatureSquash(ctx context.Context, pr *issues_model.PullRequest, authors []*git.Signature) (*git.Signature, error) {
	if err := pr.Issue.LoadPoster(ctx); err != nil {
		log.Error("%-v Issue[%d].LoadPoster: %v", pr, pr.Issue.ID, err)
		return nil, err
	}

	// Try to get an signature from the same user in one of the commits, as the
	// poster email might be private or commits might have a different signature
	// than the primary email address of the poster.
	for _, author := range authors {
		commitUser, _ := user_model.GetUserByEmail(ctx, author.Email)
		if commitUser != nil && commitUser.ID == pr.Issue.Poster.ID {
			return author, nil
		}
	}

	return pr.Issue.Poster.NewGitSig(), nil
}

// IsUserAllowedToMerge check if user is allowed to merge PR with given permissions and branch protections
func IsUserAllowedToMerge(ctx context.Context, pr *issues_model.PullRequest, p access_model.Permission, user *user_model.User) (bool, error) {
	if user == nil {
		return false, nil
	}

	pb, err := git_model.GetFirstMatchProtectedBranchRule(ctx, pr.BaseRepoID, pr.BaseBranch)
	if err != nil {
		return false, err
	}

	if (p.CanWrite(unit.TypeCode) && pb == nil) || (pb != nil && git_model.IsUserMergeWhitelisted(ctx, pb, user.ID, p)) {
		return true, nil
	}

	return false, nil
}

// CheckPullBranchProtections checks whether the PR is ready to be merged (reviews and status checks).
// Returns the protected branch rule when `ErrDisallowedToMerge` is returned as error.
func CheckPullBranchProtections(ctx context.Context, pr *issues_model.PullRequest, skipProtectedFilesCheck bool) (protectedBranchRule *git_model.ProtectedBranch, err error) {
	if err = pr.LoadBaseRepo(ctx); err != nil {
		return nil, fmt.Errorf("LoadBaseRepo: %w", err)
	}

	pb, err := git_model.GetFirstMatchProtectedBranchRule(ctx, pr.BaseRepoID, pr.BaseBranch)
	if err != nil {
		return nil, fmt.Errorf("LoadProtectedBranch: %v", err)
	}
	if pb == nil {
		return nil, nil
	}

	isPass, err := IsPullCommitStatusPass(ctx, pr)
	if err != nil {
		return nil, err
	}
	if !isPass {
		return pb, models.ErrDisallowedToMerge{
			Reason: "Not all required status checks successful",
		}
	}

	if !HasEnoughApprovals(ctx, pb, pr) {
		return pb, models.ErrDisallowedToMerge{
			Reason: "Does not have enough approvals",
		}
	}
	if issues_model.MergeBlockedByRejectedReview(ctx, pb, pr) {
		return pb, models.ErrDisallowedToMerge{
			Reason: "There are requested changes",
		}
	}
	if issues_model.MergeBlockedByOfficialReviewRequests(ctx, pb, pr) {
		return pb, models.ErrDisallowedToMerge{
			Reason: "There are official review requests",
		}
	}

	if issues_model.MergeBlockedByOutdatedBranch(pb, pr) {
		return pb, models.ErrDisallowedToMerge{
			Reason: "The head branch is behind the base branch",
		}
	}

	if skipProtectedFilesCheck {
		return nil, nil
	}

	if pb.MergeBlockedByProtectedFiles(pr.ChangedProtectedFiles) {
		return pb, models.ErrDisallowedToMerge{
			Reason: "Changed protected files",
		}
	}

	return nil, nil
}
