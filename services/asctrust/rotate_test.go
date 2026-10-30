// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRotateKeyAddsThenRevokes(t *testing.T) {
	f, c := startFake(t)
	line := edLine(t)
	fp := "SHA256:abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"
	f.addKey = func(req *ascv1.AddSigningKeyRequest) (*ascv1.AddSigningKeyResponse, error) {
		return &ascv1.AddSigningKeyResponse{Key: &ascv1.SigningKey{Id: "k_new", Title: req.GetTitle(), Kind: ascv1.SigningKeyKind_SIGNING_KEY_KIND_SSH_ED25519, Fingerprint: fp, Created: time.Now().Unix()}}, nil
	}
	f.revokeKey = func(req *ascv1.RevokeSigningKeyRequest) (*ascv1.RevokeSigningKeyResponse, error) {
		return &ascv1.RevokeSigningKeyResponse{Key: &ascv1.SigningKey{Id: req.GetId(), Fingerprint: fp, Created: 1, RevokedAt: time.Now().Unix(), RevokedReason: req.GetReason()}}, nil
	}
	ctx := userCtx(t, "tok-person")
	op := NewOpID()

	row, err := RotateKey(ctx, c, "k_old", "New laptop", line, 0, op)
	require.NoError(t, err)
	assert.Equal(t, "k_new", row.ID)
	add, rev := f.last(t, "AddKey"), f.last(t, "RevokeKey")
	assert.Equal(t, op, add.OpID)
	assert.NotEqual(t, op, rev.OpID, "the revocation has an id of its own")
	assert.Equal(t, "k_old", rev.Req.(*ascv1.RevokeSigningKeyRequest).GetId())
	assert.Equal(t, "rotated", rev.Req.(*ascv1.RevokeSigningKeyRequest).GetReason())

	_, err = RotateKey(ctx, c, "k_old", "New laptop", line, 0, op)
	require.NoError(t, err)
	assert.Equal(t, rev.OpID, f.last(t, "RevokeKey").OpID, "the same form is the same revocation")
}

func TestRotateKeyChangesNothingWhenTheAddFails(t *testing.T) {
	f, c := startFake(t)
	f.addKey = func(*ascv1.AddSigningKeyRequest) (*ascv1.AddSigningKeyResponse, error) {
		return nil, status.Error(codes.AlreadyExists, "key exists")
	}
	_, err := RotateKey(userCtx(t, "tok-person"), c, "k_old", "x", edLine(t), 0, NewOpID())
	require.Error(t, err)
	assert.Zero(t, f.count("RevokeKey"), "the old key stays when the new one cannot be added")

	_, err = RotateKey(userCtx(t, "tok-person"), c, "../x", "x", edLine(t), 0, NewOpID())
	assert.ErrorIs(t, err, ErrBadInput)
	_, err = RotateKey(userCtx(t, "tok-person"), c, "k_old", "x", edLine(t), 0, "nope")
	assert.ErrorIs(t, err, ErrBadInput)
	assert.Equal(t, 1, f.count("AddKey"), "bad input is refused before a call")
}

func TestRotateKeySaysWhenOnlyTheRevokeFailed(t *testing.T) {
	f, c := startFake(t)
	f.addKey = func(req *ascv1.AddSigningKeyRequest) (*ascv1.AddSigningKeyResponse, error) {
		return &ascv1.AddSigningKeyResponse{Key: &ascv1.SigningKey{Id: "k_new", Title: "t", Kind: ascv1.SigningKeyKind_SIGNING_KEY_KIND_SSH_ED25519, Fingerprint: "SHA256:abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG", Created: time.Now().Unix()}}, nil
	}
	f.revokeKey = func(*ascv1.RevokeSigningKeyRequest) (*ascv1.RevokeSigningKeyResponse, error) {
		return nil, status.Error(codes.Unavailable, "down")
	}
	row, err := RotateKey(userCtx(t, "tok-person"), c, "k_old", "t", edLine(t), 0, NewOpID())
	var half *RotateRevokeError
	require.True(t, errors.As(err, &half))
	assert.Equal(t, "k_new", half.New.ID)
	assert.Equal(t, "k_new", row.ID)
}

func edLine(t *testing.T) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	return authorizedKey(t, pub)
}
