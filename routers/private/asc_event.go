// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package private

import (
	"net/http"
	"strings"
	"time"

	"forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/modules/private"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/translation"
	"forgejo.org/services/ascnotify"
	"forgejo.org/services/ascpush"
	"forgejo.org/services/context"
	"forgejo.org/services/mailer"
)

// Limits of an ascEventRequest.
const (
	ascEventMaxUsers  = 50
	ascEventMaxTitle  = 200
	ascEventMaxDetail = 500
)

// ascEventRequest is the body of `POST /api/internal/asc/event`. asc serve sends it when something happens that a
// person has to know about: an agent paused on a question, a run finished, checks failed, a restack hit a conflict,
// the merge queue merged or dropped a change, a policy changed. Like every internal call it carries the internal
// token; the hub decides who may be told and how.
type ascEventRequest struct {
	Event  string   `json:"event"`  // one of ascnotify.EmailEvents
	Repo   string   `json:"repo"`   // owner/name
	Users  []string `json:"users"`  // logins of the people the event is for
	Title  string   `json:"title"`  // one line
	Detail string   `json:"detail"` // a few words more
	Path   string   `json:"path"`   // where to look, a path of this hub such as /acme/payments/pulls/3
}

// ascEventResponse tells how many people were told. It never says who was skipped or why, so the call cannot be used to
// find out who has access to a repository.
type ascEventResponse struct {
	Notified int `json:"notified"`
}

// AscEvent answers `POST /api/internal/asc/event`: it records the event for each person who may read the repository
// and sends them an email when they chose to get email for the event.
func AscEvent(ctx *context.PrivateContext) {
	if !setting.Asc.Enabled {
		ctx.Error(http.StatusNotFound)
		return
	}
	req := &ascEventRequest{}
	if err := decodeAscStrict(ctx, req); err != nil {
		ctx.JSON(http.StatusBadRequest, private.Response{UserMsg: err.Error()})
		return
	}
	owner, name, ok := strings.Cut(req.Repo, "/")
	if !ascnotify.IsIngestEvent(req.Event) || !ok || owner == "" || name == "" || strings.Contains(name, "/") ||
		len(req.Repo) > ascRequestMergeMaxRepo || len(req.Users) == 0 || len(req.Users) > ascEventMaxUsers ||
		req.Title == "" || len(req.Title) > ascEventMaxTitle || len(req.Detail) > ascEventMaxDetail ||
		(req.Path != "" && (!strings.HasPrefix(req.Path, "/") || strings.HasPrefix(req.Path, "//") || len(req.Path) > 300 || strings.ContainsAny(req.Path, " \r\n\\"))) ||
		strings.ContainsAny(req.Title+req.Detail, "\r\n") {
		ctx.JSON(http.StatusBadRequest, private.Response{UserMsg: "invalid request"})
		return
	}
	repo, err := repo_model.GetRepositoryByOwnerAndName(ctx, owner, name)
	if err != nil {
		if repo_model.IsErrRepoNotExist(err) {
			ctx.JSON(http.StatusOK, ascEventResponse{})
			return
		}
		log.Error("asc event: repository %q: %v", req.Repo, err)
		ctx.JSON(http.StatusInternalServerError, private.Response{Err: "internal error"})
		return
	}
	notified := 0
	seen := map[int64]bool{}
	for _, login := range req.Users {
		u, err := user_model.GetUserByName(ctx, login)
		if err != nil || !u.IsActive || u.IsOrganization() || seen[u.ID] {
			continue
		}
		seen[u.ID] = true
		perm, err := access.GetUserRepoPermission(ctx, repo, u)
		if err != nil || !perm.HasAccess() {
			continue // a person who cannot read the repository is told nothing about it
		}
		now := time.Now()
		prefs := ascnotify.Load(ctx, u.ID)
		emailed := false
		// with the daily summary on, the email is the summary and not one mail for each event
		if !prefs.Digest && setting.MailService != nil && u.EmailNotificationsPreference != user_model.EmailNotificationsDisabled && ascnotify.EmailAllowed(ctx, u.ID, req.Event, now) {
			sendAscEventMail(u, repo, req)
			emailed = true
		}
		ascpush.Notify(ctx, u.ID, req.Event, ascpush.Message{Title: req.Title, Body: repo.FullName(), Path: ascEventPath(repo, req), Tag: req.Event + "-" + repo.FullName()})
		if err := user_model.RecordAscNotification(ctx, u.ID, repo.ID, req.Event, req.Title, req.Path, emailed); err != nil {
			log.Error("asc event: recording for user %d: %v", u.ID, err)
			continue
		}
		notified++
	}
	ctx.JSON(http.StatusOK, ascEventResponse{Notified: notified})
}

// sendAscEventMail writes the email of an event in the language of the person. The text is only what asc sent plus a
// link of this hub; nothing is looked up that the person could not already read.
func sendAscEventMail(u *user_model.User, repo *repo_model.Repository, req *ascEventRequest) {
	l := translation.NewLocale(u.Language)
	label := l.TrString("asc_work.ev_" + req.Event)
	subject := "[" + repo.FullName() + "] " + label + ": " + req.Title
	body := label + "\n" + req.Title + "\n"
	if req.Detail != "" {
		body += req.Detail + "\n"
	}
	link := repo.HTMLURL()
	if req.Path != "" {
		link = strings.TrimSuffix(setting.AppURL, "/") + req.Path
	}
	body += "\n" + link + "\n\n" + l.TrString("asc_work.ev_mail_footer") + " " + strings.TrimSuffix(setting.AppURL, "/") + "/user/settings/notifications\n"
	mailer.SendAsync(mailer.NewMessage(u.EmailTo(), subject, body))
}

// ascEventPath is where a click on a notification of the event goes: the path asc sent, or the repository.
func ascEventPath(repo *repo_model.Repository, req *ascEventRequest) string {
	if req.Path != "" {
		return req.Path
	}
	return repo.Link()
}
