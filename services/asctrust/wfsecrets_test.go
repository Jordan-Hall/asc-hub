// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"strings"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkflowSecretsAreListedByNameAndNeverCarryAValue(t *testing.T) {
	f, c := startFake(t)
	f.wfList = func(*ascv1.ListWorkflowSecretsRequest) (*ascv1.ListWorkflowSecretsResponse, error) {
		return &ascv1.ListWorkflowSecretsResponse{Secrets: []*ascv1.WorkflowSecret{
			{Name: "registry", Hint: "f3a9", Updated: 1_700_000_000, UpdatedBy: "qa"},
			{Name: "bad name!", Hint: "x"},
			{Name: "ok-2"},
		}}, nil
	}
	rows, err := ListWorkflowSecrets(userCtx(t, "tok"), c, "acme/payments")
	require.NoError(t, err)
	require.Len(t, rows, 2, "a name that is not a secret name is dropped")
	assert.Equal(t, "registry", rows[0].Name)
	assert.Equal(t, "f3a9", rows[0].Hint)
	assert.Equal(t, "qa", rows[0].UpdatedBy)
	assert.False(t, rows[0].Updated.IsZero())
	assert.Equal(t, "acme/payments", f.last(t, "ListSecrets").Req.(*ascv1.ListWorkflowSecretsRequest).GetRepo().GetName())
}

func TestSetWorkflowSecretChecksBeforeTheCallAndSendsTheValueOnce(t *testing.T) {
	f, c := startFake(t)
	f.wfSet = func(req *ascv1.SetWorkflowSecretRequest) (*ascv1.SetWorkflowSecretResponse, error) {
		return &ascv1.SetWorkflowSecretResponse{Secret: &ascv1.WorkflowSecret{Name: req.GetName(), Hint: "cret"}}, nil
	}
	op := NewOpID()
	row, err := SetWorkflowSecret(userCtx(t, "tok-admin"), c, "acme/payments", "registry", "s3cret-value-cret", op)
	require.NoError(t, err)
	assert.Equal(t, "registry", row.Name)
	call := f.last(t, "SetSecret")
	assert.Equal(t, "Bearer tok-admin", call.Auth)
	assert.Equal(t, op, call.OpID)
	assert.Equal(t, "s3cret-value-cret", call.Req.(*ascv1.SetWorkflowSecretRequest).GetValue())

	before := f.count("SetSecret")
	for _, bad := range []struct{ name, value, op string }{
		{"", "v", op}, {"has space", "v", op}, {strings.Repeat("a", 65), "v", op},
		{"ok", "", op}, {"ok", strings.Repeat("x", MaxWorkflowSecretValue+1), op}, {"ok", "v", "not-a-uuid"},
	} {
		_, err := SetWorkflowSecret(userCtx(t, "tok"), c, "acme/payments", bad.name, bad.value, bad.op)
		require.ErrorIs(t, err, ErrBadInput, "%q", bad.name)
		assert.NotContains(t, err.Error(), bad.value+"zz", "an error never carries the value")
	}
	assert.Equal(t, before, f.count("SetSecret"), "nothing is sent for bad input")
}

func TestSetWorkflowSecretRefusesAnAnswerForAnotherName(t *testing.T) {
	f, c := startFake(t)
	f.wfSet = func(*ascv1.SetWorkflowSecretRequest) (*ascv1.SetWorkflowSecretResponse, error) {
		return &ascv1.SetWorkflowSecretResponse{Secret: &ascv1.WorkflowSecret{Name: "other"}}, nil
	}
	_, err := SetWorkflowSecret(userCtx(t, "tok"), c, "acme/payments", "registry", "v", NewOpID())
	assert.ErrorIs(t, err, ErrBadAnswer)
}

func TestDeleteWorkflowSecret(t *testing.T) {
	f, c := startFake(t)
	f.wfDel = func(*ascv1.DeleteWorkflowSecretRequest) (*ascv1.DeleteWorkflowSecretResponse, error) {
		return &ascv1.DeleteWorkflowSecretResponse{}, nil
	}
	op := NewOpID()
	require.NoError(t, DeleteWorkflowSecret(userCtx(t, "tok"), c, "acme/payments", "registry", op))
	call := f.last(t, "DeleteSecret")
	assert.Equal(t, op, call.OpID)
	assert.Equal(t, "registry", call.Req.(*ascv1.DeleteWorkflowSecretRequest).GetName())
	assert.ErrorIs(t, DeleteWorkflowSecret(userCtx(t, "tok"), c, "acme/payments", "a b", op), ErrBadInput)
	assert.ErrorIs(t, DeleteWorkflowSecret(userCtx(t, "tok"), c, "acme/payments", "ok", "nope"), ErrBadInput)
}
