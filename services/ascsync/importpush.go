// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascsync

import (
	"context"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"

	repo_model "forgejo.org/models/repo"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git"
	"forgejo.org/modules/log"
)

// PushRef is one ref command of a git push: the ref and its old and new object ids as 40 hex digits.
// An all-zero id means the ref is absent.
type PushRef struct {
	Name string
	Old  string
	New  string
}

// PushDirs are the object directories git holds a push in: the quarantine itself and the directories
// it reads through (the repository's own objects). The values are GIT_OBJECT_DIRECTORY and
// GIT_ALTERNATE_OBJECT_DIRECTORIES of the hook environment.
type PushDirs struct {
	ObjectDir     string
	AlternateDirs []string
}

// DirsFromHook builds PushDirs from the three hook fields. alternates is a list separated by the
// operating system's path list separator, as git writes it.
func DirsFromHook(objectDirectory, quarantinePath, alternates string) PushDirs {
	d := PushDirs{ObjectDir: objectDirectory}
	if d.ObjectDir == "" {
		d.ObjectDir = quarantinePath
	}
	for _, a := range filepath.SplitList(alternates) {
		if a != "" {
			d.AlternateDirs = append(d.AlternateDirs, a)
		}
	}
	return d
}

// ImportRequest is one batch of ref commands of a git-origin push to a storage_backend=asc repository.
type ImportRequest struct {
	Repo *repo_model.Repository
	// PusherID is the Forgejo user id of the pusher. A negative id (the Actions user) is sent as 0.
	PusherID int64
	Dirs     PushDirs
	Refs     []PushRef
}

// ImportPush hands a git push to asc (the ImportGitPush RPC): asc reads the quarantine, imports the commits
// it does not know, and moves its refs with the normal pipeline in fast-forward mode. Call it from
// pre-receive after every policy check. A *RejectError carries asc's refusal for the pusher; any other
// error is an infrastructure failure. Either way the push must be refused.
//
// AGit refs (refs/for/...) are never sent: asc stores no such ref (SPEC section 13.5).
func ImportPush(ctx context.Context, req ImportRequest) error {
	h, err := open(req.Repo)
	if err != nil {
		return err
	}
	updates, err := gitRefUpdates(req.Refs)
	if err != nil {
		return err
	}
	if len(updates) == 0 {
		return nil
	}
	objectDir, alternates, err := checkDirs(h.bridge, req.Dirs)
	if err != nil {
		return err
	}
	pusher := max(req.PusherID, 0)
	accepted := false
	if importer := importerOf(ctx); pusher == 0 && importer > 0 {
		// An import the hub makes for a user (a migration, a first mirror run) names that user. The push is
		// remembered like a hook push, so asc's fan-out of it is attributed to the user, and a refused or failed
		// import can never be claimed later. (A hook push has been remembered by pre-receive already.)
		pusher = importer
		token, rerr := RememberPusher(req.Repo, pusher, req.Refs)
		if rerr != nil {
			log.Debug("the pusher of an import to %-v is not remembered: %v", req.Repo, rerr)
		}
		defer func() {
			if !accepted {
				token.Forget()
			}
		}()
	}

	timeout, api := ImportTimeout, h.api
	if isBulk(ctx) {
		timeout = BulkImportTimeout
		if api, err = bulkAPI(); err != nil {
			return err
		}
	}
	cctx, cancel := system(ctx, timeout)
	defer cancel()
	resp, err := api.ImportGitPush(cctx, &ascv1.ImportGitPushRequest{
		Repo:          &ascv1.RepoRef{Name: h.name, IdMode: ascv1.IdMode_ID_MODE_ASC},
		GitDir:        h.bridge,
		ObjectDir:     objectDir,
		AlternateDirs: alternates,
		Updates:       updates,
		PusherId:      pusher,
	})
	if err != nil {
		return callError("import push", err)
	}
	// asc answers once per command. Fewer would mean a ref was dropped without a word.
	if len(resp.GetResults()) != len(updates) {
		return fmt.Errorf("%w: %d results for %d commands", ErrBadAnswer, len(resp.GetResults()), len(updates))
	}
	accepted = true
	return nil
}

