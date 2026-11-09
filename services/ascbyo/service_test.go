// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascbyo_test

import (
	"fmt"
	"strings"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/translation"
	"forgejo.org/services/ascbyo"
	"forgejo.org/services/ascbyo/ascbyotest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func scope(t *testing.T, k ascbyo.Kind, name string) ascbyo.Scope {
	t.Helper()
	sc, err := ascbyo.NewScope(k, name)
	require.NoError(t, err)
	return sc
}

func goodInput() *ascbyo.StorageInput {
	return &ascbyo.StorageInput{
		Name: "primary", Endpoint: "https://s3.example.com", Region: "eu", Bucket: "my-bucket", Prefix: "asc",
		AccessKeyID: "AKIAIOSFODNN7EXAMPLE", SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", SessionToken: "FwoGZXIvYXdzEXAMPLETOKEN",
	}
}

func newService(t *testing.T) (*ascbyo.Service, *ascbyotest.Fake) {
	t.Helper()
	f := ascbyotest.Start(t)
	s, err := ascbyo.New()
	require.NoError(t, err)
	return s, f
}

func TestStorageCreateAndList(t *testing.T) {
	s, f := newService(t)
	alice := scope(t, ascbyo.KindUser, "Alice")
	bob := scope(t, ascbyo.KindUser, "bob")
	in := goodInput()

	op := ascbyo.NewOpID()
	tg, fe, err := s.CreateTarget(t.Context(), alice, in, op)
	require.NoError(t, err)
	require.Empty(t, fe)
	assert.Equal(t, ascv1.StorageTargetState_STORAGE_TARGET_STATE_PENDING, tg.GetState())

	calls := f.CallsTo("CreateTarget")
	require.Len(t, calls, 1)
	assert.Equal(t, "Bearer "+ascbyotest.InternalToken, calls[0].Auth, "a system call with the internal token")
	assert.NotEqual(t, op, calls[0].OpID, "the form's op id is never sent as it is")
	_, perr := uuid.Parse(calls[0].OpID)
	require.NoError(t, perr, "the derived op id is a uuid")
	req := calls[0].Req.(*ascv1.CreateStorageTargetRequest)
	assert.Equal(t, "alice", req.GetOwner().GetName(), "the owner is the scope, lower case")
	assert.Equal(t, ascv1.OwnerKind_OWNER_KIND_USER, req.GetOwner().GetKind())
	assert.Equal(t, "asc/", req.GetPrefix())

	// credentials are never in what comes back, and so never in a view
	view := ascbyo.TargetViewOf(tg)
	dump := fmt.Sprintf("%+v %+v", view, tg)
	for _, secret := range []string{in.SecretAccessKey, in.SessionToken, "IOSFODNN7"} {
		assert.NotContains(t, dump, secret)
	}
	assert.Equal(t, "AKIA...MPLE", view.KeyHint)
	assert.True(t, view.HasSecret)
	assert.True(t, view.NeedsVerify)
	assert.False(t, view.CanEnable)

	ts, err := s.ListTargets(t.Context(), alice)
	require.NoError(t, err)
	require.Len(t, ts, 1)
	ts, err = s.ListTargets(t.Context(), bob)
	require.NoError(t, err)
	assert.Empty(t, ts, "another owner sees nothing")

	t.Run("same name twice", func(t *testing.T) {
		_, _, err := s.CreateTarget(t.Context(), alice, goodInput(), "")
		assert.True(t, ascbyo.IsKind(err, ascbyo.FailExists), "%v", err)
	})
	t.Run("invalid input never reaches asc", func(t *testing.T) {
		before := len(f.Calls())
		bad := goodInput()
		bad.Endpoint, bad.Name = "ftp://x", "other"
		_, fe, err := s.CreateTarget(t.Context(), alice, bad, "")
		require.NoError(t, err)
		assert.Contains(t, fe, "endpoint")
		assert.Len(t, f.Calls(), before)
	})
	t.Run("an op id that is not a uuid is ignored", func(t *testing.T) {
		in := goodInput()
		in.Name = "third"
		_, _, err := s.CreateTarget(t.Context(), alice, in, "not-a-uuid\r\nx-evil: 1")
		require.NoError(t, err)
		c := f.CallsTo("CreateTarget")
		assert.NotContains(t, c[len(c)-1].OpID, "evil")
		assert.NotEmpty(t, c[len(c)-1].OpID, "the client made one")
	})
}

func TestStorageEndpointBlockedIsMapped(t *testing.T) {
	s, _ := newService(t)
	alice := scope(t, ascbyo.KindUser, "alice")
	for endpoint, reason := range map[string]string{"http://s3.example.com": "not_https", "https://169.254.169.254": "metadata"} {
		in := goodInput()
		in.Endpoint = endpoint
		_, _, err := s.CreateTarget(t.Context(), alice, in, "")
		f, ok := ascbyo.AsFailure(err)
		require.True(t, ok, "%v", err)
		assert.Equal(t, ascbyo.FailEndpointBlocked, f.Kind)
		assert.Equal(t, reason, f.Reason)
		assert.Equal(t, "ascbyo.blocked."+reason, f.ReasonLocaleKey())
		assert.Equal(t, "ascbyo.error.endpoint_blocked", f.LocaleKey())
	}
}

