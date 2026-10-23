// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"context"
	"time"

	git_model "forgejo.org/models/git"
	issues_model "forgejo.org/models/issues"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git"
	"forgejo.org/modules/git/ascbackend"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/translation"
)

// Relaxed approvals by change class (K17, SPEC 8.10).
//
// A branch protection rule with asc_relaxed_classes lets a pull request that consists only of those
// classes (docs, lockfiles, ...) merge with asc_relaxed_approvals approvals instead of
// required_approvals. The relaxation can only add a way to pass: every doubt, error or missing
// piece of information leaves the normal requirement in force.
//
//   - It applies only to a pull request whose base repository has storage_backend=asc and while
//     [asc] ENABLED is true. Classification comes from asc, which reads its class globs from the merge
//     base, so the pull request cannot change the rules that classify it.
//   - It never raises the requirement: it is ignored unless asc_relaxed_approvals is below
//     required_approvals.
//   - A pull request that touches a protected file (pattern match on any classified path, or the
//     changed protected files Forgejo recorded) is never relaxed.
//   - An empty change, a Classify error or timeout, an unverified answer or any class outside the
//     rule's list (logic is never listable) keeps the normal count.

// ascClassifyTimeout bounds one Classify call made while checking approvals.
const ascClassifyTimeout = 15 * time.Second

// ascClassify classifies the changes of the pull request's base repository. Tests replace it.
var ascClassify = func(ctx context.Context, repoPath, base, head string) (*ascbackend.Classification, error) {
	return ascbackend.ClassifyRepository(ctx, repoPath, base, head, true)
}

// HasEnoughApprovals reports whether pr has the approvals its protected branch rule asks for,
// taking the asc relaxation into account. It is issues_model.HasEnoughApprovals plus one more way to pass.
func HasEnoughApprovals(ctx context.Context, pb *git_model.ProtectedBranch, pr *issues_model.PullRequest) bool {
	if issues_model.HasEnoughApprovals(ctx, pb, pr) {
		return true
	}
	return ascRelaxedApprovalsMet(ctx, pb, pr)
}

// ascRelaxedApprovalsMet is true when the rule relaxes the requirement for what pr changes and pr has
// the relaxed number of approvals.
func ascRelaxedApprovalsMet(ctx context.Context, pb *git_model.ProtectedBranch, pr *issues_model.PullRequest) bool {
	classes := pb.AscRelaxedClassSet()
	relaxed := pb.AscRelaxedApprovals
	if len(classes) == 0 || relaxed < 0 || relaxed >= pb.RequiredApprovals || !setting.Asc.Enabled {
		return false
	}
	// Cheap checks first: do not call asc for a pull request that lacks even the relaxed count.
	if issues_model.GetGrantedApprovalsCount(ctx, pb, pr) < relaxed {
		return false
	}
	if len(pr.ChangedProtectedFiles) > 0 {
		return false
	}
	cl := classifyAscPullRequest(ctx, pr)
	if cl == nil {
		return false
	}
	return ascChangesAreRelaxed(pb, classes, cl)
}

// classifyAscPullRequest returns asc's verified classification of the pull request, or nil when the
// pull request is not on an asc repository or asc cannot answer.
func classifyAscPullRequest(ctx context.Context, pr *issues_model.PullRequest) *ascbackend.Classification {
	if err := pr.LoadBaseRepo(ctx); err != nil || pr.BaseRepo == nil {
		return nil
	}
	if sb, err := gitrepo.StorageBackendOf(pr.BaseRepo); err != nil || sb != gitrepo.StorageBackendAsc {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, ascClassifyTimeout)
	defer cancel()
	cl, err := ascClassify(ctx, pr.BaseRepo.RepoPath(), git.BranchPrefix+pr.BaseBranch, pr.GetGitRefName())
	if err != nil {
		log.Warn("asc Classify for pull request %d failed, the normal approval count applies: %v", pr.ID, err)
		return nil
	}
	return cl
}

// ascChangesAreRelaxed reports whether every file of cl is in a relaxed class and none is protected.
func ascChangesAreRelaxed(pb *git_model.ProtectedBranch, classes map[string]struct{}, cl *ascbackend.Classification) bool {
	if len(cl.Files) == 0 {
		return false
	}
	if _, ok := classes[ascClassName(cl.Overall)]; !ok {
		return false
	}
	patterns := pb.GetProtectedFilePatterns()
	for _, f := range cl.Files {
		if _, ok := classes[ascClassName(f.Class)]; !ok {
			return false
		}
		if len(patterns) > 0 && pb.IsProtectedFile(patterns, f.Path) {
			return false
		}
	}
	return true
}

// ascClassName is the branch protection name of an asc change class. A value it does not know is
// "logic", the class that is never relaxed.
func ascClassName(c ascv1.ChangeClass) string {
	switch c {
	case ascv1.ChangeClass_CHANGE_CLASS_RENAME_ONLY:
		return git_model.AscClassRenameOnly
	case ascv1.ChangeClass_CHANGE_CLASS_MODE_ONLY:
		return git_model.AscClassModeOnly
	case ascv1.ChangeClass_CHANGE_CLASS_WHITESPACE_ONLY:
		return git_model.AscClassWhitespaceOnly
	case ascv1.ChangeClass_CHANGE_CLASS_COMMENT_ONLY:
		return git_model.AscClassCommentOnly
	case ascv1.ChangeClass_CHANGE_CLASS_DOCS:
		return git_model.AscClassDocs
	case ascv1.ChangeClass_CHANGE_CLASS_GENERATED:
		return git_model.AscClassGenerated
	case ascv1.ChangeClass_CHANGE_CLASS_LOCKFILE:
		return git_model.AscClassLockfile
	case ascv1.ChangeClass_CHANGE_CLASS_TEST:
		return git_model.AscClassTest
	}
	return "logic"
}

// AscFileClassBadges returns the badge label of each file the pull request changes, keyed by path,
// for the "files changed" page. It is nil (no badges) for a pull request that is not on an asc
// repository, when asc is off, or when asc cannot answer.
func AscFileClassBadges(ctx context.Context, locale translation.Locale, pr *issues_model.PullRequest) map[string]string {
	if !setting.Asc.Enabled || pr.HasMerged {
		return nil
	}
	cl := classifyAscPullRequest(ctx, pr)
	if cl == nil {
		return nil
	}
	labels := make(map[string]string, len(cl.Files))
	for _, f := range cl.Files {
		labels[f.Path] = ascClassLabel(locale, ascClassName(f.Class))
	}
	return labels
}

func ascClassLabel(locale translation.Locale, name string) string {
	switch name {
	case git_model.AscClassRenameOnly:
		return locale.TrString("repo.diff.asc_class.rename_only")
	case git_model.AscClassModeOnly:
		return locale.TrString("repo.diff.asc_class.mode_only")
	case git_model.AscClassWhitespaceOnly:
		return locale.TrString("repo.diff.asc_class.whitespace_only")
	case git_model.AscClassCommentOnly:
		return locale.TrString("repo.diff.asc_class.comment_only")
	case git_model.AscClassDocs:
		return locale.TrString("repo.diff.asc_class.docs")
	case git_model.AscClassGenerated:
		return locale.TrString("repo.diff.asc_class.generated")
	case git_model.AscClassLockfile:
		return locale.TrString("repo.diff.asc_class.lockfile")
	case git_model.AscClassTest:
		return locale.TrString("repo.diff.asc_class.test")
	}
	return locale.TrString("repo.diff.asc_class.logic")
}
