// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package packages

import (
	"testing"

	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/structs"
	"forgejo.org/modules/test"

	"github.com/stretchr/testify/assert"
)

func TestAPackageIsAsVisibleAsItsOwner(t *testing.T) {
	owner := func(v structs.VisibleType) *user_model.User {
		return &user_model.User{Name: "acme", Visibility: v}
	}
	assert.Equal(t, PackageVisibility{Key: "public", Owner: "acme"}, VisibilityOf(owner(structs.VisibleTypePublic)))
	assert.Equal(t, PackageVisibility{Key: "internal", Owner: "acme"}, VisibilityOf(owner(structs.VisibleTypeLimited)))
	assert.Equal(t, PackageVisibility{Key: "private", Owner: "acme"}, VisibilityOf(owner(structs.VisibleTypePrivate)))

	t.Run("A hub that needs a sign-in makes a public owner internal", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Service.RequireSignInView, true)()
		assert.Equal(t, "internal", VisibilityOf(owner(structs.VisibleTypePublic)).Key)
		assert.Equal(t, "private", VisibilityOf(owner(structs.VisibleTypePrivate)).Key)
	})
}
