// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"errors"
	"fmt"
	"testing"

	repostore_asc "forgejo.org/modules/repostore/asc"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
)

func TestAscRefusalFlash(t *testing.T) {
	refused := &repostore_asc.Error{Op: "UpdateRefs", Code: codes.FailedPrecondition, Msg: "POLICY_DENIED: 0 of 1 approvals <b>"}
	text, ok := ascRefusalFlash(fmt.Errorf("merge: %w", refused))
	assert.True(t, ok)
	assert.Equal(t, "POLICY_DENIED: 0 of 1 approvals &lt;b&gt;", text, "asc's sentence, escaped")

	_, ok = ascRefusalFlash(&repostore_asc.Error{Op: "UpdateRefs", Code: codes.Unavailable, Msg: "down"})
	assert.False(t, ok, "an outage is a server error")
	_, ok = ascRefusalFlash(errors.New("other"))
	assert.False(t, ok)
}
