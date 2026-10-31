// Copyright 2023 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repository

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/options"
	repo_module "forgejo.org/modules/repository"
	"forgejo.org/modules/repostore"
	"forgejo.org/modules/setting"
	api "forgejo.org/modules/structs"
	"forgejo.org/modules/templates/vars"
	"forgejo.org/modules/util"
)

// CreateRepoOptions contains the create repository options
type CreateRepoOptions struct {
	Name             string
	Description      string
	OriginalURL      string
	GitServiceType   api.GitServiceType
	Gitignores       string
	IssueLabels      string
	License          string
	Readme           string
	DefaultBranch    string
	IsPrivate        bool
	IsInternal       bool // private to the public and open to every signed-in person; needs IsPrivate
	IsMirror         bool
	IsTemplate       bool
	AutoInit         bool
	Status           repo_model.RepositoryStatus
	TrustModel       repo_model.TrustModelType
	MirrorInterval   string
	ObjectFormatName string
	Website          string

	// AgentsFile and PolicyFile ask for AGENTS.md and .asc/policy.toml in the first commit (the policy file only where asc
	// keeps the repository). PolicyMaintainer is the login that maintains the repository, PolicyApprovals and PolicyChecks
	// are what landing needs; see asc_init_files.go. Either makes a first commit even without AutoInit.
	AgentsFile       bool
	PolicyFile       bool
	PolicyMaintainer string
	PolicyApprovals  int
	PolicyChecks     []string
}

// repoInitFiles returns the files of the first commit of a repository: the README, the .gitignore and the license.
func repoInitFiles(repo *repo_model.Repository, opts CreateRepoOptions) ([]repostore.FileOp, error) {
	extra, err := ascExtraFiles(repo, opts)
	if err != nil {
		return nil, err
	}
	if !opts.AutoInit {
		return extra, nil
	}
	ops, err := baseInitFiles(repo, opts)
	if err != nil {
		return nil, err
	}
	return append(ops, extra...), nil
}

// baseInitFiles are the README, the .gitignore and the license.
func baseInitFiles(repo *repo_model.Repository, opts CreateRepoOptions) ([]repostore.FileOp, error) {
	authorSig := repo.Owner.NewGitSig()
	var ops []repostore.FileOp

	// README
	data, err := options.Readme(opts.Readme)
	if err != nil {
		return nil, fmt.Errorf("GetRepoInitFile[%s]: %w", opts.Readme, err)
	}

	cloneLink := repo.CloneLink()
	match := map[string]string{
		"Name":           repo.Name,
		"Description":    repo.Description,
		"CloneURL.SSH":   cloneLink.SSH,
		"CloneURL.HTTPS": cloneLink.HTTPS,
		"OwnerName":      repo.OwnerName,
	}
	res, err := vars.Expand(string(data), match)
	if err != nil {
		// here we could just log the error and continue the rendering
		log.Error("unable to expand template vars for repo README: %s, err: %v", opts.Readme, err)
	}
	ops = append(ops, repostore.FileOp{Kind: repostore.OpPut, Path: "README.md", Content: strings.NewReader(res)})

	// .gitignore
	if len(opts.Gitignores) > 0 {
		var buf bytes.Buffer
		names := strings.SplitSeq(opts.Gitignores, ",")
		for name := range names {
			data, err = options.Gitignore(name)
			if err != nil {
				return nil, fmt.Errorf("GetRepoInitFile[%s]: %w", name, err)
			}
			buf.WriteString("# ---> " + name + "\n")
			buf.Write(data)
			buf.WriteString("\n")
		}

		if buf.Len() > 0 {
			ops = append(ops, repostore.FileOp{Kind: repostore.OpPut, Path: ".gitignore", Content: &buf})
		}
	}

	// LICENSE
	if len(opts.License) > 0 {
		data, err = repo_module.GetLicense(opts.License, &repo_module.LicenseValues{
			Owner: repo.OwnerName,
			Email: authorSig.Email,
			Repo:  repo.Name,
			Year:  time.Now().Format("2006"),
		})
		if err != nil {
			return nil, fmt.Errorf("getLicense[%s]: %w", opts.License, err)
		}

		ops = append(ops, repostore.FileOp{Kind: repostore.OpPut, Path: "LICENSE", Content: bytes.NewReader(data)})
	}

	return ops, nil
}

// initRepository makes the store of a new repository (the git directory, or the repository in asc) and fills it as asked.
// created says whether this call made the store: when it did not (the store exists already, which is somebody else's
// repository, or it could not be made) nothing at its path or in asc is this call's to remove.
func initRepository(ctx context.Context, repoPath string, u *user_model.User, repo *repo_model.Repository, opts CreateRepoOptions) (created bool, err error) {
	if err = repo_module.CheckInitRepositoryFor(ctx, repo, opts.ObjectFormatName); err != nil {
		return false, err
	}
	return true, fillRepository(ctx, repoPath, u, repo, opts)
}

