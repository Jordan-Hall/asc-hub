// Copyright 2019 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package files

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	git_model "forgejo.org/models/git"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git"
	"forgejo.org/modules/lfs"
	repo_module "forgejo.org/modules/repository"
	"forgejo.org/modules/repostore"
)

// UploadRepoFileOptions contains the uploaded repository file options
type UploadRepoFileOptions struct {
	LastCommitID string
	OldBranch    string
	NewBranch    string
	TreePath     string
	Message      string
	Author       *IdentityOptions
	Committer    *IdentityOptions
	Files        []string // In UUID format.
	FullPaths    []string
	Signoff      bool
}

type uploadInfo struct {
	upload        *repo_model.Upload
	lfsMetaObject *git_model.LFSMetaObject
}

func cleanUpAfterFailure(ctx context.Context, infos *[]uploadInfo, repoID int64, original error) error {
	for _, info := range *infos {
		if info.lfsMetaObject == nil {
			continue
		}
		if !info.lfsMetaObject.Existing {
			if _, err := git_model.RemoveLFSMetaObjectByOid(ctx, repoID, info.lfsMetaObject.Oid); err != nil {
				original = fmt.Errorf("%w, %v", original, err) // We wrap the original error - as this is the underlying error that required the fallback
			}
		}
	}
	return original
}

// UploadRepoFiles uploads files to the given repository
func UploadRepoFiles(ctx context.Context, repo *repo_model.Repository, doer *user_model.User, opts *UploadRepoFileOptions) error {
	if len(opts.Files) == 0 {
		return nil
	}

	if len(opts.Files) != len(opts.FullPaths) {
		return fmt.Errorf("the length of opts.Files and opts.FullPaths is not the same")
	}

	uploads := make([]*repo_model.Upload, len(opts.Files))
	names := make([]string, len(opts.Files))
	infos := make([]uploadInfo, len(opts.Files))
	var err error
	for i := 0; i < len(opts.Files); i++ {
		uploads[i], err = repo_model.GetUploadByUUID(ctx, opts.Files[i])
		if err != nil {
			return fmt.Errorf("GetUploadByUUID [uuids: %v]: %w", opts.Files[i], err)
		}
		uploads[i].Name, err = SanitizePath(opts.FullPaths[i])
		if err != nil {
			return fmt.Errorf("SanitizePath [path: %v]: %w", opts.FullPaths[i], err)
		}
		// Check file is not lfs locked, will return nil if lock setting not enabled
		filepath := path.Join(opts.TreePath, uploads[i].Name)
		lfsLock, err := git_model.GetTreePathLock(ctx, repo.ID, filepath)
		if err != nil {
			return err
		}
		if lfsLock != nil && lfsLock.OwnerID != doer.ID {
			u, err := user_model.GetUserByID(ctx, lfsLock.OwnerID)
			if err != nil {
				return err
			}
			return git_model.ErrLFSFileLocked{RepoID: repo.ID, Path: filepath, UserName: u.Name}
		}

		names[i] = uploads[i].Name
		infos[i] = uploadInfo{upload: uploads[i]}
	}

	writer, err := repostore.Default()
	if err != nil {
		return err
	}

	gitRepo, closeRepo, base, err := openBranch(ctx, repo, opts.OldBranch)
	if closeRepo != nil {
		defer closeRepo()
	}
	if err != nil && (!git.IsErrBranchNotExist(err) || !repo.IsEmpty) {
		return err
	}
	// An empty repository takes its first commit.
	var parent git.ObjectID
	if base != nil {
		parent = base.ID
		if err := checkLastCommit(gitRepo, base, opts.LastCommitID, opts.OldBranch == opts.NewBranch); err != nil {
			return err
		}
	}

	// Copy uploaded files into repository.
	ops, closeFiles, err := uploadedFileOps(gitRepo, base, infos, repo.ID, opts.TreePath, repo.LFSAllowed())
	defer closeFiles()
	if err != nil {
		return err
	}

	author, committer := GetAuthorAndCommitterUsers(opts.Author, opts.Committer, doer)

	// Now deal with LFS objects, once the commit exists and before it is published.
	beforePublish := func(context.Context, repostore.PublishInput) error {
		for i := range infos {
			if infos[i].lfsMetaObject == nil {
				continue
			}
			infos[i].lfsMetaObject, err = git_model.NewLFSMetaObject(ctx, infos[i].lfsMetaObject.RepositoryID, infos[i].lfsMetaObject.Pointer)
			if err != nil {
				// OK Now we need to cleanup
				return cleanUpAfterFailure(ctx, &infos, repo.ID, err)
			}
			// Don't move the files yet - we need to ensure that
			// everything can be inserted first
		}

		// OK now we can insert the data into the store - there's no way to clean up the store
		// once it's in there, it's in there.
		contentStore := lfs.NewContentStore()
		for _, info := range infos {
			if err := uploadToLFSContentStore(info, contentStore); err != nil {
				return cleanUpAfterFailure(ctx, &infos, repo.ID, err)
			}
		}
		return nil
	}

	// Then push the commit to NewBranch
	if _, err := writer.CreateCommit(ctx, repostore.CommitRequest{
		Repo:          repo.StoreRef(),
		Parent:        parent,
		Ops:           ops,
		Meta:          CommitMeta(author, committer, opts.Message, opts.Signoff, nil, CRUDSigner(repo, author)),
		Update:        branchRef(repo, opts.NewBranch, false),
		BeforePublish: beforePublish,
		HookEnv:       repo_module.PushingEnvironment(doer, repo),
	}); err != nil {
		return convertStoreError(err)
	}

	return repo_model.DeleteUploads(ctx, uploads...)
}

