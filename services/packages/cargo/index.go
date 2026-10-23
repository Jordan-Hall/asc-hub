// Copyright 2022 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package cargo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path"
	"strconv"
	"time"

	packages_model "forgejo.org/models/packages"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/json"
	cargo_module "forgejo.org/modules/packages/cargo"
	repo_module "forgejo.org/modules/repository"
	"forgejo.org/modules/repostore"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/structs"
	"forgejo.org/modules/util"
	repo_service "forgejo.org/services/repository"
	files_service "forgejo.org/services/repository/files"
)

const (
	IndexRepositoryName = "_cargo-index"
	ConfigFileName      = "config.json"
)

// https://doc.rust-lang.org/cargo/reference/registries.html#index-format

func BuildPackagePath(name string) string {
	switch len(name) {
	case 0:
		panic("Cargo package name can not be empty")
	case 1:
		return path.Join("1", name)
	case 2:
		return path.Join("2", name)
	case 3:
		return path.Join("3", string(name[0]), name)
	default:
		return path.Join(name[0:2], name[2:4], name)
	}
}

func InitializeIndexRepository(ctx context.Context, doer, owner *user_model.User) error {
	repo, err := getOrCreateIndexRepository(ctx, doer, owner)
	if err != nil {
		return err
	}

	if err := createOrUpdateConfigFile(ctx, repo, doer, owner); err != nil {
		return fmt.Errorf("createOrUpdateConfigFile: %w", err)
	}

	return nil
}

func RebuildIndex(ctx context.Context, doer, owner *user_model.User) error {
	repo, err := repo_model.GetRepositoryByOwnerAndName(ctx, owner.Name, IndexRepositoryName)
	if err != nil {
		return fmt.Errorf("GetRepositoryByOwnerAndName: %w", err)
	}

	ps, err := packages_model.GetPackagesByType(ctx, owner.ID, packages_model.TypeCargo)
	if err != nil {
		return fmt.Errorf("GetPackagesByType: %w", err)
	}

	return alterRepositoryContent(
		ctx,
		doer,
		repo,
		"Rebuild Cargo Index",
		func(existing []string) ([]repostore.FileOp, error) {
			// Remove all existing content but the Cargo config
			var ops []repostore.FileOp
			for _, file := range existing {
				if file != ConfigFileName {
					ops = append(ops, repostore.FileOp{Kind: repostore.OpDelete, Path: file})
				}
			}

			// Add all packages
			for _, p := range ps {
				op, err := packageIndexOp(ctx, p)
				if err != nil {
					return nil, err
				}
				if op != nil {
					ops = append(ops, *op)
				}
			}

			return ops, nil
		},
	)
}

