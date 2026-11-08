// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	repo_model "forgejo.org/models/repo"
	"forgejo.org/services/forms"
)

// ascMaintainMode is the value of the role menu of the access page for the Maintain role
// (models/repo/asc_maintain.go); the other roles are sent as their access mode number.
const ascMaintainMode = "maintain"

// ascRepoVisibility is the visibility of the repository as the visibility cards of the General page name it.
func ascRepoVisibility(repo *repo_model.Repository) string {
	switch {
	case repo.IsPrivate && repo.IsInternal:
		return "internal"
	case repo.IsPrivate:
		return "private"
	default:
		return "public"
	}
}

// ascMaintainerKeepsAdminFields checks the General form of a maintainer, who cannot rename the repository or
// change its visibility. It reports false when the form asks for either. Otherwise it sets the visibility of the
// form to the current one, as the page of a maintainer does not carry the visibility cards.
func ascMaintainerKeepsAdminFields(repo *repo_model.Repository, form *forms.RepoSettingForm) bool {
	if form.RepoName != repo.Name {
		return false
	}
	current := ascRepoVisibility(repo)
	if form.Visibility != "" && form.Visibility != current {
		return false
	}
	form.Visibility = current
	form.Private = repo.IsPrivate
	return true
}
