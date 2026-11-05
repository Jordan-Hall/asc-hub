// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/http"
	"testing"

	perm_model "forgejo.org/models/perm"
	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/services/asctrust"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func undoLastServer(f *fakeTrust, entries ...*ascv1.RefLogEntry) {
	f.listRefLog = func(r *ascv1.ListRefLogRequest) (*ascv1.ListRefLogResponse, error) {
		return &ascv1.ListRefLogResponse{Entries: entries}, nil
	}
}

func TestAscOpsUndoLastGoesToTheNewestUndoableOperation(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureStacks)
	undoLastServer(f,
		opsEntry(9, "refs/heads/feature", "user2", 2, 3, false, "moved"),
		opsEntry(8, "refs/heads/feature", "user2", 1, 2, true, ""),
		opsEntry(7, "refs/heads/other", "user2", 1, 2, true, ""),
	)
	page := opsGet(t, "/user2/repo1/asc/ops/undo/last", 2, perm_model.AccessModeWrite, AscOpsUndoLast)

	require.Equal(t, http.StatusSeeOther, page.status, page.body)
	assert.Equal(t, "/user2/repo1/asc/ops/undo?ref=refs%2Fheads%2Ffeature&seq=8", page.loc, "the dry run of the undo, nothing written")
	calls := f.callsOf("ListRefLog")
	require.Len(t, calls, 1)
	req := calls[0].Req.(*ascv1.ListRefLogRequest)
	assert.Equal(t, "user2", req.GetActor(), "only the person's own operations")
	assert.Equal(t, uint32(ascUndoLastScan), req.GetLimit())
	assert.Empty(t, f.callsOf("UndoRef"))
}

func TestAscOpsUndoLastWithNothingToUndo(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureStacks)
	undoLastServer(f, opsEntry(9, "refs/heads/feature", "user2", 2, 3, false, "expired"))
	page := opsGet(t, "/user2/repo1/asc/ops/undo/last", 2, perm_model.AccessModeWrite, AscOpsUndoLast)

	require.Equal(t, http.StatusSeeOther, page.status, page.body)
	assert.Equal(t, "/user2/repo1/asc/ops?mine=1", page.loc)
	assert.Equal(t, "asc_trust.ops.nothing_mine", page.ctx.Flash.InfoMsg)
}

func TestAscOpsUndoLastRefusals(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureStacks)
	undoLastServer(f, opsEntry(8, "refs/heads/feature", "user2", 1, 2, true, ""))

	page := opsGet(t, "/user2/repo1/asc/ops/undo/last", 2, perm_model.AccessModeRead, AscOpsUndoLast)
	assert.Equal(t, http.StatusForbidden, page.status, "a reader cannot undo")
	page = opsGet(t, "/user2/repo1/asc/ops/undo/last", 0, perm_model.AccessModeRead, AscOpsUndoLast)
	assert.NotEqual(t, http.StatusSeeOther, page.status, "a visitor is not sent to an undo")
	assert.Empty(t, f.callsOf("ListRefLog"))
}

func TestAscOpsUndoLastWithoutStacks(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	startFakeTrust(t)
	page := opsGet(t, "/user2/repo1/asc/ops/undo/last", 2, perm_model.AccessModeWrite, AscOpsUndoLast)
	assert.Equal(t, http.StatusNotFound, page.status)
}
