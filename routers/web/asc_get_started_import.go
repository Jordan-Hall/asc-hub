// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package web

import (
	"errors"
	"net/http"
	"net/url"
	"path"
	"strings"

	"forgejo.org/models"
	admin_model "forgejo.org/models/admin"
	"forgejo.org/models/db"
	quota_model "forgejo.org/models/quota"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/git"
	"forgejo.org/modules/json"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/structs"
	"forgejo.org/services/context"
	"forgejo.org/services/forms"
	"forgejo.org/services/migrations"
	migrations_allowlist "forgejo.org/services/migrations/allowlist"
	repo_service "forgejo.org/services/repository"
	"forgejo.org/services/task"
)

// The import of step 3 of Get started (board GetStarted): the person pastes the https address of a Git repository, the
// hub queues the same migration as /repo/migrate (history only, private, owned by the person, named after the last
// part of the address), and the step shows "Reading history" until the task ends, then the number of commits read.

// errAscImportHTTPS is an address that is not https: the hub fetches a repository it imports over https only.
var errAscImportHTTPS = errors.New("not an https address")

// errAscImportName is an address with no repository name in it.
var errAscImportName = errors.New("no repository name in the address")

// ascImportTarget checks the address of an import and returns the address to fetch and the name of the new
// repository.
func ascImportTarget(ctx *context.Context, addr string) (remote, name string, err error) {
	remote, err = forms.ParseRemoteAddr(addr, "", "")
	if err != nil {
		return "", "", err
	}
	u, err := url.Parse(remote)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return "", "", errAscImportHTTPS
	}
	if err := migrations_allowlist.IsMigrateURLAllowed(remote, ctx.Doer); err != nil {
		return "", "", err
	}
	name = strings.TrimSuffix(path.Base(strings.TrimRight(u.Path, "/")), ".git")
	if name == "" || name == "." || name == "/" {
		return "", "", errAscImportName
	}
	return remote, name, nil
}

// ascImportError is the sentence a person reads for an import that could not start.
func ascImportError(ctx *context.Context, err error) string {
	var addrErr *models.ErrInvalidCloneAddr
	switch {
	case errors.Is(err, errAscImportHTTPS):
		return ctx.Locale.TrString("asc_site.started_imp_https")
	case errors.Is(err, errAscImportName):
		return ctx.Locale.TrString("asc_site.started_imp_noname")
	case errors.As(err, &addrErr) && addrErr.HasCredentials:
		return ctx.Locale.TrString("migrate.form.error.url_credentials")
	case errors.As(err, &addrErr) && addrErr.IsPermissionDenied:
		return ctx.Locale.TrString("repo.migrate.permission_denied_blocked")
	case errors.As(err, &addrErr):
		return ctx.Locale.TrString("form.url_error", addrErr.Host)
	case repo_model.IsErrReachLimitOfRepo(err):
		return ctx.Locale.TrString("repo.form.reach_limit_of_creation_n", ctx.Doer.MaxCreationLimit())
	case repo_model.IsErrRepoAlreadyExist(err), repo_model.IsErrRepoFilesAlreadyExist(err):
		return ctx.Locale.TrString("form.repo_name_been_taken")
	case db.IsErrNameReserved(err), db.IsErrNamePatternNotAllowed(err), db.IsErrNameCharsNotAllowed(err):
		return ctx.Locale.TrString("asc_site.started_imp_badname")
	}
	log.Error("Get started import: %v", err)
	return ctx.Locale.TrString("asc_site.started_imp_failed")
}

// ascImportStart checks and queues the import of addr and returns the new repository.
func ascImportStart(ctx *context.Context, addr string) (*repo_model.Repository, string) {
	if setting.Repository.DisableMigrations {
		return nil, ctx.Locale.TrString("asc_site.started_imp_off")
	}
	ok, err := quota_model.EvaluateForUser(ctx, ctx.Doer.ID, quota_model.LimitSubjectSizeReposAll)
	if err != nil {
		return nil, ascImportError(ctx, err)
	}
	if !ok {
		return nil, ctx.Locale.TrString("repo.settings.pull_mirror_sync_quota_exceeded")
	}
	remote, name, err := ascImportTarget(ctx, addr)
	if err != nil {
		return nil, ascImportError(ctx, err)
	}
	if err := repo_model.CheckCreateRepository(ctx, ctx.Doer, ctx.Doer, name); err != nil {
		return nil, ascImportError(ctx, err)
	}
	opts := migrations.MigrateOptions{
		OriginalURL:    strings.TrimSpace(addr),
		GitServiceType: structs.PlainGitService,
		CloneAddr:      remote,
		RepoName:       name,
		Private:        true,
	}
	if err := task.MigrateRepository(ctx, ctx.Doer, ctx.Doer, opts); err != nil {
		return nil, ascImportError(ctx, err)
	}
	repo, err := repo_model.GetRepositoryByName(ctx, ctx.Doer.ID, name)
	if err != nil {
		return nil, ascImportError(ctx, err)
	}
	return repo, ""
}

