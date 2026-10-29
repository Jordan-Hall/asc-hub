// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/git"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	repo_module "forgejo.org/modules/repository"
	"forgejo.org/modules/repostore"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascsync"

	"xorm.io/builder"
)

// How long a migration waits for the hub's own view of an asc repository to hold the imported default branch, and
// how often it looks. asc has just taken a whole history and is still busy deriving from it (a repository of
// 160 MB made its first reads after the import take minutes), so the wait is long; the import itself is complete
// and a failure here only means the hub cannot read it yet.
var (
	ascImportVisibleWait = 10 * time.Minute
	ascImportVisiblePoll = time.Second
)

// ascImportTrunk is the default branch a repository stored in asc is made with for a migration. asc's gate protects
// the default branch (and the land branches, which start as the same name), so an import into a repository made
// with the real default branch would be held to the gate: a secret in the history would refuse the import. The
// placeholder is not a branch of any source; the real default branch is set once the history is in (setAscTrunk).
const ascImportTrunk = ascsync.ImportTrunkPlaceholder

// MigrationInitBranch is the default branch a repository made for a migration starts with: the placeholder for
// a repository that will be stored in asc, and none (the instance default) for any other.
func MigrationInitBranch(isMirror bool) string {
	if newRepoStorageBackend(CreateRepoOptions{IsMirror: isMirror, ObjectFormatName: git.Sha1ObjectFormat.Name()}) == string(gitrepo.StorageBackendAsc) {
		return ascImportTrunk
	}
	return ""
}

// isAscStored reports whether the migrated repository lives in asc. A bad storage value is an error, so a
// repository that is not plainly git is never imported through git alone.
func isAscStored(repo *repo_model.Repository) (bool, error) {
	return ascsync.BackedBy(repo)
}

// setAscTrunk tells asc which branch is the default branch of a repository stored in asc. asc protects that branch
// (path rules, secret scanning, review) and no other, and it learns the name only when the repository is made or
// here, so every change of the default branch in the hub, and every import, has to come through here. It does
// nothing for a repository that is not stored in asc.
func setAscTrunk(ctx context.Context, repo *repo_model.Repository, branch string) error {
	stored, err := isAscStored(repo)
	if err != nil || !stored {
		return err
	}
	if err := ascsync.SetDefaultBranch(ctx, repo, branch); err != nil {
		return fmt.Errorf("asc does not take %q as the default branch: %w", branch, err)
	}
	if t, ok := ctx.Value(trunkTrackerKey{}).(*trunkTracker); ok {
		t.set = true
	}
	return nil
}

// trunkTracker records that a migration made the real default branch the trunk of its asc repository.
type trunkTracker struct{ set bool }

type trunkTrackerKey struct{}

// withTrunkTracker returns a context whose setAscTrunk calls are recorded in the returned tracker.
func withTrunkTracker(ctx context.Context) (context.Context, *trunkTracker) {
	t := &trunkTracker{}
	return context.WithValue(ctx, trunkTrackerKey{}, t), t
}

// settleEmptyAscMigration gives a migrated repository stored in asc that has no history its real default branch (the
// one the source names, or the instance's) and makes it the trunk, before anything else can happen to it.
func settleEmptyAscMigration(ctx context.Context, repo *repo_model.Repository) error {
	stored, err := isAscStored(repo)
	if err != nil || !stored {
		return err
	}
	if repo.DefaultBranch == "" || repo.DefaultBranch == ascImportTrunk {
		repo.DefaultBranch = setting.Repository.DefaultBranch
	}
	return setAscTrunk(ctx, repo, repo.DefaultBranch)
}

// settleAscTrunk makes sure that a repository made for a migration does not keep the placeholder trunk, whatever
// way the migration ends: it is run when the migration ends (success, an empty source, a failure, a cancel), and
// does nothing once the migration has made the real trunk. The placeholder is an ordinary branch name to asc, so a
// repository that keeps it has no gate on the branch people push to. The default branch of the repository is the
// trunk; when it is unknown or still the placeholder, the instance's default branch is.
func settleAscTrunk(ctx context.Context, repo *repo_model.Repository, t *trunkTracker) {
	stored, err := isAscStored(repo)
	if err != nil || !stored || t == nil || t.set {
		return
	}
	branch := repo.DefaultBranch
	if branch == "" || branch == ascImportTrunk {
		branch = setting.Repository.DefaultBranch
		repo.DefaultBranch = branch
	}
	// The migration may have ended by a cancel: this call must not.
	if err := ascsync.SetDefaultBranch(context.WithoutCancel(ctx), repo, branch); err != nil {
		log.Error("migration of %-v: asc keeps the placeholder %q as the default branch (the repository has no gate on %q until it is repaired: forgejo doctor check --run asc-default-branch --fix): %v", repo, ascImportTrunk, branch, err)
		return
	}
	t.set = true
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

// syncBranchesOfMigration writes the branches of the clone to the database. For a repository stored in asc the
// import made asc fan out the pushed refs, and the hub's own handling of that writes the same branch rows at the
// same time: a unique-constraint failure then only means the row is there, so the sync is repeated, which finds
// the row and updates it.
func syncBranchesOfMigration(ctx context.Context, repo *repo_model.Repository, gitRepo *git.Repository, doerID int64, ascStored bool) (int64, error) {
	for attempt := 0; ; attempt++ {
		n, err := repo_module.SyncRepoBranchesWithRepo(ctx, repo, gitRepo, doerID)
		if err == nil || !ascStored || attempt >= 4 || !strings.Contains(strings.ToLower(err.Error()), "unique constraint") {
			return n, err
		}
		select {
		case <-ctx.Done():
			return n, ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 200 * time.Millisecond):
		}
	}
}

