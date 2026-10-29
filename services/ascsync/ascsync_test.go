// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascsync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	repo_model "forgejo.org/models/repo"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const zeroID = "0000000000000000000000000000000000000000"

func TestBackedBy(t *testing.T) {
	for value, want := range map[string]bool{"": false, "git": false, "dual": false, "asc": true} {
		got, err := BackedBy(&repo_model.Repository{StorageBackend: value})
		require.NoError(t, err, value)
		assert.Equal(t, want, got, value)
	}
	// A bad value is an error, never plain git.
	_, err := BackedBy(&repo_model.Repository{StorageBackend: "ASC"})
	require.Error(t, err)
	_, err = BackedBy(nil)
	require.ErrorIs(t, err, ErrBadRequest)
}

func TestOpenFailsClosed(t *testing.T) {
	fx := newFixture(t)
	push := func(repo *repo_model.Repository) error {
		return ImportPush(t.Context(), ImportRequest{Repo: repo, Refs: []PushRef{{Name: "refs/heads/x", Old: zeroID, New: fx.commit("c")}}})
	}

	require.NoError(t, push(fx.repo))
	require.Len(t, fx.fake.imports, 1)

	setting.Asc.Enabled = false
	require.ErrorIs(t, push(fx.repo), ErrDisabled)
	setting.Asc.Enabled = true

	plain := *fx.repo
	plain.StorageBackend = "git"
	require.ErrorIs(t, push(&plain), ErrNotAsc)

	other := *fx.repo
	other.ObjectFormatName = "sha256"
	require.ErrorIs(t, push(&other), ErrObjectFormat)

	require.Len(t, fx.fake.imports, 1, "nothing was sent for the refused calls")
}

func TestImportPushSendsTheCommands(t *testing.T) {
	fx := newFixture(t)
	c1, c2 := fx.commit("one"), fx.commit("two")
	objects := filepath.Join(fx.bridge, "objects")
	quarantine := filepath.Join(objects, "tmp_objdir-incoming-abc")
	require.NoError(t, os.MkdirAll(quarantine, 0o755))

	err := ImportPush(t.Context(), ImportRequest{
		Repo:     fx.repo,
		PusherID: -2, // the Actions user
		// git writes the quarantine path with a "./" in it
		Dirs: PushDirs{ObjectDir: fx.bridge + "/./objects/tmp_objdir-incoming-abc", AlternateDirs: []string{fx.bridge + "/./objects"}},
		Refs: []PushRef{
			{Name: "refs/heads/main", Old: c1, New: c2},
			{Name: "refs/for/main/topic", Old: zeroID, New: c2}, // AGit: never stored
			{Name: "refs/tags/v1", Old: zeroID, New: c1},
			{Name: "refs/heads/gone", Old: c1, New: zeroID},
		},
	})
	require.NoError(t, err)

	require.Len(t, fx.fake.imports, 1)
	req := fx.fake.imports[0]
	assert.Equal(t, "owner/repo", req.GetRepo().GetName())
	assert.Equal(t, ascv1.IdMode_ID_MODE_ASC, req.GetRepo().GetIdMode())
	assert.Equal(t, fx.bridge, req.GetGitDir())
	assert.Equal(t, quarantine, req.GetObjectDir())
	assert.Equal(t, []string{objects}, req.GetAlternateDirs())
	assert.Equal(t, int64(0), req.GetPusherId(), "a negative user id is sent as 0")
	require.Len(t, req.GetUpdates(), 3, "refs/for is left out")
	assert.Equal(t, "refs/heads/main", req.GetUpdates()[0].GetName())
	assert.Equal(t, mustHex(c1), req.GetUpdates()[0].GetOldGit())
	assert.Equal(t, mustHex(c2), req.GetUpdates()[0].GetNewGit())
	assert.Equal(t, mustHex(zeroID), req.GetUpdates()[1].GetOldGit())
	assert.Equal(t, mustHex(zeroID), req.GetUpdates()[2].GetNewGit())
}

