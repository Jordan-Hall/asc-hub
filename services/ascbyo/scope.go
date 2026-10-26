// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package ascbyo is the hub side of "bring your own storage" and "bring your own AI keys": the settings
// pages of a user, an organisation and a repository talk to asc (the Storage and AiProviders services of
// asc.v1) through it.
//
// # Trust
//
// A web session carries no asc token, so every call is a system call (ascclient.WithSystem). The caller of
// this package has already decided, from Forgejo's own permissions, that the signed-in user administers
// the scope, and the Scope it passes is built from the route (never from a form value). This package adds
// the second check that a system call needs: every call that names a resource by id first reads the
// resource and refuses it unless it belongs to the Scope, so an id taken from a form cannot reach another
// owner's target, key or migration. A resource of another owner is reported as not found.
//
// # Secrets
//
// S3 secret keys, session tokens and AI API keys are write-only: they are sent to asc on create and update,
// and nothing here keeps them, logs them or puts them in an error. Text from asc that reaches a page is
// cut, stripped of control characters and refused whole when it repeats a secret that was just submitted.
// The views carry only what asc returns for display: a hint, a flag and a state.
package ascbyo

import (
	"fmt"
	"strings"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
)

// Kind is what a Scope belongs to.
type Kind uint8

const (
	KindUser Kind = iota + 1
	KindOrg
	KindRepo
)

// String is the path-safe name of the kind.
func (k Kind) String() string {
	switch k {
	case KindUser:
		return "user"
	case KindOrg:
		return "org"
	case KindRepo:
		return "repo"
	}
	return "unknown"
}

// MaxScopeNameBytes bounds a scope name before it is sent to asc.
const MaxScopeNameBytes = 255

// Scope names the owner of storage targets and AI keys: a user, an organisation or a repository. Names are
// lower case as asc stores them ("owner/repo" for a repository).
type Scope struct {
	Kind Kind
	Name string
}

// NewScope builds a Scope and checks the name. A repository name must be "owner/repo".
func NewScope(kind Kind, name string) (Scope, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || len(name) > MaxScopeNameBytes {
		return Scope{}, fmt.Errorf("ascbyo: bad scope name")
	}
	for i := range len(name) {
		c := name[i]
		ok := c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.'
		if kind == KindRepo && c == '/' {
			ok = true
		}
		if !ok {
			return Scope{}, fmt.Errorf("ascbyo: bad scope name")
		}
	}
	switch kind {
	case KindUser, KindOrg:
		if strings.Contains(name, "/") {
			return Scope{}, fmt.Errorf("ascbyo: bad scope name")
		}
	case KindRepo:
		owner, repo, ok := strings.Cut(name, "/")
		if !ok || owner == "" || repo == "" || strings.Contains(repo, "/") {
			return Scope{}, fmt.Errorf("ascbyo: a repository scope is owner/repo")
		}
	default:
		return Scope{}, fmt.Errorf("ascbyo: bad scope kind")
	}
	return Scope{Kind: kind, Name: name}, nil
}

// Owner is the OwnerRef asc expects.
func (s Scope) Owner() *ascv1.OwnerRef {
	var k ascv1.OwnerKind
	switch s.Kind {
	case KindUser:
		k = ascv1.OwnerKind_OWNER_KIND_USER
	case KindOrg:
		k = ascv1.OwnerKind_OWNER_KIND_ORG
	case KindRepo:
		k = ascv1.OwnerKind_OWNER_KIND_REPO
	}
	return &ascv1.OwnerRef{Kind: k, Name: s.Name}
}

// Repo is the RepoRef of a repository scope.
func (s Scope) Repo() *ascv1.RepoRef { return &ascv1.RepoRef{Name: s.Name} }

// Valid reports whether the scope was built by NewScope.
func (s Scope) Valid() bool {
	t, err := NewScope(s.Kind, s.Name)
	return err == nil && t == s
}

// String is "kind:name".
func (s Scope) String() string { return s.Kind.String() + ":" + s.Name }

// Owns reports whether o names the scope. The comparison ignores case, as asc does.
func (s Scope) Owns(o *ascv1.OwnerRef) bool {
	if o == nil || s.Kind == 0 {
		return false
	}
	return s.Owner().GetKind() == o.GetKind() && strings.EqualFold(s.Name, o.GetName())
}
