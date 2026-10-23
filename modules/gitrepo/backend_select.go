// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package gitrepo

import (
	"errors"
	"fmt"
	"slices"

	"forgejo.org/modules/git"
	"forgejo.org/modules/setting"
)

// StorageBackend is the value of repository.storage_backend: where a repository's objects and refs live.
type StorageBackend string

const (
	// StorageBackendGit serves the repository from its bare git directory (the default).
	StorageBackendGit StorageBackend = "git"
	// StorageBackendDual serves from the bare git directory; K11 adds a sampled shadow read against asc.
	StorageBackendDual StorageBackend = "dual"
	// StorageBackendAsc is a repository whose truth is an asc server.
	StorageBackendAsc StorageBackend = "asc"
)

// ascBackendName is the name under which K10 registers the asc-backed git.Backend.
const ascBackendName = "asc"

var (
	// ErrUnknownStorageBackend is returned for a storage_backend value that is not git, dual or asc.
	// A repository with such a value is never opened: selection fails closed.
	ErrUnknownStorageBackend = errors.New("gitrepo: unknown repository storage backend")
	// ErrAscDisabled is returned for a dual or asc repository while [asc] ENABLED is false.
	// Move such repositories back to git before turning asc off.
	ErrAscDisabled = errors.New("gitrepo: repository storage backend needs [asc] ENABLED")
)

// maxStorageBackendEcho caps how much of a bad stored value an error repeats.
const maxStorageBackendEcho = 16

// ParseStorageBackend accepts exactly git, dual and asc. The empty string is rejected: the model
// layer turns a missing value into git before a repository reaches selection.
func ParseStorageBackend(s string) (StorageBackend, error) {
	switch b := StorageBackend(s); b {
	case StorageBackendGit, StorageBackendDual, StorageBackendAsc:
		return b, nil
	}
	if len(s) > maxStorageBackendEcho {
		s = s[:maxStorageBackendEcho] + "..."
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownStorageBackend, s)
}

// storageBackendHolder is implemented by repository models that carry the storage_backend column.
type storageBackendHolder interface {
	GetStorageBackend() string
}

// StorageBackendOf returns the storage backend of repo. A Repository that does not carry the
// column (NamedRepository, used by migration and restore) is a plain git repository.
func StorageBackendOf(repo Repository) (StorageBackend, error) {
	holder, ok := repo.(storageBackendHolder)
	if !ok {
		return StorageBackendGit, nil
	}
	return ParseStorageBackend(holder.GetStorageBackend())
}

// selectBackend returns the name of the git.Backend that serves repo. This is the one place
// that maps storage_backend to a backend; everything opens repositories through OpenRepository.
//
//   - git, dual: gitcli on the repository directory. dual's shadow comparison (K11) wraps this.
//   - asc: the "asc" backend (K10) when it is linked in. Until then the repository directory is
//     the git bridge (Stage 2, "forgejo_reads=git") and gitcli reads it. Once "asc" is linked in,
//     an asc repository never falls back to git: an asc failure is an error, not a stale read.
//
// dual and asc need [asc] ENABLED; otherwise the open fails.
func selectBackend(repo Repository) (string, error) {
	sb, err := StorageBackendOf(repo)
	if err != nil {
		return "", err
	}
	switch sb {
	case StorageBackendGit:
		return git.DefaultBackendName, nil
	case StorageBackendDual, StorageBackendAsc:
		if !setting.Asc.Enabled {
			return "", ErrAscDisabled
		}
		if sb == StorageBackendAsc && slices.Contains(git.Backends(), ascBackendName) {
			return ascBackendName, nil
		}
		return git.DefaultBackendName, nil
	}
	return "", ErrUnknownStorageBackend // unreachable: ParseStorageBackend admits only the cases above
}
