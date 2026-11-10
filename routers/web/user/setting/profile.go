// Copyright 2014 The Gogs Authors. All rights reserved.
// Copyright 2018 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package setting

import (
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"

	"forgejo.org/models/avatars"
	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/moderation"
	"forgejo.org/models/organization"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/base"
	"forgejo.org/modules/log"
	"forgejo.org/modules/optional"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/translation"
	"forgejo.org/modules/util"
	"forgejo.org/modules/web"
	"forgejo.org/modules/web/middleware"
	"forgejo.org/routers/web/shared/byoweb"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/ascnotify"
	"forgejo.org/services/context"
	"forgejo.org/services/forms"
	repo_service "forgejo.org/services/repository"
	user_service "forgejo.org/services/user"
)

const (
	tplSettingsProfile      base.TplName = "user/settings/profile"
	tplSettingsAppearance   base.TplName = "user/settings/appearance"
	tplSettingsOrganization base.TplName = "user/settings/organization"
	tplSettingsRepositories base.TplName = "user/settings/repos"
)

var commonPronouns = []string{"he/him", "she/her", "they/them", "it/its", "any pronouns"}

// profileContext sets common context for profile settings template.
func profileContext(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("settings.profile")
	ctx.Data["PageIsSettingsProfile"] = true
	prefs := ascnotify.Load(ctx, ctx.Doer.ID)
	ctx.Data["TimeZone"] = prefs.TZ
	ctx.Data["TimeZones"] = ascTimeZonesWith(prefs.TZ)
	ctx.Data["AllowedUserVisibilityModes"] = setting.Service.AllowedUserVisibilityModesSlice.ToVisibleTypeSlice()
	ctx.Data["DisableGravatar"] = setting.Config().Picture.DisableGravatar.Value(ctx)
	ctx.Data["CooldownPeriod"] = setting.Service.UsernameCooldownPeriod
	ctx.Data["CommonPronouns"] = commonPronouns
	role, err := user_model.GetUserSetting(ctx, ctx.Doer.ID, user_model.SettingsKeyAscRole)
	if err != nil {
		log.Error("GetUserSetting(role) for user[%d]: %v", ctx.Doer.ID, err)
	}
	ctx.Data["AscRole"] = role
	ctx.Data["MaxAvatarFileSize"] = setting.Avatar.MaxFileSize
	ctx.Data["MaxAvatarWidth"] = setting.Avatar.MaxWidth
	ctx.Data["MaxAvatarHeight"] = setting.Avatar.MaxHeight

	canUserRename, err := user_service.CanUserRename(ctx, ctx.Doer)
	if err != nil {
		log.Error("CanUserRename for user[%d]: %v", ctx.Doer.ID, err)
	}

	ctx.Data["UserRenameDisabled"] = !canUserRename
}

// Profile render user's profile page
func Profile(ctx *context.Context) {
	profileContext(ctx)

	ctx.HTML(http.StatusOK, tplSettingsProfile)
}