// gitRefUpdates validates the commands and converts them to the wire form. refs/for/ commands are
// left out.
func gitRefUpdates(refs []PushRef) ([]*ascv1.GitRefUpdate, error) {
	out := make([]*ascv1.GitRefUpdate, 0, len(refs))
	for _, r := range refs {
		if strings.HasPrefix(r.Name, git.ForPrefix) {
			continue
		}
		if err := checkRefName(r.Name); err != nil {
			return nil, err
		}
		oldID, err := gitID(r.Old)
		if err != nil {
			return nil, fmt.Errorf("%w: old id of a ref", ErrBadRequest)
		}
		newID, err := gitID(r.New)
		if err != nil {
			return nil, fmt.Errorf("%w: new id of a ref", ErrBadRequest)
		}
		out = append(out, &ascv1.GitRefUpdate{Name: r.Name, OldGit: oldID, NewGit: newID})
	}
	if len(out) > MaxRefUpdates {
		return nil, fmt.Errorf("%w: %d ref commands (at most %d)", ErrBadRequest, len(out), MaxRefUpdates)
	}
	return out, nil
}

// checkRefName refuses a name that is empty, too long, or holds a control character, a NUL or a
// backslash. git's own ref format rules are asc's to enforce; this keeps junk off the wire.
func checkRefName(name string) error {
	if name == "" || len(name) > MaxRefNameBytes || !strings.HasPrefix(name, "refs/") {
		return fmt.Errorf("%w: ref name", ErrBadRequest)
	}
	for i := range len(name) {
		if c := name[i]; c < 0x20 || c == 0x7f || c == '\\' {
			return fmt.Errorf("%w: ref name", ErrBadRequest)
		}
	}
	return nil
}

// gitID decodes 40 lowercase hex digits to a 20-byte id.
func gitID(s string) ([]byte, error) {
	if len(s) != 40 || s != strings.ToLower(s) {
		return nil, ErrBadRequest
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, ErrBadRequest
	}
	return b, nil
}

// checkDirs returns the quarantine and alternate directories to send, cleaned. All of them must be
// absolute, free of newlines and NULs, and inside <bridge>/objects: asc re-checks the same, but a path that
// does not belong to the repository is never sent. With no object directory the repository's own objects
// directory is used.
func checkDirs(bridge string, d PushDirs) (string, []string, error) {
	objects := filepath.Join(bridge, "objects")
	// git reports the physical path of the quarantine, so containment is judged on resolved paths.
	realObjects, err := filepath.EvalSymlinks(objects)
	if err != nil {
		return "", nil, fmt.Errorf("%w: repository objects directory", ErrBadRequest)
	}
	if len(d.AlternateDirs) > MaxAlternates {
		return "", nil, fmt.Errorf("%w: too many alternate directories", ErrBadRequest)
	}
	check := func(p string) (string, error) {
		bad := p == "" || len(p) > MaxDirPathBytes || strings.ContainsAny(p, "\x00\r\n") || !filepath.IsAbs(p)
		if bad {
			return "", fmt.Errorf("%w: object directory", ErrBadRequest)
		}
		// git writes the quarantine as <repo>/./objects/tmp_objdir-incoming-xxxx, so the path is cleaned,
		// and the cleaned path is the one sent.
		p = filepath.Clean(p)
		real, err := filepath.EvalSymlinks(p)
		if err != nil {
			return "", fmt.Errorf("%w: object directory", ErrBadRequest)
		}
		rel, err := filepath.Rel(realObjects, real)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("%w: object directory outside the repository", ErrBadRequest)
		}
		return p, nil
	}
	object := d.ObjectDir
	if object == "" {
		object = objects
	}
	object, err = check(object)
	if err != nil {
		return "", nil, err
	}
	alternates := make([]string, 0, len(d.AlternateDirs))
	for _, a := range d.AlternateDirs {
		a, err := check(a)
		if err != nil {
			return "", nil, err
		}
		alternates = append(alternates, a)
	}
	return object, alternates, nil
}