func TestStorageErrorsNeverEchoSecrets(t *testing.T) {
	s, f := newService(t)
	f.SecretEcho = true
	alice := scope(t, ascbyo.KindUser, "alice")
	in := goodInput()
	f.SetFail("/asc.v1.Storage/CreateTarget", status.Error(codes.InvalidArgument, "bad key "+in.SecretAccessKey+" for "+in.AccessKeyID))
	_, _, err := s.CreateTarget(t.Context(), alice, in, "")
	fail, ok := ascbyo.AsFailure(err)
	require.True(t, ok)
	assert.Equal(t, ascbyo.FailInvalid, fail.Kind)
	assert.Empty(t, fail.Detail, "a message that repeats a credential is dropped whole")
	assert.NotContains(t, err.Error(), in.SecretAccessKey)

	f.SetFail("/asc.v1.Storage/CreateTarget", status.Error(codes.InvalidArgument, "bucket name is not valid\x1b[2J\nsecond line"))
	_, _, err = s.CreateTarget(t.Context(), alice, in, "")
	fail, _ = ascbyo.AsFailure(err)
	assert.Equal(t, "bucket name is not valid [2J second line", fail.Detail, "control characters are gone")
}

func TestStorageOwnershipOfIDs(t *testing.T) {
	s, f := newService(t)
	alice := scope(t, ascbyo.KindUser, "alice")
	bob := scope(t, ascbyo.KindUser, "bob")
	bobsOrg := scope(t, ascbyo.KindOrg, "bob") // same name, other kind
	bobT := f.AddTarget(bob.Owner(), "bobs", ascv1.StorageTargetState_STORAGE_TARGET_STATE_ENABLED)
	orgT := f.AddTarget(bobsOrg.Owner(), "orgs", ascv1.StorageTargetState_STORAGE_TARGET_STATE_ENABLED)
	mutationsBefore := f.Mutations()

	ctx := t.Context()
	for name, run := range map[string]func(id string) error{
		"get":    func(id string) error { _, err := s.GetTarget(ctx, alice, id); return err },
		"delete": func(id string) error { return s.DeleteTarget(ctx, alice, id, "") },
		"verify": func(id string) error { _, err := s.VerifyTarget(ctx, alice, id, ""); return err },
		"enable": func(id string) error { _, err := s.SetEnabled(ctx, alice, id, false, ""); return err },
		"update": func(id string) error {
			in := goodInput()
			_, _, _, err := s.UpdateTarget(ctx, alice, id, in, "")
			return err
		},
	} {
		for _, id := range []string{bobT.GetId(), orgT.GetId(), "st_doesnotexist000000", "../etc", "", "ak_abcdefghijklmnopqrst"} {
			err := run(id)
			assert.True(t, ascbyo.IsKind(err, ascbyo.FailNotFound), "%s %q: %v", name, id, err)
		}
	}
	assert.Equal(t, mutationsBefore, f.Mutations(), "no mutating call was made for another owner's id")
	assert.Len(t, f.CallsTo("DeleteTarget"), 0)

	// and the owner can
	require.NoError(t, s.DeleteTarget(ctx, bob, bobT.GetId(), ""))
}

func TestStorageUpdateSendsOnlyChanges(t *testing.T) {
	s, f := newService(t)
	alice := scope(t, ascbyo.KindUser, "alice")
	tg, _, err := s.CreateTarget(t.Context(), alice, goodInput(), "")
	require.NoError(t, err)

	cur := ascbyo.TargetViewOf(tg)
	same := &ascbyo.StorageInput{Name: cur.Name, Endpoint: cur.Endpoint, Region: cur.Region, Bucket: cur.Bucket, Prefix: cur.Prefix}
	_, changed, fe, err := s.UpdateTarget(t.Context(), alice, tg.GetId(), same, "")
	require.NoError(t, err)
	require.Empty(t, fe)
	assert.False(t, changed, "nothing to send")
	assert.Empty(t, f.CallsTo("UpdateTarget"))

	edit := *same
	edit.Name = "renamed"
	_, changed, _, err = s.UpdateTarget(t.Context(), alice, tg.GetId(), &edit, "")
	require.NoError(t, err)
	assert.True(t, changed)
	req := f.CallsTo("UpdateTarget")[0].Req.(*ascv1.UpdateStorageTargetRequest)
	assert.Equal(t, "renamed", req.GetName())
	assert.Nil(t, req.Bucket)
	assert.Nil(t, req.Endpoint)
	assert.Nil(t, req.Credentials, "credentials stay when none were typed")

	creds := edit
	creds.Bucket = "new-bucket"
	creds.AccessKeyID, creds.SecretAccessKey = "AKIANEWKEYNEWKEY1234", "brand-new-secret-value"
	got, changed, _, err := s.UpdateTarget(t.Context(), alice, tg.GetId(), &creds, "")
	require.NoError(t, err)
	assert.True(t, changed)
	req = f.CallsTo("UpdateTarget")[1].Req.(*ascv1.UpdateStorageTargetRequest)
	assert.Equal(t, "brand-new-secret-value", req.GetCredentials().GetSecretAccessKey())
	assert.Equal(t, "new-bucket", req.GetBucket())
	assert.Equal(t, ascv1.StorageTargetState_STORAGE_TARGET_STATE_PENDING, got.GetState(), "a change moves the target back to pending")
	assert.NotContains(t, fmt.Sprintf("%+v", ascbyo.TargetViewOf(got)), "brand-new-secret-value")

	invalid := creds
	invalid.SecretAccessKey = ""
	_, _, fe, err = s.UpdateTarget(t.Context(), alice, tg.GetId(), &invalid, "")
	require.NoError(t, err)
	assert.Contains(t, fe, "secret_access_key")
	assert.Len(t, f.CallsTo("UpdateTarget"), 2)
}

