// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	user_model "forgejo.org/models/user"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/context"
)

// The draft of a new change (board NewChange): the title and the description of the open-change form are saved while the
// person writes (on leaving a field, after a one second pause) or with "Save as draft", and come back when the same
// comparison is opened again. A draft is the person's own text, so it is kept in their user settings (no table of its
// own) under one key. The key holds a few drafts, each of a bounded size, and the oldest goes when there are more; the
// whole value is therefore at most about 330 KiB. A draft is removed when the change is opened or when it is discarded.
//
//	POST /{owner}/{repo}/asc/newchange_draft   id, title, content (save) or id, discard=1 (remove)
//
// The id names the comparison. The page makes it from the repository and the compare path and puts it in the form; a
// caller who sends another id only writes into their own settings, within the same limits. Two tabs that save at the
// same moment can lose one of the two drafts: the key is read and written whole, and a draft is advice, not a record.

const (
	ascNewChangeDraftKey = "asc.newchange_drafts"
	// ascNewChangeDraftMax is how many comparisons keep a draft per person.
	ascNewChangeDraftMax = 10
	// ascNewChangeDraftTitle and ascNewChangeDraftBody are the limits of one draft in bytes. The title limit is that of a title.
	ascNewChangeDraftTitle = 255
	ascNewChangeDraftBody  = 32 << 10
	// ascNewChangeDraftForm bounds what the handler reads: the limits above and the other fields.
	ascNewChangeDraftForm = ascNewChangeDraftBody + ascNewChangeDraftTitle + 2<<10
	// ascNewChangeDraftValue bounds the stored value that is read back.
	ascNewChangeDraftValue = ascNewChangeDraftMax * (ascNewChangeDraftBody + ascNewChangeDraftTitle + 256) * 2
)

var ascNewChangeDraftID = regexp.MustCompile(`^[0-9a-f]{24}$`)

// ascNewChangeDraft is one saved draft.
type ascNewChangeDraft struct {
	Title string `json:"t"`
	Body  string `json:"c"`
	At    int64  `json:"u"`
}

// ascNewChangeDrafts are the drafts of one person by comparison id.
type ascNewChangeDrafts map[string]ascNewChangeDraft

// ascNewChangeDraftIDOf names a comparison: the repository and the compare path "base...head".
func ascNewChangeDraftIDOf(repoID int64, compare string) string {
	sum := sha256.Sum256([]byte(strconv.FormatInt(repoID, 10) + "\x00" + compare))
	return hex.EncodeToString(sum[:12])
}

// put stores a draft and drops the oldest ones above the limit.
func (d ascNewChangeDrafts) put(id string, e ascNewChangeDraft) {
	d[id] = e
	if len(d) <= ascNewChangeDraftMax {
		return
	}
	ids := make([]string, 0, len(d))
	for k := range d {
		ids = append(ids, k)
	}
	sort.Slice(ids, func(i, j int) bool {
		if d[ids[i]].At != d[ids[j]].At {
			return d[ids[i]].At > d[ids[j]].At
		}
		return ids[i] < ids[j]
	})
	for _, k := range ids[ascNewChangeDraftMax:] {
		delete(d, k)
	}
}

// parseAscNewChangeDrafts reads a stored value. A value that cannot be read is no drafts; a draft over the limits is dropped.
func parseAscNewChangeDrafts(raw string) ascNewChangeDrafts {
	out := ascNewChangeDrafts{}
	if raw == "" || len(raw) > ascNewChangeDraftValue {
		return out
	}
	var in ascNewChangeDrafts
	if json.Unmarshal([]byte(raw), &in) != nil {
		return out
	}
	for id, e := range in {
		if ascNewChangeDraftID.MatchString(id) && len(e.Title) <= ascNewChangeDraftTitle && len(e.Body) <= ascNewChangeDraftBody {
			out[id] = e
		}
	}
	return out
}

func ascNewChangeDraftsOf(ctx *context.Context, userID int64) ascNewChangeDrafts {
	raw, err := user_model.GetUserSetting(ctx, userID, ascNewChangeDraftKey)
	if err != nil {
		log.Warn("asc new change drafts of user %d: %v", userID, err)
		return ascNewChangeDrafts{}
	}
	return parseAscNewChangeDrafts(raw)
}

func saveAscNewChangeDrafts(ctx *context.Context, userID int64, d ascNewChangeDrafts) error {
	if len(d) == 0 {
		return user_model.DeleteUserSetting(ctx, userID, ascNewChangeDraftKey)
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return err
	}
	return user_model.SetUserSetting(ctx, userID, ascNewChangeDraftKey, string(raw))
}

