// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	api "forgejo.org/modules/structs"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// Branch protection of an asc repository is the asc policy (services/asctrust/protection.go has the mapping and its
// limits). For such a repository these handlers read the protection from `.asc/policy.toml` and write it by
// proposing an edit of that file as a change under review: the answer is 202 with the change and its pull request,
// and nothing is stored in the protected_branch table. A repository on git keeps the behaviour of Forgejo.

// ascProtectionRepo reports whether repo takes its branch protection from the asc policy.
func ascProtectionRepo(repo *repo_model.Repository) bool {
	return setting.Asc.Enabled && repo.GetStorageBackend() == "asc"
}

// ascProtectionResult is the 202 answer of a write: the protection as proposed and where to review it.
type ascProtectionResult struct {
	Message     string                `json:"message"`
	Change      string                `json:"change,omitempty"`
	PullRequest string                `json:"pull_request_url,omitempty"`
	Protection  *api.BranchProtection `json:"protection"`
}

func ascProtectionToAPI(p asctrust.BranchRule) *api.BranchProtection {
	return &api.BranchProtection{
		BranchName: p.Branch, RuleName: p.Branch,
		EnablePush:            !p.QueueOnly,
		EnableStatusCheck:     len(p.StatusChecks) > 0,
		StatusCheckContexts:   append([]string{}, p.StatusChecks...),
		RequiredApprovals:     p.RequiredApprovals,
		RequireSignedCommits:  p.RequireSigned,
		ProtectedFilePatterns: strings.Join(p.ProtectedPaths, ";"),
	}
}

// ascProtectionRead answers GET (one rule) and the list for an asc repository.
func ascProtectionRead(ctx *context.APIContext, name string, list bool) {
	repo := ctx.Repo().Repository
	if !asctrust.HasFeature(ctx, asctrust.FeaturePolicy) {
		ctx.Error(http.StatusNotImplemented, "asc policy", "this server does not have the policy API")
		return
	}
	conn, err := asctrust.Conn()
	if err != nil {
		ctx.Error(http.StatusBadGateway, "asc policy", "the asc server is not reachable")
		return
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer(), asctrust.CallRepoRead, repo)
	if err != nil {
		ctx.Error(http.StatusForbidden, "asc policy", "no access to the policy")
		return
	}
	defer release()
	rn, err := asctrust.RepoName(repo)
	if err != nil {
		ctx.Error(http.StatusInternalServerError, "asc policy", err)
		return
	}
	page, err := asctrust.LoadPolicyPage(callCtx, conn, rn)
	if err != nil {
		log.Error("asc protection read of %s: %v", repo.FullName(), err)
		ctx.Error(http.StatusBadGateway, "asc policy", "the policy could not be read")
		return
	}
	if !page.View.Present {
		if list {
			ctx.JSON(http.StatusOK, []*api.BranchProtection{})
			return
		}
		ctx.NotFound()
		return
	}
	p := asctrust.ProtectionOfText(page.View.Text, repo.DefaultBranch)
	if !list && name != "" && name != repo.DefaultBranch {
		ctx.NotFound()
		return
	}
	if list {
		ctx.JSON(http.StatusOK, []*api.BranchProtection{ascProtectionToAPI(p)})
		return
	}
	ctx.JSON(http.StatusOK, ascProtectionToAPI(p))
}

