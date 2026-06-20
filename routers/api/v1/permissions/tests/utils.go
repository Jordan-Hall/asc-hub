// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package tests

import (
	"fmt"
	"slices"
	"strings"

	auth_model "forgejo.org/models/auth"
)

func requiredScopesToString(scopeCategories ...auth_model.AccessTokenScopeCategory) string {
	var categories []string
	for _, category := range scopeCategories {
		switch category {
		case auth_model.AccessTokenScopeCategoryActivityPub:
			categories = append(categories, "ActivityPub")
		case auth_model.AccessTokenScopeCategoryAdmin:
			categories = append(categories, "Admin")
		case auth_model.AccessTokenScopeCategoryMisc:
			categories = append(categories, "Misc")
		case auth_model.AccessTokenScopeCategoryNotification:
			categories = append(categories, "Notification")
		case auth_model.AccessTokenScopeCategoryOrganization:
			categories = append(categories, "Organization")
		case auth_model.AccessTokenScopeCategoryPackage:
			categories = append(categories, "Package")
		case auth_model.AccessTokenScopeCategoryIssue:
			categories = append(categories, "Issue")
		case auth_model.AccessTokenScopeCategoryRepository:
			categories = append(categories, "Repository")
		case auth_model.AccessTokenScopeCategoryUser:
			categories = append(categories, "User")
		default:
			panic(fmt.Errorf("unkwnon scope category %v", category))
		}
	}
	slices.Sort(categories)
	return strings.Join(categories, "")
}
