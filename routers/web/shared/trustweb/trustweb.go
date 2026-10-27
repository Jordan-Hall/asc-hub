// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package trustweb serves the parts of the trust pages that the repository, organisation, user and
// administrator levels share: the audit log and the helpers every trust handler uses.
//
// # Trust
//
// A level is built by code that has already checked, from Forgejo's own permissions, that the signed-in person may
// open the page (a repository administrator, an organisation owner, a site administrator). Nothing in a request can
// change the scope of a call: it comes from the route. The call itself is made as the person (asctrust.UserCall),
// so asc decides again and records the person in the audit log.
//
// # Output
//
// Every page of the audit log is sent with Cache-Control: no-store. A text from asc is cleaned in services/asctrust
// and escaped by the template. A failure shows one fixed sentence; the text of the failure goes to the log, which
// never receives a credential.
package trustweb

import (
	"errors"
	"net/http"
	"strings"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// NoStore marks the response as not to be kept by a browser or a proxy.
func NoStore(ctx *context.Context) {
	ctx.Resp.Header().Set("Cache-Control", "no-store")
}

// Failure is what a page shows for a failed action: a status and one fixed sentence.
type Failure struct {
	Status  int
	Message string
}

// FailureOf reduces err to a Failure. A local check (ErrBadInput) shows its own sentence, which names no input.
// A failure of asc shows the sentence of its kind and code. Anything else is a generic failure, logged under op.
func FailureOf(op string, err error) Failure {
	switch {
	case errors.Is(err, asctrust.ErrBadInput):
		msg := strings.TrimPrefix(err.Error(), asctrust.ErrBadInput.Error()+": ")
		return Failure{Status: http.StatusBadRequest, Message: asctrust.CleanText(msg, 200)}
	case errors.Is(err, asctrust.ErrDisabled):
		return Failure{Status: http.StatusNotFound, Message: "This page is not available for this repository."}
	case errors.Is(err, asctrust.ErrBadAnswer):
		log.Error("asc trust %s: %v", op, err)
		return Failure{Status: http.StatusBadGateway, Message: "The ASC server sent an answer that could not be trusted, so nothing is shown."}
	case errors.Is(err, asctrust.ErrNoDoer):
		return Failure{Status: http.StatusForbidden, Message: "Sign in to do this."}
	}
	e := asctrust.Classify(err)
	if e.Kind == asctrust.KindOther || e.Kind == asctrust.KindUnavailable {
		// Only the kind and the code are logged: the text of the failure may name internals.
		log.Warn("asc trust %s: %s", op, e.Error())
	}
	return Failure{Status: e.Status(), Message: e.Message()}
}

// AdminRepo is the guard of the settings pages of a repository. It returns the repository and its asc name, or
// writes the refusal and returns false: 404 when [asc] is off or the repository is not stored in asc, 403 when the
// signed-in person does not administer it. The route is behind the repository admin middleware too; this is the
// second check, and it fails closed.
func AdminRepo(ctx *context.Context) (*repo_model.Repository, string, bool) {
	repo := ctx.Repo.Repository
	if !setting.Asc.Enabled || repo == nil {
		ctx.NotFound("asc trust", nil)
		return nil, "", false
	}
	name, err := asctrust.RepoName(repo)
	if err != nil {
		ctx.NotFound("asc trust", nil)
		return nil, "", false
	}
	if !ctx.IsSigned || ctx.Doer == nil || !ctx.Repo.IsAdmin() {
		ctx.Error(http.StatusForbidden)
		return nil, "", false
	}
	return repo, name, true
}

// ReaderRepo is the guard of the pages everyone who may read the code can open (the checks). The person may be
// anonymous on a public repository.
func ReaderRepo(ctx *context.Context) (*repo_model.Repository, string, bool) {
	repo := ctx.Repo.Repository
	if !setting.Asc.Enabled || repo == nil {
		ctx.NotFound("asc trust", nil)
		return nil, "", false
	}
	name, err := asctrust.RepoName(repo)
	if err != nil || !ctx.Repo.CanRead(unit.TypeCode) {
		ctx.NotFound("asc trust", nil)
		return nil, "", false
	}
	return repo, name, true
}

// WriterRepo is the guard of the actions on the checks page: a signed-in person who may write to the code.
func WriterRepo(ctx *context.Context) (*repo_model.Repository, string, bool) {
	repo, name, ok := ReaderRepo(ctx)
	if !ok {
		return nil, "", false
	}
	if !ctx.IsSigned || ctx.Doer == nil || !ctx.Repo.CanWrite(unit.TypeCode) {
		ctx.Error(http.StatusForbidden)
		return nil, "", false
	}
	return repo, name, true
}

// ParseForm reads the form of a POST under a size limit. The limit is the only thing that bounds a textarea, so it
// is checked before the body is read. On a form that is too large or broken it writes the refusal and returns false.
func ParseForm(ctx *context.Context, max int64) bool {
	ctx.Req.Body = http.MaxBytesReader(ctx.Resp, ctx.Req.Body, max)
	if err := ctx.Req.ParseForm(); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			ctx.Error(http.StatusRequestEntityTooLarge, "The form is too large.")
		} else {
			ctx.Error(http.StatusBadRequest, "The form could not be read.")
		}
		return false
	}
	return true
}

// Lines turns the line breaks of a browser textarea into "\n".
func Lines(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}
