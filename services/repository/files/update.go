// Copyright 2019 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package files

import (
	"context"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"forgejo.org/models"
	git_model "forgejo.org/models/git"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/lfs"
	"forgejo.org/modules/log"
	repo_module "forgejo.org/modules/repository"
	"forgejo.org/modules/repostore"
	"forgejo.org/modules/structs"
	asymkey_service "forgejo.org/services/asymkey"
)

// IdentityOptions for a person's identity like an author or committer
type IdentityOptions struct {
	Name  string
	Email string
}

// CommitDateOptions store dates for GIT_AUTHOR_DATE and GIT_COMMITTER_DATE
type CommitDateOptions struct {
	Author    time.Time
	Committer time.Time
}

type ChangeRepoFile struct {
	Operation     string
	TreePath      string
	FromTreePath  string
	ContentReader io.ReadSeeker
	SHA           string
	Options       *RepoFileOptions
}

// ChangeRepoFilesOptions holds the repository files update options
type ChangeRepoFilesOptions struct {
	LastCommitID            string
	OldBranch               string
	NewBranch               string
	Message                 string
	Files                   []*ChangeRepoFile
	Author                  *IdentityOptions
	Committer               *IdentityOptions
	Dates                   *CommitDateOptions
	Signoff                 bool
	ForceOverwriteNewBranch bool
}

type RepoFileOptions struct {
	treePath     string
	fromTreePath string
	entryMode    git.EntryMode
}

func RepoFileOptionMode(entryMode git.EntryMode) *RepoFileOptions {
	return &RepoFileOptions{
		entryMode: entryMode,
	}
}