// ProfilePost response for change user's profile
func ProfilePost(ctx *context.Context) {
	profileContext(ctx)

	if ctx.HasError() {
		ctx.HTML(http.StatusOK, tplSettingsProfile)
		return
	}

	form := web.GetForm(ctx).(*forms.UpdateProfileForm)

	if form.Name != "" {
		if err := user_service.RenameUser(ctx, ctx.Doer, form.Name); err != nil {
			switch {
			case user_model.IsErrUserIsNotLocal(err):
				ctx.Flash.Error(ctx.Tr("form.username_change_not_local_user"))
			case user_model.IsErrUserAlreadyExist(err):
				ctx.Flash.Error(ctx.Tr("form.username_been_taken"))
			case user_model.IsErrCooldownPeriod(err):
				ctx.Flash.Error(ctx.Tr("form.username_claiming_cooldown", err.(user_model.ErrCooldownPeriod).ExpireTime.Format(time.RFC1123Z)))
			case db.IsErrNameReserved(err):
				ctx.Flash.Error(ctx.Tr("user.form.name_reserved", form.Name))
			case db.IsErrNamePatternNotAllowed(err):
				ctx.Flash.Error(ctx.Tr("user.form.name_pattern_not_allowed", form.Name))
			case db.IsErrNameCharsNotAllowed(err):
				ctx.Flash.Error(ctx.Tr("user.form.name_chars_not_allowed", form.Name))
			default:
				ctx.ServerError("RenameUser", err)
				return
			}
			ctx.Redirect(setting.AppSubURL + "/user/settings")
			return
		}
	}

	// a profile a moderator hid stays private until the decision is reversed
	if form.Visibility != ctx.Doer.Visibility {
		if err := moderation.CheckNotHidden(ctx, ctx.Doer.IsAdmin, moderation.ReportedContentTypeUser, ctx.Doer.ID); err != nil {
			if errors.Is(err, moderation.ErrHidden) {
				ctx.Flash.Error(ctx.Tr("asc_admin.mod_hidden_locked"))
				ctx.Redirect(setting.AppSubURL + "/user/settings")
			} else {
				ctx.ServerError("CheckNotHidden", err)
			}
			return
		}
	}

	opts := &user_service.UpdateOptions{
		FullName:            optional.Some(form.FullName),
		KeepEmailPrivate:    optional.Some(form.KeepEmailPrivate),
		Description:         optional.Some(form.Biography),
		Pronouns:            optional.Some(form.Pronouns),
		Website:             optional.Some(form.Website),
		Location:            optional.Some(form.Location),
		Visibility:          optional.Some(form.Visibility),
		KeepActivityPrivate: optional.Some(form.KeepActivityPrivate),
		KeepPronounsPrivate: optional.Some(form.KeepPronounsPrivate),
	}
	if err := user_service.UpdateUser(ctx, ctx.Doer, opts); err != nil {
		ctx.ServerError("UpdateUser", err)
		return
	}

	if ctx.Req.PostForm.Has("role") {
		var err error
		if role := strings.TrimSpace(form.Role); role == "" {
			err = user_model.DeleteUserSetting(ctx, ctx.Doer.ID, user_model.SettingsKeyAscRole)
		} else {
			err = user_model.SetUserSetting(ctx, ctx.Doer.ID, user_model.SettingsKeyAscRole, role)
		}
		if err != nil {
			ctx.ServerError("SetUserSetting", err)
			return
		}
	}

	if ctx.Req.PostForm.Has("tz") {
		prefs := ascnotify.Load(ctx, ctx.Doer.ID)
		prefs.TZ = ctx.FormString("tz")
		if err := ascnotify.Save(ctx, ctx.Doer.ID, prefs); err != nil {
			ctx.Flash.Error(err.Error())
			ctx.Redirect(setting.AppSubURL + "/user/settings")
			return
		}
	}

	log.Trace("User settings updated: %s", ctx.Doer.Name)
	ctx.Flash.Success(ctx.Tr("settings.update_profile_success"))
	ctx.Redirect(setting.AppSubURL + "/user/settings")
}

// UpdateAvatarSetting update user's avatar
func UpdateAvatarSetting(ctx *context.Context, form *forms.AvatarForm, ctxUser *user_model.User) error {
	ctxUser.UseCustomAvatar = form.Source == forms.AvatarLocal
	if len(form.Gravatar) > 0 {
		if form.Avatar != nil {
			ctxUser.Avatar = avatars.HashEmail(form.Gravatar)
		} else {
			ctxUser.Avatar = ""
		}
		ctxUser.AvatarEmail = form.Gravatar
	}

	data, err := forms.ReadAvatar(form.Avatar, ctx.Locale)
	if err != nil {
		return err
	}
	if data != nil {
		if err := user_service.UploadAvatar(ctx, ctxUser, data); err != nil {
			return fmt.Errorf("UploadAvatar: %w", err)
		}
	} else if ctxUser.UseCustomAvatar && ctxUser.Avatar == "" {
		// No avatar is uploaded but setting has been changed to enable,
		// generate a random one when needed.
		if err := user_model.GenerateRandomAvatar(ctx, ctxUser); err != nil {
			log.Error("GenerateRandomAvatar[%d]: %v", ctxUser.ID, err)
		}
	}

	if err := user_model.UpdateUserCols(ctx, ctxUser, "avatar", "avatar_email", "use_custom_avatar"); err != nil {
		return fmt.Errorf("UpdateUserCols: %w", err)
	}

	return nil
}

