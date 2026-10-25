// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package ascbridge

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git"
	"forgejo.org/modules/repostore"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascsync"
)

// zeroID is the id of an absent ref in a git push.
const zeroID = "0000000000000000000000000000000000000000"

// SyncFromBridge brings the branches and tags that a git fetch left in the bridge of a pull mirror into asc
// (SPEC 13.4, "Mirrors (pull)"): the objects are imported from the bridge with ImportGitPush, a ref that asc
// refuses to fast-forward (an upstream that rewrote history) is then forced, and with prune a branch or tag
// that the bridge no longer has is deleted. It does nothing for a repository that is not stored in asc.
//
// asc is the truth of such a repository, so a failure is an error and the mirror run is reported as failed; the
// refs that did move stay moved, and the next run finishes the rest.
func SyncFromBridge(ctx context.Context, repo *repo_model.Repository, prune bool) error {
	isAsc, err := IsAsc(repo)
	if err != nil || !isAsc {
		return err
	}
	if !setting.Asc.Enabled {
		return ErrDisabled
	}
	path := Path(repo)
	name, err := ascName(path)
	if err != nil {
		return err
	}
	api, err := repoAPI()
	if err != nil {
		return err
	}
	local, err := bridgeRefs(ctx, path)
	if err != nil {
		return err
	}
	remote, err := ascRefs(ctx, api, name)
	if err != nil {
		return err
	}

	var changes []ascsync.PushRef
	for _, ref := range sortedKeys(local) {
		if remote[ref] != local[ref] {
			old := remote[ref]
			if old == "" {
				old = zeroID
			}
			changes = append(changes, ascsync.PushRef{Name: ref, Old: old, New: local[ref]})
		}
	}
	for start := 0; start < len(changes); start += ascsync.MaxRefUpdates {
		batch := changes[start:min(start+ascsync.MaxRefUpdates, len(changes))]
		if err := importBatch(ctx, repo, path, batch); err != nil {
			return err
		}
	}

	if !prune {
		return nil
	}
	var gone []repostore.RefUpdateSpec
	for _, ref := range sortedKeys(remote) {
		if _, ok := local[ref]; !ok {
			gone = append(gone, repostore.RefUpdateSpec{Name: ref, Delete: true})
		}
	}
	for start := 0; start < len(gone); start += repostore.MaxRefUpdates {
		if err := updateRefs(ctx, repo, gone[start:min(start+repostore.MaxRefUpdates, len(gone))]); err != nil {
			return err
		}
	}
	return nil
}

// importBatch imports one batch. When asc refuses the batch as a whole, each ref is tried alone and the ones asc
// refuses to fast-forward are forced; their objects are in asc already.
func importBatch(ctx context.Context, repo *repo_model.Repository, bridge string, batch []ascsync.PushRef) error {
	dirs := ascsync.PushDirs{ObjectDir: filepath.Join(bridge, "objects")}
	err := ascsync.ImportPush(ctx, ascsync.ImportRequest{Repo: repo, Dirs: dirs, Refs: batch})
	if err == nil {
		return nil
	}
	if _, refused := ascsync.RejectMessage(err); !refused {
		return err
	}
	for _, ref := range batch {
		err := ascsync.ImportPush(ctx, ascsync.ImportRequest{Repo: repo, Dirs: dirs, Refs: []ascsync.PushRef{ref}})
		if err == nil {
			continue
		}
		if _, refused := ascsync.RejectMessage(err); !refused {
			return err
		}
		if err := updateRefs(ctx, repo, []repostore.RefUpdateSpec{{Name: ref.Name, NewRev: ref.New, Force: true}}); err != nil {
			return fmt.Errorf("ascbridge: forcing %s: %w", ref.Name, err)
		}
	}
	return nil
}

