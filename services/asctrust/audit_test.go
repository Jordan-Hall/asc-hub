// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zeebo/blake3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func formOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestAuditQueryOfAcceptsGoodFilters(t *testing.T) {
	q, f, problems := AuditQueryOf(formOf(map[string]string{
		"actor": "alice, bob", "action": "storage.*, repo.ref.update", "outcome": "denied", "target": "refs/heads/",
		"since": "2026-03-01", "until": "2026-03-31", "order": "oldest", "limit": "100",
	}))
	require.Empty(t, problems)
	require.NotNil(t, f)
	assert.Equal(t, []string{"alice", "bob"}, f.Actors)
	assert.Equal(t, []string{"storage.*", "repo.ref.update"}, f.Actions)
	assert.Equal(t, ascv1.AuditOutcome_AUDIT_OUTCOME_DENIED, f.Outcome)
	assert.Equal(t, "refs/heads/", f.Target)
	assert.Equal(t, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC).UnixMilli(), f.SinceMs)
	assert.Equal(t, time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC).UnixMilli()-1, f.UntilMs, "the end date is inclusive")
	assert.True(t, q.OldestFirst)
	assert.EqualValues(t, 100, q.Limit)
}

func TestAuditQueryOfDefaults(t *testing.T) {
	q, f, problems := AuditQueryOf(formOf(nil))
	require.Empty(t, problems)
	assert.EqualValues(t, AuditPageSizeDefault, q.Limit)
	assert.Empty(t, f.Actors)
	assert.Equal(t, ascv1.AuditOutcome_AUDIT_OUTCOME_UNSPECIFIED, f.Outcome)
	assert.Empty(t, q.Values())
}

func TestAuditQueryOfRefusesBadFilters(t *testing.T) {
	bad := []map[string]string{
		{"actor": "al ice"},
		{"actor": "a;b"},
		{"actor": strings.Repeat("a", 300)},
		{"actor": "a,b,c,d,e,f,g,h,i"},
		{"action": "Repo.Ref"},
		{"action": "repo..ref"},
		{"action": "repo.*.x"},
		{"action": "x; drop"},
		{"outcome": "maybe"},
		{"target": "a\x00b"},
		{"target": strings.Repeat("t", 300)},
		{"since": "yesterday"},
		{"until": "2026-13-45"},
		{"since": "2026-05-01", "until": "2026-04-01"},
		{"limit": "5000"},
		{"limit": "-1"},
		{"page": "has space"},
		{"page": strings.Repeat("p", 2000)},
	}
	for _, in := range bad {
		_, f, problems := AuditQueryOf(formOf(in))
		assert.NotEmpty(t, problems, "%v", in)
		assert.Nil(t, f, "nothing is sent with a problem: %v", in)
	}
}

func TestAuditQueryValuesRoundTrip(t *testing.T) {
	q, _, problems := AuditQueryOf(formOf(map[string]string{"actor": "alice", "order": "oldest", "limit": "25", "outcome": "failed"}))
	require.Empty(t, problems)
	assert.Equal(t, map[string]string{"actor": "alice", "order": "oldest", "limit": "25", "outcome": "failed"}, q.Values())
}

func auditEvent(seq uint64, action string) *ascv1.AuditEvent {
	return &ascv1.AuditEvent{
		Seq: seq, Id: "0f1e2d3c-0000-4000-8000-000000000001", TimeMs: time.Date(2026, 3, 1, 10, 12, 0, 0, time.UTC).UnixMilli(),
		Actor: &ascv1.PrincipalRef{Kind: "user", Name: "priya"}, Action: action, Repo: "acme/payments", Target: "refs/heads/main",
		Outcome: ascv1.AuditOutcome_AUDIT_OUTCOME_SUCCESS, RemoteAddr: "203.0.113.0/24",
	}
}

