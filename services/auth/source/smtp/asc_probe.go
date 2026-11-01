// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package smtp

import (
	"context"
	"strings"

	"forgejo.org/models/auth"
)

// Probe checks the name and password against the SMTP server and gives the account it would make, without making it.
func (source *Source) Probe(_ context.Context, login, password string) ([]auth.ProbeClaim, error) {
	if err := source.check(login, password); err != nil {
		return nil, err
	}
	username, _, _ := strings.Cut(login, "@")
	return []auth.ProbeClaim{{Name: "login", Value: login}, {Name: "username", Value: strings.ToLower(username)}, {Name: "email", Value: login}}, nil
}
