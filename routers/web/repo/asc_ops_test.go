// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"bytes"
	"net/http"
	"net/url"
	"strings"
	"testing"

	perm_model "forgejo.org/models/perm"
	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/services/ascidentity"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func opsTarget(b byte) *ascv1.RefTarget {
	return &ascv1.RefTarget{T: &ascv1.RefTarget_Changeset{Changeset: bytes.Repeat([]byte{b}, 32)}}
}

// opsHex is the 64 hex letters of a target made of 32 bytes b (b below 16).
func opsHex(b byte) string { return strings.Repeat("0"+string("0123456789abcdef"[b&15]), 32) }

// opsEntry is a ref log entry. The signed-in person of these tests is user2.
func opsEntry(seq uint64, ref, actor string, from, to byte, undoable bool, blocked string) *ascv1.RefLogEntry {
	e := &ascv1.RefLogEntry{Seq: seq, Ref: ref, Actor: actor, Via: "push", Time: 1_700_000_000, Undoable: undoable, UndoBlockedReason: blocked}
	if from != 0 {
		e.Old = opsTarget(from)
	}
	if to != 0 {
		e.New = opsTarget(to)
	}
	return e
}

func confirmForm(seq string, mutate func(url.Values)) url.Values {
	v := url.Values{
		"ref": {"refs/heads/feature"}, "seq": {seq}, "current": {opsHex(2)}, "restore": {opsHex(1)},
		"op": {"0190b7f5-8d5e-7000-8000-000000000001"}, "reason": {"wrong branch"},
	}
	if mutate != nil {
		mutate(v)
	}
	return v
}

func previewServer(f *fakeTrust, newest *ascv1.RefLogEntry) {
	f.listRefLog = func(r *ascv1.ListRefLogRequest) (*ascv1.ListRefLogResponse, error) {
		return &ascv1.ListRefLogResponse{Entries: []*ascv1.RefLogEntry{newest}}, nil
	}
	f.undoRef = func(r *ascv1.UndoRefRequest) (*ascv1.UndoRefResponse, error) {
		return &ascv1.UndoRefResponse{
			Result:  &ascv1.RefResult{Name: r.GetRef(), Old: opsTarget(2), New: opsTarget(1)},
			Applied: !r.GetDryRun(), NewSeq: 10, ChangesBack: 4, ApprovalsKept: 2,
		}, nil
	}
}

func opsGet(t *testing.T, path string, user int64, access perm_model.AccessMode, h func(*context.Context)) trustPage {
	t.Helper()
	return runTrust(t, trustReq{path: path, user: user, access: access}, h)
}

func TestAscOpsListShowsUndoOnlyForOwnOperations(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureStacks)
	f.listRefLog = func(r *ascv1.ListRefLogRequest) (*ascv1.ListRefLogResponse, error) {
		return &ascv1.ListRefLogResponse{Entries: []*ascv1.RefLogEntry{
			opsEntry(9, "refs/heads/feature", "user2", 1, 2, true, ""),
			opsEntry(8, "refs/heads/other", "someone", 3, 4, false, "not your operation"),
			opsEntry(7, "refs/heads/old", "user2", 5, 6, false, "outside the undo window of a protected ref"),
			opsEntry(6, "refs/heads/odd", "other-name", 5, 6, true, ""),
			opsEntry(5, "refs/heads/new", "user2", 0, 6, false, "something the hub does not know"),
		}, NextPageToken: "5"}, nil
	}
	page := opsGet(t, "/user2/repo1/asc/ops?mine=1&ref=refs/heads/", 2, perm_model.AccessModeWrite, AscOps)

	require.Equal(t, http.StatusOK, page.status, page.body)
	assert.Contains(t, page.body, `data-testid="asc-ops-list"`)
	assert.Contains(t, page.body, "asc_trust.ops.title", "text comes from the locale")
	assert.Equal(t, 1, strings.Count(page.body, `data-testid="asc-op-undo"`), "only the one undoable operation of the person gets a button")
	assert.Contains(t, page.body, "/user2/repo1/asc/ops/undo?ref=refs%2Fheads%2Ffeature&seq=9")
	assert.Contains(t, page.body, "asc_trust.ops.blocked.not_yours")
	assert.Contains(t, page.body, "asc_trust.ops.blocked.expired")
	assert.Contains(t, page.body, "asc_trust.ops.blocked.other")
	assert.Contains(t, page.body, `aria-label="asc_trust.ops.undo_aria"`, "the button names its row")
	assert.Contains(t, page.body, `rel="next"`)

	calls := f.callsOf("ListRefLog")
	require.NotEmpty(t, calls)
	req := calls[0].Req.(*ascv1.ListRefLogRequest)
	assert.Equal(t, "user2/repo1", req.GetRepo().GetName())
	assert.Equal(t, "user2", req.GetActor(), "the filter is the signed-in person's own name")
	assert.Equal(t, "refs/heads/", req.GetRef())
	assertAs(t, calls[0], "2:user2", ascidentity.RoleAdmin, "user2/repo1")
}