func TestAuditPageOfCleansAndHidesSecrets(t *testing.T) {
	e := auditEvent(7, "policy.proposed")
	e.Actor = &ascv1.PrincipalRef{Kind: "agent", Name: "bot\u202e\x00"}
	e.OnBehalfOf = &ascv1.PrincipalRef{Kind: "user", Name: "jordan"}
	e.Target = "refs/heads/<script>alert(1)</script>"
	e.Details = map[string]string{"rule": "aws-access-key", "api_key": "AKIAIOSFODNN7EXAMPLE", "Authorization": "Bearer abc", "note": "line\nbreak"}
	denied := auditEvent(6, "storage.target.verified")
	denied.Outcome = ascv1.AuditOutcome_AUDIT_OUTCOME_DENIED

	page, err := AuditPageOf(&ascv1.ListAuditResponse{Events: []*ascv1.AuditEvent{e, denied}, NextPageToken: "next_1"},
		[]ActionInfo{{Action: "policy.proposed", Description: "A change to the policy was proposed"}})
	require.NoError(t, err)
	assert.Equal(t, "next_1", page.Next)
	require.Len(t, page.Rows, 2)
	r := page.Rows[0]
	assert.Equal(t, "bot (agent)", r.Actor)
	assert.Equal(t, "jordan", r.OnBehalf)
	assert.Equal(t, "A change to the policy was proposed", r.What)
	assert.Equal(t, "ok", r.Tone)
	assert.True(t, r.Time.Equal(time.Date(2026, 3, 1, 10, 12, 0, 0, time.UTC)))
	assert.Contains(t, r.Target, "<script>", "escaping is the template's job; the text is kept")
	byKey := map[string]string{}
	for _, kv := range r.Details {
		byKey[kv.Key] = kv.Value
	}
	assert.Equal(t, "hidden", byKey["api_key"], "a value under a secret-looking key is never shown")
	assert.Equal(t, "hidden", byKey["Authorization"])
	assert.Equal(t, "aws-access-key", byKey["rule"])
	assert.Equal(t, "line break", byKey["note"])
	assert.NotContains(t, strings.Join([]string{r.Actor, r.Target, byKey["api_key"]}, " "), "AKIA")

	assert.Equal(t, "Denied", page.Rows[1].Outcome)
	assert.Equal(t, "no", page.Rows[1].Tone)
	assert.Equal(t, "storage.target.verified", page.Rows[1].What, "an action outside the catalogue shows its name")
	// Details are in key order.
	keys := []string{}
	for _, kv := range r.Details {
		keys = append(keys, kv.Key)
	}
	assert.Equal(t, []string{"Authorization", "api_key", "note", "rule"}, keys)
}

func TestAuditPageOfFailsOnMalformedEvents(t *testing.T) {
	cases := map[string]*ascv1.AuditEvent{
		"no sequence number": auditEvent(0, "x.y"),
		"no action":          auditEvent(1, ""),
		"negative time":      func() *ascv1.AuditEvent { e := auditEvent(1, "x.y"); e.TimeMs = -5; return e }(),
		"far future":         func() *ascv1.AuditEvent { e := auditEvent(1, "x.y"); e.TimeMs = 99999999999999; return e }(),
	}
	for name, e := range cases {
		_, err := AuditPageOf(&ascv1.ListAuditResponse{Events: []*ascv1.AuditEvent{e}}, nil)
		assert.ErrorIs(t, err, ErrBadAnswer, name)
	}
	_, err := AuditPageOf(&ascv1.ListAuditResponse{NextPageToken: "bad token!"}, nil)
	assert.ErrorIs(t, err, ErrBadAnswer)
	many := make([]*ascv1.AuditEvent, AuditPageSizeMax+1)
	for i := range many {
		many[i] = auditEvent(uint64(i+1), "x.y")
	}
	_, err = AuditPageOf(&ascv1.ListAuditResponse{Events: many}, nil)
	assert.ErrorIs(t, err, ErrBadAnswer, "more events than a page can hold")
}

func TestActionsOfDropsBadNames(t *testing.T) {
	got := ActionsOf(&ascv1.ListAuditActionsResponse{Actions: []*ascv1.AuditActionInfo{
		{Action: "storage.target.created", Description: "A target was created"},
		{Action: "Bad Name"},
		{Action: "policy.proposed"},
	}})
	require.Len(t, got, 2)
	assert.Equal(t, "policy.proposed", got[0].Action, "sorted")
}

// fakeExport is a stream of export messages.
type fakeExport struct {
	grpc.ClientStream
	msgs []*ascv1.ExportAuditResponse
	err  error
	i    int
}

func (f *fakeExport) Recv() (*ascv1.ExportAuditResponse, error) {
	if f.i < len(f.msgs) {
		m := f.msgs[f.i]
		f.i++
		return m, nil
	}
	if f.err != nil {
		return nil, f.err
	}
	return nil, io.EOF
}

func digestOf(parts ...[]byte) []byte {
	h := blake3.New()
	for _, p := range parts {
		_, _ = h.Write(p)
	}
	return h.Sum(nil)
}

