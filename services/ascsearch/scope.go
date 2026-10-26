// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascsearch

import (
	"errors"

	issues_model "forgejo.org/models/issues"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
)

// ErrBadChange is returned for a change id that is not 32 letters from k to z.
var ErrBadChange = errors.New("ascsearch: bad change id")

// ChangeBytes decodes the text form of a change id: letter c is the hex digit 'z' - c, high digit first.
func ChangeBytes(change string) ([]byte, error) {
	if !issues_model.IsAscChange(change) {
		return nil, ErrBadChange
	}
	out := make([]byte, 16)
	for i := range out {
		out[i] = (('z' - change[2*i]) << 4) | ('z' - change[2*i+1])
	}
	return out, nil
}

// AtChange makes the scope of a change: the change at its current head. With stack the scope is the top of
// the stack that ends in the change, and with diffOnly only the files the stack changes are searched.
func AtChange(repo string, change string, stack, diffOnly bool) (*ascv1.Scope, error) {
	id, err := ChangeBytes(change)
	if err != nil {
		return nil, err
	}
	s := &ascv1.Scope{Repo: &ascv1.RepoRef{Name: repo, IdMode: ascv1.IdMode_ID_MODE_GIT}}
	if stack {
		s.At = &ascv1.Scope_StackTop{StackTop: id}
		s.StackDiffOnly = diffOnly
	} else {
		s.At = &ascv1.Scope_Change{Change: id}
	}
	return s, nil
}
