// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package migration

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMessengerTravelsInTheContext(t *testing.T) {
	assert.Nil(t, MessengerOf(t.Context()), "no messenger was put in")
	assert.Equal(t, t.Context(), WithMessenger(t.Context(), nil), "a nil messenger changes nothing")

	var got []any
	ctx := WithMessenger(t.Context(), func(key string, args ...any) { got = append(append(got, key), args...) })
	m := MessengerOf(ctx)
	if assert.NotNil(t, m) {
		m("repo.migrate.asc_import_progress", int64(1), int64(2))
	}
	assert.Equal(t, []any{"repo.migrate.asc_import_progress", int64(1), int64(2)}, got)
}