func TestImportPushWithOnlyAgitRefsCallsNothing(t *testing.T) {
	fx := newFixture(t)
	c := fx.commit("one")
	require.NoError(t, ImportPush(t.Context(), ImportRequest{
		Repo: fx.repo,
		Refs: []PushRef{{Name: "refs/for/main", Old: zeroID, New: c}},
	}))
	assert.Empty(t, fx.fake.callNames())
}

func TestImportPushDefaultsToTheObjectsDirectory(t *testing.T) {
	fx := newFixture(t)
	require.NoError(t, ImportPush(t.Context(), ImportRequest{
		Repo: fx.repo, PusherID: 7,
		Refs: []PushRef{{Name: "refs/heads/x", Old: zeroID, New: fx.commit("c")}},
	}))
	assert.Equal(t, filepath.Join(fx.bridge, "objects"), fx.fake.imports[0].GetObjectDir())
	assert.Equal(t, int64(7), fx.fake.imports[0].GetPusherId())
}

func TestImportPushRefusesWhatItMustNotSend(t *testing.T) {
	fx := newFixture(t)
	c := fx.commit("one")
	ok := PushRef{Name: "refs/heads/x", Old: zeroID, New: c}
	elsewhere := t.TempDir()
	// A symlink inside objects that leads out of the repository.
	require.NoError(t, os.Symlink(elsewhere, filepath.Join(fx.bridge, "objects", "escape")))

	many := make([]string, MaxAlternates+1)
	for i := range many {
		many[i] = filepath.Join(fx.bridge, "objects")
	}
	tooMany := make([]PushRef, MaxRefUpdates+1)
	for i := range tooMany {
		tooMany[i] = PushRef{Name: fmt.Sprintf("refs/heads/b%d", i), Old: zeroID, New: c}
	}

	cases := map[string]ImportRequest{
		"object dir elsewhere":      {Dirs: PushDirs{ObjectDir: elsewhere}},
		"object dir is a symlink":   {Dirs: PushDirs{ObjectDir: filepath.Join(fx.bridge, "objects", "escape")}},
		"relative object dir":       {Dirs: PushDirs{ObjectDir: "objects/incoming"}},
		"object dir dot dot":        {Dirs: PushDirs{ObjectDir: filepath.Join(fx.bridge, "objects", "..", "..")}},
		"object dir missing":        {Dirs: PushDirs{ObjectDir: filepath.Join(fx.bridge, "objects", "nope")}},
		"newline in the object dir": {Dirs: PushDirs{ObjectDir: filepath.Join(fx.bridge, "objects") + "\nx"}},
		"alternate elsewhere":       {Dirs: PushDirs{AlternateDirs: []string{elsewhere}}},
		"too many alternates":       {Dirs: PushDirs{AlternateDirs: many}},
		"ref without refs/":         {Refs: []PushRef{{Name: "heads/x", Old: zeroID, New: c}}},
		"ref with a control byte":   {Refs: []PushRef{{Name: "refs/heads/x\x1b[31m", Old: zeroID, New: c}}},
		"empty ref":                 {Refs: []PushRef{{Name: "", Old: zeroID, New: c}}},
		"long ref":                  {Refs: []PushRef{{Name: "refs/heads/" + strings.Repeat("a", MaxRefNameBytes), Old: zeroID, New: c}}},
		"short id":                  {Refs: []PushRef{{Name: "refs/heads/x", Old: zeroID, New: c[:39]}}},
		"upper case id":             {Refs: []PushRef{{Name: "refs/heads/x", Old: zeroID, New: strings.ToUpper(c)}}},
		"not hex":                   {Refs: []PushRef{{Name: "refs/heads/x", Old: strings.Repeat("g", 40), New: c}}},
		"too many commands":         {Refs: tooMany},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			req.Repo = fx.repo
			if req.Refs == nil {
				req.Refs = []PushRef{ok}
			}
			err := ImportPush(t.Context(), req)
			require.ErrorIs(t, err, ErrBadRequest)
			_, isReject := RejectMessage(err)
			assert.False(t, isReject)
		})
	}
	assert.Empty(t, fx.fake.callNames(), "nothing reached asc")
}

