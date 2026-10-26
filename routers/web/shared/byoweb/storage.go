// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package byoweb

import (
	"net/http"

	"forgejo.org/modules/log"
	svc "forgejo.org/services/ascbyo"
	"forgejo.org/services/context"
)

// ------------------------------------------------------------------------------------------ loading

// loadStorage fills the target lists (and, for a repository, the usable targets and the migrations).
func loadStorage(ctx *context.Context, lvl *Level, s *svc.Service, p *Page) error {
	if lvl.Scope.Kind != svc.KindRepo {
		ts, err := s.ListTargets(ctx, lvl.Scope)
		if err != nil {
			return err
		}
		p.Targets = svc.TargetViews(ts)
		return nil
	}
	groups, err := s.ListRepoTargets(ctx, lvl.Allowed)
	if err != nil {
		return err
	}
	for _, g := range groups {
		own := g.Scope == lvl.Scope
		tg := TargetGroup{Manage: own, Targets: svc.TargetViews(g.Targets)}
		if own {
			tg.Title = ctx.Locale.TrString("ascbyo.storage.group_repo")
			p.Targets = tg.Targets
		} else {
			tg.Title = ctx.Locale.TrString("ascbyo.storage.group_owner", g.Scope.Name)
			tg.URL = lvl.OwnerStorageURL
		}
		for _, t := range tg.Targets {
			if t.IsEnabled {
				p.Usable = append(p.Usable, UsableTarget{ID: t.ID, Label: tg.Title + " / " + t.Name})
			}
		}
		p.Groups = append(p.Groups, tg)
	}
	ms, err := s.ListMigrations(ctx, lvl.Scope)
	if err != nil {
		return err
	}
	p.Migrations = ms
	for _, m := range ms {
		if m.Active {
			p.ActiveMigration = m
			break
		}
	}
	return nil
}

// renderStorageIndex loads the list and renders the page with status.
func renderStorageIndex(ctx *context.Context, lvl *Level, s *svc.Service, p *Page, status int) {
	ctx.Data["Title"] = ctx.Locale.TrString("ascbyo.storage.title")
	if s == nil {
		p.Unavailable = true
	} else if err := loadStorage(ctx, lvl, s, p); err != nil {
		markAvailability(p, err)
		if p.Problem == nil {
			p.Problem = problemOf(ctx, err)
		}
	}
	if (p.Unavailable || p.Unsupported) && status < http.StatusBadRequest {
		status = http.StatusServiceUnavailable
	}
	ctx.HTML(status, lvl.StorageTpl)
}

// renderStorageTarget loads one target and renders its page with status. A target that is not the scope's
// is the 404 page.
func renderStorageTarget(ctx *context.Context, lvl *Level, s *svc.Service, p *Page, id string, status int) {
	ctx.Data["Title"] = ctx.Locale.TrString("ascbyo.storage.target_title")
	if s == nil {
		p.Unavailable = true
		ctx.HTML(http.StatusServiceUnavailable, lvl.StorageTpl)
		return
	}
	t, err := s.GetTarget(ctx, lvl.Scope, id)
	if err != nil {
		if svc.IsKind(err, svc.FailNotFound) {
			ctx.NotFound("asc byo target", nil)
			return
		}
		markAvailability(p, err)
		p.Problem = problemOf(ctx, err)
		ctx.HTML(statusOf(err), lvl.StorageTpl)
		return
	}
	v := svc.TargetViewOf(t)
	p.Target = &v
	if p.StorageForm == (StorageForm{}) {
		p.StorageForm = StorageForm{Name: v.Name, Endpoint: v.Endpoint, Region: v.Region, Bucket: v.Bucket, Prefix: v.Prefix, Addressing: v.Addressing}
	}
	ctx.HTML(status, lvl.StorageTpl)
}

// targetID returns the {id} of the route when it has the shape of a target id.
func targetID(ctx *context.Context) (string, bool) {
	id := ctx.Params("id")
	if !svc.ValidTargetID(id) {
		ctx.NotFound("asc byo target id", nil)
		return "", false
	}
	return id, true
}

// ------------------------------------------------------------------------------------------ pages

// StorageIndex is GET .../asc/storage.
func StorageIndex(ctx *context.Context, lvl *Level) {
	s, p, ok := begin(ctx, lvl, dataStorage)
	if !ok {
		return
	}
	p.StorageForm = StorageForm{Addressing: "auto"}
	renderStorageIndex(ctx, lvl, s, p, http.StatusOK)
}

// StorageTarget is GET .../asc/storage/targets/{id}.
func StorageTarget(ctx *context.Context, lvl *Level) {
	s, p, ok := begin(ctx, lvl, dataStorage)
	if !ok {
		return
	}
	id, ok := targetID(ctx)
	if !ok {
		return
	}
	renderStorageTarget(ctx, lvl, s, p, id, http.StatusOK)
}