func TestCopyExportVerifiesTheDigest(t *testing.T) {
	a, b := []byte("{\"seq\":1}\n"), []byte("{\"seq\":2}\n")
	s := &fakeExport{msgs: []*ascv1.ExportAuditResponse{
		{Data: a}, {Data: b, Last: true, Digest: digestOf(a, b), Events: 2},
	}}
	var out bytes.Buffer
	n, err := CopyExport(s, &out)
	require.NoError(t, err)
	assert.EqualValues(t, 2, n)
	assert.Equal(t, string(a)+string(b), out.String())
}

func TestCopyExportRefusesWhatDoesNotMatch(t *testing.T) {
	a := []byte("row\n")
	tampered := &fakeExport{msgs: []*ascv1.ExportAuditResponse{{Data: []byte("rox\n")}, {Last: true, Digest: digestOf(a)}}}
	_, err := CopyExport(tampered, io.Discard)
	assert.ErrorIs(t, err, ErrExportDigest)

	noDigest := &fakeExport{msgs: []*ascv1.ExportAuditResponse{{Data: a, Last: true}}}
	_, err = CopyExport(noDigest, io.Discard)
	assert.ErrorIs(t, err, ErrExportDigest, "a last message without a digest does not verify")

	truncated := &fakeExport{msgs: []*ascv1.ExportAuditResponse{{Data: a}}}
	_, err = CopyExport(truncated, io.Discard)
	assert.ErrorIs(t, err, ErrBadAnswer, "the stream ended before its last message")

	trailing := &fakeExport{msgs: []*ascv1.ExportAuditResponse{{Data: a, Last: true, Digest: digestOf(a)}, {Data: []byte("more")}}}
	_, err = CopyExport(trailing, io.Discard)
	assert.ErrorIs(t, err, ErrBadAnswer, "nothing may follow the last message")

	huge := &fakeExport{msgs: []*ascv1.ExportAuditResponse{{Data: make([]byte, maxExportChunk+1)}}}
	_, err = CopyExport(huge, io.Discard)
	assert.ErrorIs(t, err, ErrBadAnswer, "a chunk larger than the server may send")
}

func TestCopyExportReturnsTheStreamError(t *testing.T) {
	denied := status.Error(codes.NotFound, "no such scope")
	s := &fakeExport{err: denied}
	_, err := CopyExport(s, io.Discard)
	require.Error(t, err)
	assert.Equal(t, KindNotFound, Classify(err).Kind)

	w := &failingWriter{}
	s = &fakeExport{msgs: []*ascv1.ExportAuditResponse{{Data: []byte("x")}}}
	_, err = CopyExport(s, w)
	assert.ErrorIs(t, err, errDisk)
}

var errDisk = errors.New("disk full")

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errDisk }

func TestListAuditAsksWithTheScopeAndFilter(t *testing.T) {
	f, c := startFake(t)
	f.auditList = func(req *ascv1.ListAuditRequest) (*ascv1.ListAuditResponse, error) {
		return &ascv1.ListAuditResponse{Events: []*ascv1.AuditEvent{auditEvent(1, "repo.ref.update")}, NextPageToken: "n2", RetentionDays: 365}, nil
	}
	f.auditActs = func() (*ascv1.ListAuditActionsResponse, error) {
		return &ascv1.ListAuditActionsResponse{Actions: []*ascv1.AuditActionInfo{{Action: "repo.ref.update", Description: "A ref moved"}}}, nil
	}
	scope := &ascv1.OwnerRef{Kind: ascv1.OwnerKind_OWNER_KIND_REPO, Name: "acme/payments"}
	q, filter, problems := AuditQueryOf(formOf(map[string]string{"actor": "priya", "limit": "25"}))
	require.Empty(t, problems)

	res, err := ListAudit(userCtx(t, "tok-person"), c, scope, q, filter)
	require.NoError(t, err)
	require.Len(t, res.Page.Rows, 1)
	assert.Equal(t, "A ref moved", res.Page.Rows[0].What, "the catalogue names the action")
	assert.Equal(t, "n2", res.Page.Next)
	assert.EqualValues(t, 365, res.RetentionDays, "how long events are kept, as asc says")

	call := f.last(t, "AuditList")
	assert.Equal(t, "Bearer tok-person", call.Auth, "the call is made as the person")
	req := call.Req.(*ascv1.ListAuditRequest)
	assert.Equal(t, "acme/payments", req.GetScope().GetName())
	assert.Equal(t, ascv1.OwnerKind_OWNER_KIND_REPO, req.GetScope().GetKind())
	assert.Equal(t, []string{"priya"}, req.GetFilter().GetActors())
	assert.EqualValues(t, 25, req.GetLimit())
}