func TestImportPushAcceptsAQuarantineBehindASymlinkedRoot(t *testing.T) {
	fx := newFixture(t)
	// The hook environment holds the physical path; the repository path goes through a symlink.
	physical := t.TempDir()
	require.NoError(t, os.Rename(fx.bridge, filepath.Join(physical, "repo.git")))
	require.NoError(t, os.Symlink(filepath.Join(physical, "repo.git"), fx.bridge))
	q := filepath.Join(physical, "repo.git", "objects", "tmp_objdir-incoming-1")
	require.NoError(t, os.MkdirAll(q, 0o755))

	require.NoError(t, ImportPush(t.Context(), ImportRequest{
		Repo: fx.repo,
		Dirs: PushDirs{ObjectDir: q},
		Refs: []PushRef{{Name: "refs/heads/x", Old: zeroID, New: strings.Repeat("a", 40)}},
	}))
	assert.Equal(t, q, fx.fake.imports[0].GetObjectDir())
}

func TestImportPushMapsAscAnswers(t *testing.T) {
	fx := newFixture(t)
	req := ImportRequest{Repo: fx.repo, Refs: []PushRef{{Name: "refs/heads/x", Old: zeroID, New: fx.commit("c")}}}

	// The pusher sees asc's refusal, cleaned of anything a terminal would act on.
	fx.fake.importErr = status.Error(codes.FailedPrecondition, "! [rejected] refs/heads/x (fetch first)\x1b[31m\r\nsecond line")
	err := ImportPush(t.Context(), req)
	msg, ok := RejectMessage(err)
	require.True(t, ok, "%v", err)
	assert.Equal(t, "! [rejected] refs/heads/x (fetch first) [31m  second line", msg)

	fx.fake.importErr = status.Error(codes.PermissionDenied, "")
	msg, ok = RejectMessage(ImportPush(t.Context(), req))
	require.True(t, ok)
	assert.Equal(t, "asc refused the request (PermissionDenied)", msg)

	fx.fake.importErr = status.Error(codes.InvalidArgument, strings.Repeat("é", MaxRejectBytes))
	msg, ok = RejectMessage(ImportPush(t.Context(), req))
	require.True(t, ok)
	assert.LessOrEqual(t, len(msg), MaxRejectBytes)
	assert.True(t, utf8.ValidString(msg))

	// Server trouble is not the pusher's to read: it is an ordinary error.
	for _, c := range []codes.Code{codes.Internal, codes.Unknown, codes.Unauthenticated, codes.DataLoss, codes.Unimplemented} {
		fx.fake.importErr = status.Error(c, "secret detail of the server")
		err = ImportPush(t.Context(), req)
		require.Error(t, err, c)
		_, isReject := RejectMessage(err)
		assert.False(t, isReject, c)
	}
}

func TestImportPushNeedsAnAnswerPerCommand(t *testing.T) {
	fx := newFixture(t)
	fx.fake.importShort = true
	err := ImportPush(t.Context(), ImportRequest{Repo: fx.repo, Refs: []PushRef{{Name: "refs/heads/x", Old: zeroID, New: fx.commit("c")}}})
	require.ErrorIs(t, err, ErrBadAnswer)
}

func TestImportPushStopsWithTheContext(t *testing.T) {
	fx := newFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := ImportPush(ctx, ImportRequest{Repo: fx.repo, Refs: []PushRef{{Name: "refs/heads/x", Old: zeroID, New: fx.commit("c")}}})
	require.Error(t, err)
	_, isReject := RejectMessage(err)
	assert.False(t, isReject)
}

