// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"bytes"
	gocontext "context"
	"encoding/hex"
	"errors"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
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
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
//
// Where a draft is (SPEC 21.3.9): the page asks with include_devices, so each hub draft names the
// devices that also hold it and the drafts a device reported but never uploaded come back in
// `local`, with the reason. Device names are re-validated ([A-Za-z0-9._-]{1,64}); a bad one is
// dropped. A server that predates the report sends neither, and every draft reads "On the hub".
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
	// ascDraftsMaxDevices bounds the devices named on one draft.
	ascDraftsMaxDevices = 8
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
	// HiddenAt is when the draft was discarded (zero for a draft that is not).
	HiddenAt time.Time
	// OnHub is false for a draft only a device holds; Devices then has that one device.
	OnHub   bool
	Devices []string
	// Reason is the locale key of why a device-only draft was not uploaded, empty if unknown.
	Reason string
}

// ascDraftsReasons maps a device-only draft's upload state to the text shown.
var ascDraftsReasons = map[ascv1.LocalDraftState]string{
	ascv1.LocalDraftState_LOCAL_DRAFT_STATE_UPLOADED:   "asc_work.drafts_why_removed",
	ascv1.LocalDraftState_LOCAL_DRAFT_STATE_WAITING:    "asc_work.drafts_why_waiting",
	ascv1.LocalDraftState_LOCAL_DRAFT_STATE_FAILED:     "asc_work.drafts_why_failed",
	ascv1.LocalDraftState_LOCAL_DRAFT_STATE_TOO_LARGE:  "asc_work.drafts_why_too_large",
	ascv1.LocalDraftState_LOCAL_DRAFT_STATE_NOT_QUEUED: "asc_work.drafts_why_not_queued",
}

// ascDraftsDeviceOK reports whether s matches [A-Za-z0-9._-]{1,64}.
func ascDraftsDeviceOK(s string) bool {
	return !strings.Contains(s, "/") && ascDraftsLabelOK(s)
}

// ascDraftsDevices keeps the valid, distinct device names, sorted, at most ascDraftsMaxDevices.
func ascDraftsDevices(in []string) []string {
	var out []string
	for _, d := range in {
		if ascDraftsDeviceOK(d) && !slices.Contains(out, d) {
			out = append(out, d)
		}
	}
	sort.Strings(out)
	if len(out) > ascDraftsMaxDevices {
		out = out[:ascDraftsMaxDevices]
	}
	return out
}

// AscDraftsRepo is the drafts of one repository, or the reason they are missing.
type AscDraftsRepo struct {
	Repo   *repo_model.Repository
	Drafts []AscDraft
	// Discarded are the drafts the person discarded, which asc keeps for ascDraftsUndoDays.
	Discarded []AscDraft
	Omitted   int
	Failed    bool
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
		if h.GetHiddenAt() > 0 {
			d.HiddenAt = time.Unix(h.GetHiddenAt(), 0).UTC()
		}
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
		d.OnHub = true
		d.Devices = ascDraftsDevices(h.GetDevices())
		out = append(out, d)
	}
	for _, l := range resp.GetLocal() {
		c := l.GetChange()
		if l == nil || len(c) == 0 || len(c) > ascDraftsMaxIDBytes || !ascDraftsDeviceOK(l.GetDevice()) {
			omitted++
			continue
		}
		d := AscDraft{Change: hex.EncodeToString(c), Devices: []string{l.GetDevice()}, Reason: ascDraftsReasons[l.GetState()]}
		if l.GetAdded() > 0 {
			d.Added = time.Unix(l.GetAdded(), 0).UTC()
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
	failed, anyDiscarded := false, false
	banner := ascDraftsParseDiscarded(ctx.FormString("discarded"))
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
			Repo:           &ascv1.RepoRef{Name: name, IdMode: ascv1.IdMode_ID_MODE_GIT},
			Owner:          ctx.Doer.Name, // always the signed-in user
			IncludeDevices: true,
			IncludeHidden:  true,
		})
		if err != nil {
			if ctx.Req.Context().Err() != nil {
				return // the browser went away
			}
			log.Error("asc drafts: GetWorkspace failed for repository %d", repo.ID)
			row.Failed = true
			failed = true
		} else {
			all, omitted := ascDraftsBuild(resp)
			row.Omitted = omitted
			for _, d := range all {
				if !d.Hidden {
					row.Drafts = append(row.Drafts, d)
					continue
				}
				row.Discarded = append(row.Discarded, d)
				anyDiscarded = true
				if banner.repo == repo.ID && banner.cs == d.Commit {
					ctx.Data["AscDraftsJustDiscarded"] = AscDraftsJust{Repo: repo, Draft: d}
				}
			}
			if len(all) == 0 && row.Omitted == 0 {
				continue
			}
		}
		rows = append(rows, row)
	}
	ctx.Data["AscDraftsRepos"] = rows
	ctx.Data["AscDraftsAnyFailed"] = failed
	ctx.Data["AscDraftsAnyDiscarded"] = anyDiscarded
	ctx.Data["AscDraftsAnyVisible"] = slices.ContainsFunc(rows, func(r AscDraftsRepo) bool {
		return r.Failed || r.Omitted > 0 || len(r.Drafts) > 0
	})
	ctx.Data["AscDraftsUndoDays"] = ascDraftsUndoDays
	ctx.HTML(http.StatusOK, tplAscDrafts)
}