// ------------------------------------------------------------------------------------------ actions

func storageInputOf(f form) *svc.StorageInput {
	in := &svc.StorageInput{
		Name: f.str("name", limName), Endpoint: f.str("endpoint", limURL), Region: f.str("region", limShort),
		Bucket: f.str("bucket", limShort), Prefix: f.str("prefix", limPrefix),
		AccessKeyID: f.str("access_key_id", limKeyID), SecretAccessKey: f.str("secret_access_key", limSecret),
		SessionToken: f.str("session_token", limToken),
	}
	return in
}

// formOf copies the non-secret fields after validation normalised them.
func formOf(in *svc.StorageInput, addressing string) StorageForm {
	return StorageForm{Name: in.Name, Endpoint: in.Endpoint, Region: in.Region, Bucket: in.Bucket, Prefix: in.Prefix, Addressing: addressing}
}

// StorageCreate is POST .../asc/storage/targets.
func StorageCreate(ctx *context.Context, lvl *Level) {
	s, p, ok := begin(ctx, lvl, dataStorage)
	if !ok {
		return
	}
	f := form{ctx}
	in := storageInputOf(f)
	addrForm := f.str("addressing", 16)
	addr, addrOK := svc.AddressingFromForm(addrForm)
	if !addrOK {
		addrForm = "auto"
	}
	in.Addressing = addr
	fe := in.Validate(true)
	if !addrOK {
		fe.Add("addressing", "ascbyo.storage.err.addressing")
	}
	p.StorageForm = formOf(in, addrForm)
	if len(fe) > 0 {
		p.Errors = fe
		p.Problem = &Problem{Message: ctx.Locale.TrString("ascbyo.error.invalid")}
		renderStorageIndex(ctx, lvl, s, p, http.StatusUnprocessableEntity)
		return
	}
	if s == nil {
		renderStorageIndex(ctx, lvl, nil, p, http.StatusServiceUnavailable)
		return
	}
	t, _, err := s.CreateTarget(ctx, lvl.Scope, in, f.op())
	if err != nil {
		p.Problem = problemOf(ctx, err)
		refuse(ctx, err, func(status int) { renderStorageIndex(ctx, lvl, s, p, status) })
		return
	}
	record(ctx, lvl, "added storage target", t.GetId())
	ctx.Flash.Success(ctx.Locale.TrString("ascbyo.flash.storage_created", t.GetName()))
	ctx.Redirect(lvl.Base + "/storage/targets/" + t.GetId())
}

// StorageUpdate is POST .../asc/storage/targets/{id}.
func StorageUpdate(ctx *context.Context, lvl *Level) {
	s, p, ok := begin(ctx, lvl, dataStorage)
	if !ok {
		return
	}
	id, ok := targetID(ctx)
	if !ok {
		return
	}
	f := form{ctx}
	in := storageInputOf(f)
	addrForm := f.str("addressing", 16)
	addr, addrOK := svc.AddressingFromForm(addrForm)
	if !addrOK {
		addrForm = "auto"
	}
	in.Addressing = addr
	fe := in.Validate(false)
	if !addrOK {
		fe.Add("addressing", "ascbyo.storage.err.addressing")
	}
	p.StorageForm = formOf(in, addrForm)
	if s == nil {
		renderStorageTarget(ctx, lvl, nil, p, id, http.StatusServiceUnavailable)
		return
	}
	if len(fe) > 0 {
		p.Errors = fe
		p.Problem = &Problem{Message: ctx.Locale.TrString("ascbyo.error.invalid")}
		renderStorageTarget(ctx, lvl, s, p, id, http.StatusUnprocessableEntity)
		return
	}
	_, changed, _, err := s.UpdateTarget(ctx, lvl.Scope, id, in, f.op())
	if err != nil {
		p.Problem = problemOf(ctx, err)
		refuse(ctx, err, func(status int) { renderStorageTarget(ctx, lvl, s, p, id, status) })
		return
	}
	if changed {
		record(ctx, lvl, "updated storage target", id)
		ctx.Flash.Success(ctx.Locale.TrString("ascbyo.flash.storage_updated"))
	} else {
		ctx.Flash.Info(ctx.Locale.TrString("ascbyo.flash.nothing_changed"))
	}
	ctx.Redirect(lvl.Base + "/storage/targets/" + id)
}

// returnTo maps the form's return field to one of two pages of the scope. Nothing else is accepted, so the
// field cannot carry a redirect to another site.
func returnTo(ctx *context.Context, lvl *Level, id string) string {
	if (form{ctx}).str("return", 8) == "list" {
		return lvl.Base + "/storage"
	}
	return lvl.Base + "/storage/targets/" + id
}