func TestStorageVerifyEnableDelete(t *testing.T) {
	s, f := newService(t)
	alice := scope(t, ascbyo.KindUser, "alice")
	tg, _, err := s.CreateTarget(t.Context(), alice, goodInput(), "")
	require.NoError(t, err)
	id := tg.GetId()

	t.Run("enable before verify is refused by asc and mapped", func(t *testing.T) {
		_, err := s.SetEnabled(t.Context(), alice, id, true, "")
		assert.True(t, ascbyo.IsKind(err, ascbyo.FailVerifyFailed), "%v", err)
	})
	t.Run("a failed verification is a result with the failing step", func(t *testing.T) {
		f.VerifyBad = true
		res, err := s.VerifyTarget(t.Context(), alice, id, "")
		require.NoError(t, err)
		assert.False(t, res.OK)
		require.Len(t, res.Checks, 3, "resolve, connect, put; it stops at the first failure")
		last := res.Checks[2]
		assert.False(t, last.OK)
		assert.Equal(t, "ascbyo.storage.step.put", last.StepKey)
		assert.Equal(t, "s3:PutObject", last.Needs, "the permission the key lacks is named")
		assert.False(t, last.Advisory)
		assert.Equal(t, "access denied [31m", last.Message, "control characters are stripped")
		assert.NotContains(t, last.Message, "\x1b")
		v := ascbyo.TargetViewOf(res.Target)
		assert.Equal(t, "failed", v.StateName)
		assert.Equal(t, ascbyo.ToneBad, v.Tone)
		assert.Equal(t, "put: access denied", v.LastError)
	})
	t.Run("a passing verification then enable and disable", func(t *testing.T) {
		f.VerifyBad = false
		res, err := s.VerifyTarget(t.Context(), alice, id, "")
		require.NoError(t, err)
		assert.True(t, res.OK)
		require.Len(t, res.Checks, 6)
		assert.Equal(t, "ascbyo.storage.step.versioning", res.Checks[5].StepKey)
		assert.True(t, res.Checks[5].Advisory)
		f.VersioningOff = true
		nover, err := s.VerifyTarget(t.Context(), alice, id, "")
		f.VersioningOff = false
		require.NoError(t, err)
		assert.True(t, nover.OK, "a bucket without versioning still verifies")
		require.Len(t, nover.Checks, 6)
		assert.False(t, nover.Checks[5].OK)
		assert.True(t, nover.Checks[5].Advisory)
		assert.Empty(t, nover.Checks[5].Needs, "a permission asc does not name is dropped")
		v := ascbyo.TargetViewOf(res.Target)
		assert.True(t, v.CanEnable)
		on, err := s.SetEnabled(t.Context(), alice, id, true, "")
		require.NoError(t, err)
		assert.True(t, ascbyo.TargetViewOf(on).CanDisable)
		off, err := s.SetEnabled(t.Context(), alice, id, false, "")
		require.NoError(t, err)
		assert.Equal(t, "disabled", ascbyo.TargetViewOf(off).StateName)
	})
	t.Run("delete while in use", func(t *testing.T) {
		f.InUse = true
		assert.True(t, ascbyo.IsKind(s.DeleteTarget(t.Context(), alice, id, ""), ascbyo.FailInUse))
		f.InUse = false
		require.NoError(t, s.DeleteTarget(t.Context(), alice, id, ""))
		_, err := s.GetTarget(t.Context(), alice, id)
		assert.True(t, ascbyo.IsKind(err, ascbyo.FailNotFound))
	})
	t.Run("asc errors map to fixed kinds", func(t *testing.T) {
		tg2 := f.AddTarget(alice.Owner(), "two", ascv1.StorageTargetState_STORAGE_TARGET_STATE_VERIFIED)
		for _, tc := range []struct {
			err  error
			want ascbyo.FailureKind
		}{
			{status.Error(codes.Unavailable, "connection refused 10.0.0.5:9000"), ascbyo.FailUnavailable},
			{status.Error(codes.DeadlineExceeded, "slow"), ascbyo.FailUnavailable},
			{status.Error(codes.Unimplemented, "no storage.v1"), ascbyo.FailUnimplemented},
			{status.Error(codes.PermissionDenied, "no"), ascbyo.FailDenied},
			{status.Error(codes.Unauthenticated, "no"), ascbyo.FailDenied},
			{status.Error(codes.ResourceExhausted, "6 per hour"), ascbyo.FailRateLimited},
			{status.Error(codes.Internal, "boom /var/lib/asc/secret"), ascbyo.FailInternal},
			{ascbyotest.Detail(codes.ResourceExhausted, "quota", "QUOTA_EXCEEDED"), ascbyo.FailQuota},
			{ascbyotest.Detail(codes.Aborted, "busy", "MIGRATION_ACTIVE"), ascbyo.FailMigrationActive},
		} {
			f.SetFail("/asc.v1.Storage/VerifyTarget", tc.err)
			_, err := s.VerifyTarget(t.Context(), alice, tg2.GetId(), "")
			fail, ok := ascbyo.AsFailure(err)
			require.True(t, ok, "%v", err)
			assert.Equal(t, tc.want, fail.Kind, "%v", tc.err)
			assert.Empty(t, fail.Detail)
			assert.NotContains(t, err.Error(), "10.0.0.5")
			assert.NotContains(t, err.Error(), "/var/lib")
		}
	})
}

