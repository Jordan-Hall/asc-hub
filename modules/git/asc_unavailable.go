// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package git

import "errors"

// ErrBackendUnavailable marks a read that failed because the storage backend of the repository (asc serve) cannot
// be reached right now, for example while it is still starting. It is not a missing object: the caller should try
// again later rather than record or report the repository as broken.
var ErrBackendUnavailable = errors.New("git: the storage backend cannot be reached")

// IsErrBackendUnavailable reports whether err is, or wraps, ErrBackendUnavailable.
func IsErrBackendUnavailable(err error) bool {
	return errors.Is(err, ErrBackendUnavailable)
}