// ascProtectionPropose applies change to the protection in force and proposes the edited policy. change gets the
// protection now in force and returns the one wanted, or an error naming what asc cannot express.
func ascProtectionPropose(ctx *context.APIContext, change func(cur asctrust.BranchRule) (asctrust.BranchRule, error), message string) {
	repo := ctx.Repo().Repository
	if !asctrust.HasFeature(ctx, asctrust.FeaturePolicy) {
		ctx.Error(http.StatusNotImplemented, "asc policy", "this server does not have the policy API")
		return
	}
	conn, err := asctrust.Conn()
	if err != nil {
		ctx.Error(http.StatusBadGateway, "asc policy", "the asc server is not reachable")
		return
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer(), asctrust.CallRepoWrite, repo)
	if err != nil {
		ctx.Error(http.StatusForbidden, "asc policy", "no access to propose a policy change")
		return
	}
	defer release()
	rn, err := asctrust.RepoName(repo)
	if err != nil {
		ctx.Error(http.StatusInternalServerError, "asc policy", err)
		return
	}
	page, err := asctrust.LoadPolicyPage(callCtx, conn, rn)
	if err != nil {
		log.Error("asc protection of %s: %v", repo.FullName(), err)
		ctx.Error(http.StatusBadGateway, "asc policy", "the policy could not be read")
		return
	}
	text := page.View.Text
	if !page.View.Present {
		text = asctrust.StarterPolicy(ctx.Doer().Name)
	}
	want, err := change(asctrust.ProtectionOfText(text, repo.DefaultBranch))
	if err != nil {
		code := http.StatusUnprocessableEntity
		ctx.Error(code, "asc protection", err.Error())
		return
	}
	next, err := asctrust.ApplyProtection(text, want)
	if err != nil {
		ctx.Error(http.StatusUnprocessableEntity, "asc protection", err.Error())
		return
	}
	if strings.TrimSpace(next) == strings.TrimSpace(text) {
		ctx.JSON(http.StatusOK, &ascProtectionResult{Message: "nothing changed: the policy already says this", Protection: ascProtectionToAPI(want)})
		return
	}
	prop, err := asctrust.ProposePolicy(callCtx, conn, rn, page.Base, next, message, asctrust.NewOpID())
	if err != nil {
		log.Error("asc protection proposal of %s: %v", repo.FullName(), err)
		ctx.Error(http.StatusUnprocessableEntity, "asc policy", "asc refused the proposed policy: "+firstSentence(err))
		return
	}
	res := &ascProtectionResult{
		Message:    fmt.Sprintf("the protection is a proposed change to .asc/policy.toml (change %s); it takes effect when it lands", prop.Short),
		Change:     prop.Short,
		Protection: ascProtectionToAPI(want),
	}
	pr, err := asctrust.OpenProposalPull(ctx, conn, repo, rn, ctx.Doer(), prop, message, asctrust.DefaultPullSeams)
	switch {
	case err == nil:
		pr.LoadIssue(ctx) //nolint:errcheck // the link is optional
		if pr.Issue != nil {
			res.PullRequest = setting.AppURL + strings.TrimPrefix(pr.Issue.Link(), setting.AppSubURL+"/")
		}
	case !errors.Is(err, asctrust.ErrNotMirrored):
		log.Error("asc protection: pull request of change %s: %v", prop.Short, err)
	}
	ctx.JSON(http.StatusAccepted, res)
}

