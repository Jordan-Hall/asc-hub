// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package forgery

import (
	"fmt"
	"io"
	"io/fs"
	"strings"
	"testing/fstest"
	"time"

	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git"
	repo_module "forgejo.org/modules/repository"
	"forgejo.org/modules/repostore"
	files_service "forgejo.org/services/repository/files"
)

type MapFS = fstest.MapFS

// when a file has one of this mode, it needs specific git handling
// other non-zero modes are rejected
const (
	modeSubmodule = fs.ModeNamedPipe // hacky, but
	modeSymlink   = fs.ModeSymlink
)

func MapFile(data string) *fstest.MapFile {
	return &fstest.MapFile{
		Data: []byte(data),
	}
}

func MapSymlink(target string) *fstest.MapFile {
	return &fstest.MapFile{
		Data: []byte(target),
		Mode: modeSymlink,
	}
}

func MapSubmodule(sha string) *fstest.MapFile {
	return &fstest.MapFile{
		Data: []byte(sha),
		Mode: modeSubmodule,
	}
}

func initRepo(doer *user_model.User, repo *repo_model.Repository, format git.ObjectFormat, fsys fs.FS, commitMessage string) (string, error) {
	writer, err := repostore.Default()
	if err != nil {
		return "", err
	}

	var ops []repostore.FileOp
	var open []io.Closer
	defer func() {
		for _, c := range open {
			_ = c.Close()
		}
	}()

	if err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		var content io.Reader
		mode := git.EntryModeBlob
		switch d.Type() {
		case modeSymlink:
			target, err := fs.ReadLink(fsys, path)
			if err != nil {
				return err
			}
			content = strings.NewReader(target)
			mode = git.EntryModeSymlink
		case modeSubmodule:
			mode = git.EntryModeCommit
			data, err := fs.ReadFile(fsys, path)
			if err != nil {
				return err
			}
			// The store takes a full commit id, and the commit need not exist. A shorter value, such as an
			// abbreviated id, stands for an id made from it.
			id := strings.TrimSpace(string(data))
			if !format.IsValid(id) {
				id = git.ComputeBlobHash(format, data).String()
			}
			content = strings.NewReader(id)
		case 0:
			f, err := fsys.Open(path)
			if err != nil {
				return err
			}
			open = append(open, f)

			content = f
		default:
			return fmt.Errorf("unexpected file type in forgery.CreateRepository %s: %s", path, d.Type())
		}

		ops = append(ops, repostore.FileOp{Kind: repostore.OpPut, Path: path, Mode: mode, Content: content})
		return nil
	}); err != nil {
		return "", err
	}

	storeRef := repo.StoreRef()
	storeRef.ObjectFormat = format.Name()
	now := time.Now()
	result, err := writer.CreateCommit(git.DefaultContext, repostore.CommitRequest{
		Repo: storeRef,
		Ops:  ops,
		Meta: files_service.CommitMeta(doer, doer, commitMessage, false, &files_service.CommitDateOptions{Author: now, Committer: now}, files_service.CRUDSigner(repo, doer)),
		Update: &repostore.RefUpdate{
			Repo: storeRef,
			Ref:  git.BranchPrefix + repo.DefaultBranch,
		},
		HookEnv: repo_module.PushingEnvironment(doer, repo),
	})
	if err != nil {
		return "", err
	}
	return result.Commit.String(), nil
}