func TestRepoStorageAssignAndMigrate(t *testing.T) {
	s, f := newService(t)
	repo := scope(t, ascbyo.KindRepo, "Alice/Widgets")
	owner := scope(t, ascbyo.KindUser, "alice")
	other := scope(t, ascbyo.KindUser, "mallory")
	allowed := []ascbyo.Scope{repo, owner}

	enabled := f.AddTarget(owner.Owner(), "ok", ascv1.StorageTargetState_STORAGE_TARGET_STATE_ENABLED)
	pending := f.AddTarget(owner.Owner(), "pending", ascv1.StorageTargetState_STORAGE_TARGET_STATE_PENDING)
	foreign := f.AddTarget(other.Owner(), "theirs", ascv1.StorageTargetState_STORAGE_TARGET_STATE_ENABLED)
	own := f.AddTarget(repo.Owner(), "repo-own", ascv1.StorageTargetState_STORAGE_TARGET_STATE_ENABLED)

	groups, err := s.ListRepoTargets(t.Context(), allowed)
	require.NoError(t, err)
	require.Len(t, groups, 2)
	assert.Len(t, groups[0].Targets, 1)
	assert.Len(t, groups[1].Targets, 2)

	t.Run("assign", func(t *testing.T) {
		require.NoError(t, s.AssignRepo(t.Context(), repo, allowed, enabled.GetId(), ""))
		require.NoError(t, s.AssignRepo(t.Context(), repo, allowed, own.GetId(), ""))
		require.NoError(t, s.AssignRepo(t.Context(), repo, allowed, "", ""), "the instance default")
		assert.True(t, ascbyo.IsKind(s.AssignRepo(t.Context(), repo, allowed, pending.GetId(), ""), ascbyo.FailNotReady), "only an enabled target")
		assert.True(t, ascbyo.IsKind(s.AssignRepo(t.Context(), repo, allowed, foreign.GetId(), ""), ascbyo.FailNotFound), "not one the user may use")
		assert.True(t, ascbyo.IsKind(s.AssignRepo(t.Context(), repo, allowed, "junk", ""), ascbyo.FailNotFound))
		assert.True(t, ascbyo.IsKind(s.AssignRepo(t.Context(), owner, allowed, enabled.GetId(), ""), ascbyo.FailInvalid), "a repository scope is required")
		assert.Len(t, f.CallsTo("AssignRepo"), 3, "refusals sent nothing")
		last := f.CallsTo("AssignRepo")[0].Req.(*ascv1.AssignRepoStorageRequest)
		assert.Equal(t, "alice/widgets", last.GetRepo().GetName())
	})

	t.Run("migrate", func(t *testing.T) {
		m, err := s.StartMigration(t.Context(), repo, allowed, enabled.GetId(), true, "")
		require.NoError(t, err)
		assert.True(t, m.Active)
		assert.True(t, m.Cancellable)
		assert.True(t, m.DeleteSrc)
		assert.Equal(t, 0, m.Percent)
		req := f.CallsTo("StartMigration")[0].Req.(*ascv1.StartStorageMigrationRequest)
		assert.True(t, req.GetDeleteSource())
		assert.Equal(t, enabled.GetId(), req.GetToTargetId())

		_, err = s.StartMigration(t.Context(), repo, allowed, own.GetId(), false, "")
		assert.True(t, ascbyo.IsKind(err, ascbyo.FailMigrationActive), "%v", err)
		_, err = s.StartMigration(t.Context(), repo, allowed, foreign.GetId(), false, "")
		assert.True(t, ascbyo.IsKind(err, ascbyo.FailNotFound))
		_, err = s.StartMigration(t.Context(), repo, allowed, pending.GetId(), false, "")
		assert.True(t, ascbyo.IsKind(err, ascbyo.FailNotReady))
		assert.Len(t, f.CallsTo("StartMigration"), 2, "only the allowed enabled target and the active-conflict reached asc")

		got, err := s.GetMigration(t.Context(), repo, m.ID)
		require.NoError(t, err)
		assert.Equal(t, m.ID, got.ID)
		list, err := s.ListMigrations(t.Context(), repo)
		require.NoError(t, err)
		require.Len(t, list, 1)

		c, err := s.CancelMigration(t.Context(), repo, m.ID, "")
		require.NoError(t, err)
		assert.False(t, c.Active)
		assert.Equal(t, "ascbyo.migration.state.cancelled", c.StateKey)
	})

	t.Run("a migration of another repository is not found", func(t *testing.T) {
		otherRepo := scope(t, ascbyo.KindRepo, "mallory/secret")
		m := f.AddMigration(&ascv1.StorageMigration{Repo: otherRepo.Repo(), State: ascv1.StorageMigrationState_STORAGE_MIGRATION_STATE_COPYING})
		_, err := s.GetMigration(t.Context(), repo, m.GetId())
		assert.True(t, ascbyo.IsKind(err, ascbyo.FailNotFound))
		_, err = s.CancelMigration(t.Context(), repo, m.GetId(), "")
		assert.True(t, ascbyo.IsKind(err, ascbyo.FailNotFound))
		assert.Equal(t, ascv1.StorageMigrationState_STORAGE_MIGRATION_STATE_COPYING, m.GetState(), "not cancelled")
		list, err := s.ListMigrations(t.Context(), repo)
		require.NoError(t, err)
		for _, x := range list {
			assert.NotEqual(t, m.GetId(), x.ID)
		}
	})
}

