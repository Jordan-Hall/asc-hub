// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"errors"
	"fmt"

	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/taskout"

	"google.golang.org/grpc"
)

// AuditCheckSlice is the most events one call of the scheduled check verifies.
const AuditCheckSlice uint64 = 50_000

// ErrAuditChainBroken is what the scheduled check returns when asc finds the instance chain broken.
var ErrAuditChainBroken = errors.New("the audit chain of the instance is broken")

// VerifyInstanceChain verifies the hash chain of the whole instance audit log with the hub's own credential, in slices of
// AuditCheckSlice events, the last slice up to the head (so a removed tail is found). It stops at the first bad slice and
// returns its result with the events checked before it added.
func VerifyInstanceChain(ctx context.Context, conn grpc.ClientConnInterface) (*ChainResult, error) {
	sys := ascclient.WithSystem(ctx)
	scope := &ascv1.OwnerRef{Kind: ascv1.OwnerKind_OWNER_KIND_INSTANCE}
	var from, done uint64
	to := AuditCheckSlice
	for {
		res, err := VerifyAuditChain(sys, conn, scope, from, to)
		if err != nil {
			return nil, err
		}
		res.Checked += done
		if to == 0 {
			taskout.Say(ctx, "checked events %d to the head (%d): %d events in all", max(from, res.FirstSeq), res.HeadSeq, res.Checked)
		} else if lo, hi := max(from, res.FirstSeq), min(to, res.HeadSeq); lo <= hi {
			taskout.Say(ctx, "checked events %d to %d of %d", lo, hi, res.HeadSeq)
		}
		if !res.OK || to == 0 {
			return res, nil
		}
		done = res.Checked
		from = max(to+1, res.FirstSeq)
		switch {
		case res.HeadSeq < from:
			// The whole log fitted in the first slice: verify it again to the head, which also checks the head.
			from, done, to = 0, 0, 0
		case res.HeadSeq-from < AuditCheckSlice:
			to = 0
		default:
			to = from + AuditCheckSlice - 1
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
}

// RunAuditCheck is the scheduled "Verify audit chain" task. It does nothing when asc is off or does not keep an audit log;
// a broken chain is an error that names the first bad event, which the cron service turns into a system notice.
func RunAuditCheck(ctx context.Context) (*ChainResult, error) {
	if !setting.Asc.Enabled {
		taskout.Say(ctx, "asc is off: nothing to verify")
		return nil, nil
	}
	if !HasFeature(ctx, FeatureAudit) {
		taskout.Say(ctx, "asc serve keeps no audit log: nothing to verify")
		return nil, nil
	}
	conn, err := Conn()
	if err != nil {
		return nil, err
	}
	res, err := VerifyInstanceChain(ctx, conn)
	if err != nil {
		return nil, err
	}
	if !res.OK {
		return res, fmt.Errorf("%w at event %d after %d events checked: %s", ErrAuditChainBroken, res.FirstBad, res.Checked, res.Reason)
	}
	taskout.Say(ctx, "the chain is intact")
	return res, nil
}
