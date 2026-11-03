// Copyright 2022 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package actions

import (
	"context"
	"errors"
	"fmt"
	"time"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/optional"
	"forgejo.org/modules/timeutil"
	"forgejo.org/modules/util"

	"xorm.io/builder"
)

// ActionRunnerToken represents runner tokens
//
// It can be:
//  1. global token, OwnerID is 0 and RepoID is 0
//  2. org/user level token, OwnerID is org/user ID and RepoID is 0
//  3. repo level token, OwnerID is 0 and RepoID is repo ID
//
// Please note that it's not acceptable to have both OwnerID and RepoID to be non-zero,
// or it will be complicated to find tokens belonging to a specific owner.
// For example, conditions like `OwnerID = 1` will also return token {OwnerID: 1, RepoID: 1},
// but it's a repo level token, not an org/user level token.
// To avoid this, make it clear with {OwnerID: 0, RepoID: 1} for repo level tokens.
type ActionRunnerToken struct {
	ID       int64
	Token    string                 `xorm:"UNIQUE"`
	OwnerID  optional.Option[int64] `xorm:"index REFERENCES(user, id)"`
	Owner    *user_model.User       `xorm:"-"`
	RepoID   optional.Option[int64] `xorm:"index REFERENCES(repository, id)"`
	Repo     *repo_model.Repository `xorm:"-"`
	IsActive bool                   // true means it can be used
	// ExpiresUnix is when the token stops registering runners; 0 is never. A token the web page shows once expires after
	// ShownOnceRunnerTokenLifetime; the ones the API and the command line give do not.
	ExpiresUnix timeutil.TimeStamp `xorm:"NOT NULL DEFAULT 0"`

	Created timeutil.TimeStamp `xorm:"created"`
	Updated timeutil.TimeStamp `xorm:"updated"`
}

// ShownOnceRunnerTokenLifetime is how long a registration token shown once on the web registers runners.
const ShownOnceRunnerTokenLifetime = time.Hour

// IsExpired is true once a token with an expiry has reached it.
func (t *ActionRunnerToken) IsExpired() bool {
	return t.ExpiresUnix != 0 && timeutil.TimeStampNow() >= t.ExpiresUnix
}

// Usable is true for a token that registers runners: active and not expired.
func (t *ActionRunnerToken) Usable() bool {
	return t.IsActive && !t.IsExpired()
}

func init() {
	db.RegisterModel(new(ActionRunnerToken))
}

// GetRunnerToken returns a action runner via token
func GetRunnerToken(ctx context.Context, token string) (*ActionRunnerToken, error) {
	var runnerToken ActionRunnerToken
	has, err := db.GetEngine(ctx).Where("token=?", token).Get(&runnerToken)
	if err != nil {
		return nil, err
	} else if !has {
		return nil, fmt.Errorf("runner token %q: %w", token, util.ErrNotExist)
	}
	return &runnerToken, nil
}

// GetRunnerTokenByID returns a runner token by its id.
func GetRunnerTokenByID(ctx context.Context, id int64) (*ActionRunnerToken, error) {
	var runnerToken ActionRunnerToken
	has, err := db.GetEngine(ctx).ID(id).Get(&runnerToken)
	if err != nil {
		return nil, err
	} else if !has {
		return nil, fmt.Errorf("runner token %d: %w", id, util.ErrNotExist)
	}
	return &runnerToken, nil
}

// UpdateRunnerToken updates runner token information.
func UpdateRunnerToken(ctx context.Context, r *ActionRunnerToken, cols ...string) (err error) {
	e := db.GetEngine(ctx)

	if len(cols) == 0 {
		_, err = e.ID(r.ID).AllCols().Update(r)
	} else {
		_, err = e.ID(r.ID).Cols(cols...).Update(r)
	}
	return err
}

// NewRunnerToken creates a new active runner token and invalidate all old tokens
// ownerID will be ignored and treated as 0 if repoID is non-zero.
func NewRunnerToken(ctx context.Context, ownerID, repoID optional.Option[int64]) (*ActionRunnerToken, error) {
	return newRunnerToken(ctx, ownerID, repoID, 0)
}

// NewShownOnceRunnerToken is NewRunnerToken for the web page that shows the token once: it expires after
// ShownOnceRunnerTokenLifetime.
func NewShownOnceRunnerToken(ctx context.Context, ownerID, repoID optional.Option[int64]) (*ActionRunnerToken, error) {
	return newRunnerToken(ctx, ownerID, repoID, timeutil.TimeStampNow().AddDuration(ShownOnceRunnerTokenLifetime))
}

// UsableRunnerToken is the latest token of the scope when it still registers runners, or else a new one without expiry
// (the API and the command line, which hand the token to a program).
func UsableRunnerToken(ctx context.Context, ownerID, repoID optional.Option[int64]) (*ActionRunnerToken, error) {
	token, err := GetLatestRunnerToken(ctx, ownerID, repoID)
	if err == nil && token.Usable() {
		return token, nil
	}
	if err != nil && !errors.Is(err, util.ErrNotExist) {
		return nil, err
	}
	return NewRunnerToken(ctx, ownerID, repoID)
}

func newRunnerToken(ctx context.Context, ownerID, repoID optional.Option[int64], expires timeutil.TimeStamp) (*ActionRunnerToken, error) {
	if ownerID.Has() && repoID.Has() {
		// It's trying to create a runner token that belongs to a repository, but OwnerID has been set accidentally.
		// Remove OwnerID to avoid confusion; it's not worth returning an error here.
		ownerID = optional.None[int64]()
	}

	token := util.CryptoRandomString(util.RandomStringHigh)
	runnerToken := &ActionRunnerToken{
		OwnerID:     ownerID,
		RepoID:      repoID,
		IsActive:    true,
		Token:       token,
		ExpiresUnix: expires,
	}

	return runnerToken, db.WithTx(ctx, func(ctx context.Context) error {
		if _, err := db.GetEngine(ctx).Where(runnerTokenCond(ownerID, repoID)).Cols("is_active").Update(&ActionRunnerToken{
			IsActive: false,
		}); err != nil {
			return err
		}

		_, err := db.GetEngine(ctx).Insert(runnerToken)
		return err
	})
}

func runnerTokenCond(ownerID, repoID optional.Option[int64]) builder.Cond {
	var condOwnerID builder.Cond
	if has, value := ownerID.Get(); !has {
		condOwnerID = builder.IsNull{"owner_id"}
	} else {
		condOwnerID = builder.Eq{"owner_id": value}
	}
	var condRepoID builder.Cond
	if has, value := repoID.Get(); !has {
		condRepoID = builder.IsNull{"repo_id"}
	} else {
		condRepoID = builder.Eq{"repo_id": value}
	}
	return builder.And(condOwnerID, condRepoID)
}

// GetLatestRunnerToken returns the latest runner token
func GetLatestRunnerToken(ctx context.Context, ownerID, repoID optional.Option[int64]) (*ActionRunnerToken, error) {
	if ownerID.Has() && repoID.Has() {
		// It's trying to create a runner token that belongs to a repository, but OwnerID has been set accidentally.
		// Remove OwnerID to avoid confusion; it's not worth returning an error here.
		ownerID = optional.None[int64]()
	}

	var runnerToken ActionRunnerToken
	has, err := db.GetEngine(ctx).Where(runnerTokenCond(ownerID, repoID)).
		OrderBy("id DESC").Get(&runnerToken)
	if err != nil {
		return nil, err
	} else if !has {
		return nil, fmt.Errorf("runner token: %w", util.ErrNotExist)
	}
	return &runnerToken, nil
}
