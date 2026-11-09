// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"time"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"google.golang.org/grpc"
)

// FeatureHealth is what Version.features says when the server answers Maintenance.CheckHealth: the probes of its
// object store and its signing key, and its clock, for the site administrator.
const FeatureHealth = "health.v1"

// HealthProbe is one probe asc serve made.
type HealthProbe struct {
	State  ascv1.HealthState
	Millis uint32
	Detail string // why it failed, a fixed phrase of asc serve
}

// OK is whether the probe passed.
func (p HealthProbe) OK() bool { return p.State == ascv1.HealthState_HEALTH_STATE_OK }

// NotConfigured is whether asc serve has no such thing to check.
func (p HealthProbe) NotConfigured() bool {
	return p.State == ascv1.HealthState_HEALTH_STATE_NOT_CONFIGURED
}

// Health is what asc serve found when it probed itself, and how far its clock is from the hub's.
type Health struct {
	ObjectStore HealthProbe
	ServerKey   HealthProbe
	Fingerprint string
	// Skew is asc serve's clock minus the hub's, measured at the middle of the call; Uncertainty is half the time the
	// call took, so the true difference is within Skew ± Uncertainty.
	Skew        time.Duration
	Uncertainty time.Duration
}

// LoadHealth asks asc serve to probe itself. The call must be made as the site administrator. now is the hub's clock.
func LoadHealth(ctx context.Context, conn grpc.ClientConnInterface, now func() time.Time) (Health, error) {
	sent := now()
	resp, err := ascv1.NewMaintenanceClient(conn).CheckHealth(ctx, &ascv1.CheckHealthRequest{})
	got := now()
	if err != nil {
		return Health{}, err
	}
	probe := func(p *ascv1.HealthProbe) HealthProbe {
		return HealthProbe{State: p.GetState(), Millis: p.GetMillis(), Detail: p.GetDetail()}
	}
	h := Health{ObjectStore: probe(resp.GetObjectStore()), ServerKey: probe(resp.GetServerKey()), Fingerprint: resp.GetServerKeyFingerprint()}
	if h.ObjectStore.State == ascv1.HealthState_HEALTH_STATE_UNSPECIFIED || h.ServerKey.State == ascv1.HealthState_HEALTH_STATE_UNSPECIFIED || resp.GetNowMs() <= 0 {
		return Health{}, ErrBadAnswer
	}
	rtt := got.Sub(sent)
	mid := sent.Add(rtt / 2)
	h.Skew = time.UnixMilli(resp.GetNowMs()).Sub(mid)
	h.Uncertainty = rtt/2 + time.Millisecond // asc gives whole milliseconds
	return h, nil
}