// scrubBridgeConfig removes the [http] section that the clone left in the config of the bridge directory
// (git clone -c http.followRedirects=false and -c http.sslVerify=false write the options into the new repository).
// asc serves the directory only while its config holds what git init and git clone --mirror write, and refuses
// to promote refs into it otherwise ("the repository config has [http]"): without this, nothing asc writes
// would ever reach the bridge again.
func scrubBridgeConfig(ctx context.Context, repoPath string) error {
	_, stderr, err := git.NewCommand(ctx, "config", "--local", "--remove-section", "http").RunStdString(&git.RunOpts{Dir: repoPath})
	if err != nil && !strings.Contains(stderr, "no such section") && !strings.Contains(strings.ToLower(stderr), "no such section") {
		return fmt.Errorf("clean the config of the clone: %w", err)
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
	if err := ensureAscRepo(ctx, repo, defaultBranch); err != nil {
		return err
	}
	if err := scrubBridgeConfig(ctx, repo.RepoPath()); err != nil {
		return err
	}
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
	if err := waitAscHoldsBranch(ctx, repo, defaultBranch); err != nil {
		return err
	}
	// The import skipped asc's gate (a history is not a push); this is where the history is looked at. It never
	// blocks, and what it finds shows on the secret scanning page as found in imported history.
	var branches []string
	for _, r := range refs {
		if name, ok := strings.CutPrefix(r.Name, git.BranchPrefix); ok {
			branches = append(branches, name)
		}
	}
	if n, err := ascsync.ScanImportedHistory(ctx, repo, defaultBranch, branches); err != nil {
		log.Warn("migration of %-v: the secret scan of the imported history: %v", repo, err)
	} else if n > 0 {
		log.Info("migration of %-v: %d secrets found in the imported history", repo, n)
	}
	// Only now is the default branch the trunk asc protects: until here the import went in as history, and from
	// here every push is judged by the policy, secret scan included.
	return setAscTrunk(ctx, repo, defaultBranch)
}

// ensureAscRepo is ensureAscRepository; tests replace it.
var ensureAscRepo = ensureAscRepository

// ensureAscRepository makes sure asc has the repository. A first migration made it when the repository was created;
// a retry deleted it from asc together with the files (the task is kept, the repository is made again), so asc
// has none and the import would fail with "no such repository".
func ensureAscRepository(ctx context.Context, repo *repo_model.Repository, _ string) error {
	writer, err := repostore.Default()
	if err != nil {
		return err
	}
	ref := repo.StoreRef()
	ref.ObjectFormat = repo.ObjectFormatName
	if ref.ObjectFormat == "" {
		ref.ObjectFormat = git.Sha1ObjectFormat.Name()
	}
	err = writer.Init(ctx, repostore.InitRequest{Repo: ref, DefaultBranch: ascImportTrunk})
	if err != nil && !errors.Is(err, repostore.ErrExists) {
		return fmt.Errorf("making the repository in asc: %w", err)
	}
	return nil
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

// AscTrunkWant is the branch asc should protect in a repository stored in asc: its default branch, or the instance's
// when the repository has none (or still has the migration placeholder, which is no branch of anyone).
func AscTrunkWant(repo *repo_model.Repository) string {
	if repo.DefaultBranch == "" || repo.DefaultBranch == ascImportTrunk {
		return setting.Repository.DefaultBranch
	}
	return repo.DefaultBranch
}

// AscTrunkCheck compares the trunk asc reports for a ready repository stored in asc with the branch it should be. ok
// is false for a repository the check does not apply to (not in asc, still being made); trunk is "" when asc does not
// report one. Empty repositories are included: an empty repository that kept the migration placeholder, or whose
// default branch never reached asc, has no gate on the first branch pushed to it.
func AscTrunkCheck(ctx context.Context, repo *repo_model.Repository) (trunk, want string, ok bool, err error) {
	stored, err := isAscStored(repo)
	if err != nil || !stored || repo.Status != repo_model.RepositoryReady {
		return "", "", false, err
	}
	want = AscTrunkWant(repo)
	trunk, err = ascsync.TrunkOf(ctx, repo)
	return trunk, want, true, err
}

// RepairAscTrunks is run when the server starts. A repository whose asc trunk is the migration placeholder, or is not
// its default branch, has no gate on the branch people push to; the ready ones (empty ones too) are told their default
// branch, and only the ones whose trunk asc reports as something else are touched. An asc that does not report its
// trunk gives nothing to compare: forgejo doctor check --run asc-default-branch --fix does it for every repository then.
func RepairAscTrunks(ctx context.Context) {
	fixed := 0
	err := db.Iterate(ctx, builder.Gt{"id": 0}, func(ctx context.Context, repo *repo_model.Repository) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		trunk, want, ok, err := AscTrunkCheck(ctx, repo)
		if !ok || err != nil || trunk == "" || trunk == want {
			return nil
		}
		if err := ascsync.SetDefaultBranch(ctx, repo, want); err != nil {
			log.Error("asc protects %q in %-v, whose default branch is %q, and did not take it: %v", trunk, repo, want, err)
			return nil
		}
		log.Warn("asc protected %q in %-v: it is %q now", trunk, repo, want)
		fixed++
		return nil
	})
	if err != nil {
		log.Error("checking the default branches asc protects: %v", err)
	}
	if fixed > 0 {
		log.Info("%d asc repositories were told their default branch", fixed)
	}
}
