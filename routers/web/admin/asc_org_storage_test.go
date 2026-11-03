// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	"testing"

	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/services/ascbyo/ascbyotest"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestLoadOrgStorage(t *testing.T) {
	f := ascbyotest.Start(t)
	org := func(name string) *ascv1.OwnerRef {
		return &ascv1.OwnerRef{Kind: ascv1.OwnerKind_OWNER_KIND_ORG, Name: name}
	}
	f.AddTarget(org("acme"), "garage-eu", ascv1.StorageTargetState_STORAGE_TARGET_STATE_ENABLED)
	f.AddTarget(org("acme"), "r2-backup", ascv1.StorageTargetState_STORAGE_TARGET_STATE_ENABLED)
	f.AddTarget(org("acme"), "draft", ascv1.StorageTargetState_STORAGE_TARGET_STATE_PENDING)
	f.AddTarget(org("orbit"), "unused", ascv1.StorageTargetState_STORAGE_TARGET_STATE_DISABLED)
	f.AddTarget(&ascv1.OwnerRef{Kind: ascv1.OwnerKind_OWNER_KIND_USER, Name: "nova"}, "mine", ascv1.StorageTargetState_STORAGE_TARGET_STATE_ENABLED)

	orgs := []*user_model.User{{ID: 1, Name: "acme"}, {ID: 2, Name: "orbit"}, {ID: 3, Name: "nova"}}
	got := loadOrgStorage(t.Context(), orgs)
	assert.Equal(t, OrgStorage{Names: []string{"garage-eu", "r2-backup"}}, got[1], "enabled targets only, oldest first")
	assert.Equal(t, 1, got[1].More())
	assert.Equal(t, OrgStorage{}, got[2], "no enabled target is the server default")
	assert.Equal(t, OrgStorage{}, got[3], "a target of a person of the same name is not the organization's")
	assert.Equal(t, 0, got[2].More())

	f.SetFail("/asc.v1.Storage/ListTargets", status.Error(codes.Unavailable, "down"))
	got = loadOrgStorage(t.Context(), orgs)
	for _, o := range orgs {
		assert.True(t, got[o.ID].Unknown, o.Name)
	}
	assert.Empty(t, loadOrgStorage(t.Context(), nil))
}