func TestAscOpsReadOnlyVisitorSeesNoUndoButton(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureStacks)
	f.listRefLog = func(*ascv1.ListRefLogRequest) (*ascv1.ListRefLogResponse, error) {
		return &ascv1.ListRefLogResponse{Entries: []*ascv1.RefLogEntry{opsEntry(9, "refs/heads/feature", "user2", 1, 2, true, "")}}, nil
	}
	page := opsGet(t, "/user2/repo1/asc/ops", 2, perm_model.AccessModeRead, AscOps)
	require.Equal(t, http.StatusOK, page.status)
	assert.NotContains(t, page.body, `data-testid="asc-op-undo"`)
}

func TestAscOpsNeedsSignInWriteAndAsc(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureStacks)
	post := func(user int64, access perm_model.AccessMode) int {
		return runTrust(t, trustReq{method: http.MethodPost, path: "/user2/repo1/asc/ops/undo", user: user, access: access, form: confirmForm("9", nil)}, AscOpsUndo).status
	}
	assert.Equal(t, http.StatusForbidden, opsGet(t, "/user2/repo1/asc/ops", 0, perm_model.AccessModeRead, AscOps).status, "list, not signed in")
	assert.Equal(t, http.StatusForbidden, opsGet(t, "/user2/repo1/asc/ops/undo?ref=refs/heads/feature&seq=9", 0, perm_model.AccessModeWrite, AscOpsUndoPreview).status, "preview, not signed in")
	assert.Equal(t, http.StatusForbidden, post(0, perm_model.AccessModeWrite), "undo, not signed in")
	assert.Equal(t, http.StatusForbidden, opsGet(t, "/user2/repo1/asc/ops/undo?ref=refs/heads/feature&seq=9", 4, perm_model.AccessModeRead, AscOpsUndoPreview).status, "preview, reader")
	assert.Equal(t, http.StatusForbidden, post(4, perm_model.AccessModeRead), "undo, reader")
	assert.Equal(t, http.StatusNotFound, opsGet(t, "/user2/repo1/asc/ops", 4, perm_model.AccessModeNone, AscOps).status, "list, no code access")
	assert.Empty(t, f.calls, "nothing was asked of asc")

	// A git repository has no operation log.
	setStorageBackend(t, 1, "git")
	assert.Equal(t, http.StatusNotFound, opsGet(t, "/user2/repo1/asc/ops", 2, perm_model.AccessModeWrite, AscOps).status)
	assert.Empty(t, f.calls)
}

func TestAscOpsUnsupportedServer(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t)
	page := opsGet(t, "/user2/repo1/asc/ops", 2, perm_model.AccessModeWrite, AscOps)
	assert.Equal(t, http.StatusOK, page.status)
	assert.Contains(t, page.body, "asc_trust.ops.unsupported")
	assert.Empty(t, f.callsOf("ListRefLog"))
}

