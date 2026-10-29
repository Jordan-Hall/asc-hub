// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"testing"

	issues_model "forgejo.org/models/issues"
	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
)

func TestAscApprovalState(t *testing.T) {
	s, ok := ascApprovalState(issues_model.ReviewTypeApprove)
	assert.True(t, ok)
	assert.Equal(t, ascv1.ApprovalState_APPROVAL_STATE_APPROVED, s)
	s, ok = ascApprovalState(issues_model.ReviewTypeReject)
	assert.True(t, ok)
	assert.Equal(t, ascv1.ApprovalState_APPROVAL_STATE_CHANGES_REQUESTED, s)
	_, ok = ascApprovalState(issues_model.ReviewTypeComment)
	assert.False(t, ok, "a comment is no verdict")
}