func TestMigrationProgress(t *testing.T) {
	repo := scope(t, ascbyo.KindRepo, "alice/widgets")
	mk := func(st ascv1.StorageMigrationState, total, copied uint64) *ascbyo.Migration {
		return ascbyo.MigrationFrom(repo, &ascv1.StorageMigration{Id: "mg_1", Repo: repo.Repo(), State: st, ObjectsTotal: total, ObjectsCopied: copied, Error: "bad\x00thing"})
	}
	assert.Equal(t, 50, mk(ascv1.StorageMigrationState_STORAGE_MIGRATION_STATE_COPYING, 200, 100).Percent)
	assert.Equal(t, 100, mk(ascv1.StorageMigrationState_STORAGE_MIGRATION_STATE_COPYING, 10, 99).Percent, "never above 100")
	assert.Equal(t, 0, mk(ascv1.StorageMigrationState_STORAGE_MIGRATION_STATE_QUEUED, 0, 0).Percent, "no total yet")
	done := mk(ascv1.StorageMigrationState_STORAGE_MIGRATION_STATE_DONE, 10, 10)
	assert.Equal(t, 100, done.Percent)
	assert.False(t, done.Active)
	assert.False(t, done.Cancellable)
	sw := mk(ascv1.StorageMigrationState_STORAGE_MIGRATION_STATE_SWITCHING, 10, 10)
	assert.True(t, sw.Active)
	assert.False(t, sw.Cancellable, "the switch is one transaction")
	failed := mk(ascv1.StorageMigrationState_STORAGE_MIGRATION_STATE_FAILED, 10, 3)
	assert.False(t, failed.Active)
	assert.Equal(t, "bad thing", failed.Error)
	assert.Nil(t, ascbyo.MigrationFrom(repo, &ascv1.StorageMigration{Id: "../x", Repo: repo.Repo()}))
	assert.Nil(t, ascbyo.MigrationFrom(repo, &ascv1.StorageMigration{Id: "mg_1", Repo: &ascv1.RepoRef{Name: "bob/other"}}))
	assert.Nil(t, ascbyo.MigrationFrom(repo, nil))
	odd := ascbyo.MigrationFrom(repo, &ascv1.StorageMigration{Id: "mg_1", Repo: repo.Repo(), ToTargetId: "javascript:alert(1)", FromTargetId: "st_abcdefghijklmnopqrst"})
	assert.Empty(t, odd.ToTarget, "a target id that is not one is dropped")
	assert.Equal(t, "st_abcdefghijklmnopqrst", odd.FromTarget)
}

func TestMigrationETA(t *testing.T) {
	repo := scope(t, ascbyo.KindRepo, "alice/widgets")
	copying := ascv1.StorageMigrationState_STORAGE_MIGRATION_STATE_COPYING
	mk := func(st ascv1.StorageMigrationState, paused bool, elapsed int64, total, copied, bytesTotal, bytes uint64) *ascbyo.Migration {
		return ascbyo.MigrationFrom(repo, &ascv1.StorageMigration{
			Id: "mg_1", Repo: repo.Repo(), State: st, Paused: paused, Created: 1000, Updated: 1000 + elapsed,
			ObjectsTotal: total, ObjectsCopied: copied, BytesTotal: bytesTotal, BytesCopied: bytes,
		})
	}
	// A quarter of the bytes in 100 s leaves 300 s, whatever the object count says.
	assert.EqualValues(t, 300, mk(copying, false, 100, 10, 9, 4000, 1000).ETASeconds)
	// Before asc counted the bytes the objects give the pace.
	assert.EqualValues(t, 100, mk(copying, false, 100, 10, 5, 0, 0).ETASeconds)
	assert.EqualValues(t, 1, mk(copying, false, 1, 1000, 999, 0, 0).ETASeconds, "never rounds to nothing while there is something left")
	assert.EqualValues(t, 365*24*3600, mk(copying, false, 1<<40, 1<<40, 1, 0, 0).ETASeconds, "a hopeless pace is capped")
	for name, m := range map[string]*ascbyo.Migration{
		"nothing moved":   mk(copying, false, 100, 10, 0, 0, 0),
		"no time passed":  mk(copying, false, 0, 10, 5, 0, 0),
		"all moved":       mk(copying, false, 100, 10, 10, 0, 0),
		"paused":          mk(copying, true, 100, 10, 5, 0, 0),
		"checking":        mk(ascv1.StorageMigrationState_STORAGE_MIGRATION_STATE_VERIFYING, false, 100, 10, 5, 0, 0),
		"waiting":         mk(ascv1.StorageMigrationState_STORAGE_MIGRATION_STATE_QUEUED, false, 100, 10, 5, 0, 0),
		"clock backwards": mk(copying, false, -50, 10, 5, 0, 0),
	} {
		assert.Zero(t, m.ETASeconds, name)
	}

	l := translation.MockLocale{}
	at := func(s int64) string { return (&ascbyo.Migration{ETASeconds: s}).ETALine(l) }
	assert.Empty(t, at(0))
	assert.Equal(t, "ascbyo.storage.repo.eta_soon", at(59))
	assert.Equal(t, "ascbyo.storage.repo.eta_minutes_1", at(60))
	assert.Equal(t, "ascbyo.storage.repo.eta_minutes_1", at(90*60-1))
	assert.Equal(t, "ascbyo.storage.repo.eta_hours_1", at(90*60))
	assert.Equal(t, "ascbyo.storage.repo.eta_days_1", at(48*3600))

	m := mk(copying, false, 100, 10, 5, 0, 0)
	assert.Equal(t, "ascbyo.storage.repo.progress \u00b7 ascbyo.storage.repo.eta_minutes_1", m.ProgressLine(l), "no bytes until they are counted")
	m = mk(copying, false, 100, 10, 5, 2048, 1024)
	assert.Equal(t, "ascbyo.storage.repo.progress \u00b7 ascbyo.storage.repo.progress_bytes \u00b7 ascbyo.storage.repo.eta_minutes_1", m.ProgressLine(l))
	assert.EqualValues(t, 2048, m.SizeTotal())
	assert.EqualValues(t, 1024, m.SizeCopied())
	assert.Empty(t, m.RedoneLine(l))
	redone := ascbyo.MigrationFrom(repo, &ascv1.StorageMigration{Id: "mg_1", Repo: repo.Repo(), Mismatched: 3})
	assert.EqualValues(t, 3, redone.Mismatched)
	assert.Equal(t, "ascbyo.storage.repo.redone_1", redone.RedoneLine(l))
}

