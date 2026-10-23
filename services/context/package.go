// Copyright 2021 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package context

import (
	"fmt"
	"net/http"

	packages_model "forgejo.org/models/packages"
	"forgejo.org/models/perm"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/templates"
	"forgejo.org/services/auth"
	packages_service "forgejo.org/services/packages"
)

// Package contains owner, access mode and optional the package descriptor
type Package struct {
	Owner      *user_model.User
	AccessMode perm.AccessMode
	Descriptor *packages_model.PackageDescriptor
}

type packageAssignmentCtx struct {
	*Base
	Doer        *user_model.User
	ContextUser *user_model.User
	// Authentication tells whether the request is made with the token of an Actions task.
	Authentication auth.AuthenticationResult
}

// PackageAssignment returns a middleware to handle Context.Package assignment
func PackageAssignment() func(ctx *Context) {
	return func(ctx *Context) {
		errorFn := func(status int, title string, obj any) {
			err, ok := obj.(error)
			if !ok {
				err = fmt.Errorf("%s", obj)
			}
			if status == http.StatusNotFound {
				ctx.NotFound(title, err)
			} else {
				ctx.ServerError(title, err)
			}
		}
		paCtx := &packageAssignmentCtx{Base: ctx.Base, Doer: ctx.Doer, ContextUser: ctx.ContextUser, Authentication: ctx.Authentication}
		ctx.Package = packageAssignment(paCtx, errorFn)
	}
}

// PackageAssignmentAPI returns a middleware to handle Context.Package assignment
func PackageAssignmentAPI() func(ctx *APIContext) {
	return func(ctx *APIContext) {
		paCtx := &packageAssignmentCtx{Base: ctx.Base, Doer: ctx.Doer(), ContextUser: ctx.User(), Authentication: ctx.Authentication()}
		ctx.pkg = packageAssignment(paCtx, ctx.Error)
	}
}

func (ctx *packageAssignmentCtx) taskID() (bool, int64) {
	if ctx.Authentication == nil || ctx.Doer == nil || !ctx.Doer.IsActions() {
		return false, 0
	}
	return ctx.Authentication.ActionsTaskID().Get()
}

func packageAssignment(ctx *packageAssignmentCtx, errCb func(int, string, any)) *Package {
	pkg := &Package{
		Owner: ctx.ContextUser,
	}
	var err error
	if hasTask, taskID := ctx.taskID(); hasTask {
		pkg.AccessMode, err = packages_service.DeterminePackageAccessModeForTask(ctx.Base, pkg.Owner, ctx.Doer, taskID)
	} else {
		pkg.AccessMode, err = packages_service.DeterminePackageAccessMode(ctx.Base, pkg.Owner, ctx.Doer)
	}
	if err != nil {
		errCb(http.StatusInternalServerError, "determineAccessMode", err)
		return pkg
	}

	packageType := ctx.Params("type")
	name := ctx.Params("name")
	version := ctx.Params("version")
	if packageType != "" && name != "" && version != "" {
		pv, err := packages_model.GetVersionByNameAndVersion(ctx, pkg.Owner.ID, packages_model.Type(packageType), name, version)
		if err != nil {
			if err == packages_model.ErrPackageNotExist {
				errCb(http.StatusNotFound, "GetVersionByNameAndVersion", err)
			} else {
				errCb(http.StatusInternalServerError, "GetVersionByNameAndVersion", err)
			}
			return pkg
		}

		pkg.Descriptor, err = packages_model.GetPackageDescriptor(ctx, pv)
		if err != nil {
			errCb(http.StatusInternalServerError, "GetPackageDescriptor", err)
			return pkg
		}
	}

	return pkg
}

// PackageContexter initializes a package context for a request.
func PackageContexter() func(next http.Handler) http.Handler {
	renderer := templates.HTMLRenderer()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
			base, baseCleanUp := NewBaseContext(resp, req)
			defer baseCleanUp()

			// it is still needed when rendering 500 page in a package handler
			ctx := NewWebContext(base, renderer, nil)
			ctx.AppendContextValue(WebContextKey, ctx)
			next.ServeHTTP(ctx.Resp, ctx.Req)
		})
	}
}