// StorageVerify is POST .../asc/storage/targets/{id}/verify. The result is shown in the response, with
// every step, because the steps are not stored.
func StorageVerify(ctx *context.Context, lvl *Level) {
	s, p, ok := begin(ctx, lvl, dataStorage)
	if !ok {
		return
	}
	id, ok := targetID(ctx)
	if !ok {
		return
	}
	if s == nil {
		renderStorageTarget(ctx, lvl, nil, p, id, http.StatusServiceUnavailable)
		return
	}
	res, err := s.VerifyTarget(ctx, lvl.Scope, id, form{ctx}.op())
	if err != nil {
		p.Problem = problemOf(ctx, err)
		refuse(ctx, err, func(status int) { renderStorageTarget(ctx, lvl, s, p, id, status) })
		return
	}
	record(ctx, lvl, "verified storage target", id)
	v := &VerifyView{OK: res.OK}
	for _, c := range res.Checks {
		v.Checks = append(v.Checks, VerifyStep{Name: ctx.Locale.TrString(c.StepKey), OK: c.OK, Latency: c.LatencyMS, Message: c.Message})
	}
	p.Verify = v
	renderStorageTarget(ctx, lvl, s, p, id, http.StatusOK)
}

// StorageEnable is POST .../asc/storage/targets/{id}/enable and /disable.
func StorageEnable(ctx *context.Context, lvl *Level, enabled bool) {
	s, p, ok := begin(ctx, lvl, dataStorage)
	if !ok {
		return
	}
	id, ok := targetID(ctx)
	if !ok {
		return
	}
	if s == nil {
		renderStorageTarget(ctx, lvl, nil, p, id, http.StatusServiceUnavailable)
		return
	}
	if _, err := s.SetEnabled(ctx, lvl.Scope, id, enabled, form{ctx}.op()); err != nil {
		p.Problem = problemOf(ctx, err)
		refuse(ctx, err, func(status int) { renderStorageTarget(ctx, lvl, s, p, id, status) })
		return
	}
	if enabled {
		record(ctx, lvl, "enabled storage target", id)
		ctx.Flash.Success(ctx.Locale.TrString("ascbyo.flash.storage_enabled"))
	} else {
		record(ctx, lvl, "disabled storage target", id)
		ctx.Flash.Success(ctx.Locale.TrString("ascbyo.flash.storage_disabled"))
	}
	ctx.Redirect(returnTo(ctx, lvl, id))
}

// StorageDelete is POST .../asc/storage/targets/{id}/delete.
func StorageDelete(ctx *context.Context, lvl *Level) {
	s, p, ok := begin(ctx, lvl, dataStorage)
	if !ok {
		return
	}
	id, ok := targetID(ctx)
	if !ok {
		return
	}
	if s == nil {
		renderStorageTarget(ctx, lvl, nil, p, id, http.StatusServiceUnavailable)
		return
	}
	if !(form{ctx}.on("confirm")) {
		p.Problem = &Problem{Message: ctx.Locale.TrString("ascbyo.error.confirm")}
		renderStorageTarget(ctx, lvl, s, p, id, http.StatusUnprocessableEntity)
		return
	}
	if err := s.DeleteTarget(ctx, lvl.Scope, id, form{ctx}.op()); err != nil {
		p.Problem = problemOf(ctx, err)
		refuse(ctx, err, func(status int) { renderStorageTarget(ctx, lvl, s, p, id, status) })
		return
	}
	record(ctx, lvl, "deleted storage target", id)
	ctx.Flash.Success(ctx.Locale.TrString("ascbyo.flash.storage_deleted"))
	ctx.Redirect(lvl.Base + "/storage")
}

// ------------------------------------------------------------------------------------------ repository

func repoOnly(ctx *context.Context, lvl *Level) bool {
	if lvl.Scope.Kind != svc.KindRepo {
		ctx.NotFound("asc byo: repository only", nil)
		return false
	}
	return true
}

// StorageAssign is POST .../asc/storage/assign: new objects of the repository go to the chosen target.
func StorageAssign(ctx *context.Context, lvl *Level) {
	if !repoOnly(ctx, lvl) {
		return
	}
	s, p, ok := begin(ctx, lvl, dataStorage)
	if !ok {
		return
	}
	if s == nil {
		renderStorageIndex(ctx, lvl, nil, p, http.StatusServiceUnavailable)
		return
	}
	f := form{ctx}
	target := f.str("target_id", 40)
	if err := s.AssignRepo(ctx, lvl.Scope, lvl.Allowed, target, f.op()); err != nil {
		p.Problem = problemOf(ctx, err)
		refuse(ctx, err, func(status int) { renderStorageIndex(ctx, lvl, s, p, status) })
		return
	}
	record(ctx, lvl, "assigned storage target", orDefault(target))
	ctx.Flash.Success(ctx.Locale.TrString("ascbyo.flash.storage_assigned"))
	ctx.Redirect(lvl.Base + "/storage")
}

