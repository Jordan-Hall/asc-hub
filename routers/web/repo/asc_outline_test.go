// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"testing"

	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrepareAscOutline(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	method := ascSym("method", "src/main.rs", 20, true, "precise")
	method.Container = "Cfg"
	fake := startFakeSymbols(t, func(req *ascv1.SymbolsRequest) (*ascv1.SymbolsResponse, error) {
		if req.GetOp() == ascv1.SymbolOp_SYMBOL_OP_OUTLINE {
			return &ascv1.SymbolsResponse{Symbols: []*ascv1.Symbol{
				ascSym("run", "src/main.rs", 3, true, "precise"), method, ascSym("call", "src/main.rs", 30, false, "precise"),
			}}, nil
		}
		return &ascv1.SymbolsResponse{Symbols: []*ascv1.Symbol{
			ascSym(req.GetName(), "src/main.rs", 3, false, "name_match"), // the file itself does not count
			ascSym(req.GetName(), "src/lib.rs", 8, false, "name_match"),
			ascSym(req.GetName(), "src/cli.rs", 9, false, "name_match"),
		}}, nil
	})

	newCtx := func() (ctxData map[string]any, call func()) {
		ctx, _ := contexttest.MockContext(t, "/user2/repo1/src/branch/master/src/main.rs")
		contexttest.LoadRepo(t, ctx, 1)
		ctx.Repo.TreePath = "src/main.rs"
		ctx.Repo.IsViewBranch = true
		ctx.Repo.BranchName = "master"
		return ctx.Data, func() { prepareAscOutline(ctx) }
	}

	t.Run("outline and references elsewhere", func(t *testing.T) {
		data, call := newCtx()
		call()
		rows, ok := data["AscOutline"].([]ascSymbolsRow)
		require.True(t, ok)
		assert.Len(t, rows, 3)
		assert.Equal(t, "/user2/repo1/src/branch/master/src/main.rs#L3", rows[0].Link)
		used, ok := data["AscUsedIn"].([]usedInRow)
		require.True(t, ok)
		require.Len(t, used, 1, "a member of a type and a non-definition are not asked")
		assert.Equal(t, "run", used[0].Name)
		assert.Equal(t, 2, used[0].Count)
		assert.Equal(t, "/user2/repo1/asc/symbols?op=def&name=run", used[0].Link)
		for _, r := range fake.all() {
			assert.Equal(t, "refs/heads/master", r.GetScope().GetRev(), "the scope is the branch being viewed")
		}
	})

	t.Run("a repository without read access to the code asks nothing", func(t *testing.T) {
		before := fake.count()
		ctx, _ := contexttest.MockContext(t, "/user2/repo1/src/branch/master/src/main.rs")
		contexttest.LoadRepo(t, ctx, 1)
		ctx.Repo.TreePath = "src/main.rs"
		ctx.Repo.IsViewBranch = true
		ctx.Repo.BranchName = "master"
		ctx.Repo.Permission.Units = nil
		prepareAscOutline(ctx)
		assert.Nil(t, ctx.Data["AscOutline"])
		assert.Equal(t, before, fake.count())
	})

	t.Run("a branch name that could be an option is refused", func(t *testing.T) {
		before := fake.count()
		ctx, _ := contexttest.MockContext(t, "/user2/repo1/src/branch/x/src/main.rs")
		contexttest.LoadRepo(t, ctx, 1)
		ctx.Repo.TreePath = "src/main.rs"
		ctx.Repo.IsViewBranch = true
		ctx.Repo.BranchName = "--upload-pack=x"
		prepareAscOutline(ctx)
		assert.Equal(t, before, fake.count())
	})
}