// fillRepository initializes README and .gitignore if needed, in the store initRepository made.
func fillRepository(ctx context.Context, repoPath string, u *user_model.User, repo *repo_model.Repository, opts CreateRepoOptions) (err error) {

	// Initialize repository according to user's choice.
	committed := false
	if opts.wantsFirstCommit() {
		ops, err := repoInitFiles(repo, opts)
		if err != nil {
			return fmt.Errorf("prepareRepoCommit: %w", err)
		}

		// Apply changes and commit. A request for the policy file alone, in a repository asc does not keep, has no file.
		if len(ops) > 0 {
			if err = initRepoCommit(ctx, repo, u, opts.DefaultBranch, ops); err != nil {
				return fmt.Errorf("initRepoCommit: %w", err)
			}
			committed = true
		}
	}

	// Re-fetch the repository from database before updating it (else it would
	// override changes that were done earlier with sql)
	if repo, err = repo_model.GetRepositoryByID(ctx, repo.ID); err != nil {
		return fmt.Errorf("getRepositoryByID: %w", err)
	}

	if !committed {
		repo.IsEmpty = true
	}

	repo.DefaultBranch = setting.Repository.DefaultBranch
	repo.WikiBranch = setting.Repository.DefaultBranch

	if len(opts.DefaultBranch) > 0 {
		repo.DefaultBranch = opts.DefaultBranch
		if err = gitrepo.SetDefaultBranch(ctx, repo, repo.DefaultBranch); err != nil {
			return fmt.Errorf("setDefaultBranch: %w", err)
		}

		if !repo.IsEmpty {
			if _, err := repo_module.SyncRepoBranches(ctx, repo.ID, u.ID); err != nil {
				return fmt.Errorf("SyncRepoBranches: %w", err)
			}
		}
	}

	if err = UpdateRepository(ctx, repo, false); err != nil {
		return fmt.Errorf("updateRepository: %w", err)
	}

	return nil
}

// newRepoStorageBackend gives the storage backend of a new repository: asc when the instance writes through asc and
// creates new repositories there ([asc] WRITER and NEW_REPOSITORIES), git otherwise. A mirror and a repository
// that is not sha1 stay on git: both are filled by git.
func newRepoStorageBackend(opts CreateRepoOptions) string {
	if setting.Asc.Enabled && setting.Asc.Writer == setting.AscWriterAsc && setting.Asc.NewRepositories == setting.AscWriterAsc &&
		!opts.IsMirror && opts.ObjectFormatName == git.Sha1ObjectFormat.Name() {
		return string(gitrepo.StorageBackendAsc)
	}
	return ""
}

