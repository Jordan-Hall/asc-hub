// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/google/uuid"
	"google.golang.org/grpc"
)

// The account services of asc serve: the credentials of the signed-in person (Sessions) and the export of their own
// data (DataExport). Both are about the account of the person, so a call is made with CallAccount: the hub signs a
// claim for "user:<login>" and asc acts as that person only. Everything that comes back is data: a text is cleaned,
// an id is checked against its shape, a time that is zero is "never".

// Features that asc serve advertises for the account services.
const (
	FeatureSessions   = "sessions.v1"
	FeatureDataExport = "dataexport.v1"
)

// maxExportDownload is the most bytes of an archive the hub streams to a browser. The server cuts its own archives at
// a size limit; this is a second bound for an answer that cannot be trusted.
const maxExportDownload = 512 << 20

var (
	sessionIDShape = regexp.MustCompile(`^(tok|ssh|agt):[0-9A-Za-z_.-]{1,64}$`)
	exportIDShape  = regexp.MustCompile(`^ex_[0-9A-Za-z]{20}$`)

	sessionPrefix = map[string]string{"token": "tok:", "ssh": "ssh:", "agent": "agt:"}
)

func opCtx(ctx context.Context, op string) (context.Context, context.CancelFunc, error) {
	id, err := uuid.Parse(op)
	if err != nil {
		return ctx, func() {}, fmt.Errorf("%w: operation id", ErrBadInput)
	}
	cctx, cancel := context.WithTimeout(ascclient.WithOpID(ctx, id), CallTimeout)
	return cctx, cancel, nil
}

func unixTime(s int64) time.Time {
	if s <= 0 {
		return time.Time{}
	}
	return time.Unix(s, 0).UTC()
}

// ------------------------------------------------------------------------------------------------ sessions

// SessionRow is one credential of the person, as a page draws it.
type SessionRow struct {
	ID       string
	Kind     string // "token", "ssh" or "agent"
	Label    string
	Scope    string
	Created  time.Time
	LastUsed time.Time // zero: unknown or never
	Expires  time.Time // zero: does not expire
}

func sessionKindOf(k ascv1.SessionKind) (string, bool) {
	switch k {
	case ascv1.SessionKind_SESSION_KIND_TOKEN:
		return "token", true
	case ascv1.SessionKind_SESSION_KIND_SSH_KEY:
		return "ssh", true
	case ascv1.SessionKind_SESSION_KIND_AGENT_TOKEN:
		return "agent", true
	case ascv1.SessionKind_SESSION_KIND_UNSPECIFIED:
	}
	return "", false
}

// SessionOf checks and cleans one session of an answer.
func SessionOf(s *ascv1.Session) (SessionRow, error) {
	kind, ok := sessionKindOf(s.GetKind())
	if !ok || !sessionIDShape.MatchString(s.GetId()) || !strings.HasPrefix(s.GetId(), sessionPrefix[kind]) {
		return SessionRow{}, fmt.Errorf("%w: session", ErrBadAnswer)
	}
	return SessionRow{
		ID:       s.GetId(),
		Kind:     kind,
		Label:    CleanText(s.GetLabel(), MaxTextBytes),
		Scope:    CleanText(s.GetScope(), MaxTextBytes),
		Created:  unixTime(s.GetCreated()),
		LastUsed: unixTime(s.GetLastUsed()),
		Expires:  unixTime(s.GetExpires()),
	}, nil
}