func TestMigrationPaused(t *testing.T) {
	repo := scope(t, ascbyo.KindRepo, "alice/widgets")
	mk := func(st ascv1.StorageMigrationState, paused bool) *ascbyo.Migration {
		return ascbyo.MigrationFrom(repo, &ascv1.StorageMigration{Id: "mg_1", Repo: repo.Repo(), State: st, Paused: paused})
	}
	running := mk(ascv1.StorageMigrationState_STORAGE_MIGRATION_STATE_COPYING, false)
	assert.True(t, running.Pausable)
	assert.False(t, running.Resumable)
	held := mk(ascv1.StorageMigrationState_STORAGE_MIGRATION_STATE_COPYING, true)
	assert.True(t, held.Paused)
	assert.True(t, held.Active, "a paused migration is still the repository's active one")
	assert.Equal(t, "copying", held.StateName, "the slug keeps the phase it stopped in")
	assert.Equal(t, "ascbyo.migration.state.paused", held.StateKey)
	assert.False(t, held.Pausable)
	assert.True(t, held.Resumable)
	for _, st := range []ascv1.StorageMigrationState{
		ascv1.StorageMigrationState_STORAGE_MIGRATION_STATE_SWITCHING,
		ascv1.StorageMigrationState_STORAGE_MIGRATION_STATE_DONE,
		ascv1.StorageMigrationState_STORAGE_MIGRATION_STATE_CANCELLED,
	} {
		m := mk(st, true)
		assert.False(t, m.Paused || m.Pausable || m.Resumable, "%v: no pause once the switch has begun", st)
	}
}

