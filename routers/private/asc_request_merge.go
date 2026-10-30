// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package private

import (
	"errors"
	"net/http"
	"strings"

	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/modules/private"
	"forgejo.org/modules/setting"
	"forgejo.org/services/automerge"
	"forgejo.org/services/context"
	pull_service "forgejo.org/services/pull"
)

// Limits of an ascRequestMergeRequest.
const (
	ascRequestMergeMaxRepo   = 2*private.AscAuthzMaxName + 1
	ascRequestMergeMaxReason = 300
	// ascRequestMergeMaxPrefix bounds the prefix asc names (asc's stack bound is 256).
	ascRequestMergeMaxPrefix = 256
)

// States of an ascRequestMergeResponse (asc.v1 RequestMergeResponse.State).
const (
	ascRequestMergeQueued  = "queued"
	ascRequestMergeLanded  = "landed"
	ascRequestMergeRefused = "refused"
)

// ascRequestMergeRequest is the body of `POST /api/internal/asc/request-merge`. asc serve sends it when a client
// calls Changes.RequestMerge (SPEC section 20.5): the user is the one asc authenticated, and Forgejo decides
// what that user may do.
//
// asc serve posts it to /api/internal/asc/request-merge/{owner}/{repo} (the repository is the path), and sends
// the name of the user and the prefix it saw as well. Repo may be given in the body of a caller that posts to the
// path without the repository; when both are given they have to agree. The prefix is asc's reading; the hub
// finds the prefix again from its own records and does not use it.
type ascRequestMergeRequest struct {
	Repo     string                  `json:"repo,omitempty"` // owner/name
	Change   string                  `json:"change"`         // 32 letters k-z: the top of the prefix 1..N
	Now      bool                    `json:"now"`            // land at once instead of enqueueing
	UserID   int64                   `json:"user_id"`        // the Forgejo user who asked
	UserName string                  `json:"user_name,omitempty"`
	Prefix   []ascRequestMergePrefix `json:"prefix,omitempty"`
}

// ascRequestMergePrefix is one change of the prefix as asc sent it.
type ascRequestMergePrefix struct {
	Change string `json:"change"`
	Git    string `json:"git"`
}

// ascRequestMergeResponse is the answer. A refusal is a 200 with state "refused"; a malformed request is 400.
type ascRequestMergeResponse struct {
	State   string   `json:"state"`
	Pending []string `json:"pending"` // changes of the prefix that have not landed
	Reason  string   `json:"reason,omitempty"`
}

// ascRequestMergeBacked reports whether the repository is stored in asc. Tests replace it.
var ascRequestMergeBacked = func(repo *repo_model.Repository) (bool, error) { return automerge.AscQueueBackedBy(repo) }

// AscRequestMerge answers `POST /api/internal/asc/request-merge`: enable merge-when-ready for the prefix
// bottom..change, or (now) land it at once. The pull request is found by (repository, change). The user is
// checked as an ordinary merger: active, may merge every pull request of the prefix.
func AscRequestMerge(ctx *context.PrivateContext) {
	if !setting.Asc.Enabled {
		ctx.Error(http.StatusNotFound)
		return
	}
	req := &ascRequestMergeRequest{}
	if err := decodeAscStrict(ctx, req); err != nil {
		ctx.JSON(http.StatusBadRequest, private.Response{UserMsg: err.Error()})
		return
	}
	if pathOwner, pathName := ctx.Params(":owner"), ctx.Params(":repo"); pathOwner != "" || pathName != "" {
		if req.Repo != "" && req.Repo != pathOwner+"/"+pathName {
			ctx.JSON(http.StatusBadRequest, private.Response{UserMsg: "invalid request"})
			return
		}
		req.Repo = pathOwner + "/" + pathName
	}
	owner, name, ok := strings.Cut(req.Repo, "/")
	if len(req.Repo) > ascRequestMergeMaxRepo || !ok || owner == "" || name == "" || strings.Contains(name, "/") ||
		!issues_model.IsAscChange(req.Change) || req.UserID <= 0 || len(req.Prefix) > ascRequestMergeMaxPrefix {
		ctx.JSON(http.StatusBadRequest, private.Response{UserMsg: "invalid request"})
		return
	}

	resp, err := ascRequestMerge(ctx, owner, name, req)
	if err != nil {
		log.Error("asc request-merge: %v", err)
		ctx.JSON(http.StatusInternalServerError, private.Response{Err: "internal error"})
		return
	}
	ctx.JSON(http.StatusOK, resp)
}