func TestAscOpsListFailureShowsAFixedSentence(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureStacks)
	f.listRefLog = func(*ascv1.ListRefLogRequest) (*ascv1.ListRefLogResponse, error) {
		return nil, status.Error(codes.Internal, "database password is hunter2 at /srv/asc/db")
	}
	page := opsGet(t, "/user2/repo1/asc/ops", 2, perm_model.AccessModeWrite, AscOps)
	assert.Equal(t, http.StatusBadGateway, page.status)
	assert.Contains(t, page.body, "asc_trust.err.other")
	assert.NotContains(t, page.body, "hunter2")

	bad := opsGet(t, "/user2/repo1/asc/ops?page=abc", 2, perm_model.AccessModeWrite, AscOps)
	assert.Equal(t, http.StatusBadRequest, bad.status)
	assert.Contains(t, bad.body, "asc_trust.err.bad_input")
}

func TestAscOpsUndoPreviewIsADryRunAndCarriesTheHeads(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureStacks)
	previewServer(f, opsEntry(9, "refs/heads/feature", "user2", 1, 2, true, ""))
	page := opsGet(t, "/user2/repo1/asc/ops/undo?ref=refs/heads/feature&seq=9", 2, perm_model.AccessModeWrite, AscOpsUndoPreview)

	require.Equal(t, http.StatusOK, page.status, page.body)
	undo := f.callsOf("UndoRef")
	require.Len(t, undo, 1)
	assert.True(t, undo[0].Req.(*ascv1.UndoRefRequest).GetDryRun(), "the preview writes nothing")
	assert.Contains(t, page.body, `name="_csrf" value="csrf-token-for-tests"`, "the form carries the CSRF token")
	assert.Contains(t, page.body, `name="current" value="`+opsHex(2)+`"`)
	assert.Contains(t, page.body, `name="restore" value="`+opsHex(1)+`"`)
	assert.Contains(t, page.body, `name="seq" value="9"`)
	assert.Contains(t, page.body, `method="post"`)
	assert.Contains(t, page.body, `for="asc-undo-reason"`, "the field has a label")
	assert.Regexp(t, `name="op" value="[0-9a-f-]{36}"`, page.body)
	// What the undo does to the changes, as asc reports it for a dry run.
	assert.Contains(t, page.body, `data-testid="asc-undo-effects"`)
	assert.Contains(t, page.body, "asc_review.ops_back_", "the count of changes that go back")
	assert.Contains(t, page.body, "asc_review.ops_approvals_kept")
}

func TestAscOpsUndoPreviewRefusals(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureStacks)
	const path = "/user2/repo1/asc/ops/undo?ref=refs/heads/feature&seq=9"

	// Not the person's own operation: refused before any dry run.
	previewServer(f, opsEntry(9, "refs/heads/feature", "someone", 1, 2, true, ""))
	assert.Equal(t, http.StatusForbidden, opsGet(t, path, 2, perm_model.AccessModeWrite, AscOpsUndoPreview).status)
	assert.Empty(t, f.callsOf("UndoRef"))

	// The operation is no longer the newest of its ref.
	previewServer(f, opsEntry(12, "refs/heads/feature", "user2", 2, 3, true, ""))
	page := opsGet(t, path, 2, perm_model.AccessModeWrite, AscOpsUndoPreview)
	assert.Equal(t, http.StatusSeeOther, page.status)
	assert.Equal(t, "/user2/repo1/asc/ops", page.loc)
	assert.Empty(t, f.callsOf("UndoRef"))

	// asc says it cannot be undone.
	previewServer(f, opsEntry(9, "refs/heads/feature", "user2", 1, 2, false, "undo of protected refs is off"))
	assert.Equal(t, http.StatusSeeOther, opsGet(t, path, 2, perm_model.AccessModeWrite, AscOpsUndoPreview).status)
	assert.Empty(t, f.callsOf("UndoRef"))

	// A malformed request is not sent to asc at all.
	n := len(f.calls)
	for _, q := range []string{"ref=refs/heads/feature&seq=0", "ref=refs/heads/feature&seq=x", "ref=refs/heads/feature"} {
		assert.Equal(t, http.StatusNotFound, opsGet(t, "/user2/repo1/asc/ops/undo?"+q, 2, perm_model.AccessModeWrite, AscOpsUndoPreview).status, q)
	}
	assert.Equal(t, http.StatusSeeOther, opsGet(t, "/user2/repo1/asc/ops/undo?ref=refs/heads/a%20b&seq=3", 2, perm_model.AccessModeWrite, AscOpsUndoPreview).status)
	assert.Len(t, f.calls, n, "malformed requests make no call")
}

