// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"errors"
	"net/http"
	"time"

	"forgejo.org/modules/setting"
	api "forgejo.org/modules/structs"
	"forgejo.org/modules/web"
	"forgejo.org/services/ascdoctor"
	"forgejo.org/services/context"
)

// ReportAscDoctor keeps the machine report of `asc doctor --report`, which the health check of Get started shows.
func ReportAscDoctor(ctx *context.APIContext) {
	// swagger:operation POST /user/asc/doctor user userCurrentReportAscDoctor
	// ---
	// summary: Report the checks of `asc doctor` on the machine of the authenticated user
	// description: Keeps the report as the last one of the user; the health check of Get started shows it to them
	//   only. The report holds the name and level of each check and the versions, no path and no detail.
	// consumes:
	// - application/json
	// parameters:
	// - name: body
	//   in: body
	//   required: true
	//   schema:
	//     "$ref": "#/definitions/AscDoctorReport"
	// responses:
	//   "204":
	//     "$ref": "#/responses/empty"
	//   "401":
	//     "$ref": "#/responses/unauthorized"
	//   "403":
	//     "$ref": "#/responses/forbidden"
	//   "404":
	//     "$ref": "#/responses/notFound"
	//   "422":
	//     "$ref": "#/responses/validationError"
	if !setting.Asc.Enabled {
		ctx.NotFound()
		return
	}
	form := web.GetForm(ctx).(*api.AscDoctorReport)
	r := &ascdoctor.Report{Asc: form.Asc, Git: form.Git, OS: form.OS, Arch: form.Arch}
	for _, c := range form.Checks {
		r.Checks = append(r.Checks, ascdoctor.Check{Name: c.Name, Level: c.Level})
	}
	if err := ascdoctor.Save(ctx, ctx.Doer().ID, r, time.Now()); err != nil {
		if errors.Is(err, ascdoctor.ErrBadReport) {
			ctx.Error(http.StatusUnprocessableEntity, "", err)
			return
		}
		ctx.InternalServerError(err)
		return
	}
	ctx.Status(http.StatusNoContent)
}
