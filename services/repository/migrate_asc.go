// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/git"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/services/ascsync"
)

// How long a migration waits for the hub's own view of an asc repository to hold the imported default branch,
// and how often it looks.
var (
	ascImportVisibleWait = 30 * time.Second
	ascImportVisiblePoll = 250 * time.Millisecond
)

// isAscStored reports whether the migrated repository lives in asc. A bad storage value is an error, so a
// repository that is not plainly git is never imported through git alone.
func isAscStored(repo *repo_model.Repository) (bool, error) {
	return ascsync.BackedBy(repo)
}

// pruneNonBranchRefs deletes every ref of the bridge that is not a branch or a tag. A mirror clone of a forge
// brings the forge's own namespaces (refs/pull/N/head of GitHub, refs/merge-requests/ of GitLab); asc stores
// only branches and tags, and refs/pull/ belongs to the pull requests of this instance.
func pruneNonBranchRefs(ctx context.Context, repoPath string) error {
	out, _, err := git.NewCommand(ctx, "for-each-ref", "--format=%(refname)").RunStdString(&git.RunOpts{Dir: repoPath})
	if err != nil {
		return fmt.Errorf("list the refs of the clone: %w", err)
	}
	var stdin strings.Builder
	for _, name := range strings.Split(out, "\n") {
		name = strings.TrimSpace(name)
		if name == "" || strings.HasPrefix(name, git.BranchPrefix) || strings.HasPrefix(name, git.TagPrefix) {
			continue
		}
		stdin.WriteString("delete " + name + "\n")
	}
	if stdin.Len() == 0 {
		return nil
	}
	if _, _, err := git.NewCommand(ctx, "update-ref", "--stdin").RunStdString(&git.RunOpts{Dir: repoPath, Stdin: strings.NewReader(stdin.String())}); err != nil {
		return fmt.Errorf("drop the forge refs of the clone: %w", err)
	}
	return nil
}

// importMigratedIntoAsc hands the history that the migration cloned into the bridge directory of an asc
// repository to asc, and returns only when the hub can read the default branch back from asc. Until then the
// hub database must not learn of any branch or commit: the clone alone is not the repository.
//
// The import is a system action done for the importing user (ascsync.WithImporter) and goes through asc's own
// pipeline, so asc's path policy and limits apply; a refusal comes back as the reason of the failed task. It
// streams: asc reads the objects from the bridge directory, nothing is held in memory. It is idempotent: a run
// repeated over a repository that holds part of the history imports what is missing and moves nothing else.
func importMigratedIntoAsc(ctx context.Context, repo *repo_model.Repository, defaultBranch string) error {
	if err := pruneNonBranchRefs(ctx, repo.RepoPath()); err != nil {
		return err
	}
	refs, err := cloneRefs(ctx, repo.RepoPath())
	if err != nil {
		return err
	}
	if err := ascsync.ImportHistory(ascsync.WithBulkImport(ctx), repo, refs); err != nil {
		if msg, ok := ascsync.RejectMessage(err); ok {
			return fmt.Errorf("asc refused the imported history: %s", msg)
		}
		return fmt.Errorf("importing the history into asc: %w", err)
	}
	return waitAscHoldsBranch(ctx, repo, defaultBranch)
}

// cloneRefs lists the branches and tags of the clone with the object each points at.
func cloneRefs(ctx context.Context, repoPath string) ([]ascsync.PushRef, error) {
	out, _, err := git.NewCommand(ctx, "for-each-ref", "--format=%(objectname) %(refname)", git.BranchPrefix, git.TagPrefix).
		RunStdString(&git.RunOpts{Dir: repoPath})
	if err != nil {
		return nil, fmt.Errorf("list the refs of the clone: %w", err)
	}
	var refs []ascsync.PushRef
	for _, line := range strings.Split(out, "\n") {
		id, name, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}
		refs = append(refs, ascsync.PushRef{Name: name, New: id})
	}
	return refs, nil
}

// waitAscHoldsBranch waits until the hub, reading through the storage backend of the repository, resolves
// branch to the commit the clone holds and can load that commit.
func waitAscHoldsBranch(ctx context.Context, repo *repo_model.Repository, branch string) error {
	want, err := gitrepo.GetBranchCommitID(ctx, gitrepo.NamedRepository{Owner: repo.OwnerName, Name: repo.Name}, branch)
	if err != nil {
		return fmt.Errorf("read the cloned branch %q: %w", branch, err)
	}
	deadline := time.Now().Add(ascImportVisibleWait)
	for {
		err := ascHolds(ctx, repo, branch, want)
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return fmt.Errorf("asc does not hold the imported branch %q: %w", branch, err)
		}
		log.Debug("migration of %-v: waiting for asc to hold %s: %v", repo, branch, err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(ascImportVisiblePoll):
		}
	}
}

// ascHolds is the check of waitAscHoldsBranch; tests replace it through stubAscHolds.
var ascHolds = readBranchThroughBackend

// stubAscHolds replaces the check until the returned function is called. It is for tests.
func stubAscHolds(f func(ctx context.Context, repo *repo_model.Repository, branch, want string) error) (restore func()) {
	prev := ascHolds
	ascHolds = f
	return func() { ascHolds = prev }
}

// readBranchThroughBackend opens the repository the way every page of the hub does (through its storage backend,
// asc for an asc repository) and checks that branch is at want and that the commit loads.
func readBranchThroughBackend(ctx context.Context, repo *repo_model.Repository, branch, want string) error {
	gitRepo, err := gitrepo.OpenRepository(ctx, repo)
	if err != nil {
		return err
	}
	defer gitRepo.Close()
	got, err := gitRepo.GetBranchCommitID(branch)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("the branch is at %s, expected %s", got, want)
	}
	commit, err := gitRepo.GetCommit(got)
	if err != nil {
		return err
	}
	if commit == nil {
		return errors.New("the commit does not load")
	}
	return nil
}