func orDefault(id string) string {
	if id == "" {
		return "(instance default)"
	}
	return id
}

// MigrationStart is POST .../asc/storage/migrations.
func MigrationStart(ctx *context.Context, lvl *Level) {
	if !repoOnly(ctx, lvl) {
		return
	}
	s, p, ok := begin(ctx, lvl, dataStorage)
	if !ok {
		return
	}
	if s == nil {
		renderStorageIndex(ctx, lvl, nil, p, http.StatusServiceUnavailable)
		return
	}
	f := form{ctx}
	target := f.str("target_id", 40)
	deleteSrc := f.on("delete_source")
	if deleteSrc && !f.on("confirm_delete_source") {
		p.Errors.Add("confirm_delete_source", "ascbyo.storage.err.confirm_delete_source")
		p.Problem = &Problem{Message: ctx.Locale.TrString("ascbyo.error.invalid")}
		renderStorageIndex(ctx, lvl, s, p, http.StatusUnprocessableEntity)
		return
	}
	m, err := s.StartMigration(ctx, lvl.Scope, lvl.Allowed, target, deleteSrc, f.op())
	if err != nil {
		p.Problem = problemOf(ctx, err)
		refuse(ctx, err, func(status int) { renderStorageIndex(ctx, lvl, s, p, status) })
		return
	}
	record(ctx, lvl, "started storage migration", m.ID)
	ctx.Flash.Success(ctx.Locale.TrString("ascbyo.flash.migration_started"))
	ctx.Redirect(lvl.Base + "/storage")
}

func migrationID(ctx *context.Context) (string, bool) {
	id := ctx.Params("id")
	if !svc.ValidMigrationID(id) {
		ctx.NotFound("asc byo migration id", nil)
		return "", false
	}
	return id, true
}

// MigrationCancel is POST .../asc/storage/migrations/{id}/cancel.
func MigrationCancel(ctx *context.Context, lvl *Level) {
	if !repoOnly(ctx, lvl) {
		return
	}
	s, p, ok := begin(ctx, lvl, dataStorage)
	if !ok {
		return
	}
	id, ok := migrationID(ctx)
	if !ok {
		return
	}
	if s == nil {
		renderStorageIndex(ctx, lvl, nil, p, http.StatusServiceUnavailable)
		return
	}
	if _, err := s.CancelMigration(ctx, lvl.Scope, id, form{ctx}.op()); err != nil {
		p.Problem = problemOf(ctx, err)
		refuse(ctx, err, func(status int) { renderStorageIndex(ctx, lvl, s, p, status) })
		return
	}
	record(ctx, lvl, "cancelled storage migration", id)
	ctx.Flash.Success(ctx.Locale.TrString("ascbyo.flash.migration_cancelled"))
	ctx.Redirect(lvl.Base + "/storage")
}

// migrationStatus is the JSON the progress script polls.
type migrationStatus struct {
	State   string `json:"state"`
	Label   string `json:"label"`
	Active  bool   `json:"active"`
	Percent int    `json:"percent"`
	Copied  uint64 `json:"copied"`
	Total   uint64 `json:"total"`
	Error   string `json:"error,omitempty"`
}

// MigrationStatus is GET .../asc/storage/migrations/{id}/status.
func MigrationStatus(ctx *context.Context, lvl *Level) {
	if !repoOnly(ctx, lvl) {
		return
	}
	s, _, ok := begin(ctx, lvl, dataStorage)
	if !ok {
		return
	}
	id, ok := migrationID(ctx)
	if !ok {
		return
	}
	if s == nil {
		ctx.JSON(http.StatusServiceUnavailable, map[string]string{"error": "unavailable"})
		return
	}
	m, err := s.GetMigration(ctx, lvl.Scope, id)
	if err != nil {
		if svc.IsKind(err, svc.FailNotFound) {
			ctx.JSON(http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		log.Debug("asc byo: migration status of %s: %v", lvl.Scope, err)
		ctx.JSON(statusOf(err), map[string]string{"error": "unavailable"})
		return
	}
	ctx.JSON(http.StatusOK, migrationStatus{
		State: m.StateName, Label: ctx.Locale.TrString(m.StateKey), Active: m.Active, Percent: m.Percent,
		Copied: m.Copied, Total: m.Total, Error: m.Error,
	})
}