// uploadedFileOps opens the uploaded files and returns the operations that put them in the tree. The
// files stay open until the returned function is called.
func uploadedFileOps(gitRepo *git.Repository, base *git.Commit, infos []uploadInfo, repoID int64, treePath string, lfsAllowed bool) ([]repostore.FileOp, func(), error) {
	var open []*os.File
	closeFiles := func() {
		for _, f := range open {
			_ = f.Close()
		}
	}

	var storeInLFSFunc func(string) (bool, error)
	if lfsAllowed && base != nil {
		checker, err := gitRepo.GitAttributeChecker(base.ID.String(), "filter")
		if err != nil {
			return nil, closeFiles, err
		}
		prevClose := closeFiles
		closeFiles = func() {
			_ = checker.Close()
			prevClose()
		}

		storeInLFSFunc = func(name string) (bool, error) {
			attrs, err := checker.CheckPath(name)
			if err != nil {
				return false, fmt.Errorf("could not CheckPath(%s): %w", name, err)
			}
			return attrs["filter"] == "lfs", nil
		}
	}

	ops := make([]repostore.FileOp, 0, len(infos))
	for i := range infos {
		info := &infos[i]
		storeInLFS := false
		if storeInLFSFunc != nil {
			var err error
			storeInLFS, err = storeInLFSFunc(info.upload.Name)
			if err != nil {
				return nil, closeFiles, err
			}
		}

		file, err := os.Open(info.upload.LocalPath())
		if err != nil {
			return nil, closeFiles, err
		}
		open = append(open, file)

		var content io.Reader = file
		if storeInLFS {
			// Handle LFS
			// FIXME: Inefficient! this should probably happen in models.Upload
			pointer, err := lfs.GeneratePointer(file)
			if err != nil {
				return nil, closeFiles, err
			}
			info.lfsMetaObject = &git_model.LFSMetaObject{Pointer: pointer, RepositoryID: repoID}
			content = strings.NewReader(pointer.StringContent())
		}
		ops = append(ops, repostore.FileOp{
			Kind:    repostore.OpPut,
			Path:    path.Join(treePath, info.upload.Name),
			Mode:    git.EntryModeBlob,
			Content: content,
		})
	}
	return ops, closeFiles, nil
}

func uploadToLFSContentStore(info uploadInfo, contentStore *lfs.ContentStore) error {
	if info.lfsMetaObject == nil {
		return nil
	}
	exist, err := contentStore.Exists(info.lfsMetaObject.Pointer)
	if err != nil {
		return err
	}
	if !exist {
		file, err := os.Open(info.upload.LocalPath())
		if err != nil {
			return err
		}

		defer file.Close()
		// FIXME: Put regenerates the hash and copies the file over.
		// I guess this strictly ensures the soundness of the store but this is inefficient.
		if err := contentStore.Put(info.lfsMetaObject.Pointer, file); err != nil {
			// OK Now we need to cleanup
			// Can't clean up the store, once uploaded there they're there.
			return err
		}
	}
	return nil
}
