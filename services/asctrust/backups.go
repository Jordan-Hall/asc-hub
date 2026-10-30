// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"google.golang.org/grpc"
)

// The Backups service of asc serve: the schedule, the history, "back up now", a check of a backup, and a staged restore.
// Every call is the site administrator's (CallInstance). A restore asks for the word RESTORE; the page checks it here
// as well as asc, so a typo is a sentence and not a refused call. No credential of a target is ever handled: targets are
// named in the configuration of asc serve and come back as a name, a kind and a place.

// FeatureBackups is what Version.features says when the server has the Backups service.
const FeatureBackups = "backups.v1"

// RestoreWord is what a person types to confirm a restore.
const RestoreWord = "RESTORE"

var (
	backupIDShape   = regexp.MustCompile(`^bk_[0-9A-Za-z]{20}$`)
	backupTargetRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	timeOfDayFormat = regexp.MustCompile(`^([01][0-9]|2[0-3]):([0-5][0-9])$`)
)

// BackupRow is one run in the history.
type BackupRow struct {
	ID          string
	State       string // running, succeeded, failed
	Trigger     string // manual, schedule
	Target      string
	Started     time.Time
	Finished    time.Time
	Size        uint64
	Objects     uint64
	Encrypted   bool
	Verify      string // not_verified, running, verified, failed
	VerifiedAt  time.Time
	Note        string
	DoneBytes   uint64
	TotalBytes  uint64
	Actor       string
	Restorable  bool
	ProgressPct int
}

// SizeText writes the size of the backup.
func (b BackupRow) SizeText() string {
	if b.Size == 0 {
		return ""
	}
	return FormatBytes(b.Size)
}

// CanVerify says whether a check can be started: the run is over and succeeded, and no check is running.
func (b BackupRow) CanVerify() bool { return b.State == "succeeded" && b.Verify != "running" }

func backupStateOf(s ascv1.BackupState) (string, bool) {
	switch s {
	case ascv1.BackupState_BACKUP_STATE_RUNNING:
		return "running", true
	case ascv1.BackupState_BACKUP_STATE_SUCCEEDED:
		return "succeeded", true
	case ascv1.BackupState_BACKUP_STATE_FAILED:
		return "failed", true
	case ascv1.BackupState_BACKUP_STATE_UNSPECIFIED:
	}
	return "", false
}

func backupVerifyOf(v ascv1.BackupVerify) string {
	switch v {
	case ascv1.BackupVerify_BACKUP_VERIFY_RUNNING:
		return "running"
	case ascv1.BackupVerify_BACKUP_VERIFY_VERIFIED:
		return "verified"
	case ascv1.BackupVerify_BACKUP_VERIFY_FAILED:
		return "failed"
	case ascv1.BackupVerify_BACKUP_VERIFY_NOT_VERIFIED:
	}
	return "not_verified"
}

// BackupOf checks and cleans one backup of an answer.
func BackupOf(b *ascv1.Backup) (BackupRow, error) {
	state, ok := backupStateOf(b.GetState())
	if !ok || !backupIDShape.MatchString(b.GetId()) {
		return BackupRow{}, fmt.Errorf("%w: backup", ErrBadAnswer)
	}
	trigger := "manual"
	if b.GetTrigger() == ascv1.BackupTrigger_BACKUP_TRIGGER_SCHEDULE {
		trigger = "schedule"
	}
	row := BackupRow{
		ID: b.GetId(), State: state, Trigger: trigger, Target: CleanText(b.GetTarget(), 64), Started: unixTime(b.GetStarted()),
		Finished: unixTime(b.GetFinished()), Size: b.GetSizeBytes(), Objects: b.GetObjects(), Encrypted: b.GetEncrypted(),
		Verify: backupVerifyOf(b.GetVerify()), VerifiedAt: unixTime(b.GetVerifiedAt()), Note: CleanText(b.GetNote(), MaxTextBytes),
		DoneBytes: b.GetDoneBytes(), TotalBytes: b.GetTotalBytes(), Actor: CleanText(b.GetActor(), 64), Restorable: b.GetRestorable(),
	}
	if row.State == "running" && row.TotalBytes > 0 {
		row.ProgressPct = int(min(row.DoneBytes, row.TotalBytes) * 100 / max(row.TotalBytes, 1))
		if row.TotalBytes > 1<<56 {
			row.ProgressPct = 0
		}
	}
	return row, nil
}

