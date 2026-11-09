// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"errors"
	"testing"
	"time"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscMaintenanceOf(t *testing.T) {
	m := ascMaintenanceOf(&ascv1.MaintenanceState{On: true, Message: "disks", Until: 1790812800, Version: "2.4.0", Updated: 1790812700, UpdatedBy: "operator"})
	assert.True(t, m.On)
	assert.Equal(t, "disks", m.Message)
	assert.Equal(t, time.Unix(1790812800, 0).UTC(), m.Until)
	assert.Equal(t, "2.4.0", m.Version)
	assert.Equal(t, "operator", m.UpdatedBy)
	assert.True(t, m.ReadOnly())

	off := ascMaintenanceOf(&ascv1.MaintenanceState{})
	assert.False(t, off.ReadOnly())
	assert.True(t, off.Until.IsZero(), "no end is the zero time")
	assert.True(t, ascMaintenanceOf(&ascv1.MaintenanceState{RestoreStaged: true}).ReadOnly(), "a staged restore is read-only too")
	assert.False(t, ascMaintenanceOf(nil).On, "a missing state is off")
}

func TestSetMaintenanceNeedsAnOperation(t *testing.T) {
	_, err := SetMaintenance(t.Context(), nil, true, "disks", time.Time{}, "", "not-an-op")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrBadInput))
}