func postUndo(t *testing.T, mutate func(url.Values)) trustPage {
	t.Helper()
	return runTrust(t, trustReq{method: http.MethodPost, path: "/user2/repo1/asc/ops/undo", user: 2, access: perm_model.AccessModeWrite, form: confirmForm("9", mutate)}, AscOpsUndo)
}

func TestAscOpsUndoAppliesWhatWasShown(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureStacks)
	previewServer(f, opsEntry(9, "refs/heads/feature", "user2", 1, 2, true, ""))
	page := postUndo(t, nil)

	require.Equal(t, http.StatusSeeOther, page.status, page.body)
	assert.Equal(t, "/user2/repo1/asc/ops?redo=10&redo_ref=refs%2Fheads%2Ffeature", page.loc, "the log offers the redo of the undo it has just made")
	undo := f.callsOf("UndoRef")
	require.Len(t, undo, 1)
	req := undo[0].Req.(*ascv1.UndoRefRequest)
	assert.False(t, req.GetDryRun())
	assert.EqualValues(t, 9, req.GetSeq())
	assert.Equal(t, "refs/heads/feature", req.GetRef())
	assert.Equal(t, "wrong branch", req.GetReason())
	assert.Equal(t, "0190b7f5-8d5e-7000-8000-000000000001", undo[0].OpID, "the form's operation id makes a double submit one undo")
	assertAs(t, undo[0], "2:user2", ascidentity.RoleAdmin, "user2/repo1")
}

func TestAscOpsUndoIsBoundToTheHeadsThatWereShown(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureStacks)
	previewServer(f, opsEntry(9, "refs/heads/feature", "user2", 1, 2, true, ""))

	for name, mutate := range map[string]func(url.Values){
		"another sequence number": func(v url.Values) { v.Set("seq", "8") },
		"the ref moved":           func(v url.Values) { v.Set("current", opsHex(7)) },
		"another restore target":  func(v url.Values) { v.Set("restore", opsHex(7)) },
		"no restore target":       func(v url.Values) { v.Set("restore", "") },
	} {
		page := postUndo(t, mutate)
		assert.Equal(t, http.StatusSeeOther, page.status, name)
		assert.Equal(t, "/user2/repo1/asc/ops", page.loc, name)
	}
	assert.Empty(t, f.callsOf("UndoRef"), "an undo that does not match what was shown is never sent")
}

func TestAscOpsUndoRefusesWhatIsNotTheirs(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureStacks)
	// asc itself would allow it (undoable), but the actor is another person.
	previewServer(f, opsEntry(9, "refs/heads/feature", "someone", 1, 2, true, ""))
	assert.Equal(t, http.StatusForbidden, postUndo(t, nil).status)
	assert.Empty(t, f.callsOf("UndoRef"))
}

