// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package org

import (
	"context"
	"errors"

	"forgejo.org/models"
	"forgejo.org/models/db"
	org_model "forgejo.org/models/organization"
)

// The organization has no owner field: its owners are the members of the Owners team. Transferring ownership is making
// someone an owner and, when asked, leaving the Owners team oneself.

var (
	// ErrTransferNotMember is returned when the new owner is not a member of the organization.
	ErrTransferNotMember = errors.New("the new owner must already be a member of the organization")
	// ErrTransferAlreadyOwner is returned when the person is an owner already.
	ErrTransferAlreadyOwner = errors.New("that person is an owner already")
	// ErrTransferNotOwner is returned when the person who transfers is not an owner.
	ErrTransferNotOwner = errors.New("only an owner can transfer ownership")
)

// TransferOwnership makes newOwnerID an owner of the organization. With leave, doerID leaves the Owners team in the same
// transaction (there are then at least two owners at the moment of leaving, so the organization is never without one).
func TransferOwnership(ctx context.Context, o *org_model.Organization, doerID, newOwnerID int64, leave bool) error {
	if ok, err := o.IsOwnedBy(ctx, doerID); err != nil {
		return err
	} else if !ok {
		return ErrTransferNotOwner
	}
	if ok, err := o.IsOrgMember(ctx, newOwnerID); err != nil {
		return err
	} else if !ok {
		return ErrTransferNotMember
	}
	if ok, err := o.IsOwnedBy(ctx, newOwnerID); err != nil {
		return err
	} else if ok {
		return ErrTransferAlreadyOwner
	}
	owners, err := o.GetOwnerTeam(ctx)
	if err != nil {
		return err
	}
	return db.WithTx(ctx, func(ctx context.Context) error {
		if err := models.AddTeamMember(ctx, owners, newOwnerID); err != nil {
			return err
		}
		if leave {
			return models.RemoveTeamMember(ctx, owners, doerID)
		}
		return nil
	})
}