func TestAIKeys(t *testing.T) {
	s, f := newService(t)
	alice := scope(t, ascbyo.KindUser, "alice")
	bob := scope(t, ascbyo.KindUser, "bob")
	org := scope(t, ascbyo.KindOrg, "alice")
	apiKey := "sk-ant-api03-SECRETSECRETSECRET1234"

	in := &ascbyo.AIKeyInput{Label: "Team", Provider: "anthropic", APIKey: apiKey, DefaultModel: "claude-sonnet-4-5", CapUSD: "20"}
	k, fe, err := s.CreateKey(t.Context(), alice, in, ascbyo.NewOpID())
	require.NoError(t, err)
	require.Empty(t, fe)
	req := f.CallsTo("CreateKey")[0].Req.(*ascv1.CreateAiKeyRequest)
	assert.Equal(t, uint64(20_000_000), req.GetMonthlyCapMicroUsd())
	assert.Equal(t, ascv1.AiProvider_AI_PROVIDER_ANTHROPIC, req.GetProvider())
	assert.Equal(t, "alice", req.GetOwner().GetName())

	v := ascbyo.KeyViewOf(k)
	assert.Equal(t, "1234", v.KeyHint)
	assert.NotContains(t, fmt.Sprintf("%+v %+v", v, k), "SECRETSECRET")
	assert.Equal(t, "untested", v.StateName)
	assert.True(t, v.NeedsTest)
	assert.Equal(t, "$20.00", v.Cap)
	assert.Equal(t, "20", v.CapInput)
	assert.Equal(t, "ascbyo.ai.provider.anthropic", v.ProviderKey)

	t.Run("owner scoping", func(t *testing.T) {
		mutations := f.Mutations()
		for _, sc := range []ascbyo.Scope{bob, org} {
			_, err := s.GetKey(t.Context(), sc, k.GetId())
			assert.True(t, ascbyo.IsKind(err, ascbyo.FailNotFound), sc.String())
			_, err = s.TestKey(t.Context(), sc, k.GetId(), "")
			assert.True(t, ascbyo.IsKind(err, ascbyo.FailNotFound))
			assert.True(t, ascbyo.IsKind(s.DeleteKey(t.Context(), sc, k.GetId(), ""), ascbyo.FailNotFound))
			_, err = s.SetKeyDisabled(t.Context(), sc, k.GetId(), true, "")
			assert.True(t, ascbyo.IsKind(err, ascbyo.FailNotFound))
			_, err = s.ListModels(t.Context(), sc, k.GetId())
			assert.True(t, ascbyo.IsKind(err, ascbyo.FailNotFound))
			_, err = s.Usage(t.Context(), sc, k.GetId(), ascv1.AiUsageGroup_AI_USAGE_GROUP_DAY)
			assert.True(t, ascbyo.IsKind(err, ascbyo.FailNotFound))
			_, _, _, err = s.UpdateKey(t.Context(), sc, k.GetId(), &ascbyo.AIKeyInput{Label: "x"}, "")
			assert.True(t, ascbyo.IsKind(err, ascbyo.FailNotFound))
		}
		assert.Equal(t, mutations, f.Mutations())
		ks, err := s.ListKeys(t.Context(), bob)
		require.NoError(t, err)
		assert.Empty(t, ks)
	})

	t.Run("test, models, usage", func(t *testing.T) {
		res, err := s.TestKey(t.Context(), alice, k.GetId(), "")
		require.NoError(t, err)
		assert.True(t, res.OK)
		assert.Equal(t, "ok", res.Status)
		assert.Equal(t, "active", ascbyo.KeyViewOf(res.Key).StateName)

		f.TestStatus = "unauthorized"
		res, err = s.TestKey(t.Context(), alice, k.GetId(), "")
		require.NoError(t, err)
		assert.False(t, res.OK)
		assert.Equal(t, "unauthorized", res.Status)
		f.TestStatus = "weird <script>"
		res, _ = s.TestKey(t.Context(), alice, k.GetId(), "")
		assert.Equal(t, "bad_response", res.Status, "an unknown status is a bad response")

		f.Models = []*ascv1.AiModel{{Id: "claude-sonnet-4-5", DisplayName: "Sonnet\n4.5", ContextTokens: 200000}, {Id: "<script>", DisplayName: "x"}}
		models, err := s.ListModels(t.Context(), alice, k.GetId())
		require.NoError(t, err)
		require.Len(t, models, 1, "an id that is not a model id is dropped")
		assert.Equal(t, "Sonnet 4.5", models[0].DisplayName)

		f.UsageResp = &ascv1.AiUsageResponse{
			Rows:  []*ascv1.AiUsageRow{{Group: "claude-sonnet-4-5", Requests: 1200, InputTokens: 3_400_000, OutputTokens: 56_000, CostMicroUsd: 4_200_000}},
			Total: &ascv1.AiUsageRow{Requests: 1200, CostMicroUsd: 4_200_000},
		}
		u, err := s.Usage(t.Context(), alice, "", ascv1.AiUsageGroup_AI_USAGE_GROUP_MODEL)
		require.NoError(t, err)
		rows := ascbyo.UsageViews(u.Rows)
		require.Len(t, rows, 1)
		assert.Equal(t, "1,200", rows[0].Requests)
		assert.Equal(t, "3,400,000", rows[0].InputTokens)
		assert.Equal(t, "$4.20", rows[0].Cost)
		assert.Equal(t, "$4.20", ascbyo.UsageRowViewOf(u.Total).Cost)
	})

	t.Run("update", func(t *testing.T) {
		_, changed, _, err := s.UpdateKey(t.Context(), alice, k.GetId(), &ascbyo.AIKeyInput{Label: "Team", DefaultModel: "claude-sonnet-4-5", CapUSD: "20"}, "")
		require.NoError(t, err)
		assert.False(t, changed)
		assert.Empty(t, f.CallsTo("UpdateKey"))

		newKey := "sk-ant-api03-NEWNEWNEWNEW9876"
		got, changed, _, err := s.UpdateKey(t.Context(), alice, k.GetId(), &ascbyo.AIKeyInput{Label: "Team", APIKey: newKey, DefaultModel: "claude-sonnet-4-5", CapUSD: "30"}, "")
		require.NoError(t, err)
		assert.True(t, changed)
		r := f.CallsTo("UpdateKey")[0].Req.(*ascv1.UpdateAiKeyRequest)
		assert.Equal(t, newKey, r.GetApiKey())
		assert.Equal(t, uint64(30_000_000), r.GetMonthlyCapMicroUsd())
		assert.Nil(t, r.Label)
		assert.Nil(t, r.BaseUrl)
		assert.Equal(t, "9876", ascbyo.KeyViewOf(got).KeyHint)
		assert.Equal(t, "untested", ascbyo.KeyViewOf(got).StateName, "a new key must be tested again")

		off, err := s.SetKeyDisabled(t.Context(), alice, k.GetId(), true, "")
		require.NoError(t, err)
		v := ascbyo.KeyViewOf(off)
		assert.True(t, v.Disabled)
		assert.True(t, v.CanEnable)
		assert.Equal(t, ascv1.AiKeyState_AI_KEY_STATE_DISABLED, off.GetState())
	})

	t.Run("blocked base url and echoed secrets", func(t *testing.T) {
		bad := &ascbyo.AIKeyInput{Label: "Local", Provider: "openai_compatible", APIKey: "local-key-0123456789", BaseURL: "https://127.0.0.1/v1"}
		_, _, err := s.CreateKey(t.Context(), alice, bad, "")
		fail, ok := ascbyo.AsFailure(err)
		require.True(t, ok)
		assert.Equal(t, ascbyo.FailEndpointBlocked, fail.Kind)
		assert.Equal(t, "loopback", fail.Reason)

		f.SecretEcho = true
		echo := &ascbyo.AIKeyInput{Label: "x", Provider: "openai", APIKey: "sk-openai-ECHOECHOECHO"}
		f.SetFail("/asc.v1.AiProviders/CreateKey", status.Error(codes.InvalidArgument, "rejected "+echo.APIKey))
		_, _, err = s.CreateKey(t.Context(), alice, echo, "")
		fail, _ = ascbyo.AsFailure(err)
		assert.Empty(t, fail.Detail)
		assert.NotContains(t, err.Error(), "ECHOECHO")
	})

	t.Run("cap and untested errors", func(t *testing.T) {
		f.SetFail("/asc.v1.AiProviders/TestKey", ascbyotest.Detail(codes.ResourceExhausted, "cap", "AI_CAP_REACHED"))
		_, err := s.TestKey(t.Context(), alice, k.GetId(), "")
		assert.True(t, ascbyo.IsKind(err, ascbyo.FailCapReached))
		f.SetFail("/asc.v1.AiProviders/TestKey", ascbyotest.Detail(codes.FailedPrecondition, "x", "AI_KEY_UNTESTED"))
		_, err = s.TestKey(t.Context(), alice, k.GetId(), "")
		assert.True(t, ascbyo.IsKind(err, ascbyo.FailKeyUntested))
		f.SetFail("/asc.v1.AiProviders/TestKey", nil)
	})

	t.Run("delete", func(t *testing.T) {
		require.NoError(t, s.DeleteKey(t.Context(), alice, k.GetId(), ""))
		_, err := s.GetKey(t.Context(), alice, k.GetId())
		assert.True(t, ascbyo.IsKind(err, ascbyo.FailNotFound))
	})
}

