// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package private

import (
	"net/http"
	"strings"

	user_model "forgejo.org/models/user"
	"forgejo.org/modules/private"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/structs"
	"forgejo.org/services/context"
)

// ascAgentProfileRequest is the body of `POST /api/internal/asc/agent-profile`. asc serve asks it when an agent run
// needs to know who a person is (the author of a change it works on, a reviewer it writes to).
type ascAgentProfileRequest struct {
	Login string `json:"login"`
}

// ascAgentProfileResponse carries the profile fields the person lets agents read. When the person switched
// "Allow agents to read my profile" off, has a profile that is not public, is not active, or does not exist, the answer
// is the same: Allowed false and nothing else. The call cannot be used to find out who exists.
type ascAgentProfileResponse struct {
	Allowed     bool   `json:"allowed"`
	Login       string `json:"login,omitempty"`
	FullName    string `json:"full_name,omitempty"`
	Description string `json:"description,omitempty"`
	Location    string `json:"location,omitempty"`
	Website     string `json:"website,omitempty"`
	Pronouns    string `json:"pronouns,omitempty"`
}

// AscAgentProfile answers `POST /api/internal/asc/agent-profile`. Email is never part of it, however the person set
// their email privacy: an agent that must write to a person uses the hub.
func AscAgentProfile(ctx *context.PrivateContext) {
	if !setting.Asc.Enabled {
		ctx.Error(http.StatusNotFound)
		return
	}
	req := &ascAgentProfileRequest{}
	if err := decodeAscStrict(ctx, req); err != nil {
		ctx.JSON(http.StatusBadRequest, private.Response{UserMsg: err.Error()})
		return
	}
	if req.Login == "" || len(req.Login) > private.AscAuthzMaxName || strings.ContainsAny(req.Login, "/ \\") {
		ctx.JSON(http.StatusBadRequest, private.Response{UserMsg: "invalid request"})
		return
	}
	u, err := user_model.GetUserByName(ctx, req.Login)
	if err != nil || !u.IsActive || !u.IsIndividual() || u.Visibility != structs.VisibleTypePublic || !user_model.AscAgentsMayReadProfile(ctx, u.ID) {
		ctx.JSON(http.StatusOK, ascAgentProfileResponse{})
		return
	}
	resp := ascAgentProfileResponse{Allowed: true, Login: u.Name, FullName: u.FullName, Description: u.Description, Location: u.Location, Website: u.Website}
	if !u.KeepPronounsPrivate {
		resp.Pronouns = u.Pronouns
	}
	ctx.JSON(http.StatusOK, resp)
}