// BackupSchedule is the schedule of the backups.
type BackupSchedule struct {
	Enabled      bool
	IntervalH    uint32
	MinuteOfDay  uint32
	Target       string
	KeepDays     uint32
	KeepWeeks    uint32
	NextRun      time.Time
	IncludeVault bool
	IncludeObj   bool
}

// TimeOfDay writes the anchor of the interval as "HH:MM" (UTC).
func (s BackupSchedule) TimeOfDay() string {
	return fmt.Sprintf("%02d:%02d", (s.MinuteOfDay/60)%24, s.MinuteOfDay%60)
}

func scheduleOf(s *ascv1.BackupSchedule) BackupSchedule {
	return BackupSchedule{
		Enabled: s.GetEnabled(), IntervalH: s.GetIntervalHours(), MinuteOfDay: s.GetMinuteOfDay(), Target: CleanText(s.GetTarget(), 64),
		KeepDays: s.GetKeepDays(), KeepWeeks: s.GetKeepWeeks(), NextRun: unixTime(s.GetNextRun()), IncludeVault: s.GetIncludeVaultKey(),
		IncludeObj: s.GetIncludeObjects(),
	}
}

// BackupStatus is the state of the backups at a glance.
type BackupStatus struct {
	LastSuccess time.Time
	LastFailure time.Time
	Overdue     bool
	Running     bool
	Staged      bool
	StagedID    string
	Configured  bool
}

// BackupTargetRow is a place backups go to.
type BackupTargetRow struct {
	Name      string
	Kind      string // local, s3
	Location  string
	Encrypted bool
}

// BackupsPage is the board.
type BackupsPage struct {
	Backups  []BackupRow
	Schedule BackupSchedule
	Status   BackupStatus
	Targets  []BackupTargetRow
}

// LoadBackups asks for the history, the schedule and the targets.
func LoadBackups(ctx context.Context, conn grpc.ClientConnInterface) (*BackupsPage, error) {
	cctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	client := ascv1.NewBackupsClient(conn)
	list, err := client.ListBackups(cctx, &ascv1.ListBackupsRequest{Limit: 50})
	if err != nil {
		return nil, err
	}
	if len(list.GetBackups()) > 200 {
		return nil, fmt.Errorf("%w: backups", ErrBadAnswer)
	}
	targets, err := client.ListTargets(cctx, &ascv1.ListBackupTargetsRequest{})
	if err != nil {
		return nil, err
	}
	if len(targets.GetTargets()) > 100 {
		return nil, fmt.Errorf("%w: backup targets", ErrBadAnswer)
	}
	p := &BackupsPage{Schedule: scheduleOf(list.GetSchedule())}
	st := list.GetStatus()
	p.Status = BackupStatus{
		LastSuccess: unixTime(st.GetLastSuccess()), LastFailure: unixTime(st.GetLastFailure()), Overdue: st.GetOverdue(), Running: st.GetRunning(),
		Staged: st.GetRestoreStaged(), StagedID: CleanText(st.GetRestoreBackupId(), 32), Configured: st.GetConfigured(),
	}
	for _, b := range list.GetBackups() {
		row, err := BackupOf(b)
		if err != nil {
			return nil, err
		}
		p.Backups = append(p.Backups, row)
	}
	for _, t := range targets.GetTargets() {
		if !backupTargetRe.MatchString(t.GetName()) {
			return nil, fmt.Errorf("%w: backup target", ErrBadAnswer)
		}
		kind := "local"
		if t.GetKind() == "s3" {
			kind = "s3"
		}
		p.Targets = append(p.Targets, BackupTargetRow{Name: t.GetName(), Kind: kind, Location: CleanText(t.GetLocation(), MaxTextBytes), Encrypted: t.GetEncrypted()})
	}
	return p, nil
}

// Failing says the last attempt to back up failed: a failure with no success after it.
func (s BackupStatus) Failing() bool {
	return !s.LastFailure.IsZero() && !s.LastFailure.Before(s.LastSuccess)
}

// ScheduleTarget is the target the schedule writes to, nil when it has none that exists.
func (p *BackupsPage) ScheduleTarget() *BackupTargetRow {
	for i := range p.Targets {
		if p.Targets[i].Name == p.Schedule.Target {
			return &p.Targets[i]
		}
	}
	return nil
}