func TestListAuditSurvivesAMissingCatalogue(t *testing.T) {
	f, c := startFake(t)
	f.auditList = func(*ascv1.ListAuditRequest) (*ascv1.ListAuditResponse, error) {
		return &ascv1.ListAuditResponse{Events: []*ascv1.AuditEvent{auditEvent(1, "repo.ref.update")}}, nil
	}
	// auditActs is not set: ListActions answers UNIMPLEMENTED. The list is still drawn.
	res, err := ListAudit(userCtx(t, "t"), c, &ascv1.OwnerRef{Kind: ascv1.OwnerKind_OWNER_KIND_INSTANCE}, AuditQuery{Limit: 50}, &ascv1.AuditFilter{})
	require.NoError(t, err)
	assert.Equal(t, "repo.ref.update", res.Page.Rows[0].What)
}

func TestListAuditPassesTheServersRefusalOn(t *testing.T) {
	f, c := startFake(t)
	f.auditList = func(*ascv1.ListAuditRequest) (*ascv1.ListAuditResponse, error) {
		return nil, status.Error(codes.NotFound, "scope acme/secret-repo: sqlite error 5")
	}
	_, err := ListAudit(userCtx(t, "t"), c, &ascv1.OwnerRef{Kind: ascv1.OwnerKind_OWNER_KIND_REPO, Name: "x/y"}, AuditQuery{}, nil)
	require.Error(t, err)
	e := Classify(err)
	assert.Equal(t, KindNotFound, e.Kind)
	assert.NotContains(t, e.Message(), "sqlite", "the text of the server is never shown")
	assert.NotContains(t, e.Error(), "sqlite")
}

func TestVerifyAuditChain(t *testing.T) {
	f, c := startFake(t)
	f.auditChain = func(*ascv1.VerifyAuditChainRequest) (*ascv1.VerifyAuditChainResponse, error) {
		return &ascv1.VerifyAuditChainResponse{Ok: false, Checked: 12, FirstBadSeq: 9, Reason: "hash mismatch\u202e at 9"}, nil
	}
	res, err := VerifyAuditChain(userCtx(t, "t"), c, &ascv1.OwnerRef{Kind: ascv1.OwnerKind_OWNER_KIND_ORG, Name: "acme"})
	require.NoError(t, err)
	assert.False(t, res.OK)
	assert.EqualValues(t, 9, res.FirstBad)
	assert.Equal(t, "hash mismatch at 9", res.Reason)
}

func TestStartAuditExportChoosesTheFormat(t *testing.T) {
	f, c := startFake(t)
	var formats []ascv1.AuditExportFormat
	f.auditExport = func(req *ascv1.ExportAuditRequest, stream grpc.ServerStreamingServer[ascv1.ExportAuditResponse]) error {
		formats = append(formats, req.GetFormat())
		row := []byte("row\n")
		return stream.Send(&ascv1.ExportAuditResponse{Data: row, Last: true, Digest: digestOf(row), Events: 1})
	}
	for _, csv := range []bool{false, true} {
		stream, err := StartAuditExport(userCtx(t, "t"), c, &ascv1.OwnerRef{Kind: ascv1.OwnerKind_OWNER_KIND_INSTANCE}, &ascv1.AuditFilter{}, csv)
		require.NoError(t, err)
		var out bytes.Buffer
		n, err := CopyExport(stream, &out)
		require.NoError(t, err)
		assert.EqualValues(t, 1, n)
		assert.Equal(t, "row\n", out.String())
	}
	assert.Equal(t, []ascv1.AuditExportFormat{ascv1.AuditExportFormat_AUDIT_EXPORT_FORMAT_JSONL, ascv1.AuditExportFormat_AUDIT_EXPORT_FORMAT_CSV}, formats)
}

func TestExportOfAScopeTheCallerMayNotSeeIsRefusedBeforeAnyByte(t *testing.T) {
	f, c := startFake(t)
	f.auditExport = func(*ascv1.ExportAuditRequest, grpc.ServerStreamingServer[ascv1.ExportAuditResponse]) error {
		return status.Error(codes.NotFound, "no such scope")
	}
	stream, err := StartAuditExport(userCtx(t, "t"), c, &ascv1.OwnerRef{Kind: ascv1.OwnerKind_OWNER_KIND_REPO, Name: "x/y"}, nil, false)
	if err == nil {
		var out bytes.Buffer
		_, err = CopyExport(stream, &out)
		assert.Zero(t, out.Len(), "nothing was written")
	}
	require.Error(t, err)
}