func UpdatePackageIndexIfExists(ctx context.Context, doer, owner *user_model.User, packageID int64) error {
	// We do not want to force the creation of the repo here
	// cargo http index does not rely on the repo itself,
	// so if the repo does not exist, we just do nothing.
	repo, err := repo_model.GetRepositoryByOwnerAndName(ctx, owner.Name, IndexRepositoryName)
	if err != nil {
		if errors.Is(err, util.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("GetRepositoryByOwnerAndName: %w", err)
	}

	p, err := packages_model.GetPackageByID(ctx, packageID)
	if err != nil {
		return fmt.Errorf("GetPackageByID[%d]: %w", packageID, err)
	}

	return alterRepositoryContent(
		ctx,
		doer,
		repo,
		"Update "+p.Name,
		func([]string) ([]repostore.FileOp, error) {
			op, err := packageIndexOp(ctx, p)
			if err != nil || op == nil {
				return nil, err
			}
			return []repostore.FileOp{*op}, nil
		},
	)
}

type IndexVersionEntry struct {
	Name         string                     `json:"name"`
	Version      string                     `json:"vers"`
	Dependencies []*cargo_module.Dependency `json:"deps"`
	FileChecksum string                     `json:"cksum"`
	Features     map[string][]string        `json:"features"`
	Yanked       bool                       `json:"yanked"`
	Links        string                     `json:"links,omitempty"`
}

func BuildPackageIndex(ctx context.Context, p *packages_model.Package) (*bytes.Buffer, error) {
	pvs, _, err := packages_model.SearchVersions(ctx, &packages_model.PackageSearchOptions{
		PackageID: p.ID,
		Sort:      packages_model.SortVersionAsc,
	})
	if err != nil {
		return nil, fmt.Errorf("SearchVersions[%s]: %w", p.Name, err)
	}
	if len(pvs) == 0 {
		return nil, nil
	}

	pds, err := packages_model.GetPackageDescriptors(ctx, pvs)
	if err != nil {
		return nil, fmt.Errorf("GetPackageDescriptors[%s]: %w", p.Name, err)
	}

	var b bytes.Buffer
	for _, pd := range pds {
		metadata := pd.Metadata.(*cargo_module.Metadata)

		dependencies := metadata.Dependencies
		if dependencies == nil {
			dependencies = make([]*cargo_module.Dependency, 0)
		}

		features := metadata.Features
		if features == nil {
			features = make(map[string][]string)
		}

		yanked, _ := strconv.ParseBool(pd.VersionProperties.GetByName(cargo_module.PropertyYanked))
		entry, err := json.Marshal(&IndexVersionEntry{
			Name:         pd.Package.Name,
			Version:      pd.Version.Version,
			Dependencies: dependencies,
			FileChecksum: pd.Files[0].Blob.HashSHA256,
			Features:     features,
			Yanked:       yanked,
			Links:        metadata.Links,
		})
		if err != nil {
			return nil, err
		}

		b.Write(entry)
		b.WriteString("\n")
	}

	return &b, nil
}

// packageIndexOp returns the operation that writes the index file of a package, or nil when the package has no versions.
func packageIndexOp(ctx context.Context, p *packages_model.Package) (*repostore.FileOp, error) {
	b, err := BuildPackageIndex(ctx, p)
	if err != nil {
		return nil, err
	}
	if b == nil {
		return nil, nil
	}

	return &repostore.FileOp{Kind: repostore.OpPut, Path: BuildPackagePath(p.LowerName), Content: b}, nil
}

func getOrCreateIndexRepository(ctx context.Context, doer, owner *user_model.User) (*repo_model.Repository, error) {
	repo, err := repo_model.GetRepositoryByOwnerAndName(ctx, owner.Name, IndexRepositoryName)
	if err != nil {
		if errors.Is(err, util.ErrNotExist) {
			repo, err = repo_service.CreateRepositoryDirectly(ctx, doer, owner, repo_service.CreateRepoOptions{
				Name: IndexRepositoryName,
			})
			if err != nil {
				return nil, fmt.Errorf("CreateRepository: %w", err)
			}
		} else {
			return nil, fmt.Errorf("GetRepositoryByOwnerAndName: %w", err)
		}
	}

	return repo, nil
}

type Config struct {
	DownloadURL  string `json:"dl"`
	APIURL       string `json:"api"`
	AuthRequired bool   `json:"auth-required"`
}

func BuildConfig(owner *user_model.User, isPrivate bool) *Config {
	return &Config{
		DownloadURL:  setting.AppURL + "api/packages/" + owner.Name + "/cargo/api/v1/crates",
		APIURL:       setting.AppURL + "api/packages/" + owner.Name + "/cargo",
		AuthRequired: isPrivate,
	}
}

func createOrUpdateConfigFile(ctx context.Context, repo *repo_model.Repository, doer, owner *user_model.User) error {
	return alterRepositoryContent(
		ctx,
		doer,
		repo,
		"Initialize Cargo Config",
		func([]string) ([]repostore.FileOp, error) {
			var b bytes.Buffer
			err := json.NewEncoder(&b).Encode(BuildConfig(owner, setting.Service.RequireSignInView || owner.Visibility != structs.VisibleTypePublic || repo.IsPrivate))
			if err != nil {
				return nil, err
			}

			return []repostore.FileOp{{Kind: repostore.OpPut, Path: ConfigFileName, Content: &b}}, nil
		},
	)
}

// This is a shorter version of CreateOrUpdateRepoFile which allows to perform multiple actions on a git repository.
// fn is given the files of the default branch and returns the operations to commit.
func alterRepositoryContent(ctx context.Context, doer *user_model.User, repo *repo_model.Repository, commitMessage string, fn func(existing []string) ([]repostore.FileOp, error)) error {
	writer, err := repostore.Default()
	if err != nil {
		return err
	}
	gitRepo, closer, err := gitrepo.RepositoryFromContextOrOpen(ctx, repo)
	if err != nil {
		return err
	}
	defer closer.Close()

	var parent git.ObjectID
	var existing []string
	if gitRepo.IsBranchExist(repo.DefaultBranch) {
		commit, err := gitRepo.GetBranchCommit(repo.DefaultBranch)
		if err != nil {
			return err
		}
		parent = commit.ID

		entries, err := commit.Tree.ListEntriesRecursiveFast()
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				existing = append(existing, entry.Name())
			}
		}
	} else if !repo.IsEmpty {
		return git.ErrBranchNotExist{Name: repo.DefaultBranch}
	}

	ops, err := fn(existing)
	if err != nil {
		return err
	}

	now := time.Now()
	_, err = writer.CreateCommit(ctx, repostore.CommitRequest{
		Repo:    repo.StoreRef(),
		Parent:  parent,
		Ops:     ops,
		Meta:    files_service.CommitMeta(doer, doer, commitMessage, false, &files_service.CommitDateOptions{Author: now, Committer: now}, files_service.CRUDSigner(repo, doer)),
		Update:  &repostore.RefUpdate{Repo: repo.StoreRef(), Ref: git.BranchPrefix + repo.DefaultBranch},
		HookEnv: repo_module.PushingEnvironment(doer, repo),
	})
	return err
}
