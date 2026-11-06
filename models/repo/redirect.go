// Copyright 2017 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"forgejo.org/models/db"
	"forgejo.org/modules/timeutil"
	"forgejo.org/modules/util"
)

// RedirectKeepDays is how long the old address of a renamed or moved repository keeps redirecting.
const RedirectKeepDays = 90

// ErrRedirectNotExist represents a "RedirectNotExist" kind of error.
type ErrRedirectNotExist struct {
	OwnerID           int64
	RepoName          string
	MissingPermission bool
}

// IsErrRedirectNotExist check if an error is an ErrRepoRedirectNotExist.
func IsErrRedirectNotExist(err error) bool {
	_, ok := err.(ErrRedirectNotExist)
	return ok
}

func (err ErrRedirectNotExist) Error() string {
	return fmt.Sprintf("repository redirect does not exist [uid: %d, name: %s]", err.OwnerID, err.RepoName)
}

func (err ErrRedirectNotExist) Unwrap() error {
	return util.ErrNotExist
}

// Redirect represents that a repo name should be redirected to another
type Redirect struct {
	ID             int64  `xorm:"pk autoincr"`
	OwnerID        int64  `xorm:"UNIQUE(s)"`
	LowerName      string `xorm:"UNIQUE(s) INDEX NOT NULL"`
	RedirectRepoID int64  // repoID to redirect to
	// ExpiresUnix is when the redirect stops; 0 for a redirect made before redirects expired, which never stops.
	ExpiresUnix timeutil.TimeStamp `xorm:"INDEX NOT NULL DEFAULT 0"`
}

// TableName represents real table name in database
func (Redirect) TableName() string {
	return "repo_redirect"
}

func init() {
	db.RegisterModel(new(Redirect))
}

// GetRedirect returns the redirect for a given pair of ownerID and repository name.
func GetRedirect(ctx context.Context, ownerID int64, repoName string) (int64, error) {
	repoName = strings.ToLower(repoName)
	redirect := &Redirect{OwnerID: ownerID, LowerName: repoName}
	if has, err := db.GetEngine(ctx).Get(redirect); err != nil {
		return 0, err
	} else if !has {
		return 0, ErrRedirectNotExist{OwnerID: ownerID, RepoName: repoName}
	}
	if redirect.ExpiresUnix > 0 && redirect.ExpiresUnix <= timeutil.TimeStampNow() {
		// the old address has stopped redirecting: it is free for a new repository
		if err := DeleteRedirect(ctx, ownerID, repoName); err != nil {
			return 0, err
		}
		return 0, ErrRedirectNotExist{OwnerID: ownerID, RepoName: repoName}
	}
	return redirect.RedirectRepoID, nil
}

// NewRedirect create a new repo redirect
func NewRedirect(ctx context.Context, ownerID, repoID int64, oldRepoName, newRepoName string) error {
	oldRepoName = strings.ToLower(oldRepoName)
	newRepoName = strings.ToLower(newRepoName)

	if err := DeleteRedirect(ctx, ownerID, newRepoName); err != nil {
		return err
	}

	return db.Insert(ctx, &Redirect{
		OwnerID:        ownerID,
		LowerName:      oldRepoName,
		RedirectRepoID: repoID,
		ExpiresUnix:    timeutil.TimeStampNow().AddDuration(RedirectKeepDays * 24 * time.Hour),
	})
}

// DeleteRedirect delete any redirect from the specified repo name to
// anything else
func DeleteRedirect(ctx context.Context, ownerID int64, repoName string) error {
	repoName = strings.ToLower(repoName)
	_, err := db.GetEngine(ctx).Delete(&Redirect{OwnerID: ownerID, LowerName: repoName})
	return err
}