// AscGetStartedImport answers POST /-/get-started/import (clone_addr). With inline=1 (the page asks so) it answers
// JSON: the name and link of the new repository, or the error; otherwise it goes to the repository, or back to Get
// started with the error.
func AscGetStartedImport(ctx *context.Context) {
	inline := ctx.FormBool("inline")
	repo, msg := ascImportStart(ctx, ctx.FormString("clone_addr"))
	if msg != "" {
		if inline {
			ctx.JSON(http.StatusUnprocessableEntity, map[string]string{"error": msg})
			return
		}
		ctx.Flash.Error(msg)
		ctx.Redirect(setting.AppSubURL + "/-/get-started")
		return
	}
	if inline {
		ctx.JSON(http.StatusOK, map[string]string{"repo": repo.Name, "link": repo.Link()})
		return
	}
	ctx.Redirect(repo.Link())
}

// ascImportState is the answer of the import status route.
type ascImportState struct {
	Status  string `json:"status"` // queued, running, failed or done
	Message string `json:"message,omitempty"`
	Commits int64  `json:"commits,omitempty"`
	Count   string `json:"count,omitempty"` // "12,408 commits"
	Link    string `json:"link"`
}

// ascImportStatus is the word of the route for a task status.
func ascImportStatus(s structs.TaskStatus) string {
	switch s {
	case structs.TaskStatusQueued:
		return "queued"
	case structs.TaskStatusRunning:
		return "running"
	case structs.TaskStatusFinished:
		return "done"
	}
	return "failed"
}

// ascImportMessage is the reason of a failed task, translated when it is a translatable message, without the
// internals of the hub.
func ascImportMessage(ctx *context.Context, msg string) string {
	if msg != "" && msg[0] == '{' {
		var tm admin_model.TranslatableMessage
		if err := json.Unmarshal([]byte(msg), &tm); err == nil {
			for i := range tm.Args {
				if arg, ok := tm.Args[i].(float64); ok {
					tm.Args[i] = int64(arg)
				}
			}
			msg = ctx.Locale.TrString(tm.Format, tm.Args...)
		}
	}
	return repo_service.RedactMigrationMessage(msg)
}

// AscGetStartedImportStatus answers GET /-/get-started/import/status?repo=name: how the import of a repository of the
// person that they started goes, and once it is done how many commits of the default branch were read.
func AscGetStartedImportStatus(ctx *context.Context) {
	repo, err := repo_model.GetRepositoryByName(ctx, ctx.Doer.ID, ctx.FormString("repo"))
	if err != nil {
		if repo_model.IsErrRepoNotExist(err) {
			ctx.NotFound("GetRepositoryByName", nil)
			return
		}
		ctx.ServerError("GetRepositoryByName", err)
		return
	}
	t, err := admin_model.GetMigratingTask(ctx, repo.ID)
	if err != nil || t.DoerID != ctx.Doer.ID {
		if err == nil || admin_model.IsErrTaskDoesNotExist(err) {
			ctx.NotFound("GetMigratingTask", nil)
			return
		}
		ctx.ServerError("GetMigratingTask", err)
		return
	}
	st := ascImportState{Status: ascImportStatus(t.Status), Link: repo.Link()}
	switch st.Status {
	case "failed":
		st.Message = ascImportMessage(ctx, t.Message)
	case "done":
		n, err := git.CommitsCount(ctx, git.CommitsCountOptions{RepoPath: repo.RepoPath(), Revision: []string{repo.DefaultBranch}})
		if err == nil {
			st.Commits = n
			st.Count = ctx.Locale.TrString("asc_site.started_imp_commits", ctx.Locale.PrettyNumber(n))
			if n == 1 {
				st.Count = ctx.Locale.TrString("asc_site.started_imp_commit")
			}
		}
	}
	ctx.Resp.Header().Set("Cache-Control", "no-store")
	ctx.JSON(http.StatusOK, st)
}
