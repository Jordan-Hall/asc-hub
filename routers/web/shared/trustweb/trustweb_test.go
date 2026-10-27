// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package trustweb

import (
	"net/http"
	"net/http/httptest"
	"testing"

	perm_model "forgejo.org/models/perm"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/context"

	"github.com/stretchr/testify/assert"
)

func repoContext(t *testing.T, signed bool, units map[unit.Type]perm_model.AccessMode) (*context.Context, *httptest.ResponseRecorder) {
	t.Helper()
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	t.Cleanup(test.MockVariableValue(&setting.RepoRootPath, t.TempDir()))
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/user2/repo1/asc/checks", nil)
	base, closeBase := context.NewBaseContext(resp, req)
	t.Cleanup(closeBase)
	ctx := context.NewWebContext(base, nil, nil)
	ctx.IsSigned = signed
	ctx.Repo = &context.Repository{
		Repository: &repo_model.Repository{ID: 1, OwnerName: "user2", Name: "repo1", StorageBackend: "asc"},
		Permission: access_model.Permission{AccessMode: perm_model.AccessModeRead, UnitsMode: units},
	}
	return ctx, resp
}

// TestReaderRepoNeedsActionsRead: the checks pages show CI failure groups, occurrences and runs. A reader of the
// code who may not read the Actions of the repository gets 404.
func TestReaderRepoNeedsActionsRead(t *testing.T) {
	for name, tc := range map[string]struct {
		units map[unit.Type]perm_model.AccessMode
		want  int
	}{
		"code and actions":   {map[unit.Type]perm_model.AccessMode{unit.TypeCode: perm_model.AccessModeRead, unit.TypeActions: perm_model.AccessModeRead}, http.StatusOK},
		"code only":          {map[unit.Type]perm_model.AccessMode{unit.TypeCode: perm_model.AccessModeRead}, http.StatusNotFound},
		"code, actions none": {map[unit.Type]perm_model.AccessMode{unit.TypeCode: perm_model.AccessModeRead, unit.TypeActions: perm_model.AccessModeNone}, http.StatusNotFound},
		"actions only":       {map[unit.Type]perm_model.AccessMode{unit.TypeActions: perm_model.AccessModeRead}, http.StatusNotFound},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, resp := repoContext(t, true, tc.units)
			_, _, ok := ReaderRepo(ctx)
			assert.Equal(t, tc.want == http.StatusOK, ok)
			if !ok {
				assert.Equal(t, http.StatusNotFound, resp.Code)
			}
			// A writer's guard is never looser than the reader's.
			ctx, resp = repoContext(t, true, tc.units)
			_, _, ok = WriterRepo(ctx)
			assert.False(t, ok)
			assert.NotEqual(t, http.StatusOK, resp.Code)
		})
	}
}
