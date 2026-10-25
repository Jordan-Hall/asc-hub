// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	gocontext "context"
	"encoding/hex"
	"errors"
	"net/http"
	"sort"
	"sync"
	"time"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/base"
	"forgejo.org/modules/git/ascbackend"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/optional"
	"forgejo.org/modules/setting"
	"forgejo.org/services/context"

	"google.golang.org/grpc"
)

// The "Your drafts" page (SPEC 21, PLAN unit B23, v1.x).
//
// `GET /-/asc/drafts` lists the signed-in user's own draft changes (the commit cloud workspace,
// Cloud.GetWorkspace) in each asc-backed repository they can reach.
//
// Trust: the page reads with the internal token, so the owner of every workspace it asks for is set
// here, from the signed-in user, and never from the request. A query string or form value cannot
// name another user. The repositories come from the access-checked repository search for that
// user. Everything asc returns is untrusted text: labels are re-validated against the label
// grammar of SPEC 21.3.5 (bad ones are dropped), ids are hex, and the template escapes every
// value. Nothing from asc's status text reaches the browser or the log, only its code.
const (
	tplAscDrafts base.TplName = "user/asc_drafts"

	// ascDraftsMaxRepos bounds the repositories asked per page view.
	ascDraftsMaxRepos = 50
	// ascDraftsMaxHeads bounds the drafts shown per repository.
	ascDraftsMaxHeads = 100
	// ascDraftsMaxLabels and ascDraftsMaxLabelBytes are the limits of SPEC 21.3.5.
	ascDraftsMaxLabels     = 8
	ascDraftsMaxLabelBytes = 64
	// ascDraftsMaxIDBytes bounds a change or commit id shown (BLAKE3 ids are 32 bytes).
	ascDraftsMaxIDBytes = 64
	// ascDraftsTimeout bounds all calls of one page view.
	ascDraftsTimeout = 20 * time.Second
)

// AscDraft is one draft change as shown.
type AscDraft struct {
	Change string // hex of the change id, empty when asc gave none
	Commit string // hex of the commit id
	Labels []string
	Added  time.Time
	Hidden bool
}

// AscDraftsRepo is the drafts of one repository, or the reason they are missing.
type AscDraftsRepo struct {
	Repo    *repo_model.Repository
	Drafts  []AscDraft
	Omitted int
	Failed  bool
}

var (
	ascDraftsConnMu  sync.Mutex
	ascDraftsConnVal grpc.ClientConnInterface
	// ascDraftsNewConn is replaced by tests.
	ascDraftsNewConn = func() (grpc.ClientConnInterface, error) {
		if setting.InternalToken == "" {
			return nil, errors.New("the internal token is not set")
		}
		return ascclient.New(ascclient.Config{Target: setting.Asc.ServerURL, InternalToken: setting.InternalToken})
	}
)

func ascDraftsConn() (grpc.ClientConnInterface, error) {
	ascDraftsConnMu.Lock()
	defer ascDraftsConnMu.Unlock()
	if ascDraftsConnVal == nil {
		c, err := ascDraftsNewConn()
		if err != nil {
			return nil, err
		}
		ascDraftsConnVal = c
	}
	return ascDraftsConnVal, nil
}

// ascDraftsLabelOK reports whether s matches [A-Za-z0-9._/-]{1,64}.
func ascDraftsLabelOK(s string) bool {
	if s == "" || len(s) > ascDraftsMaxLabelBytes {
		return false
	}
	for i := range len(s) {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == '_', c == '/', c == '-':
		default:
			return false
		}
	}
	return true
}

