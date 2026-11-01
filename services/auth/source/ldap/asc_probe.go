// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ldap

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"forgejo.org/models/auth"
	user_model "forgejo.org/models/user"
)

// Probe binds as the person and reads their entry, without making or changing an account.
func (source *Source) Probe(_ context.Context, login, password string) ([]auth.ProbeClaim, error) {
	sr := source.SearchEntry(login, password, source.authSource.Type == auth.DLDAP)
	if sr == nil {
		return nil, user_model.ErrUserNotExist{Name: login}
	}
	groups := sr.Groups.Values()
	slices.Sort(groups)
	return []auth.ProbeClaim{
		{Name: "username", Value: sr.Username},
		{Name: "name", Value: sr.Name},
		{Name: "surname", Value: sr.Surname},
		{Name: "email", Value: sr.Mail},
		{Name: "admin", Value: strconv.FormatBool(sr.IsAdmin)},
		{Name: "restricted", Value: strconv.FormatBool(sr.IsRestricted)},
		{Name: "groups", Value: strings.Join(groups, ", ")},
		{Name: "ssh_keys", Value: strconv.Itoa(len(sr.SSHPublicKey))},
		{Name: "avatar", Value: strconv.FormatBool(len(sr.Avatar) > 0)},
	}, nil
}