// LoadSessions lists the live credentials of login, newest first.
func LoadSessions(ctx context.Context, conn grpc.ClientConnInterface, login string) ([]SessionRow, error) {
	cctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	resp, err := ascv1.NewSessionsClient(conn).ListSessions(cctx, &ascv1.ListSessionsRequest{Login: login})
	if err != nil {
		return nil, err
	}
	if len(resp.GetSessions()) > MaxListItems {
		return nil, fmt.Errorf("%w: sessions", ErrBadAnswer)
	}
	rows := make([]SessionRow, 0, len(resp.GetSessions()))
	for _, s := range resp.GetSessions() {
		row, err := SessionOf(s)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// RevokeSession ends one credential of login. The call is idempotent.
func RevokeSession(ctx context.Context, conn grpc.ClientConnInterface, login, id, op string) error {
	if !sessionIDShape.MatchString(id) {
		return fmt.Errorf("%w: session id", ErrBadInput)
	}
	cctx, cancel, err := opCtx(ctx, op)
	if err != nil {
		return err
	}
	defer cancel()
	_, err = ascv1.NewSessionsClient(conn).RevokeSession(cctx, &ascv1.RevokeSessionRequest{Login: login, Id: id})
	return err
}

// RevokeOtherSessions ends every credential of login except keepID (which may be empty). It returns how many ended.
func RevokeOtherSessions(ctx context.Context, conn grpc.ClientConnInterface, login, keepID, op string) (uint32, error) {
	if keepID != "" && !sessionIDShape.MatchString(keepID) {
		return 0, fmt.Errorf("%w: session id", ErrBadInput)
	}
	cctx, cancel, err := opCtx(ctx, op)
	if err != nil {
		return 0, err
	}
	defer cancel()
	resp, err := ascv1.NewSessionsClient(conn).RevokeOtherSessions(cctx, &ascv1.RevokeOtherSessionsRequest{Login: login, KeepId: keepID})
	if err != nil {
		return 0, err
	}
	return resp.GetRevoked(), nil
}

// ------------------------------------------------------------------------------------------------ data export

// ExportRow is one export job as a page draws it. State is one of pending, running, ready, failed, expired.
type ExportRow struct {
	ID        string
	State     string
	Requested time.Time
	Finished  time.Time
	Expires   time.Time
	Size      int64
	Rows      uint64
	Truncated bool
	Error     string
}

// Downloadable says whether the archive can be fetched now.
func (e ExportRow) Downloadable(now time.Time) bool {
	return e.State == "ready" && (e.Expires.IsZero() || e.Expires.After(now))
}

// Waiting says whether the job is not over, so a page offers to look again.
func (e ExportRow) Waiting() bool { return e.State == "pending" || e.State == "running" }

func exportStateOf(s ascv1.ExportState) (string, bool) {
	switch s {
	case ascv1.ExportState_EXPORT_STATE_PENDING:
		return "pending", true
	case ascv1.ExportState_EXPORT_STATE_RUNNING:
		return "running", true
	case ascv1.ExportState_EXPORT_STATE_READY:
		return "ready", true
	case ascv1.ExportState_EXPORT_STATE_FAILED:
		return "failed", true
	case ascv1.ExportState_EXPORT_STATE_EXPIRED:
		return "expired", true
	case ascv1.ExportState_EXPORT_STATE_UNSPECIFIED:
	}
	return "", false
}

// ExportOf checks and cleans one job of an answer.
func ExportOf(j *ascv1.ExportJob) (ExportRow, error) {
	state, ok := exportStateOf(j.GetState())
	if !ok || !exportIDShape.MatchString(j.GetId()) || j.GetSizeBytes() > 1<<62 {
		return ExportRow{}, fmt.Errorf("%w: export", ErrBadAnswer)
	}
	return ExportRow{
		ID:        j.GetId(),
		State:     state,
		Requested: unixTime(j.GetRequested()),
		Finished:  unixTime(j.GetFinished()),
		Expires:   unixTime(j.GetExpires()),
		Size:      int64(j.GetSizeBytes()),
		Rows:      j.GetRows(),
		Truncated: j.GetTruncated(),
		Error:     CleanText(j.GetError(), MaxTextBytes),
	}, nil
}

// ListExports lists the exports of the person, newest first.
func ListExports(ctx context.Context, conn grpc.ClientConnInterface) ([]ExportRow, error) {
	cctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	resp, err := ascv1.NewDataExportClient(conn).ListExports(cctx, &ascv1.ListExportsRequest{})
	if err != nil {
		return nil, err
	}
	if len(resp.GetJobs()) > MaxListItems {
		return nil, fmt.Errorf("%w: exports", ErrBadAnswer)
	}
	rows := make([]ExportRow, 0, len(resp.GetJobs()))
	for _, j := range resp.GetJobs() {
		row, err := ExportOf(j)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// RequestExport starts an export of the data of the person. The same op id is the same request.
func RequestExport(ctx context.Context, conn grpc.ClientConnInterface, op string) (*ExportRow, error) {
	cctx, cancel, err := opCtx(ctx, op)
	if err != nil {
		return nil, err
	}
	defer cancel()
	resp, err := ascv1.NewDataExportClient(conn).RequestExport(cctx, &ascv1.RequestExportRequest{})
	if err != nil {
		return nil, err
	}
	row, err := ExportOf(resp.GetJob())
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// DeleteExport deletes an export and its archive. The call is idempotent.
func DeleteExport(ctx context.Context, conn grpc.ClientConnInterface, id, op string) error {
	if !exportIDShape.MatchString(id) {
		return fmt.Errorf("%w: export id", ErrBadInput)
	}
	cctx, cancel, err := opCtx(ctx, op)
	if err != nil {
		return err
	}
	defer cancel()
	_, err = ascv1.NewDataExportClient(conn).DeleteExport(cctx, &ascv1.DeleteExportRequest{Id: id})
	return err
}

// ErrExportTooLarge is returned when an archive is longer than the hub streams.
var ErrExportTooLarge = errors.New("asctrust: the export is larger than the hub will stream")

// CopyDataExport streams the archive id into w, in the order the server sends it, and returns the number of bytes. A
// chunk that does not follow the previous one, a stream that ends before its last chunk and an archive past
// maxExportDownload stop the copy: what was written is then incomplete and the caller must not present it as finished.
func CopyDataExport(ctx context.Context, conn grpc.ClientConnInterface, id string, w io.Writer) (int64, error) {
	if !exportIDShape.MatchString(id) {
		return 0, fmt.Errorf("%w: export id", ErrBadInput)
	}
	cctx, cancel := context.WithTimeout(ctx, ExportTimeout)
	defer cancel()
	stream, err := ascv1.NewDataExportClient(conn).Download(cctx, &ascv1.DownloadExportRequest{Id: id})
	if err != nil {
		return 0, err
	}
	var n int64
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return n, fmt.Errorf("%w: the archive ended without its last chunk", ErrBadAnswer)
		}
		if err != nil {
			return n, err
		}
		if uint64(n) != chunk.GetOffset() {
			return n, fmt.Errorf("%w: archive offset", ErrBadAnswer)
		}
		if n+int64(len(chunk.GetData())) > maxExportDownload {
			return n, ErrExportTooLarge
		}
		m, err := w.Write(chunk.GetData())
		n += int64(m)
		if err != nil {
			return n, err
		}
		if chunk.GetLast() {
			return n, nil
		}
	}
}