func TestDirsFromHook(t *testing.T) {
	sep := string(os.PathListSeparator)
	d := DirsFromHook("/q", "/other", "/a"+sep+sep+"/b")
	assert.Equal(t, PushDirs{ObjectDir: "/q", AlternateDirs: []string{"/a", "/b"}}, d)
	assert.Equal(t, "/quarantine", DirsFromHook("", "/quarantine", "").ObjectDir)
	assert.Empty(t, DirsFromHook("", "", "").AlternateDirs)
}

func TestClean(t *testing.T) {
	assert.Equal(t, "a b c", clean("a\x00b\tc", 100))
	assert.Equal(t, "abc", clean("a\xffbc", 100))
	assert.Equal(t, "é", clean("éé", 3), "a cut never splits a rune")
	assert.Equal(t, "x", clean("  x \n", 100))
	assert.Empty(t, clean("\x1b", 10))
}

// ---------------------------------------------------------------------------------- pull heads

func TestSetPullHeadForAKnownCommit(t *testing.T) {
	fx := newFixture(t)
	c := fx.commit("one")
	fx.fake.learn(c)
	// The bridge follows 100 ms after asc: SetPullHead must wait for that and never write the bridge itself.
	fx.fake.promoteDelay = 100 * time.Millisecond

	start := time.Now()
	require.NoError(t, SetPullHead(t.Context(), fx.repo, "refs/pull/3/head", c))
	assert.GreaterOrEqual(t, time.Since(start), 90*time.Millisecond)
	assert.Equal(t, c, fx.bridgeRef("refs/pull/3/head"))

	assert.Equal(t, []string{"GitMap", "UpdateRefs"}, fx.fake.callNames(), "a known commit is not imported")
	req := fx.fake.updates[0]
	assert.Equal(t, "owner/repo", req.GetRepo().GetName())
	assert.Equal(t, ascv1.IdMode_ID_MODE_ASC, req.GetRepo().GetIdMode())
	assert.Equal(t, ascv1.HookMode_HOOK_MODE_SKIP_PRE, req.GetHooks())
	assert.Equal(t, ascv1.UpdateMode_UPDATE_MODE_FORCE, req.GetMode(), "a pull request head may be rewritten")
	require.Len(t, req.GetUpdates(), 1)
	u := req.GetUpdates()[0]
	assert.Equal(t, "refs/pull/3/head", u.GetName())
	assert.True(t, u.GetAny())
	assert.False(t, u.GetDelete())
	assert.Equal(t, fx.fake.known[c], u.GetNew().GetChangeset())
}

func TestSetPullHeadImportsAnUnknownCommit(t *testing.T) {
	fx := newFixture(t)
	c1 := fx.commit("one")
	require.NoError(t, SetPullHead(t.Context(), fx.repo, "refs/pull/4/head", c1))

	assert.Equal(t, []string{"GitMap", "ListRefs", "ImportGitPush"}, fx.fake.callNames())
	imp := fx.fake.imports[0]
	assert.Equal(t, fx.bridge, imp.GetGitDir())
	assert.Equal(t, filepath.Join(fx.bridge, "objects"), imp.GetObjectDir(), "the commit is read from the bridge's objects")
	assert.Empty(t, imp.GetAlternateDirs())
	require.Len(t, imp.GetUpdates(), 1)
	assert.Equal(t, "refs/pull/4/head", imp.GetUpdates()[0].GetName())
	assert.Equal(t, mustHex(zeroID), imp.GetUpdates()[0].GetOldGit(), "the ref was absent")
	assert.Equal(t, mustHex(c1), imp.GetUpdates()[0].GetNewGit())
	assert.Empty(t, fx.fake.updates, "the import moved the ref")
	assert.Equal(t, c1, fx.bridgeRef("refs/pull/4/head"))

	// The next head is a fast-forward of the first: the import names the first as the old value.
	c2 := fx.commit("two", c1)
	require.NoError(t, SetPullHead(t.Context(), fx.repo, "refs/pull/4/head", c2))
	imp = fx.fake.imports[1]
	assert.Equal(t, mustHex(c1), imp.GetUpdates()[0].GetOldGit())
	assert.Equal(t, mustHex(c2), imp.GetUpdates()[0].GetNewGit())
	assert.Equal(t, c2, fx.bridgeRef("refs/pull/4/head"))
}