// Discard and undo (design board "Drafts": "Discard asks nothing ... can be undone for 30 days").
//
// `POST /-/asc/drafts/discard` hides one draft of the signed-in person's workspace in one repository and
// `POST /-/asc/drafts/undo` shows it again (Cloud.UpdateWorkspace hide and unhide). asc deletes a draft that has been
// hidden for its hidden_grace_days (30 by default, ascDraftsUndoDays here); until then the page lists it under Discarded
// with an Undo button. Nothing is asked before a discard: the undo is the safety.
//
// Trust: the calls carry the hub's signed claim about the signed-in person (asctrust.UserCall, read on the repository,
// decided again by the hub's permission model at the moment of the call), so asc acts as that person and changes only
// their own workspace. The form names the repository by id and the draft by its hex commit id; neither can name another
// person. The draft must be in the workspace (read first): a draft that is gone, or only on a device, is refused.

// ascDraftsUndoDays is how long a discarded draft can be restored: asc's default hidden_grace_days.
const ascDraftsUndoDays = 30

// ascDraftsStaleTries bounds the attempts when another device changes the workspace between the read and the write.
const ascDraftsStaleTries = 3

// AscDraftsJust is the draft just discarded, for the banner with its Undo button.
type AscDraftsJust struct {
	Repo  *repo_model.Repository
	Draft AscDraft
}

type ascDraftsRef struct {
	repo int64
	cs   string
}

// ascDraftsParseDiscarded reads the "<repo id>.<hex commit id>" the discard redirects with. A bad value is no banner.
func ascDraftsParseDiscarded(s string) ascDraftsRef {
	id, cs, ok := strings.Cut(s, ".")
	if !ok {
		return ascDraftsRef{}
	}
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n <= 0 || ascDraftsCommit(cs) == nil {
		return ascDraftsRef{}
	}
	return ascDraftsRef{repo: n, cs: cs}
}

// ascDraftsCommit decodes a lower-case hex commit id of 1 to ascDraftsMaxIDBytes bytes; anything else is nil.
func ascDraftsCommit(s string) []byte {
	if s == "" || len(s) > 2*ascDraftsMaxIDBytes || strings.ToLower(s) != s {
		return nil
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil
	}
	return b
}

// errAscDraftGone says the draft is not in the person's workspace on the hub.
var errAscDraftGone = errors.New("the draft is not on the hub")

// ascDraftsSetHidden hides or shows draft cs of owner's workspace in repository name. It reads the workspace for its
// version and the draft's state, then writes; a write that lost a race with another device is tried again.
func ascDraftsSetHidden(ctx gocontext.Context, conn grpc.ClientConnInterface, name, owner string, cs []byte, hide bool) error {
	client := ascv1.NewCloudClient(conn)
	ref := &ascv1.RepoRef{Name: name, IdMode: ascv1.IdMode_ID_MODE_GIT}
	for try := 1; ; try++ {
		ws, err := client.GetWorkspace(ctx, &ascv1.GetWorkspaceRequest{Repo: ref, Owner: owner, IncludeHidden: true})
		if err != nil {
			return err
		}
		var head *ascv1.CloudHead
		for _, h := range ws.GetHeads() {
			if bytes.Equal(h.GetCs(), cs) {
				head = h
				break
			}
		}
		if head == nil {
			return errAscDraftGone
		}
		if (head.GetHiddenAt() != 0) == hide {
			return nil // already as asked: a second click or another tab
		}
		req := &ascv1.UpdateWorkspaceRequest{Repo: ref, BaseVersion: ws.GetVersion()}
		if hide {
			req.Hide = [][]byte{cs}
		} else {
			req.Unhide = [][]byte{cs}
		}
		_, err = client.UpdateWorkspace(ctx, req)
		if status.Code(err) == codes.Aborted && try < ascDraftsStaleTries {
			continue
		}
		return err
	}
}

// AscDraftsDiscard serves `POST /-/asc/drafts/discard`.
func AscDraftsDiscard(ctx *context.Context) { ascDraftsAct(ctx, true) }

// AscDraftsUndo serves `POST /-/asc/drafts/undo`.
func AscDraftsUndo(ctx *context.Context) { ascDraftsAct(ctx, false) }

func ascDraftsAct(ctx *context.Context, hide bool) {
	if !setting.Asc.Enabled || ctx.Doer == nil {
		ctx.NotFound("AscDraftsAct", nil)
		return
	}
	back := setting.AppSubURL + "/-/asc/drafts"
	gone := func() {
		ctx.Flash.Error(ctx.Tr("asc_work.drafts_act_gone"))
		ctx.Redirect(back)
	}
	cs := ascDraftsCommit(ctx.FormString("cs"))
	if cs == nil {
		gone()
		return
	}
	repo, err := repo_model.GetRepositoryByID(ctx, ctx.FormInt64("repo"))
	if err != nil {
		gone()
		return
	}
	name, err := asctrust.RepoName(repo)
	if err != nil {
		gone()
		return
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallRepoRead, repo)
	if err != nil {
		if !errors.Is(err, asctrust.ErrForbidden) {
			log.Error("asc drafts: the claim for repository %d: %v", repo.ID, err)
		}
		gone()
		return
	}
	defer release()
	conn, err := ascDraftsConn()
	if err == nil {
		c, cancel := gocontext.WithTimeout(callCtx, ascDraftsTimeout)
		err = ascDraftsSetHidden(c, conn, name, ctx.Doer.Name, cs, hide)
		cancel()
	}
	switch {
	case errors.Is(err, errAscDraftGone):
		gone()
	case err != nil:
		log.Error("asc drafts: changing a draft of repository %d failed: %s", repo.ID, status.Code(err))
		ctx.Flash.Error(ctx.Tr("asc_work.drafts_act_failed"))
		ctx.Redirect(back)
	case hide:
		ctx.Redirect(back + "?discarded=" + strconv.FormatInt(repo.ID, 10) + "." + hex.EncodeToString(cs))
	default:
		ctx.Flash.Success(ctx.Tr("asc_work.drafts_restored"))
		ctx.Redirect(back)
	}
}