func TestKeyViewSpend(t *testing.T) {
	mk := func(spent, cap uint64, st ascv1.AiKeyState) ascbyo.KeyView {
		return ascbyo.KeyViewOf(&ascv1.AiKey{Id: "ak_abcdefghijklmnopqrst", Label: "<b>x</b>", Provider: ascv1.AiProvider_AI_PROVIDER_OPENAI, State: st, MonthSpentMicroUsd: spent, MonthlyCapMicroUsd: cap})
	}
	v := mk(5_000_000, 20_000_000, ascv1.AiKeyState_AI_KEY_STATE_ACTIVE)
	assert.True(t, v.HasCap)
	assert.Equal(t, 25, v.Percent)
	assert.Equal(t, "$5.00", v.Spent)
	assert.Equal(t, 100, mk(30_000_000, 20_000_000, ascv1.AiKeyState_AI_KEY_STATE_CAPPED).Percent, "never above 100")
	none := mk(5_000_000, 0, ascv1.AiKeyState_AI_KEY_STATE_ACTIVE)
	assert.False(t, none.HasCap)
	assert.Empty(t, none.CapInput)
	assert.Equal(t, "capped", mk(1, 1, ascv1.AiKeyState_AI_KEY_STATE_CAPPED).StateName)
	assert.Equal(t, "failed", mk(0, 0, ascv1.AiKeyState_AI_KEY_STATE_FAILED).StateName)
	assert.Equal(t, "ascbyo.ai.state.unknown", mk(0, 0, ascv1.AiKeyState_AI_KEY_STATE_UNSPECIFIED).StateKey)
	assert.True(t, strings.Contains(v.Label, "<b>"), "the view keeps text as is; the template escapes it")
}

func TestUnavailableWhenNotConfigured(t *testing.T) {
	_, err := ascbyo.New()
	fail, ok := ascbyo.AsFailure(err)
	require.True(t, ok, "%v", err)
	assert.Equal(t, ascbyo.FailUnavailable, fail.Kind)
}

func TestRepoStorageUsage(t *testing.T) {
	s, f := newService(t)
	repo := scope(t, ascbyo.KindRepo, "alice/widgets")
	K := func(k ascv1.StorageUsageKind, b, n uint64) *ascv1.StorageUsageRow {
		return &ascv1.StorageUsageRow{Kind: k, Bytes: b, Objects: n}
	}
	f.RepoUsage = &ascv1.GetRepoStorageUsageResponse{
		Rows: []*ascv1.StorageUsageRow{
			K(ascv1.StorageUsageKind_STORAGE_USAGE_KIND_INDEXES, 300, 3),
			K(ascv1.StorageUsageKind_STORAGE_USAGE_KIND_HISTORY, 1000, 10),
			K(ascv1.StorageUsageKind_STORAGE_USAGE_KIND_HISTORY, 9999, 99), // a repeated kind is ignored
			K(ascv1.StorageUsageKind_STORAGE_USAGE_KIND_CI, 0, 0),          // an empty kind is not shown
			K(ascv1.StorageUsageKind(42), 5000, 5),                         // a kind the hub does not know is dropped
			K(ascv1.StorageUsageKind_STORAGE_USAGE_KIND_LARGE_FILES, 700, 1),
		},
		TotalBytes: 123, ComputedAt: 1_700_000_000, Computing: true, ReadThisMonth: 4096, Month: "2026-11",
	}
	u, err := s.RepoUsage(t.Context(), repo, false)
	require.NoError(t, err)
	names := []string{}
	for _, k := range u.Kinds {
		names = append(names, k.Name)
	}
	assert.Equal(t, []string{"history", "large_files", "indexes"}, names, "fixed order, empty and unknown kinds left out")
	assert.Equal(t, uint64(2000), u.Total, "the total is the sum of what is shown, not the answer's figure")
	assert.Equal(t, uint64(14), u.Objects)
	assert.Equal(t, "ascbyo.storage.usage.kind.history", u.Kinds[0].Key)
	assert.True(t, u.Computing)
	assert.Equal(t, uint64(4096), u.ReadBytes)
	assert.Equal(t, "2026-11", u.Month)
	assert.EqualValues(t, 1_700_000_000, u.ComputedAt)
	assert.Zero(t, f.UsageRefreshes)

	_, err = s.RepoUsage(t.Context(), repo, true)
	require.NoError(t, err)
	assert.Equal(t, 1, f.UsageRefreshes, "refresh reaches asc")

	f.RepoUsage = &ascv1.GetRepoStorageUsageResponse{Month: "2026-13\x1b"}
	u, err = s.RepoUsage(t.Context(), repo, false)
	require.NoError(t, err)
	assert.Empty(t, u.Month, "a month that is not YYYY-MM is dropped")
	assert.Zero(t, u.ComputedAt)

	_, err = s.RepoUsage(t.Context(), scope(t, ascbyo.KindUser, "alice"), false)
	assert.True(t, ascbyo.IsKind(err, ascbyo.FailInvalid), "only a repository has usage")
	f.SetFail("/asc.v1.Storage/GetRepoUsage", status.Error(codes.PermissionDenied, "no"))
	_, err = s.RepoUsage(t.Context(), repo, false)
	require.Error(t, err)
}