func TestSetPullHeadForcesAHeadThatIsNotAFastForward(t *testing.T) {
	fx := newFixture(t)
	c1 := fx.commit("one")
	require.NoError(t, SetPullHead(t.Context(), fx.repo, "refs/pull/5/head", c1))

	// A rewritten branch: asc refuses the fast-forward, but it has imported the commit by then.
	rewritten := fx.commit("rewritten")
	fx.fake.importErr = status.Error(codes.FailedPrecondition, "! [rejected] refs/pull/5/head (fetch first)")
	fx.fake.importLearns = true
	require.NoError(t, SetPullHead(t.Context(), fx.repo, "refs/pull/5/head", rewritten))

	require.Len(t, fx.fake.updates, 1)
	assert.Equal(t, fx.fake.known[rewritten], fx.fake.updates[0].GetUpdates()[0].GetNew().GetChangeset())
	assert.Equal(t, rewritten, fx.bridgeRef("refs/pull/5/head"))
}

func TestSetPullHeadFailsWhenTheImportDidNotLearnTheCommit(t *testing.T) {
	fx := newFixture(t)
	fx.fake.importErr = status.Error(codes.FailedPrecondition, "! [rejected] refs/pull/6/head (fetch first)")
	err := SetPullHead(t.Context(), fx.repo, "refs/pull/6/head", fx.commit("one"))
	require.ErrorIs(t, err, ErrBadAnswer)
	assert.Empty(t, fx.fake.updates)
}

func TestSetPullHeadSurfacesARefusalAndInfrastructureFailure(t *testing.T) {
	fx := newFixture(t)
	c := fx.commit("one")

	fx.fake.importErr = status.Error(codes.InvalidArgument, "a path is not allowed")
	err := SetPullHead(t.Context(), fx.repo, "refs/pull/7/head", c)
	msg, ok := RejectMessage(err)
	require.True(t, ok)
	assert.Equal(t, "a path is not allowed", msg)

	fx.fake.learn(c)
	fx.fake.updateErr = status.Error(codes.Unavailable, "down")
	err = SetPullHead(t.Context(), fx.repo, "refs/pull/7/head", c)
	require.Error(t, err)
	_, isReject := RejectMessage(err)
	assert.False(t, isReject)
	assert.Empty(t, fx.bridgeRef("refs/pull/7/head"), "a failed update leaves the bridge alone")
}

func TestSetPullHeadFailsClosedWhenTheBridgeNeverFollows(t *testing.T) {
	fx := newFixture(t)
	c := fx.commit("one")
	fx.fake.learn(c)
	fx.fake.promoteDelay = -1
	bridgeWait = 150 * time.Millisecond
	err := SetPullHead(t.Context(), fx.repo, "refs/pull/8/head", c)
	require.ErrorIs(t, err, ErrBridgeBehind)
	assert.Empty(t, fx.bridgeRef("refs/pull/8/head"), "the bridge is written only by asc")
}