func refuse(reason string) *ascRequestMergeResponse {
	if len(reason) > ascRequestMergeMaxReason {
		reason = reason[:ascRequestMergeMaxReason]
	}
	return &ascRequestMergeResponse{State: ascRequestMergeRefused, Pending: []string{}, Reason: reason}
}

func ascRequestMerge(ctx *context.PrivateContext, owner, name string, req *ascRequestMergeRequest) (*ascRequestMergeResponse, error) {
	// A missing repository, a missing user and a missing pull request all look the same to the caller.
	repo, err := repo_model.GetRepositoryByOwnerAndName(ctx, owner, name)
	if err != nil {
		if repo_model.IsErrRepoNotExist(err) {
			return refuse("no such pull request"), nil
		}
		return nil, err
	}
	doer, err := user_model.GetUserByID(ctx, req.UserID)
	if err != nil {
		if user_model.IsErrUserNotExist(err) {
			return refuse("not allowed"), nil
		}
		return nil, err
	}
	if !doer.IsActive || doer.ProhibitLogin {
		return refuse("not allowed"), nil
	}
	pr, err := issues_model.GetOpenPullRequestByAscChange(ctx, repo.ID, req.Change)
	if err != nil {
		if issues_model.IsErrPullRequestNotExist(err) {
			return refuse("no such pull request"), nil
		}
		return nil, err
	}
	if ok, err := ascRequestMergeBacked(repo); err != nil {
		return nil, err
	} else if !ok {
		return refuse("the repository is not stored in asc"), nil
	}

	if req.Now {
		_, err := automerge.AscQueueLandNow(ctx, doer, pr)
		if err != nil {
			if reason, user := ascRequestMergeReason(err); user {
				return refuse(reason), nil
			}
			return nil, err
		}
		return &ascRequestMergeResponse{State: ascRequestMergeLanded, Pending: []string{}}, nil
	}

	res, err := automerge.AscQueueEnqueue(ctx, doer, pr)
	if err != nil {
		if reason, user := ascRequestMergeReason(err); user {
			return refuse(reason), nil
		}
		return nil, err
	}
	pending := make([]string, 0, len(res.Queued))
	for _, e := range res.Queued {
		pending = append(pending, e.AscChange)
	}
	return &ascRequestMergeResponse{State: ascRequestMergeQueued, Pending: pending}, nil
}

// ascRequestMergeReason maps the caller's mistakes to the reason that is shown. Anything else is a server
// error whose text is not sent.
func ascRequestMergeReason(err error) (string, bool) {
	switch {
	case errors.Is(err, automerge.ErrAscQueueNotAllowed), errors.Is(err, pull_service.ErrAscStackNotAllowed):
		return "not allowed to merge this stack", true
	case errors.Is(err, automerge.ErrAscQueuePaused):
		return "the merge queue of the branch is paused", true
	case errors.Is(err, automerge.ErrAscQueueBusy):
		return "a land is in flight on the branch; try again", true
	case errors.Is(err, automerge.ErrAscQueueDisabled), errors.Is(err, pull_service.ErrAscStackDisabled):
		return "the repository is not stored in asc", true
	case errors.Is(err, automerge.ErrAscQueueNotQueueable), errors.Is(err, pull_service.ErrAscStackNotMergable),
		errors.Is(err, pull_service.ErrAscStackNotStacked), errors.Is(err, pull_service.ErrAscStackHeadMoved):
		return err.Error(), true
	}
	return "", false
}