func TestAscOpsUndoBadInputAndServerRefusals(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureStacks)
	previewServer(f, opsEntry(9, "refs/heads/feature", "user2", 1, 2, true, ""))
	for name, mutate := range map[string]func(url.Values){
		"seq not a number":  func(v url.Values) { v.Set("seq", "x") },
		"seq zero":          func(v url.Values) { v.Set("seq", "0") },
		"bad head":          func(v url.Values) { v.Set("current", "xyz") },
		"bad operation id":  func(v url.Values) { v.Set("op", "not-a-uuid") },
		"reason too long":   func(v url.Values) { v.Set("reason", strings.Repeat("a", 300)) },
		"reason has escape": func(v url.Values) { v.Set("reason", "a\x1b[31mb") },
		"prefix as a ref":   func(v url.Values) { v.Set("ref", "refs/heads/") },
	} {
		assert.Contains(t, []int{http.StatusBadRequest, http.StatusSeeOther}, postUndo(t, mutate).status, name)
	}
	assert.Empty(t, f.callsOf("UndoRef"))

	// The compare-and-swap inside asc found the ref moved: the person is told to look again.
	f.undoRef = func(*ascv1.UndoRefRequest) (*ascv1.UndoRefResponse, error) {
		return nil, status.Error(codes.Aborted, "moved")
	}
	assert.Equal(t, http.StatusSeeOther, postUndo(t, nil).status)
	assert.Len(t, f.callsOf("UndoRef"), 1)

	// An answer that does not match the request is not believed.
	f.undoRef = func(r *ascv1.UndoRefRequest) (*ascv1.UndoRefResponse, error) {
		return &ascv1.UndoRefResponse{Result: &ascv1.RefResult{Name: "refs/heads/elsewhere", Old: opsTarget(2), New: opsTarget(1)}, Applied: true}, nil
	}
	assert.Equal(t, http.StatusSeeOther, postUndo(t, nil).status)
}

func TestAscOpsOffersTheRedoOfAnUndo(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureStacks)
	f.listRefLog = func(*ascv1.ListRefLogRequest) (*ascv1.ListRefLogResponse, error) {
		return &ascv1.ListRefLogResponse{}, nil
	}
	page := opsGet(t, "/user2/repo1/asc/ops?redo=12&redo_ref=refs%2Fheads%2Fmain", 2, perm_model.AccessModeWrite, AscOps)
	require.Equal(t, http.StatusOK, page.status, page.body)
	assert.Contains(t, page.body, "asc-ops-redo")
	assert.Contains(t, page.body, "seq=12")
	for _, bad := range []string{"?redo=0", "?redo=-1", "?redo=abc"} {
		page = opsGet(t, "/user2/repo1/asc/ops"+bad, 2, perm_model.AccessModeWrite, AscOps)
		assert.NotContains(t, page.body, "asc-ops-redo", bad)
	}
	page = opsGet(t, "/user2/repo1/asc/ops?redo=12&redo_ref=refs%2Fheads%2Fmain", 2, perm_model.AccessModeRead, AscOps)
	assert.NotContains(t, page.body, "asc-ops-redo", "a reader who cannot undo is not offered a redo")
}

// A restack the person made, and can still undo, is the banner above the list; someone else's restack, or one that cannot be
// undone, is not.
func TestAscOpsRestackBanner(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureStacks)
	restack := opsEntry(9, "refs/heads/retry", "user2", 1, 2, true, "")
	restack.Via = "restack"
	f.listRefLog = func(*ascv1.ListRefLogRequest) (*ascv1.ListRefLogResponse, error) {
		return &ascv1.ListRefLogResponse{Entries: []*ascv1.RefLogEntry{restack, opsEntry(8, "refs/heads/other", "user2", 3, 4, true, "")}}, nil
	}
	page := opsGet(t, "/user2/repo1/asc/ops", 2, perm_model.AccessModeWrite, AscOps)
	require.Equal(t, http.StatusOK, page.status, page.body)
	assert.Contains(t, page.body, `data-testid="asc-ops-restack"`)
	assert.Contains(t, page.body, "asc_review.ops_restack_undo")
	assert.Contains(t, page.body, "/user2/repo1/asc/ops/undo?ref=refs%2Fheads%2Fretry&seq=9")

	restack.Actor = "someone"
	restack.Undoable = false
	restack.UndoBlockedReason = "not your operation"
	page = opsGet(t, "/user2/repo1/asc/ops", 2, perm_model.AccessModeWrite, AscOps)
	assert.NotContains(t, page.body, `data-testid="asc-ops-restack"`)
}
