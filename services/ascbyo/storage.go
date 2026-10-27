// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascbyo

import (
	"context"
	"slices"
	"strings"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/timeutil"
)

// ListTargets returns the storage targets of the scope, oldest first.
func (s *Service) ListTargets(ctx context.Context, sc Scope) ([]*ascv1.StorageTarget, error) {
	if !sc.Valid() {
		return nil, failure(FailInvalid)
	}
	cctx, cancel := s.call(ctx, sc, "ListTargets", "", false)
	defer cancel()
	resp, err := s.api.Storage.ListTargets(cctx, &ascv1.ListStorageTargetsRequest{Owner: sc.Owner()})
	if err != nil {
		return nil, done("list storage targets", sc, err)
	}
	out := make([]*ascv1.StorageTarget, 0, len(resp.GetTargets()))
	for _, t := range resp.GetTargets() {
		// A target of another owner in the answer would be a server bug; it is dropped, not shown.
		if t != nil && sc.Owns(t.GetOwner()) && ValidTargetID(t.GetId()) {
			out = append(out, t)
		}
	}
	slices.SortStableFunc(out, func(a, b *ascv1.StorageTarget) int { return int(a.GetCreated() - b.GetCreated()) })
	return out, nil
}

// GetTarget returns one target and refuses it unless it belongs to the scope.
func (s *Service) GetTarget(ctx context.Context, sc Scope, id string) (*ascv1.StorageTarget, error) {
	if !sc.Valid() || !ValidTargetID(id) {
		return nil, failure(FailNotFound)
	}
	t, err := s.fetchTarget(ctx, sc, id)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (s *Service) fetchTarget(ctx context.Context, sc Scope, id string) (*ascv1.StorageTarget, error) {
	cctx, cancel := s.call(ctx, sc, "fetchTarget", "", false)
	defer cancel()
	resp, err := s.api.Storage.GetTarget(cctx, &ascv1.GetStorageTargetRequest{Id: id})
	if err != nil {
		return nil, done("get storage target", sc, err)
	}
	t := resp.GetTarget()
	if t == nil || t.GetId() != id || !sc.Owns(t.GetOwner()) {
		// Another owner's target looks exactly like a missing one.
		return nil, failure(FailNotFound)
	}
	return t, nil
}

// CreateTarget validates the input and creates a PENDING target. op is the hidden op id of the form.
func (s *Service) CreateTarget(ctx context.Context, sc Scope, in *StorageInput, op string) (*ascv1.StorageTarget, FieldErrors, error) {
	if !sc.Valid() {
		return nil, nil, failure(FailInvalid)
	}
	if fe := in.Validate(true); len(fe) > 0 {
		return nil, fe, nil
	}
	cctx, cancel := s.call(ctx, sc, "CreateTarget", op, false)
	defer cancel()
	resp, err := s.api.Storage.CreateTarget(cctx, &ascv1.CreateStorageTargetRequest{
		Owner:      sc.Owner(),
		Name:       in.Name,
		Endpoint:   in.Endpoint,
		Region:     in.Region,
		Bucket:     in.Bucket,
		Prefix:     in.Prefix,
		Addressing: in.Addressing,
		Credentials: &ascv1.StorageCredentials{
			AccessKeyId:     in.AccessKeyID,
			SecretAccessKey: in.SecretAccessKey,
			SessionToken:    in.SessionToken,
		},
	})
	if err != nil {
		return nil, nil, done("create storage target", sc, err, in.secrets()...)
	}
	t := resp.GetTarget()
	if t == nil || !ValidTargetID(t.GetId()) || !sc.Owns(t.GetOwner()) {
		// An answer about no target, or about another owner's, is a server bug; nothing of it is shown.
		return nil, nil, failure(FailInternal)
	}
	return t, nil, nil
}

// ownedTarget checks the target that asc returned from a change of target id: it has to be there, be that
// target and belong to the scope. Anything else is not returned.
func ownedTarget(sc Scope, id string, t *ascv1.StorageTarget) (*ascv1.StorageTarget, error) {
	if t == nil {
		return nil, failure(FailInternal)
	}
	if t.GetId() != id || !sc.Owns(t.GetOwner()) {
		return nil, failure(FailNotFound)
	}
	return t, nil
}

// ConnectionChanged reports whether in names another place than cur: the endpoint, the bucket, the region or
// the addressing differ. The stored credentials were given for the old place; keeping them for a new one
// would send them to whoever runs that place. in has to be validated (normalised) first.
func (in *StorageInput) ConnectionChanged(cur *ascv1.StorageTarget) bool {
	return in.Endpoint != cur.GetEndpoint() || in.Bucket != cur.GetBucket() || in.Region != cur.GetRegion() ||
		in.Addressing != cur.GetAddressing()
}

// UpdateTarget changes the fields of a target that differ from the stored ones and replaces the credentials
// when the input carries them. It reports changed=false when there was nothing to send.
func (s *Service) UpdateTarget(ctx context.Context, sc Scope, id string, in *StorageInput, op string) (t *ascv1.StorageTarget, changed bool, fe FieldErrors, err error) {
	cur, err := s.GetTarget(ctx, sc, id)
	if err != nil {
		return nil, false, nil, err
	}
	fe = in.Validate(false)
	if in.ConnectionChanged(cur) {
		// New place, new credentials: the key id and the secret have to be typed again.
		if in.AccessKeyID == "" {
			fe.Add("access_key_id", "ascbyo.storage.err.access_key_id_reenter")
		}
		if in.SecretAccessKey == "" {
			fe.Add("secret_access_key", "ascbyo.storage.err.secret_access_key_reenter")
		}
	}
	if len(fe) > 0 {
		return cur, false, fe, nil
	}
	req := &ascv1.UpdateStorageTargetRequest{Id: id}
	if in.Name != cur.GetName() {
		req.Name, changed = &in.Name, true
	}
	if in.Endpoint != cur.GetEndpoint() {
		req.Endpoint, changed = &in.Endpoint, true
	}
	if in.Region != cur.GetRegion() {
		req.Region, changed = &in.Region, true
	}
	if in.Bucket != cur.GetBucket() {
		req.Bucket, changed = &in.Bucket, true
	}
	if in.Prefix != cur.GetPrefix() {
		req.Prefix, changed = &in.Prefix, true
	}
	if in.Addressing != cur.GetAddressing() {
		req.Addressing, changed = &in.Addressing, true
	}
	if in.HasCredentials() {
		req.Credentials = &ascv1.StorageCredentials{
			AccessKeyId:     in.AccessKeyID,
			SecretAccessKey: in.SecretAccessKey,
			SessionToken:    in.SessionToken,
		}
		changed = true
	}
	if !changed {
		return cur, false, nil, nil
	}
	cctx, cancel := s.call(ctx, sc, "UpdateTarget", op, false)
	defer cancel()
	resp, err := s.api.Storage.UpdateTarget(cctx, req)
	if err != nil {
		return nil, false, nil, done("update storage target", sc, err, in.secrets()...)
	}
	t, err = ownedTarget(sc, id, resp.GetTarget())
	if err != nil {
		return nil, false, nil, err
	}
	return t, true, nil, nil
}

// DeleteTarget removes a target. asc refuses while a repository uses it.
func (s *Service) DeleteTarget(ctx context.Context, sc Scope, id, op string) error {
	if _, err := s.GetTarget(ctx, sc, id); err != nil {
		return err
	}
	cctx, cancel := s.call(ctx, sc, "DeleteTarget", op, false)
	defer cancel()
	_, err := s.api.Storage.DeleteTarget(cctx, &ascv1.DeleteStorageTargetRequest{Id: id})
	return done("delete storage target", sc, err)
}

// VerifyResult is the answer of a verification: the steps in order and the target after it.
type VerifyResult struct {
	OK     bool
	Checks []VerifyCheck
	Target *ascv1.StorageTarget
}

// VerifyCheck is one step of a verification.
type VerifyCheck struct {
	Step      ascv1.StorageVerifyStep
	StepKey   string // translation key of the step name
	OK        bool
	LatencyMS uint32
	Message   string // cleaned
}

func verifyStepKey(st ascv1.StorageVerifyStep) string {
	switch st {
	case ascv1.StorageVerifyStep_STORAGE_VERIFY_STEP_RESOLVE:
		return "ascbyo.storage.step.resolve"
	case ascv1.StorageVerifyStep_STORAGE_VERIFY_STEP_CONNECT:
		return "ascbyo.storage.step.connect"
	case ascv1.StorageVerifyStep_STORAGE_VERIFY_STEP_PUT:
		return "ascbyo.storage.step.put"
	case ascv1.StorageVerifyStep_STORAGE_VERIFY_STEP_GET:
		return "ascbyo.storage.step.get"
	case ascv1.StorageVerifyStep_STORAGE_VERIFY_STEP_DELETE:
		return "ascbyo.storage.step.delete"
	}
	return "ascbyo.storage.step.unknown"
}

// maxVerifyChecks bounds the steps taken from an answer.
const maxVerifyChecks = 16

// VerifyTarget runs the probe round trip (write, read, delete a probe object) through asc. A failed
// verification is a result with OK=false, not an error; an error means the call itself did not work.
func (s *Service) VerifyTarget(ctx context.Context, sc Scope, id, op string) (*VerifyResult, error) {
	if _, err := s.GetTarget(ctx, sc, id); err != nil {
		return nil, err
	}
	cctx, cancel := s.call(ctx, sc, "VerifyTarget", op, true)
	defer cancel()
	resp, err := s.api.Storage.VerifyTarget(cctx, &ascv1.VerifyStorageTargetRequest{Id: id})
	if err != nil {
		if IsKind(classify(err), FailVerifyFailed) {
			return &VerifyResult{OK: false}, nil
		}
		return nil, done("verify storage target", sc, err)
	}
	res := &VerifyResult{OK: resp.GetOk()}
	if t := resp.GetTarget(); t != nil && t.GetId() == id && sc.Owns(t.GetOwner()) {
		res.Target = t
	}
	for _, c := range resp.GetChecks() {
		if len(res.Checks) >= maxVerifyChecks {
			break
		}
		res.Checks = append(res.Checks, VerifyCheck{
			Step: c.GetStep(), StepKey: verifyStepKey(c.GetStep()), OK: c.GetOk(), LatencyMS: c.GetLatencyMs(),
			Message: CleanText(c.GetMessage(), 200),
		})
	}
	return res, nil
}

// SetEnabled turns the use of a target on or off. asc allows enabling a verified target only.
func (s *Service) SetEnabled(ctx context.Context, sc Scope, id string, enabled bool, op string) (*ascv1.StorageTarget, error) {
	if _, err := s.GetTarget(ctx, sc, id); err != nil {
		return nil, err
	}
	cctx, cancel := s.call(ctx, sc, "SetEnabled", op, false)
	defer cancel()
	resp, err := s.api.Storage.SetEnabled(cctx, &ascv1.SetStorageTargetEnabledRequest{Id: id, Enabled: enabled})
	if err != nil {
		return nil, done("set storage target enabled", sc, err)
	}
	return ownedTarget(sc, id, resp.GetTarget())
}

// RepoTargets are the targets a repository may use: its own and those of the owners in allowed (the owners
// whose targets the signed-in user administers, which the handler decides). Targets that are not ENABLED
// are returned too, marked by their state; assignment and migration refuse them.
type RepoTargets struct {
	Scope   Scope
	Targets []*ascv1.StorageTarget
}

// ListRepoTargets collects the targets of every scope in allowed, in order, without duplicates.
func (s *Service) ListRepoTargets(ctx context.Context, allowed []Scope) ([]RepoTargets, error) {
	var out []RepoTargets
	for _, sc := range allowed {
		ts, err := s.ListTargets(ctx, sc)
		if err != nil {
			return nil, err
		}
		out = append(out, RepoTargets{Scope: sc, Targets: ts})
	}
	return out, nil
}

// usableTarget finds target id among the allowed scopes and requires it to be ENABLED.
func (s *Service) usableTarget(ctx context.Context, allowed []Scope, id string) error {
	if !ValidTargetID(id) {
		return failure(FailNotFound)
	}
	var lastErr error = failure(FailNotFound)
	for _, sc := range allowed {
		t, err := s.GetTarget(ctx, sc, id)
		if err != nil {
			if IsKind(err, FailNotFound) {
				continue
			}
			lastErr = err
			continue
		}
		if t.GetState() != ascv1.StorageTargetState_STORAGE_TARGET_STATE_ENABLED {
			return failure(FailNotReady)
		}
		return nil
	}
	return lastErr
}

// AssignRepo makes new objects of the repository go to target (an empty id selects the instance default).
// The target must be ENABLED and belong to one of allowed.
func (s *Service) AssignRepo(ctx context.Context, repo Scope, allowed []Scope, targetID, op string) error {
	if repo.Kind != KindRepo || !repo.Valid() {
		return failure(FailInvalid)
	}
	if targetID != "" {
		if err := s.usableTarget(ctx, allowed, targetID); err != nil {
			return err
		}
	}
	cctx, cancel := s.call(ctx, repo, "AssignRepo", op, false)
	defer cancel()
	_, err := s.api.Storage.AssignRepo(cctx, &ascv1.AssignRepoStorageRequest{Repo: repo.Repo(), TargetId: targetID})
	return done("assign repository storage", repo, err)
}

// Migration is a storage migration as shown.
type Migration struct {
	ID          string
	FromTarget  string
	ToTarget    string
	State       ascv1.StorageMigrationState
	StateName   string // slug, stable for scripts
	StateKey    string
	Active      bool
	Total       uint64
	Copied      uint64
	Bytes       uint64
	Percent     int
	DeleteSrc   bool
	Created     timeutil.TimeStamp
	Updated     timeutil.TimeStamp
	Error       string
	Cancellable bool
}

// MigrationFrom converts the wire message; it returns nil when the message does not belong to repo.
func MigrationFrom(repo Scope, m *ascv1.StorageMigration) *Migration {
	if m == nil || !ValidMigrationID(m.GetId()) || !strings.EqualFold(m.GetRepo().GetName(), repo.Name) {
		return nil
	}
	out := &Migration{
		ID: m.GetId(), FromTarget: m.GetFromTargetId(), ToTarget: m.GetToTargetId(), State: m.GetState(),
		Total: m.GetObjectsTotal(), Copied: m.GetObjectsCopied(), Bytes: m.GetBytesCopied(), DeleteSrc: m.GetDeleteSource(),
		Created: timeutil.TimeStamp(m.GetCreated()), Updated: timeutil.TimeStamp(m.GetUpdated()), Error: CleanText(m.GetError(), 300),
	}
	if out.FromTarget != "" && !ValidTargetID(out.FromTarget) {
		out.FromTarget = ""
	}
	if out.ToTarget != "" && !ValidTargetID(out.ToTarget) {
		out.ToTarget = ""
	}
	switch m.GetState() {
	case ascv1.StorageMigrationState_STORAGE_MIGRATION_STATE_QUEUED:
		out.StateName, out.StateKey, out.Active, out.Cancellable = "queued", "ascbyo.migration.state.queued", true, true
	case ascv1.StorageMigrationState_STORAGE_MIGRATION_STATE_COPYING:
		out.StateName, out.StateKey, out.Active, out.Cancellable = "copying", "ascbyo.migration.state.copying", true, true
	case ascv1.StorageMigrationState_STORAGE_MIGRATION_STATE_VERIFYING:
		out.StateName, out.StateKey, out.Active, out.Cancellable = "verifying", "ascbyo.migration.state.verifying", true, true
	case ascv1.StorageMigrationState_STORAGE_MIGRATION_STATE_SWITCHING:
		// The switch is one transaction; cancelling now has nothing to stop.
		out.StateName, out.StateKey, out.Active = "switching", "ascbyo.migration.state.switching", true
	case ascv1.StorageMigrationState_STORAGE_MIGRATION_STATE_DONE:
		out.StateName, out.StateKey = "done", "ascbyo.migration.state.done"
	case ascv1.StorageMigrationState_STORAGE_MIGRATION_STATE_FAILED:
		out.StateName, out.StateKey = "failed", "ascbyo.migration.state.failed"
	case ascv1.StorageMigrationState_STORAGE_MIGRATION_STATE_CANCELLED:
		out.StateName, out.StateKey = "cancelled", "ascbyo.migration.state.cancelled"
	default:
		out.StateName, out.StateKey = "unknown", "ascbyo.migration.state.unknown"
	}
	switch {
	case out.State == ascv1.StorageMigrationState_STORAGE_MIGRATION_STATE_DONE:
		out.Percent = 100
	case out.Total > 0:
		out.Percent = int(min(out.Copied, out.Total) * 100 / out.Total)
	}
	return out
}

// ListMigrations returns the newest migrations of the repository.
func (s *Service) ListMigrations(ctx context.Context, repo Scope) ([]*Migration, error) {
	if repo.Kind != KindRepo || !repo.Valid() {
		return nil, failure(FailInvalid)
	}
	cctx, cancel := s.call(ctx, repo, "ListMigrations", "", false)
	defer cancel()
	resp, err := s.api.Storage.ListMigrations(cctx, &ascv1.ListStorageMigrationsRequest{Repo: repo.Repo(), Limit: MaxMigrationsShown})
	if err != nil {
		return nil, done("list storage migrations", repo, err)
	}
	var out []*Migration
	for _, m := range resp.GetMigrations() {
		if v := MigrationFrom(repo, m); v != nil {
			out = append(out, v)
		}
		if len(out) >= MaxMigrationsShown {
			break
		}
	}
	slices.SortStableFunc(out, func(a, b *Migration) int { return int(b.Created - a.Created) })
	return out, nil
}

// GetMigration returns one migration of the repository.
func (s *Service) GetMigration(ctx context.Context, repo Scope, id string) (*Migration, error) {
	if repo.Kind != KindRepo || !repo.Valid() || !ValidMigrationID(id) {
		return nil, failure(FailNotFound)
	}
	cctx, cancel := s.call(ctx, repo, "GetMigration", "", false)
	defer cancel()
	resp, err := s.api.Storage.GetMigration(cctx, &ascv1.GetStorageMigrationRequest{Id: id})
	if err != nil {
		return nil, done("get storage migration", repo, err)
	}
	m := MigrationFrom(repo, resp.GetMigration())
	if m == nil || m.ID != id {
		return nil, failure(FailNotFound)
	}
	return m, nil
}

// StartMigration moves the objects of the repository to target (an empty id means the instance default).
// The target must be ENABLED and belong to one of allowed.
func (s *Service) StartMigration(ctx context.Context, repo Scope, allowed []Scope, targetID string, deleteSource bool, op string) (*Migration, error) {
	if repo.Kind != KindRepo || !repo.Valid() {
		return nil, failure(FailInvalid)
	}
	if targetID != "" {
		if err := s.usableTarget(ctx, allowed, targetID); err != nil {
			return nil, err
		}
	}
	cctx, cancel := s.call(ctx, repo, "StartMigration", op, false)
	defer cancel()
	resp, err := s.api.Storage.StartMigration(cctx, &ascv1.StartStorageMigrationRequest{Repo: repo.Repo(), ToTargetId: targetID, DeleteSource: deleteSource})
	if err != nil {
		return nil, done("start storage migration", repo, err)
	}
	m := MigrationFrom(repo, resp.GetMigration())
	if m == nil {
		return nil, failure(FailInternal)
	}
	return m, nil
}

// CancelMigration stops a migration of the repository. It is idempotent in asc.
func (s *Service) CancelMigration(ctx context.Context, repo Scope, id, op string) (*Migration, error) {
	if _, err := s.GetMigration(ctx, repo, id); err != nil {
		return nil, err
	}
	cctx, cancel := s.call(ctx, repo, "CancelMigration", op, false)
	defer cancel()
	resp, err := s.api.Storage.CancelMigration(cctx, &ascv1.CancelStorageMigrationRequest{Id: id})
	if err != nil {
		return nil, done("cancel storage migration", repo, err)
	}
	m := MigrationFrom(repo, resp.GetMigration())
	if m == nil {
		return nil, failure(FailInternal)
	}
	return m, nil
}