// AvatarPost response for change user's avatar request
func AvatarPost(ctx *context.Context) {
	form := web.GetForm(ctx).(*forms.AvatarForm)
	if err := UpdateAvatarSetting(ctx, form, ctx.Doer); err != nil {
		ctx.Flash.Error(err.Error())
	} else {
		ctx.Flash.Success(ctx.Tr("settings.update_avatar_success"))
	}

	ctx.Redirect(setting.AppSubURL + "/user/settings")
}

// DeleteAvatar render delete avatar page
func DeleteAvatar(ctx *context.Context) {
	if err := user_service.DeleteAvatar(ctx, ctx.Doer); err != nil {
		ctx.Flash.Error(err.Error())
	}

	ctx.JSONRedirect(setting.AppSubURL + "/user/settings")
}

// Organization render all the organization of the user
func Organization(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("settings.organization")
	ctx.Data["PageIsSettingsOrganization"] = true

	opts := organization.FindOrgOptions{
		ListOptions: db.ListOptions{
			PageSize: setting.UI.Admin.UserPagingNum,
			Page:     ctx.FormInt("page"),
		},
		UserID:         ctx.Doer.ID,
		IncludePrivate: ctx.IsSigned,
	}

	if opts.Page <= 0 {
		opts.Page = 1
	}

	orgs, total, err := db.FindAndCount[organization.Organization](ctx, opts)
	if err != nil {
		ctx.ServerError("FindOrgs", err)
		return
	}

	ctx.Data["Orgs"] = orgs
	rows, err := settingsOrgRows(ctx, orgs, ctx.Doer.ID)
	if err != nil {
		ctx.ServerError("settingsOrgRows", err)
		return
	}
	ctx.Data["OrgRows"] = rows
	ctx.Data["OrgInvites"] = orgInviteRows(ctx)
	pager := context.NewPagination(int(total), opts.PageSize, opts.Page, 5)
	pager.SetDefaultParams(ctx)
	ctx.Data["Page"] = pager
	ctx.HTML(http.StatusOK, tplSettingsOrganization)
}

// SettingsOrgRow is one organization of the settings list with the role of the person in it: an owner gets a
// Settings link, and everyone gets Leave, which asks first. The last owner cannot leave until someone else is an
// owner (the leave action refuses it too, with organization.ErrLastOrgOwner).
type SettingsOrgRow struct {
	Org         *organization.Organization
	IsOwner     bool
	IsLastOwner bool
}