func TestSetPullHeadStopsWaitingWithTheContext(t *testing.T) {
	fx := newFixture(t)
	c := fx.commit("one")
	fx.fake.learn(c)
	fx.fake.promoteDelay = -1
	bridgeWait = time.Minute
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
	defer cancel()
	err := SetPullHead(ctx, fx.repo, "refs/pull/9/head", c)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestSetPullHeadChecksItsInputs(t *testing.T) {
	fx := newFixture(t)
	c := fx.commit("one")
	for name, args := range map[string][2]string{
		"a branch":           {"refs/heads/x", c},
		"index zero":         {"refs/pull/0/head", c},
		"leading zero":       {"refs/pull/01/head", c},
		"not a head":         {"refs/pull/1/merge", c},
		"path trick":         {"refs/pull/1/head/../../heads/x", c},
		"a long index":       {"refs/pull/1234567890123456789/head", c},
		"the zero commit":    {"refs/pull/1/head", zeroID},
		"a short commit":     {"refs/pull/1/head", c[:20]},
		"an upper case hash": {"refs/pull/1/head", strings.ToUpper(c)},
		"a ref name":         {"refs/pull/1/head", "master"},
	} {
		t.Run(name, func(t *testing.T) {
			require.ErrorIs(t, SetPullHead(t.Context(), fx.repo, args[0], args[1]), ErrBadRequest)
		})
	}
	assert.Empty(t, fx.fake.callNames())
}

func TestSetPullHeadRefusesAForeignBackend(t *testing.T) {
	fx := newFixture(t)
	plain := *fx.repo
	plain.StorageBackend = "dual"
	require.ErrorIs(t, SetPullHead(t.Context(), &plain, "refs/pull/1/head", fx.commit("c")), ErrNotAsc)
	assert.Empty(t, fx.fake.callNames())
}

func TestDeletePullHead(t *testing.T) {
	fx := newFixture(t)
	c := fx.commit("one")
	require.NoError(t, SetPullHead(t.Context(), fx.repo, "refs/pull/2/head", c))
	require.NotEmpty(t, fx.bridgeRef("refs/pull/2/head"))

	// The clean-up of a failed pull request still runs when the request is gone.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.NoError(t, DeletePullHead(ctx, fx.repo, "refs/pull/2/head"))
	u := fx.fake.updates[len(fx.fake.updates)-1]
	assert.Equal(t, ascv1.HookMode_HOOK_MODE_SKIP_PRE, u.GetHooks())
	require.Len(t, u.GetUpdates(), 1)
	assert.True(t, u.GetUpdates()[0].GetDelete())
	assert.Empty(t, fx.bridgeRef("refs/pull/2/head"))

	require.ErrorIs(t, DeletePullHead(t.Context(), fx.repo, "refs/heads/master"), ErrBadRequest)
}

func TestIsFetchFirst(t *testing.T) {
	assert.True(t, isFetchFirst("! [rejected] refs/pull/1/head (fetch first)"))
	assert.False(t, isFetchFirst("! [rejected] refs/pull/1/head (already exists)"))
	assert.False(t, isFetchFirst("fetch first"))
}

func TestImportPushForAnImporterNamesTheUserAndUsesTheBulkClient(t *testing.T) {
	fx := newFixture(t)
	c1 := fx.commit("one")
	refs := []PushRef{{Name: "refs/heads/main", Old: zeroID, New: c1}}

	ctx := WithImporter(WithBulkImport(t.Context()), 42)
	require.NoError(t, ImportPush(ctx, ImportRequest{Repo: fx.repo, Refs: refs}))
	require.Len(t, fx.fake.imports, 1)
	assert.Equal(t, int64(42), fx.fake.imports[0].GetPusherId(), "the importing user is the pusher")

	// A request that names a pusher keeps it; the importer fills the gap only.
	require.NoError(t, ImportPush(WithImporter(t.Context(), 42), ImportRequest{Repo: fx.repo, PusherID: 7, Refs: refs}))
	assert.Equal(t, int64(7), fx.fake.imports[1].GetPusherId())

	// No importer, no pusher.
	require.NoError(t, ImportPush(t.Context(), ImportRequest{Repo: fx.repo, Refs: refs}))
	assert.Equal(t, int64(0), fx.fake.imports[2].GetPusherId())
}

func TestBulkImportAllowsALongerCall(t *testing.T) {
	assert.False(t, isBulk(t.Context()))
	assert.True(t, isBulk(WithBulkImport(t.Context())))
	assert.Greater(t, BulkImportTimeout, ImportTimeout)
	assert.Equal(t, int64(0), importerOf(t.Context()))
	assert.Equal(t, int64(0), importerOf(WithImporter(t.Context(), -3)), "the Actions user is no importer")
}

func TestImportHistoryImportsOnceAndRepeatsAsNoOp(t *testing.T) {
	fx := newFixture(t)
	c1 := fx.commit("one")
	c2 := fx.commit("two", c1)
	want := []PushRef{
		{Name: "refs/heads/main", New: c2},
		{Name: "refs/tags/v1", New: c1},
		{Name: "refs/heads/gone", New: zeroID}, // nothing to import
	}
	ctx := WithImporter(WithBulkImport(t.Context()), 9)
	require.NoError(t, ImportHistory(ctx, fx.repo, want))
	require.Len(t, fx.fake.imports, 1)
	req := fx.fake.imports[0]
	assert.Equal(t, filepath.Join(fx.bridge, "objects"), req.GetObjectDir())
	assert.Equal(t, int64(9), req.GetPusherId())
	require.Len(t, req.GetUpdates(), 2)
	assert.Equal(t, "refs/heads/main", req.GetUpdates()[0].GetName())
	assert.Equal(t, mustHex(zeroID), req.GetUpdates()[0].GetOldGit(), "a ref asc lacks is created")
	assert.Equal(t, mustHex(c2), req.GetUpdates()[0].GetNewGit())

	// Run again: asc holds every ref, so nothing is sent.
	require.NoError(t, ImportHistory(ctx, fx.repo, want))
	assert.Len(t, fx.fake.imports, 1)

	// A branch that moved on is sent as a fast-forward from where asc has it, never forced.
	c3 := fx.commit("three", c2)
	require.NoError(t, ImportHistory(ctx, fx.repo, []PushRef{{Name: "refs/heads/main", New: c3}}))
	require.Len(t, fx.fake.imports, 2)
	u := fx.fake.imports[1].GetUpdates()[0]
	assert.Equal(t, mustHex(c2), u.GetOldGit())
	assert.Equal(t, mustHex(c3), u.GetNewGit())
}

func TestImportHistoryStopsAtARefusal(t *testing.T) {
	fx := newFixture(t)
	c1 := fx.commit("one")
	fx.fake.importErr = status.Error(codes.InvalidArgument, "path policy: .env is not allowed")
	err := ImportHistory(t.Context(), fx.repo, []PushRef{{Name: "refs/heads/main", New: c1}})
	msg, ok := RejectMessage(err)
	require.True(t, ok)
	assert.Equal(t, "path policy: .env is not allowed", msg)
	assert.Len(t, fx.fake.imports, 1, "a refused import is not retried with force")

	other := *fx.repo
	other.StorageBackend = "git"
	require.ErrorIs(t, ImportHistory(t.Context(), &other, nil), ErrNotAsc)
}

// A refusal whose status details disagree with its status reaches the hub as Internal with the real status in the
// text. The pusher must still read why (a secret was found), not "try again later".
func TestCallErrorReadsTheRefusalOutOfAStatusMismatch(t *testing.T) {
	err := callError("import push", status.Error(codes.Internal,
		`grpc-status-details-bin mismatch: grpc-status=FailedPrecondition, grpc-message="SECRET_DETECTED: creds.txt:1 aws-access-key-id, creds.txt:2 aws-secret-access-key", grpc-status-details-bin=message:"x"`))
	msg, ok := RejectMessage(err)
	require.True(t, ok, "%v", err)
	assert.Equal(t, "SECRET_DETECTED: creds.txt:1 aws-access-key-id, creds.txt:2 aws-secret-access-key", msg)

	// An Internal error that is only that stays internal.
	_, ok = RejectMessage(callError("import push", status.Error(codes.Internal, "boom")))
	assert.False(t, ok)
	// So does a mismatch that names a status the pusher is not to read.
	_, ok = RejectMessage(callError("import push", status.Error(codes.Internal,
		`grpc-status-details-bin mismatch: grpc-status=Unavailable, grpc-message="down"`)))
	assert.False(t, ok)
}
