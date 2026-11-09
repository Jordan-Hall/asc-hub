// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"strings"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/taskout"
	"forgejo.org/modules/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// chainOf answers VerifyChain for a log of head events whose first kept event is first, broken at bad (0: intact).
func chainOf(first, head, bad uint64) func(*ascv1.VerifyAuditChainRequest) (*ascv1.VerifyAuditChainResponse, error) {
	return func(req *ascv1.VerifyAuditChainRequest) (*ascv1.VerifyAuditChainResponse, error) {
		from, to := max(req.GetFromSeq(), first), req.GetToSeq()
		if to == 0 || to > head {
			to = head
		}
		resp := &ascv1.VerifyAuditChainResponse{Ok: true, FirstSeq: first, HeadSeq: head}
		if to >= from {
			resp.Checked = to - from + 1
		}
		if bad != 0 && bad >= from && bad <= to {
			resp.Ok, resp.FirstBadSeq, resp.Checked, resp.Reason = false, bad, bad-from, "the hash of the event does not match"
		}
		return resp, nil
	}
}

func verifyCalls(f *fakeAsc) [][2]uint64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out [][2]uint64
	for _, c := range f.calls {
		if c.Method == "VerifyChain" {
			r := c.Req.(*ascv1.VerifyAuditChainRequest)
			out = append(out, [2]uint64{r.GetFromSeq(), r.GetToSeq()})
		}
	}
	return out
}

func TestVerifyInstanceChainGoesInSlicesToTheHead(t *testing.T) {
	f, c := startFake(t)
	f.auditChain = chainOf(1, 2*AuditCheckSlice+10, 0)
	var out taskout.Buffer
	out.Begin("")
	ctx := taskout.With(t.Context(), &out)

	res, err := VerifyInstanceChain(ctx, c)
	require.NoError(t, err)
	assert.True(t, res.OK)
	assert.Equal(t, 2*AuditCheckSlice+10, res.Checked, "every event is counted once")
	assert.Equal(t, [][2]uint64{{0, AuditCheckSlice}, {AuditCheckSlice + 1, 2 * AuditCheckSlice}, {2*AuditCheckSlice + 1, 0}}, verifyCalls(f),
		"the last slice goes to the head, so a removed tail is found")
	call := f.last(t, "VerifyChain")
	assert.Equal(t, "Bearer "+testInternalToken, call.Auth, "the scheduled check uses the hub's own credential")
	assert.Equal(t, ascv1.OwnerKind_OWNER_KIND_INSTANCE, call.Req.(*ascv1.VerifyAuditChainRequest).GetScope().GetKind())
	lines := out.View(0).Lines
	require.Len(t, lines, 3)
	assert.Contains(t, lines[0], "checked events 1 to 50000 of 100010")
	assert.Contains(t, lines[2], "100010 events in all")

	// A short log is one call to the head.
	f.mu.Lock()
	f.calls = nil
	f.mu.Unlock()
	f.auditChain = chainOf(1, 12, 0)
	res, err = VerifyInstanceChain(t.Context(), c)
	require.NoError(t, err)
	assert.Equal(t, uint64(12), res.Checked)
	assert.Equal(t, [][2]uint64{{0, AuditCheckSlice}, {0, 0}}, verifyCalls(f), "a log within one slice is verified again to the head")
}

func TestVerifyInstanceChainStopsAtTheFirstBadSlice(t *testing.T) {
	f, c := startFake(t)
	// After retention the log starts at event 70000; it is broken at 130000.
	f.auditChain = chainOf(70_000, 200_000, 130_000)
	res, err := VerifyInstanceChain(t.Context(), c)
	require.NoError(t, err)
	assert.False(t, res.OK)
	assert.Equal(t, uint64(130_000), res.FirstBad)
	assert.Equal(t, uint64(60_000), res.Checked, "the events before the bad one, across the slices")
	calls := verifyCalls(f)
	assert.Equal(t, [2]uint64{70_000, 70_000 + AuditCheckSlice - 1}, calls[1], "the second slice starts at the first event kept")
	assert.Len(t, calls, 3, "no slice after the bad one")
}

func TestRunAuditCheckFailsOnABrokenChain(t *testing.T) {
	f, _ := startFake(t)
	f.version = func() *ascv1.VersionResponse { return &ascv1.VersionResponse{Features: []string{FeatureAudit}} }
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()

	f.auditChain = chainOf(1, 40, 0)
	res, err := RunAuditCheck(t.Context())
	require.NoError(t, err)
	assert.True(t, res.OK)

	f.auditChain = chainOf(1, 40, 17)
	var out taskout.Buffer
	out.Begin("")
	_, err = RunAuditCheck(taskout.With(t.Context(), &out))
	require.ErrorIs(t, err, ErrAuditChainBroken)
	assert.Contains(t, err.Error(), "at event 17 after 16 events checked: the hash of the event does not match")

	// A server without an audit log, and asc off: nothing to verify, and no call.
	f.mu.Lock()
	f.calls = nil
	f.mu.Unlock()
	resetFeatures()
	f.version = func() *ascv1.VersionResponse { return &ascv1.VersionResponse{} }
	out.Begin("")
	res, err = RunAuditCheck(taskout.With(t.Context(), &out))
	require.NoError(t, err)
	assert.Nil(t, res)
	assert.Contains(t, strings.Join(out.View(0).Lines, "\n"), "no audit log")
	defer test.MockVariableValue(&setting.Asc.Enabled, false)()
	res, err = RunAuditCheck(t.Context())
	require.NoError(t, err)
	assert.Nil(t, res)
	assert.Empty(t, verifyCalls(f))
}