// ChangeRepoFiles adds, updates or removes multiple files in the given repository
func ChangeRepoFiles(ctx context.Context, repo *repo_model.Repository, doer *user_model.User, opts *ChangeRepoFilesOptions) (*structs.FilesResponse, error) {
	err := repo.MustNotBeArchived()
	if err != nil {
		return nil, err
	}

	// If no branch name is set, assume default branch
	if opts.OldBranch == "" {
		opts.OldBranch = repo.DefaultBranch
	}
	if opts.NewBranch == "" {
		opts.NewBranch = opts.OldBranch
	}

	gitRepo, closer, err := gitrepo.RepositoryFromContextOrOpen(ctx, repo)
	if err != nil {
		return nil, err
	}
	defer closer.Close()

	// oldBranch must exist for this operation
	if _, err := gitRepo.GetBranch(opts.OldBranch); err != nil && !repo.IsEmpty {
		return nil, err
	}

	var treePaths []string
	for _, file := range opts.Files {
		// If FromTreePath is not set, set it to the opts.TreePath
		if file.TreePath != "" && file.FromTreePath == "" {
			file.FromTreePath = file.TreePath
		}

		// Check that the path given in opts.treePath is valid (not a git path)
		treePath := CleanUploadFileName(file.TreePath)
		if treePath == "" {
			return nil, models.ErrFilenameInvalid{
				Path: file.TreePath,
			}
		}
		// If there is a fromTreePath (we are copying it), also clean it up
		fromTreePath := CleanUploadFileName(file.FromTreePath)
		if fromTreePath == "" && file.FromTreePath != "" {
			return nil, models.ErrFilenameInvalid{
				Path: file.FromTreePath,
			}
		}

		mode := git.EntryModeBlob
		if file.Options != nil && file.Options.entryMode != 0 {
			mode = file.Options.entryMode
		}
		file.Options = &RepoFileOptions{
			treePath:     treePath,
			fromTreePath: fromTreePath,
			entryMode:    mode,
		}
		treePaths = append(treePaths, treePath)
	}

	// A NewBranch can be specified for the file to be created/updated in a new branch.
	// Check to make sure the branch does not already exist, otherwise we can't proceed.
	// If we aren't branching to a new branch, make sure user can commit to the given branch
	if opts.NewBranch != opts.OldBranch {
		existingBranch, err := gitRepo.GetBranch(opts.NewBranch)
		if existingBranch != nil && !opts.ForceOverwriteNewBranch {
			return nil, git_model.ErrBranchAlreadyExists{
				BranchName: opts.NewBranch,
			}
		}
		if err != nil && !git.IsErrBranchNotExist(err) {
			return nil, err
		}
	} else if err := VerifyBranchProtection(ctx, repo, doer, opts.OldBranch, treePaths); err != nil {
		return nil, err
	}

	message := strings.TrimSpace(opts.Message)

	author, committer := GetAuthorAndCommitterUsers(opts.Author, opts.Committer, doer)

	writer, err := repostore.Default()
	if err != nil {
		return nil, err
	}

	// The commit the change builds on. A repository without commits has none.
	var base *git.Commit
	hasOldBranch := true
	if gitRepo.IsBranchExist(opts.OldBranch) {
		if base, err = gitRepo.GetBranchCommit(opts.OldBranch); err != nil {
			return nil, err
		}
	} else {
		missing := git.ErrBranchNotExist{Name: opts.OldBranch}
		for _, file := range opts.Files {
			if file.Operation == "delete" {
				return nil, missing
			}
		}
		if !repo.IsEmpty {
			return nil, missing
		}
		hasOldBranch = false
		opts.LastCommitID = ""
	}

	for _, file := range opts.Files {
		if file.Operation == "delete" {
			// Find the file we want to delete in the tree
			entry, err := base.GetTreeEntryByPath(file.Options.treePath)
			if err != nil {
				if git.IsErrNotExist(err) {
					return nil, models.ErrRepoFileDoesNotExist{Path: file.TreePath}
				}
				return nil, fmt.Errorf("DeleteRepoFile: %w", err)
			}
			if entry.IsDir() {
				return nil, models.ErrRepoFileDoesNotExist{Path: file.TreePath}
			}
		}
	}

	if hasOldBranch {
		var lastKnownCommit git.ObjectID // when nil, the sha provided in the opts.Files must match the current blob-sha
		if opts.OldBranch != opts.NewBranch {
			// when creating a new branch, ignore if a file has been changed in the meantime
			// (such changes will visible when doing the merge)
			lastKnownCommit = base.ID
		} else if opts.LastCommitID != "" {
			lastKnownCommit, err = gitRepo.ConvertToGitID(opts.LastCommitID)
			if err != nil {
				return nil, fmt.Errorf("ConvertToSHA1: Invalid last commit ID: %w", err)
			}
		}

		for _, file := range opts.Files {
			if err := handleCheckErrors(file, base, lastKnownCommit); err != nil {
				return nil, err
			}
		}

		if err := checkLastCommit(gitRepo, base, opts.LastCommitID, opts.OldBranch == opts.NewBranch); err != nil {
			return nil, err
		}
	}

	// The policy file of an asc repository is proposed, not committed, when the edit is for the default branch.
	if AscPolicyEdit != nil && opts.OldBranch == opts.NewBranch {
		if err := AscPolicyEdit(ctx, repo, doer, opts); err != nil {
			return nil, err
		}
	}

	changes := &fileChanges{gitRepo: gitRepo, base: base, lfsAllowed: repo.LFSAllowed()}
	for _, file := range opts.Files {
		switch file.Operation {
		case "create", "update":
			if err := changes.createOrUpdate(file, repo.ID, hasOldBranch); err != nil {
				return nil, err
			}
		case "delete":
			changes.remove(file.Options.treePath)
		default:
			return nil, fmt.Errorf("invalid file operation: %s %s, supported operations are create, update, delete", file.Operation, file.Options.treePath)
		}
	}

	// Build the commit and push it to NewBranch
	var parent git.ObjectID
	if base != nil {
		parent = base.ID
	}
	result, err := writer.CreateCommit(ctx, repostore.CommitRequest{
		Repo:          repo.StoreRef(),
		Parent:        parent,
		Ops:           changes.ops,
		Meta:          CommitMeta(author, committer, message, opts.Signoff, opts.Dates, CRUDSigner(repo, author)),
		Update:        branchRef(repo, opts.NewBranch, opts.ForceOverwriteNewBranch),
		BeforePublish: changes.storeLFSObjects(ctx),
		HookEnv:       repo_module.PushingEnvironment(doer, repo),
	})
	if err != nil {
		log.Error("%T %v", err, err)
		return nil, convertStoreError(err)
	}

	commit, err := gitRepo.GetCommit(result.Commit.String())
	if err != nil {
		return nil, err
	}

	filesResponse, err := GetFilesResponseFromCommit(ctx, repo, commit, opts.NewBranch, treePaths)
	if err != nil {
		return nil, err
	}

	if repo.IsEmpty {
		if isEmpty, err := gitRepo.IsEmpty(); err == nil && !isEmpty {
			_ = repo_model.UpdateRepositoryCols(ctx, &repo_model.Repository{ID: repo.ID, IsEmpty: false, DefaultBranch: opts.NewBranch}, "is_empty", "default_branch")
		}
	}

	return filesResponse, nil
}

