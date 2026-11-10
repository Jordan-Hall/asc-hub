// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascbackend

import (
	"fmt"
	"testing"

	"forgejo.org/modules/git"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// asc serve that is not up yet answers Unavailable: that is the backend being down, not a missing object, so callers
// such as the stats queue can try again instead of logging the repository as broken.
func TestCallErrMarksUnavailableAsTheBackendBeingDown(t *testing.T) {
	err := callErr("Stat", "abc", status.Error(codes.Unavailable, "connection refused"))
	assert.True(t, git.IsErrBackendUnavailable(err))
	assert.True(t, git.IsErrBackendUnavailable(fmt.Errorf("wrapped: %w", err)))
	assert.False(t, git.IsErrNotExist(err))

	for _, c := range []codes.Code{codes.Internal, codes.NotFound, codes.PermissionDenied, codes.ResourceExhausted} {
		assert.False(t, git.IsErrBackendUnavailable(callErr("Stat", "abc", status.Error(c, "x"))), "%s", c)
	}
	assert.False(t, git.IsErrBackendUnavailable(nil))
}