// RunningRow is the backup that is being made now, nil when none is.
func (p *BackupsPage) RunningRow() *BackupRow {
	for i := range p.Backups {
		if p.Backups[i].State == "running" {
			return &p.Backups[i]
		}
	}
	return nil
}

// LastFailureNote is the note of the newest failed backup ("" when none has one).
func (p *BackupsPage) LastFailureNote() string {
	for _, b := range p.Backups {
		if b.State == "failed" {
			return b.Note
		}
	}
	return ""
}

// ActorName is the login of the person who started the backup, without the "id:" asc puts before it.
func (b BackupRow) ActorName() string {
	_, name, found := strings.Cut(b.Actor, ":")
	if !found {
		return b.Actor
	}
	return name
}

// DoneText and TotalText write the bytes copied so far and the bytes to copy of a running backup.
func (b BackupRow) DoneText() string  { return FormatBytes(min(b.DoneBytes, b.TotalBytes)) }
func (b BackupRow) TotalText() string { return FormatBytes(b.TotalBytes) }

// HasTarget says whether name is one of the targets.
func (p *BackupsPage) HasTarget(name string) bool {
	for _, t := range p.Targets {
		if t.Name == name {
			return true
		}
	}
	return false
}

// CheckBackupID returns id when it has the shape of a backup id.
func CheckBackupID(id string) (string, error) {
	if !backupIDShape.MatchString(id) {
		return "", fmt.Errorf("%w: backup id", ErrBadInput)
	}
	return id, nil
}