func settingsOrgRows(ctx *context.Context, orgs []*organization.Organization, uid int64) ([]SettingsOrgRow, error) {
	rows := make([]SettingsOrgRow, 0, len(orgs))
	for _, org := range orgs {
		row := SettingsOrgRow{Org: org}
		isOwner, err := org.IsOwnedBy(ctx, uid)
		if err != nil {
			return nil, err
		}
		row.IsOwner = isOwner
		if isOwner {
			owners, err := org.GetOwnerTeam(ctx)
			if err != nil {
				return nil, err
			}
			row.IsLastOwner = owners.NumMembers <= 1
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// UnadoptedShown is the most unadopted directories the Repositories settings page lists.
const UnadoptedShown = 100

// Repos lists the repositories the user owns, with the open stacks of each and where it keeps its objects, and,
// when the user may adopt or delete them, the directories on disk that have no repository record.
func Repos(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("settings.repos")
	ctx.Data["PageIsSettingsRepos"] = true
	allowAdopt := ctx.IsUserSiteAdmin() || setting.Repository.AllowAdoptionOfUnadoptedRepositories
	allowDelete := ctx.IsUserSiteAdmin() || setting.Repository.AllowDeleteOfUnadoptedRepositories
	ctx.Data["allowAdopt"] = allowAdopt
	ctx.Data["allowDelete"] = allowDelete

	opts := db.ListOptions{
		PageSize: setting.UI.Admin.UserPagingNum,
		Page:     ctx.FormInt("page"),
	}
	if opts.Page <= 0 {
		opts.Page = 1
	}

	ctxUser := ctx.Doer
	repos, count, err := repo_model.GetUserRepositories(ctx, &repo_model.SearchRepoOptions{Actor: ctxUser, Private: true, ListOptions: opts})
	if err != nil {
		ctx.ServerError("GetUserRepositories", err)
		return
	}
	ids := make([]int64, len(repos))
	pending := make(map[int64]bool)
	for i, r := range repos {
		ids[i] = r.ID
		pending[r.ID] = r.Status == repo_model.RepositoryPendingTransfer
		if r.IsFork {
			if err := r.GetBaseRepo(ctx); err != nil {
				ctx.ServerError("GetBaseRepo", err)
				return
			}
		}
	}
	stacks, err := issues_model.CountAscOpenStacks(ctx, ids)
	if err != nil {
		ctx.ServerError("CountAscOpenStacks", err)
		return
	}
	ctx.Data["Repos"] = repos
	ctx.Data["RepoStacks"] = stacks
	ctx.Data["PendingTransfer"] = pending
	// Every repository here is the user's own, so asking asc with the system credential shows nothing more.
	ctx.Data["RepoStorage"] = byoweb.LoadRepoStorage(ctx, repos)

	if allowAdopt || allowDelete {
		names, total, err := repo_service.ListUnadoptedRepositories(ctx, ctxUser.LowerName+"/*", &db.ListOptions{Page: 1, PageSize: UnadoptedShown})
		if err != nil {
			ctx.ServerError("ListUnadoptedRepositories", err)
			return
		}
		dirs := make([]string, 0, len(names))
		for _, n := range names {
			if _, name, ok := strings.Cut(n, "/"); ok {
				dirs = append(dirs, name)
			}
		}
		ctx.Data["Dirs"] = dirs
		ctx.Data["DirsMore"] = total - len(dirs)
	}

	ctx.Data["ContextUser"] = ctxUser
	pager := context.NewPagination(int(count), opts.PageSize, opts.Page, 5)
	pager.SetDefaultParams(ctx)
	ctx.Data["Page"] = pager
	ctx.HTML(http.StatusOK, tplSettingsRepositories)
}

// Appearance render user's appearance settings
func Appearance(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("settings.appearance")
	ctx.Data["PageIsSettingsAppearance"] = true
	ctx.Data["AscAgentFeatures"] = setting.Asc.Enabled && ascagentui.FeaturesEnabled(ctx, ctx.Doer)
	ctx.Data["AllThemes"] = setting.UI.Themes
	ctx.Data["ThemeName"] = func(themeName string) string {
		fullThemeName := "themes.names." + themeName
		if ctx.Locale.HasKey(fullThemeName) {
			return ctx.Locale.TrString(fullThemeName)
		}
		return themeName
	}

	var hiddenCommentTypes *big.Int
	val, err := user_model.GetUserSetting(ctx, ctx.Doer.ID, user_model.SettingsKeyHiddenCommentTypes)
	if err != nil {
		ctx.ServerError("GetUserSetting", err)
		return
	}
	hiddenCommentTypes, _ = new(big.Int).SetString(val, 10) // we can safely ignore the failed conversion here

	hide, err := user_model.GetUserSetting(ctx, ctx.Doer.ID, user_model.SettingsKeyHideResolved)
	if err != nil {
		ctx.ServerError("GetUserSetting", err)
		return
	}
	ctx.Data["AscHideResolved"] = hide != "false"
	ctx.Data["AscClock"], ctx.Data["AscDateOrder"] = user_model.AscTimeFormat(ctx, ctx.Doer.ID)

	ctx.Data["IsCommentTypeGroupChecked"] = func(commentTypeGroup string) bool {
		return forms.IsUserHiddenCommentTypeGroupChecked(commentTypeGroup, hiddenCommentTypes)
	}

	ctx.HTML(http.StatusOK, tplSettingsAppearance)
}

// UpdateUIThemePost is used to update users' specific theme
func UpdateUIThemePost(ctx *context.Context) {
	form := web.GetForm(ctx).(*forms.UpdateThemeForm)
	ctx.Data["Title"] = ctx.Tr("settings")
	ctx.Data["PageIsSettingsAppearance"] = true
	ctx.Data["AscAgentFeatures"] = setting.Asc.Enabled && ascagentui.FeaturesEnabled(ctx, ctx.Doer)

	if ctx.HasError() {
		ctx.Redirect(setting.AppSubURL + "/user/settings/appearance")
		return
	}

	if !user_model.IsThemeValid(form.Theme) {
		ctx.Flash.Error(ctx.Tr("settings.theme_update_error"))
		ctx.Redirect(setting.AppSubURL + "/user/settings/appearance")
		return
	}

	opts := &user_service.UpdateOptions{
		Theme: optional.Some(form.Theme),
	}
	if err := user_service.UpdateUser(ctx, ctx.Doer, opts); err != nil {
		ctx.Flash.Error("UpdateUser err")
	} else {
		ctx.Flash.Success(ctx.Tr("settings.theme_update_success"))
	}

	ctx.Redirect(setting.AppSubURL + "/user/settings/appearance")
}

// UpdateUserLang update a user's language
func UpdateUserLang(ctx *context.Context) {
	form := web.GetForm(ctx).(*forms.UpdateLanguageForm)
	ctx.Data["Title"] = ctx.Tr("settings")
	ctx.Data["PageIsSettingsAppearance"] = true
	ctx.Data["AscAgentFeatures"] = setting.Asc.Enabled && ascagentui.FeaturesEnabled(ctx, ctx.Doer)

	if form.Language != "" {
		if !util.SliceContainsString(setting.Langs, form.Language) {
			ctx.Flash.Error(ctx.Tr("settings.update_language_not_found", form.Language))
			ctx.Redirect(setting.AppSubURL + "/user/settings/appearance")
			return
		}
	}

	opts := &user_service.UpdateOptions{
		Language: optional.Some(form.Language),
	}
	if err := user_service.UpdateUser(ctx, ctx.Doer, opts); err != nil {
		ctx.ServerError("UpdateUser", err)
		return
	}

	// Update the language to the one we just set
	middleware.SetLocaleCookie(ctx.Resp, ctx.Doer.Language, 0)

	if ctx.Req.PostForm.Has("tz") {
		prefs := ascnotify.Load(ctx, ctx.Doer.ID)
		prefs.TZ = ctx.FormString("tz")
		if err := ascnotify.Save(ctx, ctx.Doer.ID, prefs); err != nil {
			ctx.Flash.Error(err.Error())
			ctx.Redirect(setting.AppSubURL + "/user/settings")
			return
		}
	}

	log.Trace("User settings updated: %s", ctx.Doer.Name)
	ctx.Flash.Success(translation.NewLocale(ctx.Doer.Language).TrString("settings.update_language_success"))
	ctx.Redirect(setting.AppSubURL + "/user/settings/appearance")
}

// UpdateUserHints updates a user's hints settings
func UpdateUserHints(ctx *context.Context) {
	form := web.GetForm(ctx).(*forms.UpdateHintsForm)
	ctx.Data["Title"] = ctx.Tr("settings")
	ctx.Data["PageIsSettingsAppearance"] = true
	ctx.Data["AscAgentFeatures"] = setting.Asc.Enabled && ascagentui.FeaturesEnabled(ctx, ctx.Doer)

	opts := &user_service.UpdateOptions{
		EnableRepoUnitHints: optional.Some(form.EnableRepoUnitHints),
	}
	if err := user_service.UpdateUser(ctx, ctx.Doer, opts); err != nil {
		ctx.ServerError("UpdateUser", err)
		return
	}

	if ctx.Req.PostForm.Has("tz") {
		prefs := ascnotify.Load(ctx, ctx.Doer.ID)
		prefs.TZ = ctx.FormString("tz")
		if err := ascnotify.Save(ctx, ctx.Doer.ID, prefs); err != nil {
			ctx.Flash.Error(err.Error())
			ctx.Redirect(setting.AppSubURL + "/user/settings")
			return
		}
	}

	log.Trace("User settings updated: %s", ctx.Doer.Name)
	ctx.Flash.Success(translation.NewLocale(ctx.Doer.Language).TrString("settings.update_hints_success"))
	ctx.Redirect(setting.AppSubURL + "/user/settings/appearance")
}

// UpdateUserHideResolved saves whether resolved conversations start collapsed (the switch "Hide resolved comments").
func UpdateUserHideResolved(ctx *context.Context) {
	value := "false"
	if ctx.FormBool("hide_resolved") {
		value = "true"
	}
	if err := user_model.SetUserSetting(ctx, ctx.Doer.ID, user_model.SettingsKeyHideResolved, value); err != nil {
		ctx.ServerError("SetUserSetting", err)
		return
	}
	ctx.Flash.Success(translation.NewLocale(ctx.Doer.Language).TrString("settings.update_hints_success"))
	ctx.Redirect(setting.AppSubURL + "/user/settings/appearance")
}

// UpdateUserTimeFormat saves the clock (24 or 12 hours) and the order of a date the person reads times in.
func UpdateUserTimeFormat(ctx *context.Context) {
	back := setting.AppSubURL + "/user/settings/appearance"
	err := user_model.SetAscTimeFormat(ctx, ctx.Doer.ID, ctx.FormString("clock"), ctx.FormString("date_order"))
	if errors.Is(err, user_model.ErrAscTimeFormat) {
		ctx.Flash.Error(ctx.Tr("settings.asc_time_format_bad"))
		ctx.Redirect(back)
		return
	}
	if err != nil {
		ctx.ServerError("SetAscTimeFormat", err)
		return
	}
	ctx.Flash.Success(translation.NewLocale(ctx.Doer.Language).TrString("settings.asc_time_format_saved"))
	ctx.Redirect(back)
}

// UpdateUserHiddenComments update a user's shown comment types
func UpdateUserHiddenComments(ctx *context.Context) {
	err := user_model.SetUserSetting(ctx, ctx.Doer.ID, user_model.SettingsKeyHiddenCommentTypes, forms.UserHiddenCommentTypesFromRequest(ctx).String())
	if err != nil {
		ctx.ServerError("SetUserSetting", err)
		return
	}

	if ctx.Req.PostForm.Has("tz") {
		prefs := ascnotify.Load(ctx, ctx.Doer.ID)
		prefs.TZ = ctx.FormString("tz")
		if err := ascnotify.Save(ctx, ctx.Doer.ID, prefs); err != nil {
			ctx.Flash.Error(err.Error())
			ctx.Redirect(setting.AppSubURL + "/user/settings")
			return
		}
	}

	log.Trace("User settings updated: %s", ctx.Doer.Name)
	ctx.Flash.Success(ctx.Tr("settings.saved_successfully"))
	ctx.Redirect(setting.AppSubURL + "/user/settings/appearance")
}
