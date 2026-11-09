// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package ascagent serves the API that agents call with their agent token (an `ascat_` secret that asc issued), not
// with an account of the hub: it has its own route under /api/asc/agents, outside the account authentication of
// /api/v1, so an agent token is never taken for an access token or a password.
//
//	POST /api/asc/agents/repos/{owner}/{repo}/reports    send an abuse report on content of the repository
//
// The body is JSON (services/ascagentreport.Request), at most 8 KiB. Answers: 201 with {id, duplicate:false} for a
// new report, 200 with {id, duplicate:true} when the token already has an open report on the content, 400 for a
// malformed report, 401 (with WWW-Authenticate) for any token that is not usable for this repository (also when the
// repository does not exist), 404 when the content is not found or the feature is off, 429 when the token has too many open reports, 503 when asc
// cannot be asked.
package ascagent

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"forgejo.org/models/moderation"
	"forgejo.org/modules/log"
	"forgejo.org/modules/web"
	"forgejo.org/services/ascagentreport"
	"forgejo.org/services/context"
)

// maxBody bounds the JSON body of a report.
const maxBody = 8 << 10

// Routes are the routes of the agent API.
func Routes() *web.Route {
	m := web.NewRoute()
	m.Use(noSniff, context.APIContexter())
	m.Post("/repos/{username}/{reponame}/reports", Report)
	return m
}

func noSniff(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

// bearer reads the agent token of the request; empty when there is none.
func bearer(r *http.Request) string {
	fields := strings.Fields(r.Header.Get("Authorization"))
	if len(fields) != 2 || !strings.EqualFold(fields[0], "bearer") {
		return ""
	}
	return fields[1]
}

func fail(ctx *context.APIContext, status int, msg string) {
	if status == http.StatusUnauthorized {
		ctx.Resp.Header().Set("WWW-Authenticate", `Bearer realm="asc agent"`)
	}
	ctx.JSON(status, map[string]string{"message": msg})
}

// Report serves POST /api/asc/agents/repos/{owner}/{repo}/reports.
func Report(ctx *context.APIContext) {
	ctx.Resp.Header().Set("Cache-Control", "no-store")
	secret := bearer(ctx.Req)
	if !ascagentreport.LooksLikeSecret(secret) {
		fail(ctx, http.StatusUnauthorized, ascagentreport.ErrToken.Error())
		return
	}
	if !strings.HasPrefix(ctx.Req.Header.Get("Content-Type"), "application/json") {
		fail(ctx, http.StatusUnsupportedMediaType, "the body must be application/json")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(ctx.Resp, ctx.Req.Body, maxBody))
	if err != nil {
		fail(ctx, http.StatusRequestEntityTooLarge, "the body is too large")
		return
	}
	var req ascagentreport.Request
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		fail(ctx, http.StatusBadRequest, ascagentreport.ErrInvalid.Error())
		return
	}
	owner, name := ctx.Params("username"), ctx.Params("reponame")
	res, err := ascagentreport.Report(ctx, owner, name, secret, &req)
	switch {
	case err == nil:
	case errors.Is(err, ascagentreport.ErrToken):
		fail(ctx, http.StatusUnauthorized, err.Error())
		return
	case errors.Is(err, ascagentreport.ErrDisabled), errors.Is(err, ascagentreport.ErrNotFound):
		fail(ctx, http.StatusNotFound, ascagentreport.ErrNotFound.Error())
		return
	case errors.Is(err, ascagentreport.ErrInvalid):
		fail(ctx, http.StatusBadRequest, err.Error())
		return
	case errors.Is(err, moderation.ErrAgentReportLimit):
		fail(ctx, http.StatusTooManyRequests, "this agent token has too many open reports")
		return
	case errors.Is(err, ascagentreport.ErrUnavailable):
		fail(ctx, http.StatusServiceUnavailable, err.Error())
		return
	default:
		ctx.InternalServerError(err)
		return
	}
	if res.Duplicate {
		ctx.JSON(http.StatusOK, res)
		return
	}
	log.Info("moderation: agent report %d on %s/%s", res.ID, owner, name)
	ctx.JSON(http.StatusCreated, res)
}
