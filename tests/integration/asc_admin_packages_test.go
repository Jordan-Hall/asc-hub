// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"strings"
	"testing"

	"forgejo.org/models/db"
	packages_model "forgejo.org/models/packages"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Board notes: the packages page default and empty, and the cleanup preview shows exactly what the rules will delete
// before they run.
func TestAscAdminPackagesBoardNotes(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	ctx := t.Context()
	session := loginUser(t, "user1")
	countVersions := func() int64 {
		n, err := db.GetEngine(ctx).Count(new(packages_model.PackageVersion))
		require.NoError(t, err)
		return n
	}

	// empty: no package published yet, and no cleanup rule enabled
	doc := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/admin/packages"), http.StatusOK).Body)
	assert.Contains(t, doc.Find(".ds-empty").Text(), "No packages yet")
	assert.Positive(t, doc.Find(`a[href="/admin/packages/cleanup/rules"]`).Length())
	doc = NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/admin/packages/cleanup/rules"), http.StatusOK).Body)
	assert.Contains(t, doc.Find(`[data-testid="cleanup-rules-preview"]`).Text(), "No cleanup rule is enabled.")
	assert.Equal(t, 0, doc.Find(`form[action="/admin/packages/cleanup/rules"]`).Length(), "nothing to run")

	// a package of user2 with three old versions and a rule that removes what is older than a day
	pkg := &packages_model.Package{OwnerID: 2, Type: packages_model.TypeGeneric, Name: "preview", LowerName: "preview"}
	_, err := db.GetEngine(ctx).Insert(pkg)
	require.NoError(t, err)
	for _, v := range []string{"1.0", "1.1", "1.2"} {
		_, err = db.GetEngine(ctx).NoAutoTime().Insert(&packages_model.PackageVersion{PackageID: pkg.ID, CreatorID: 2, Version: v, LowerVersion: v, CreatedUnix: 1_000_000})
		require.NoError(t, err)
	}
	_, err = packages_model.InsertCleanupRule(ctx, &packages_model.PackageCleanupRule{Enabled: true, OwnerID: 2, Type: packages_model.TypeGeneric, RemoveDays: 1})
	require.NoError(t, err)

	// default: the package is listed
	doc = NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/admin/packages"), http.StatusOK).Body)
	assert.Contains(t, doc.Find(".ds-table tbody").Text(), "preview")

	// the preview names the owner and the type and counts the versions and packages, and deletes nothing
	doc = NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/admin/packages/cleanup/rules"), http.StatusOK).Body)
	card := doc.Find(`[data-testid="cleanup-rules-preview"]`)
	assert.Contains(t, card.Find(".ds-chip-warn").Text(), "3 versions would be deleted")
	row := strings.Join(strings.Fields(card.Find(".ds-li").Text()), " ")
	assert.Contains(t, row, "user2")
	assert.Contains(t, row, "Generic")
	assert.Contains(t, row, "3 versions")
	assert.Contains(t, row, "in 1 package")
	_, disabled := card.Find(`button[type="submit"]`).Attr("disabled")
	assert.False(t, disabled)
	assert.EqualValues(t, 3, countVersions(), "looking deletes nothing")

	// running deletes exactly what the preview counted, and the preview then has nothing left
	req := NewRequestWithValues(t, "POST", "/admin/packages/cleanup/rules", map[string]string{})
	session.MakeRequest(t, req, http.StatusSeeOther)
	assert.EqualValues(t, 0, countVersions())
	doc = NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/admin/packages/cleanup/rules"), http.StatusOK).Body)
	card = doc.Find(`[data-testid="cleanup-rules-preview"]`)
	assert.Contains(t, card.Find(".ds-chip-ok").Text(), "0 versions would be deleted")
	_, disabled = card.Find(`button[type="submit"]`).Attr("disabled")
	assert.True(t, disabled, "nothing to delete, so Run is off")
}