// clearAscNewChangeDraft removes the draft of a comparison once its change is open. It is best effort.
func clearAscNewChangeDraft(ctx *context.Context, compare string) {
	if ctx.Doer == nil || ctx.Repo == nil || ctx.Repo.Repository == nil || ctx.Repo.Repository.GetStorageBackend() != string(gitrepo.StorageBackendAsc) {
		return
	}
	id := ascNewChangeDraftIDOf(ctx.Repo.Repository.ID, compare)
	d := ascNewChangeDraftsOf(ctx, ctx.Doer.ID)
	if _, ok := d[id]; !ok {
		return
	}
	delete(d, id)
	if err := saveAscNewChangeDrafts(ctx, ctx.Doer.ID, d); err != nil {
		log.Warn("asc new change draft of user %d: %v", ctx.Doer.ID, err)
	}
}

// AscNewChangeDraftState is the template data of the form: where to save, the id and the restored note.
type AscNewChangeDraftState struct {
	URL, ID  string
	Restored bool
	SavedAt  time.Time
}

// prepareAscNewChangeDraft gives the form of a new change its draft: the saved title and description fill the form when
// the address did not bring its own, and the form is told where to save. It does nothing for a person who is not signed in
// or a repository that is not kept in asc.
func prepareAscNewChangeDraft(ctx *context.Context, compare string) {
	if ctx.Doer == nil || !setting.Asc.Enabled || ctx.Repo.Repository.GetStorageBackend() != string(gitrepo.StorageBackendAsc) {
		return
	}
	id := ascNewChangeDraftIDOf(ctx.Repo.Repository.ID, compare)
	st := &AscNewChangeDraftState{URL: ctx.Repo.RepoLink + "/asc/newchange_draft", ID: id}
	ctx.Data["AscNewChangeDraft"] = st
	d, ok := ascNewChangeDraftsOf(ctx, ctx.Doer.ID)[id]
	if !ok || (d.Title == "" && d.Body == "") {
		return
	}
	if q, _ := ctx.Data["TitleQuery"].(string); q == "" && d.Title != "" {
		ctx.Data["TitleQuery"] = d.Title
	}
	if q, _ := ctx.Data["BodyQuery"].(string); q == "" && d.Body != "" {
		ctx.Data["BodyQuery"] = d.Body
	}
	st.Restored, st.SavedAt = true, time.Unix(d.At, 0).UTC()
}

// AscNewChangeDraftSave serves POST /{owner}/{repo}/asc/newchange_draft.
func AscNewChangeDraftSave(ctx *context.Context) {
	ctx.Resp.Header().Set("Cache-Control", "private, no-store")
	if !setting.Asc.Enabled || ctx.Repo.Repository.GetStorageBackend() != string(gitrepo.StorageBackendAsc) {
		ctx.NotFound("not an asc repository", nil)
		return
	}
	ctx.Req.Body = http.MaxBytesReader(ctx.Resp, ctx.Req.Body, ascNewChangeDraftForm)
	id := ctx.Req.PostFormValue("id")
	if !ascNewChangeDraftID.MatchString(id) {
		ctx.JSON(http.StatusBadRequest, map[string]string{"error": "id"})
		return
	}
	drafts := ascNewChangeDraftsOf(ctx, ctx.Doer.ID)
	title := strings.ReplaceAll(ctx.Req.PostFormValue("title"), "\r", "")
	body := strings.ReplaceAll(ctx.Req.PostFormValue("content"), "\r\n", "\n")
	if ctx.Req.PostFormValue("discard") != "" {
		title, body = "", ""
	}
	if len(title) > ascNewChangeDraftTitle || len(body) > ascNewChangeDraftBody || !utf8.ValidString(title) || !utf8.ValidString(body) || strings.ContainsAny(title, "\x00\n") || strings.Contains(body, "\x00") {
		ctx.JSON(http.StatusRequestEntityTooLarge, map[string]string{"error": "size"})
		return
	}
	saved := strings.TrimSpace(title) != "" || strings.TrimSpace(body) != ""
	now := time.Now().Unix()
	if saved {
		drafts.put(id, ascNewChangeDraft{Title: title, Body: body, At: now})
	} else {
		// nothing written, or discarded: keep no draft
		delete(drafts, id)
	}
	if err := saveAscNewChangeDrafts(ctx, ctx.Doer.ID, drafts); err != nil {
		ctx.ServerError("asc new change draft", err)
		return
	}
	ctx.JSON(http.StatusOK, map[string]any{"saved": saved, "at": now})
}
