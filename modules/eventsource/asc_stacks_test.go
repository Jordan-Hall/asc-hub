// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package eventsource

import (
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/timeutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscStackReaders(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := db.DefaultContext

	// repo 1 is public; repo 31 is private to user 2. Both have pull requests on.
	got := ascStackReaders(ctx, []int64{2, 4}, []int64{1, 31})
	assert.Equal(t, []int64{1, 31}, got[2], "the owner reads both")
	assert.Equal(t, []int64{1}, got[4], "another user never learns of the private repository")

	got = ascStackReaders(ctx, []int64{4}, []int64{31})
	assert.NotContains(t, got, int64(4), "a user who can read none is left out")
	assert.Empty(t, ascStackReaders(ctx, nil, []int64{1}))
	assert.Empty(t, ascStackReaders(ctx, []int64{2}, nil))
}

func TestSendAscStacks(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := db.DefaultContext
	const at = timeutil.TimeStamp(2000000000)
	_, err := db.GetEngine(ctx).ID(2).NoAutoTime().Cols("updated_unix").Update(&issues_model.Issue{UpdatedUnix: at})
	require.NoError(t, err)

	m := &Manager{messengers: map[int64]*Messenger{}, connection: make(chan struct{}, 1)}
	ch := m.Register(4)
	defer m.Unregister(4, ch)

	m.sendAscStacks(ctx, at-1, at)
	select {
	case ev := <-ch:
		assert.Equal(t, "asc-stacks", ev.Name)
		assert.Equal(t, "[1]", ev.Data)
	default:
		t.Fatal("no asc-stacks event")
	}

	m.sendAscStacks(ctx, at, at+10)
	select {
	case ev := <-ch:
		t.Fatalf("an empty window sends nothing, got %v", ev)
	default:
	}
}