func firstSentence(err error) string {
	s := err.Error()
	if i := strings.IndexAny(s, "\n"); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// ascProtectionFromCreate maps the options of a create call; the force push setting is not part of the options, so
// the policy keeps what it says.
func ascProtectionFromCreate(form *api.CreateBranchProtectionOption, ruleName string, cur asctrust.BranchRule) (asctrust.BranchRule, error) {
	return asctrust.ProtectionFromRequest(asctrust.ProtectionRequest{
		RuleName: ruleName, EnablePush: form.EnablePush,
		PushWhitelist:      form.EnablePushWhitelist || len(form.PushWhitelistUsernames) > 0 || len(form.PushWhitelistTeams) > 0 || form.PushWhitelistDeployKeys,
		MergeWhitelist:     form.EnableMergeWhitelist || len(form.MergeWhitelistUsernames) > 0 || len(form.MergeWhitelistTeams) > 0,
		ApprovalsWhitelist: form.EnableApprovalsWhitelist || len(form.ApprovalsWhitelistUsernames) > 0 || len(form.ApprovalsWhitelistTeams) > 0,
		EnableStatusCheck:  form.EnableStatusCheck, StatusCheckContexts: form.StatusCheckContexts,
		RequiredApprovals:      form.RequiredApprovals,
		BlockOnRejectedReviews: form.BlockOnRejectedReviews, BlockOnOfficialReviews: form.BlockOnOfficialReviewRequests,
		BlockOnOutdatedBranch: form.BlockOnOutdatedBranch, DismissStaleApprovals: form.DismissStaleApprovals,
		RequireSignedCommits: form.RequireSignedCommits, ProtectedFilePatterns: form.ProtectedFilePatterns,
		UnprotectedFilePatterns: form.UnprotectedFilePatterns, EnableForcePush: cur.AllowForcePush,
	}, cur.Branch)
}

// ascProtectionFromEdit overlays the set fields of an edit call on the protection in force.
func ascProtectionFromEdit(form *api.EditBranchProtectionOption, cur asctrust.BranchRule) (asctrust.BranchRule, error) {
	r := asctrust.ProtectionRequest{
		RuleName: cur.Branch, EnablePush: !cur.QueueOnly, EnableStatusCheck: len(cur.StatusChecks) > 0,
		StatusCheckContexts: cur.StatusChecks, RequiredApprovals: cur.RequiredApprovals,
		RequireSignedCommits: cur.RequireSigned, ProtectedFilePatterns: strings.Join(cur.ProtectedPaths, ";"),
		EnableForcePush: cur.AllowForcePush,
	}
	setBool := func(dst *bool, v *bool) {
		if v != nil {
			*dst = *v
		}
	}
	setBool(&r.EnablePush, form.EnablePush)
	setBool(&r.EnableStatusCheck, form.EnableStatusCheck)
	if form.StatusCheckContexts != nil {
		r.StatusCheckContexts = form.StatusCheckContexts
	}
	if form.RequiredApprovals != nil {
		r.RequiredApprovals = *form.RequiredApprovals
	}
	setBool(&r.RequireSignedCommits, form.RequireSignedCommits)
	if form.ProtectedFilePatterns != nil {
		r.ProtectedFilePatterns = *form.ProtectedFilePatterns
	}
	r.PushWhitelist = (form.EnablePushWhitelist != nil && *form.EnablePushWhitelist) || len(form.PushWhitelistUsernames) > 0 || len(form.PushWhitelistTeams) > 0 || (form.PushWhitelistDeployKeys != nil && *form.PushWhitelistDeployKeys)
	r.MergeWhitelist = (form.EnableMergeWhitelist != nil && *form.EnableMergeWhitelist) || len(form.MergeWhitelistUsernames) > 0 || len(form.MergeWhitelistTeams) > 0
	r.ApprovalsWhitelist = (form.EnableApprovalsWhitelist != nil && *form.EnableApprovalsWhitelist) || len(form.ApprovalsWhitelistUsernames) > 0 || len(form.ApprovalsWhitelistTeams) > 0
	isSet := func(v *bool) bool { return v != nil && *v }
	r.BlockOnRejectedReviews = isSet(form.BlockOnRejectedReviews)
	r.BlockOnOfficialReviews = isSet(form.BlockOnOfficialReviewRequests)
	r.BlockOnOutdatedBranch = isSet(form.BlockOnOutdatedBranch)
	r.DismissStaleApprovals = isSet(form.DismissStaleApprovals)
	if form.UnprotectedFilePatterns != nil {
		r.UnprotectedFilePatterns = *form.UnprotectedFilePatterns
	}
	return asctrust.ProtectionFromRequest(r, cur.Branch)
}

// ascProtectionFromDelete is the protection with nothing required: removing the rule lifts the keys of the mapping.
func ascProtectionFromDelete(cur asctrust.BranchRule) (asctrust.BranchRule, error) {
	return asctrust.BranchRule{Branch: cur.Branch, AllowForcePush: cur.AllowForcePush}, nil
}
