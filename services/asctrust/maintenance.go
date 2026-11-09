// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"time"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"google.golang.org/grpc"
)

// The Maintenance service of asc serve (SPEC §14.5): read-only maintenance of the whole server. The hub's own maintenance
// switch calls it as the site administrator (CallInstance), so the hub and asc serve go read-only together and pushes
// from the command line are refused with the same message.

// FeatureMaintenance is what Version.features says when the server has the Maintenance service.
const FeatureMaintenance = "maintenance.v1"

// AscMaintenance is the switch of asc serve as the admin page draws it.
type AscMaintenance struct {
	On            bool
	Message       string
	Until         time.Time
	Version       string
	Updated       time.Time
	UpdatedBy     string
	RestoreStaged bool
}

func ascMaintenanceOf(s *ascv1.MaintenanceState) AscMaintenance {
	return AscMaintenance{
		On: s.GetOn(), Message: s.GetMessage(), Until: unixTime(s.GetUntil()), Version: s.GetVersion(),
		Updated: unixTime(s.GetUpdated()), UpdatedBy: s.GetUpdatedBy(), RestoreStaged: s.GetRestoreStaged(),
	}
}

// ReadOnly reports whether asc serve refuses writes now, for either reason.
func (m AscMaintenance) ReadOnly() bool { return m.On || m.RestoreStaged }

// GetMaintenance reads the switch of asc serve.
func GetMaintenance(ctx context.Context, conn grpc.ClientConnInterface) (*AscMaintenance, error) {
	cctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	resp, err := ascv1.NewMaintenanceClient(conn).GetMaintenance(cctx, &ascv1.GetMaintenanceRequest{})
	if err != nil {
		return nil, err
	}
	out := ascMaintenanceOf(resp.GetState())
	return &out, nil
}

// SetMaintenance switches asc serve. A zero until is no expected end.
func SetMaintenance(ctx context.Context, conn grpc.ClientConnInterface, on bool, message string, until time.Time, version, op string) (*AscMaintenance, error) {
	cctx, cancel, err := opCtx(ctx, op)
	if err != nil {
		return nil, err
	}
	defer cancel()
	var at int64
	if !until.IsZero() {
		at = until.Unix()
	}
	resp, err := ascv1.NewMaintenanceClient(conn).SetMaintenance(cctx, &ascv1.SetMaintenanceRequest{On: on, Message: message, Until: at, Version: version})
	if err != nil {
		return nil, err
	}
	out := ascMaintenanceOf(resp.GetState())
	return &out, nil
}