// CreateRepositoryDirectly creates a repository for the user/organization.
func CreateRepositoryDirectly(ctx context.Context, doer, u *user_model.User, opts CreateRepoOptions) (*repo_model.Repository, error) {
	if !doer.IsAdmin && !u.CanCreateRepo() {
		return nil, repo_model.ErrReachLimitOfRepo{
			Limit: u.MaxRepoCreation,
		}
	}

	if len(opts.DefaultBranch) == 0 {
		opts.DefaultBranch = setting.Repository.DefaultBranch
	}

	// Check if label template exist
	if len(opts.IssueLabels) > 0 {
		if _, err := repo_module.LoadTemplateLabelsByDisplayName(opts.IssueLabels); err != nil {
			return nil, err
		}
	}

	if opts.ObjectFormatName == "" {
		opts.ObjectFormatName = git.Sha1ObjectFormat.Name()
	}

	repo := &repo_model.Repository{
		OwnerID:                         u.ID,
		Owner:                           u,
		OwnerName:                       u.Name,
		Name:                            opts.Name,
		LowerName:                       strings.ToLower(opts.Name),
		Description:                     opts.Description,
		OriginalURL:                     opts.OriginalURL,
		OriginalServiceType:             opts.GitServiceType,
		IsPrivate:                       opts.IsPrivate,
		IsInternal:                      opts.IsInternal && opts.IsPrivate,
		IsFsckEnabled:                   !opts.IsMirror,
		IsTemplate:                      opts.IsTemplate,
		CloseIssuesViaCommitInAnyBranch: setting.Repository.DefaultCloseIssuesViaCommitsInAnyBranch,
		Status:                          opts.Status,
		IsEmpty:                         !opts.wantsFirstCommit(),
		TrustModel:                      opts.TrustModel,
		IsMirror:                        opts.IsMirror,
		DefaultBranch:                   opts.DefaultBranch,
		WikiBranch:                      setting.Repository.DefaultBranch,
		ObjectFormatName:                opts.ObjectFormatName,
		Website:                         opts.Website,
		StorageBackend:                  newRepoStorageBackend(opts),
	}

	var rollbackRepo *repo_model.Repository
	// storeIsOurs is true only once this call made the store of the repository (the git directory, or the repository
	// in asc). Until then a failure removes the new database rows and nothing else: the files at the path, and the
	// repository of that name in asc, belong to somebody else.
	storeIsOurs := false

	create := func(ctx context.Context) error {
		if err := repo_module.CreateRepositoryByExample(ctx, doer, u, repo, false, false); err != nil {
			return err
		}

		// No need for init mirror.
		if opts.IsMirror {
			return nil
		}

		repoPath := repo_model.RepoPath(u.Name, repo.Name)
		isExist, err := util.IsExist(repoPath)
		if err != nil {
			log.Error("Unable to check if %s exists. Error: %v", repoPath, err)
			return err
		}
		if isExist {
			// repo already exists - We have two or three options.
			// 1. We fail stating that the directory exists
			// 2. We create the db repository to go with this data and adopt the git repo
			// 3. We delete it and start afresh
			//
			// Previously Gitea would just delete and start afresh - this was naughty.
			// So we will now fail and delegate to other functionality to adopt or delete
			log.Error("Files already exist in %s and we are not going to adopt or delete.", repoPath)
			return repo_model.ErrRepoFilesAlreadyExist{
				Uname: u.Name,
				Name:  repo.Name,
			}
		}

		created, err := initRepository(ctx, repoPath, doer, repo, opts)
		storeIsOurs = created
		if err != nil {
			if !created {
				// The store was there already (or could not be made): it is not ours to delete.
				return fmt.Errorf("initRepository: %w", err)
			}
			if repo.GetStorageBackend() == string(gitrepo.StorageBackendAsc) {
				// This call made the repository in asc; Delete drops it and its bridge.
				if writer, werr := repostore.Default(); werr == nil {
					_ = writer.Delete(ctx, repo.StoreRef())
				}
			}
			if err2 := util.RemoveAll(repoPath); err2 != nil {
				log.Error("initRepository: %v", err)
				return fmt.Errorf(
					"delete repo directory %s/%s failed(2): %v", u.Name, repo.Name, err2)
			}
			storeIsOurs = false // removed
			return fmt.Errorf("initRepository: %w", err)
		}

		// Initialize Issue Labels if selected
		if len(opts.IssueLabels) > 0 {
			if err = repo_module.InitializeLabels(ctx, repo.ID, opts.IssueLabels, false); err != nil {
				rollbackRepo = repo
				rollbackRepo.OwnerID = u.ID
				return fmt.Errorf("InitializeLabels: %w", err)
			}
		}

		if err := repo_module.CheckDaemonExportOK(ctx, repo); err != nil {
			return fmt.Errorf("checkDaemonExportOK: %w", err)
		}

		if stdout, _, err := git.NewCommand(ctx, "update-server-info").
			SetDescription(fmt.Sprintf("CreateRepository(git update-server-info): %s", repoPath)).
			RunStdString(&git.RunOpts{Dir: repoPath}); err != nil {
			log.Error("CreateRepository(git update-server-info) in %v: Stdout: %s\nError: %v", repo, stdout, err)
			rollbackRepo = repo
			rollbackRepo.OwnerID = u.ID
			return fmt.Errorf("CreateRepository(git update-server-info): %w", err)
		}
		return nil
	}
	var err error
	if repo.GetStorageBackend() == string(gitrepo.StorageBackendAsc) {
		// asc runs the hooks of the first commit as a call back to this hub, through a connection of its own, which
		// sees the repository only once its row is committed; so the row is committed first and a failure removes it.
		err = create(ctx)
		if err != nil && repo.ID != 0 && rollbackRepo == nil {
			rollbackRepo = repo
			rollbackRepo.OwnerID = u.ID
		}
	} else {
		err = db.WithTx(ctx, create)
	}
	if err != nil {
		if rollbackRepo != nil {
			// The files are removed only when this call made them: a repository that failed because the files of
			// another one were in its way loses its rows and nothing else.
			if errDelete := DeleteRepositoryDirectly(ctx, rollbackRepo.ID, DeleteRepositoryOpts{KeepFiles: !storeIsOurs}); errDelete != nil {
				log.Error("Rollback deleteRepository: %v", errDelete)
			}
		}

		return nil, err
	}

	return repo, nil
}