// RunBackup starts a backup at once. An empty target is the target of the schedule.
func RunBackup(ctx context.Context, conn grpc.ClientConnInterface, target, op string) (*BackupRow, error) {
	if target != "" && !backupTargetRe.MatchString(target) {
		return nil, fmt.Errorf("%w: backup target", ErrBadInput)
	}
	cctx, cancel, err := opCtx(ctx, op)
	if err != nil {
		return nil, err
	}
	defer cancel()
	resp, err := ascv1.NewBackupsClient(conn).RunBackup(cctx, &ascv1.RunBackupRequest{Target: target})
	if err != nil {
		return nil, err
	}
	row, err := BackupOf(resp.GetBackup())
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// ScheduleFromForm reads the fields enabled, interval_hours, time (HH:MM, UTC), target, keep_days, keep_weeks,
// include_vault_key and include_objects ("1" is on) with the limits of asc.
func ScheduleFromForm(get func(string) string) (BackupSchedule, error) {
	hours, err := ParseCount(get("interval_hours"), 168)
	if err != nil || hours < 1 {
		return BackupSchedule{}, fmt.Errorf("%w: interval", ErrBadInput)
	}
	m := timeOfDayFormat.FindStringSubmatch(strings.TrimSpace(get("time")))
	if m == nil {
		return BackupSchedule{}, fmt.Errorf("%w: time of day", ErrBadInput)
	}
	hh, _ := strconv.Atoi(m[1])
	mm, _ := strconv.Atoi(m[2])
	days, err := ParseCount(get("keep_days"), 3650)
	if err != nil || days < 1 {
		return BackupSchedule{}, fmt.Errorf("%w: days to keep", ErrBadInput)
	}
	weeks, err := ParseCount(get("keep_weeks"), 520)
	if err != nil {
		return BackupSchedule{}, fmt.Errorf("%w: weeks to keep", ErrBadInput)
	}
	target := get("target")
	if !backupTargetRe.MatchString(target) {
		return BackupSchedule{}, fmt.Errorf("%w: backup target", ErrBadInput)
	}
	return BackupSchedule{
		Enabled: get("enabled") == "1", IntervalH: uint32(hours), MinuteOfDay: uint32(hh*60 + mm), Target: target, KeepDays: uint32(days),
		KeepWeeks: uint32(weeks), IncludeVault: get("include_vault_key") == "1", IncludeObj: get("include_objects") == "1",
	}, nil
}

// SetSchedule sets the schedule.
func SetSchedule(ctx context.Context, conn grpc.ClientConnInterface, s BackupSchedule, op string) (*BackupSchedule, error) {
	cctx, cancel, err := opCtx(ctx, op)
	if err != nil {
		return nil, err
	}
	defer cancel()
	resp, err := ascv1.NewBackupsClient(conn).SetSchedule(cctx, &ascv1.SetBackupScheduleRequest{Schedule: &ascv1.BackupSchedule{
		Enabled: s.Enabled, IntervalHours: s.IntervalH, MinuteOfDay: s.MinuteOfDay, Target: s.Target, KeepDays: s.KeepDays,
		KeepWeeks: s.KeepWeeks, IncludeVaultKey: s.IncludeVault, IncludeObjects: s.IncludeObj,
	}})
	if err != nil {
		return nil, err
	}
	out := scheduleOf(resp.GetSchedule())
	return &out, nil
}

// VerifyBackup starts a check of a backup. The result arrives in the history.
func VerifyBackup(ctx context.Context, conn grpc.ClientConnInterface, id, op string) (*BackupRow, error) {
	if _, err := CheckBackupID(id); err != nil {
		return nil, err
	}
	cctx, cancel, err := opCtx(ctx, op)
	if err != nil {
		return nil, err
	}
	defer cancel()
	resp, err := ascv1.NewBackupsClient(conn).VerifyBackup(cctx, &ascv1.VerifyBackupRequest{Id: id})
	if err != nil {
		return nil, err
	}
	row, err := BackupOf(resp.GetBackup())
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// RestorePreview is what a restore would do.
type RestorePreview struct {
	Backup       BackupRow
	Confirmation string
	LosesSince   time.Time
	ReadOnly     bool
	Verified     bool
	Database     string
	Objects      uint64
	ObjectBytes  uint64
	HasVaultKey  bool
	Warnings     []string
}

// ObjectBytesText writes the size of the objects.
func (r *RestorePreview) ObjectBytesText() string { return FormatBytes(r.ObjectBytes) }

// PreviewRestore asks what restoring id would do. It changes nothing.
func PreviewRestore(ctx context.Context, conn grpc.ClientConnInterface, id string) (*RestorePreview, error) {
	if _, err := CheckBackupID(id); err != nil {
		return nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	resp, err := ascv1.NewBackupsClient(conn).PreviewRestore(cctx, &ascv1.PreviewRestoreRequest{Id: id})
	if err != nil {
		return nil, err
	}
	b, err := BackupOf(resp.GetBackup())
	if err != nil {
		return nil, err
	}
	if len(resp.GetWarnings()) > 50 {
		return nil, fmt.Errorf("%w: restore warnings", ErrBadAnswer)
	}
	p := &RestorePreview{
		Backup: b, Confirmation: RestoreWord, LosesSince: unixTime(resp.GetLosesChangesSince()), ReadOnly: resp.GetReadOnlyDuring(),
		Verified: resp.GetVerified(), Database: CleanText(resp.GetDatabase(), 32), Objects: resp.GetObjects(), ObjectBytes: resp.GetObjectBytes(),
		HasVaultKey: resp.GetHasVaultKey(),
	}
	for _, w := range resp.GetWarnings() {
		if w = CleanText(w, MaxTextBytes); w != "" {
			p.Warnings = append(p.Warnings, w)
		}
	}
	return p, nil
}

// RestoreResult is what a staged restore says.
type RestoreResult struct {
	BackupID        string
	RestartRequired bool
	Command         string
}

// Restore stages the restore of id. confirm must be exactly the word RESTORE; the page checks it here first.
func Restore(ctx context.Context, conn grpc.ClientConnInterface, id, confirm, op string) (*RestoreResult, error) {
	if _, err := CheckBackupID(id); err != nil {
		return nil, err
	}
	if confirm != RestoreWord {
		return nil, fmt.Errorf("%w: confirmation", ErrBadInput)
	}
	cctx, cancel, err := opCtx(ctx, op)
	if err != nil {
		return nil, err
	}
	defer cancel()
	resp, err := ascv1.NewBackupsClient(conn).Restore(cctx, &ascv1.RestoreBackupRequest{Id: id, Confirm: confirm})
	if err != nil {
		return nil, err
	}
	return &RestoreResult{BackupID: CleanText(resp.GetBackupId(), 32), RestartRequired: resp.GetRestartRequired(), Command: CleanText(resp.GetCommand(), MaxTextBytes)}, nil
}