// ascDraftsBuild turns the answer into rows. Labels that break the grammar are dropped, ids that are
// too long are shown as empty, and the newest drafts come first.
func ascDraftsBuild(resp *ascv1.GetWorkspaceResponse) ([]AscDraft, int) {
	heads := resp.GetHeads()
	omitted := int(resp.GetOmitted())
	out := make([]AscDraft, 0, min(len(heads), ascDraftsMaxHeads))
	for _, h := range heads {
		if h == nil || len(h.GetCs()) == 0 || len(h.GetCs()) > ascDraftsMaxIDBytes {
			omitted++
			continue
		}
		d := AscDraft{Commit: hex.EncodeToString(h.GetCs()), Hidden: h.GetHiddenAt() != 0}
		if c := h.GetChange(); len(c) > 0 && len(c) <= ascDraftsMaxIDBytes {
			d.Change = hex.EncodeToString(c)
		}
		for _, l := range h.GetLabels() {
			if len(d.Labels) < ascDraftsMaxLabels && ascDraftsLabelOK(l) {
				d.Labels = append(d.Labels, l)
			}
		}
		if h.GetAdded() > 0 {
			d.Added = time.Unix(h.GetAdded(), 0).UTC()
		}
		out = append(out, d)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Added.After(out[j].Added) })
	if len(out) > ascDraftsMaxHeads {
		omitted += len(out) - ascDraftsMaxHeads
		out = out[:ascDraftsMaxHeads]
	}
	return out, omitted
}

// AscDrafts serves `GET /-/asc/drafts`.
func AscDrafts(ctx *context.Context) {
	if !setting.Asc.Enabled || ctx.Doer == nil {
		ctx.NotFound("AscDrafts", nil)
		return
	}
	ctx.Data["Title"] = ctx.Tr("user.asc_drafts")
	ctx.Data["PageIsAscDrafts"] = true

	repos, _, err := repo_model.SearchRepository(ctx, &repo_model.SearchRepoOptions{
		ListOptions: db.ListOptions{Page: 1, PageSize: ascDraftsMaxRepos * 4},
		Actor:       ctx.Doer,
		OwnerID:     ctx.Doer.ID,
		Private:     true,
		Archived:    optional.Some(false),
		OrderBy:     "updated_unix DESC",
	})
	if err != nil {
		ctx.ServerError("SearchRepository", err)
		return
	}
	conn, err := ascDraftsConn()
	if err != nil {
		log.Error("asc drafts: no connection to asc serve: %v", err)
		ctx.Data["AscDraftsError"] = "user.asc_drafts.error.unavailable"
		ctx.HTML(http.StatusBadGateway, tplAscDrafts)
		return
	}
	callCtx, cancel := gocontext.WithTimeout(ascclient.WithSystem(ctx.Req.Context()), ascDraftsTimeout)
	defer cancel()
	client := ascv1.NewCloudClient(conn)

	var rows []AscDraftsRepo
	failed := false
	for _, repo := range repos {
		if len(rows) >= ascDraftsMaxRepos {
			break
		}
		if repo.GetStorageBackend() != string(gitrepo.StorageBackendAsc) {
			continue
		}
		name, err := ascbackend.RepoName(setting.RepoRootPath, repo_model.RepoPath(repo.OwnerName, repo.Name))
		if err != nil {
			continue
		}
		row := AscDraftsRepo{Repo: repo}
		resp, err := client.GetWorkspace(callCtx, &ascv1.GetWorkspaceRequest{
			Repo:  &ascv1.RepoRef{Name: name, IdMode: ascv1.IdMode_ID_MODE_GIT},
			Owner: ctx.Doer.Name, // always the signed-in user
		})
		if err != nil {
			if ctx.Req.Context().Err() != nil {
				return // the browser went away
			}
			log.Error("asc drafts: GetWorkspace failed for repository %d", repo.ID)
			row.Failed = true
			failed = true
		} else {
			row.Drafts, row.Omitted = ascDraftsBuild(resp)
			if len(row.Drafts) == 0 && row.Omitted == 0 {
				continue
			}
		}
		rows = append(rows, row)
	}
	ctx.Data["AscDraftsRepos"] = rows
	ctx.Data["AscDraftsAnyFailed"] = failed
	ctx.HTML(http.StatusOK, tplAscDrafts)
}