// handles the check for various issues for ChangeRepoFiles
func handleCheckErrors(file *ChangeRepoFile, actualBaseCommit *git.Commit, lastKnownCommit git.ObjectID) error {
	if file.Operation == "update" || file.Operation == "delete" {
		fromEntry, err := actualBaseCommit.GetTreeEntryByPath(file.Options.fromTreePath)
		if err != nil {
			return err
		}
		if file.SHA != "" {
			// If a SHA was given and the SHA given doesn't match the SHA of the fromTreePath, throw error
			if file.SHA != fromEntry.ID.String() {
				return models.ErrSHADoesNotMatch{
					Path:       file.Options.treePath,
					GivenSHA:   file.SHA,
					CurrentSHA: fromEntry.ID.String(),
				}
			}
		} else if lastKnownCommit != nil {
			if actualBaseCommit.ID.String() != lastKnownCommit.String() {
				// If a lastKnownCommit was given and it doesn't match the actualBaseCommit,
				// check if the file has been changed in between
				if changed, err := actualBaseCommit.FileChangedSinceCommit(file.Options.treePath, lastKnownCommit.String()); err != nil {
					return err
				} else if changed {
					return models.ErrCommitIDDoesNotMatch{
						GivenCommitID:   lastKnownCommit.String(),
						CurrentCommitID: actualBaseCommit.ID.String(),
					}
				}
				// The file wasn't modified, so we are good to update it
			}
		} else {
			// When updating a file, a lastKnownCommit or SHA needs to be given to make sure other commits
			// haven't been made. We throw an error if one wasn't provided.
			return models.ErrSHAOrCommitIDNotProvided{}
		}
		file.Options.entryMode = fromEntry.Mode()
	}
	if file.Operation == "create" || file.Operation == "update" {
		// For the path where this file will be created/updated, we need to make
		// sure no parts of the path are existing files or links except for the last
		// item in the path which is the file name, and that shouldn't exist IF it is
		// a new file OR is being moved to a new path.
		treePathParts := strings.Split(file.Options.treePath, "/")
		subTreePath := ""
		for index, part := range treePathParts {
			subTreePath = path.Join(subTreePath, part)
			entry, err := actualBaseCommit.GetTreeEntryByPath(subTreePath)
			if err != nil {
				if git.IsErrNotExist(err) {
					// Means there is no item with that name, so we're good
					break
				}
				return err
			}
			if index < len(treePathParts)-1 {
				if !entry.IsDir() {
					return models.ErrFilePathInvalid{
						Message: fmt.Sprintf("a file exists where you’re trying to create a subdirectory [path: %s]", subTreePath),
						Path:    subTreePath,
						Name:    part,
						Type:    git.EntryModeBlob,
					}
				}
			} else if entry.IsLink() {
				return models.ErrFilePathInvalid{
					Message: fmt.Sprintf("a symbolic link exists where you’re trying to create a subdirectory [path: %s]", subTreePath),
					Path:    subTreePath,
					Name:    part,
					Type:    git.EntryModeSymlink,
				}
			} else if entry.IsDir() {
				return models.ErrFilePathInvalid{
					Message: fmt.Sprintf("a directory exists where you’re trying to create a file [path: %s]", subTreePath),
					Path:    subTreePath,
					Name:    part,
					Type:    git.EntryModeTree,
				}
			} else if file.Options.fromTreePath != file.Options.treePath || file.Operation == "create" {
				// The entry shouldn't exist if we are creating new file or moving to a new path
				return models.ErrRepoFileAlreadyExists{
					Path: file.Options.treePath,
				}
			}
		}
	}

	return nil
}

// lfsObject is a file whose content goes to the LFS store instead of the tree.
type lfsObject struct {
	meta    *git_model.LFSMetaObject
	content io.ReadSeeker
}

// fileChanges collects the file operations of a request. It tracks which paths the request has changed
// so that a later operation sees the effect of an earlier one, as it would in an index.
type fileChanges struct {
	gitRepo *git.Repository
	base    *git.Commit // nil for a repository without commits
	ops     []repostore.FileOp
	state   map[string]bool // path -> whether a file is present, for the paths this request changed
	lfs     []lfsObject
	// lfsAllowed is false for a repository stored in asc, which has no LFS: a file is stored as it is.
	lfsAllowed bool
}

