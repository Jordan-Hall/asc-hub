// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"google.golang.org/grpc"
)

// RotateRevokeError is returned by RotateKey when the new key was added but the old one could not be revoked. The new
// key is live; the old one still is too, and the person can revoke it from the list.
type RotateRevokeError struct {
	New *KeyRow
	Err error
}

func (e *RotateRevokeError) Error() string {
	return "asctrust: the new key was added, the old one was not revoked: " + e.Err.Error()
}
func (e *RotateRevokeError) Unwrap() error { return e.Err }

var rotateNamespace = uuid.MustParse("6d6b5b0e-2f43-5f1e-9a53-0c2d5a7a9f11")

// RotateKey replaces a signing key in two calls: the new key is added first, then the old one is revoked, so there is
// no moment without a live key and a failure of the first call changes nothing. Signatures made with the old key
// before the revocation stay explainable. op is the id of the form; the revocation gets an id derived from it, so the
// same form submitted twice is the same rotation.
func RotateKey(ctx context.Context, conn grpc.ClientConnInterface, oldID, title, publicKey string, expiresAt int64, op string) (*KeyRow, error) {
	if !aiKeyIDShape.MatchString(oldID) {
		return nil, fmt.Errorf("%w: key id", ErrBadInput)
	}
	formOp, err := uuid.Parse(op)
	if err != nil {
		return nil, fmt.Errorf("%w: operation id", ErrBadInput)
	}
	added, err := AddKey(ctx, conn, title, publicKey, expiresAt, op)
	if err != nil {
		return nil, err
	}
	if added.ID == oldID {
		return nil, fmt.Errorf("%w: the new key is the old key", ErrBadInput)
	}
	revokeOp := uuid.NewSHA1(rotateNamespace, []byte(formOp.String()+"/revoke")).String()
	if _, err := RevokeKey(ctx, conn, oldID, "rotated", revokeOp); err != nil {
		return added, &RotateRevokeError{New: added, Err: err}
	}
	return added, nil
}
