// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pam

import (
	"context"
	"errors"

	"forgejo.org/models/auth"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/auth/pam"
)

// Probe checks the name and password against PAM and gives the account it would make, without making it.
func (source *Source) Probe(_ context.Context, login, password string) ([]auth.ProbeClaim, error) {
	pamLogin, err := pam.Auth(source.ServiceName, login, password)
	if err != nil {
		if errors.Is(err, pam.ErrInvalidCredentials) {
			return nil, user_model.ErrUserNotExist{Name: login}
		}
		return nil, err
	}
	username, email := source.names(pamLogin)
	return []auth.ProbeClaim{{Name: "login", Value: pamLogin}, {Name: "username", Value: username}, {Name: "email", Value: email}}, nil
}