// present reports whether a file is at the path, counting the changes made so far.
func (c *fileChanges) present(treePath string) (bool, error) {
	if v, ok := c.state[treePath]; ok {
		return v, nil
	}
	if c.base == nil {
		return false, nil
	}
	entry, err := c.base.GetTreeEntryByPath(treePath)
	if err != nil {
		if git.IsErrNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return !entry.IsDir(), nil
}

func (c *fileChanges) set(treePath string, present bool) {
	if c.state == nil {
		c.state = map[string]bool{}
	}
	c.state[treePath] = present
}

// remove deletes a file.
func (c *fileChanges) remove(treePath string) {
	c.ops = append(c.ops, repostore.FileOp{Kind: repostore.OpDelete, Path: treePath})
	c.set(treePath, false)
}

// createOrUpdate handles creating or updating a file for ChangeRepoFiles
func (c *fileChanges) createOrUpdate(file *ChangeRepoFile, repoID int64, hasOldBranch bool) error {
	treePath, fromTreePath := file.Options.treePath, file.Options.fromTreePath

	// If is a new file (not updating) then the given path shouldn't exist
	if file.Operation == "create" {
		exists, err := c.present(treePath)
		if err != nil {
			return fmt.Errorf("UpdateRepoFile: %w", err)
		}
		if exists {
			return models.ErrRepoFileAlreadyExists{Path: file.TreePath}
		}
	}

	// Remove the old path from the tree
	if fromTreePath != treePath {
		exists, err := c.present(fromTreePath)
		if err != nil {
			return fmt.Errorf("UpdateRepoFile: %w", err)
		}
		if exists {
			c.remove(fromTreePath)
		}
	}

	treeObjectContentReader := io.Reader(file.ContentReader)
	if c.lfsAllowed && hasOldBranch {
		// Check there is no way this can return multiple infos
		filterAttribute, err := c.gitRepo.GitAttributeFirst(c.base.ID.String(), treePath, "filter")
		if err != nil {
			return err
		}

		if filterAttribute == "lfs" {
			// OK so we are supposed to LFS this data!
			pointer, err := lfs.GeneratePointer(file.ContentReader)
			if err != nil {
				return err
			}
			c.lfs = append(c.lfs, lfsObject{
				meta:    &git_model.LFSMetaObject{Pointer: pointer, RepositoryID: repoID},
				content: file.ContentReader,
			})
			treeObjectContentReader = strings.NewReader(pointer.StringContent())
		}
	}

	c.ops = append(c.ops, repostore.FileOp{Kind: repostore.OpPut, Path: treePath, Mode: file.Options.entryMode, Content: treeObjectContentReader})
	c.set(treePath, true)
	return nil
}

// storeLFSObjects returns the step that runs once the commit exists and before it is published:
// it records the LFS objects the commit refers to and puts their content in the store.
func (c *fileChanges) storeLFSObjects(ctx context.Context) repostore.BeforePublish {
	if len(c.lfs) == 0 {
		return nil
	}
	contentStore := lfs.NewContentStore()
	return func(context.Context, repostore.PublishInput) error {
		for _, object := range c.lfs {
			meta, err := git_model.NewLFSMetaObject(ctx, object.meta.RepositoryID, object.meta.Pointer)
			if err != nil {
				return err
			}
			exist, err := contentStore.Exists(meta.Pointer)
			if err != nil {
				return err
			}
			if !exist {
				if _, err := object.content.Seek(0, io.SeekStart); err != nil {
					return err
				}
				if err := contentStore.Put(meta.Pointer, object.content); err != nil {
					if _, err2 := git_model.RemoveLFSMetaObjectByOid(ctx, object.meta.RepositoryID, meta.Oid); err2 != nil {
						return fmt.Errorf("unable to remove failed inserted LFS object %s: %v (Prev Error: %w)", meta.Oid, err2, err)
					}
					return err
				}
			}
		}
		return nil
	}
}

// VerifyBranchProtection verify the branch protection for modifying the given treePath on the given branch
func VerifyBranchProtection(ctx context.Context, repo *repo_model.Repository, doer *user_model.User, branchName string, treePaths []string) error {
	protectedBranch, err := git_model.GetFirstMatchProtectedBranchRule(ctx, repo.ID, branchName)
	if err != nil {
		return err
	}
	if protectedBranch != nil {
		protectedBranch.Repo = repo
		globUnprotected := protectedBranch.GetUnprotectedFilePatterns()
		globProtected := protectedBranch.GetProtectedFilePatterns()
		canUserPush := protectedBranch.CanUserPush(ctx, doer)
		for _, treePath := range treePaths {
			isUnprotectedFile := false
			if len(globUnprotected) != 0 {
				isUnprotectedFile = protectedBranch.IsUnprotectedFile(globUnprotected, treePath)
			}
			if !canUserPush && !isUnprotectedFile {
				return models.ErrUserCannotCommit{
					UserName: doer.LowerName,
				}
			}
			if protectedBranch.IsProtectedFile(globProtected, treePath) {
				return models.ErrFilePathProtected{
					Path: treePath,
				}
			}
		}
		if protectedBranch.RequireSignedCommits {
			_, _, _, err := asymkey_service.SignCRUDAction(ctx, repo.RepoPath(), doer, repo.RepoPath(), branchName)
			if err != nil {
				if !asymkey_service.IsErrWontSign(err) {
					return err
				}
				return models.ErrUserCannotCommit{
					UserName: doer.LowerName,
				}
			}
		}
	}
	return nil
}