// updateRefs moves refs of the repository through the repostore writer, which routes to asc.
func updateRefs(ctx context.Context, repo *repo_model.Repository, updates []repostore.RefUpdateSpec) error {
	if !repostore.AscWriterEnabled() {
		return errors.New("ascbridge: a mirror can move refs in asc only with [asc] WRITER = asc")
	}
	w, err := repostore.Default()
	if err != nil {
		return err
	}
	_, err = w.UpdateRefs(ctx, repostore.UpdateRefsRequest{Repo: repo.StoreRef(), Updates: updates})
	return err
}

// bridgeRefs lists the branches and tags of the bridge, by reading it as a git directory.
func bridgeRefs(ctx context.Context, path string) (map[string]string, error) {
	gitRepo, err := git.OpenRepositoryWithBackend(ctx, path, git.DefaultBackendName)
	if err != nil {
		return nil, fmt.Errorf("ascbridge: open the bridge: %w", err)
	}
	defer gitRepo.Close()
	out := map[string]string{}
	for _, prefix := range []string{git.BranchPrefix, git.TagPrefix} {
		refs, err := gitRepo.GetRefsFiltered(prefix)
		if err != nil {
			return nil, fmt.Errorf("ascbridge: list the refs of the bridge: %w", err)
		}
		for _, r := range refs {
			if !strings.HasPrefix(r.Name, prefix) {
				continue
			}
			if len(out) >= MaxRefs {
				return nil, fmt.Errorf("ascbridge: more than %d refs", MaxRefs)
			}
			out[r.Name] = r.Object.String()
		}
	}
	return out, nil
}

// ascRefs lists the branches and tags of the repository in asc with git ids.
func ascRefs(ctx context.Context, api ascv1.RepoClient, name string) (map[string]string, error) {
	type pending struct {
		ref string
		asc []byte
	}
	var found []pending
	after := ""
	for {
		cctx, cancel := context.WithTimeout(ascclient.WithSystem(ctx), CallTimeout)
		resp, err := api.ListRefs(cctx, &ascv1.ListRefsRequest{
			Repo: &ascv1.RepoRef{Name: name, IdMode: ascv1.IdMode_ID_MODE_ASC}, Prefix: "refs/", After: after, Limit: RefPage,
		})
		cancel()
		if err != nil {
			return nil, fmt.Errorf("ascbridge: list refs: %w", err)
		}
		for _, r := range resp.GetRefs() {
			if !strings.HasPrefix(r.GetName(), git.BranchPrefix) && !strings.HasPrefix(r.GetName(), git.TagPrefix) {
				continue
			}
			if len(found) >= MaxRefs {
				return nil, fmt.Errorf("ascbridge: more than %d refs", MaxRefs)
			}
			target := r.GetTarget().GetChangeset()
			if len(target) != 32 {
				// An annotated tag object has no changeset id; the next import will bring it over.
				continue
			}
			found = append(found, pending{r.GetName(), target})
		}
		if after = resp.GetNext(); after == "" {
			break
		}
	}
	out := make(map[string]string, len(found))
	for start := 0; start < len(found); start += MapBatch {
		chunk := found[start:min(start+MapBatch, len(found))]
		ids := make([][]byte, len(chunk))
		for i, p := range chunk {
			ids[i] = p.asc
		}
		cctx, cancel := context.WithTimeout(ascclient.WithSystem(ctx), CallTimeout)
		resp, err := api.GitMap(cctx, &ascv1.GitMapRequest{Repo: &ascv1.RepoRef{Name: name, IdMode: ascv1.IdMode_ID_MODE_ASC}, Asc: ids})
		cancel()
		if err != nil {
			return nil, fmt.Errorf("ascbridge: git map: %w", err)
		}
		mapped := resp.GetAscToGit()
		if len(mapped) != len(chunk) {
			return nil, fmt.Errorf("%w: GitMap answered %d ids for %d", ErrBadAnswer, len(mapped), len(chunk))
		}
		for i, g := range mapped {
			if len(g) == 0 {
				continue
			}
			if len(g) != 20 {
				return nil, fmt.Errorf("%w: git id of %d bytes", ErrBadAnswer, len(g))
			}
			out[chunk[i].ref] = git.Sha1ObjectFormat.MustID(g).String()
		}
	}
	return out, nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
