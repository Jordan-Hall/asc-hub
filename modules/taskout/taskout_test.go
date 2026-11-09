// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package taskout

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSayWritesToTheRunOfTheContext(t *testing.T) {
	var b Buffer
	assert.Equal(t, Output{Lines: []string{}}, b.View(0), "no run yet")

	// Outside a run nothing is written and nothing panics.
	Say(t.Context(), "lost")
	b.add("before the run")

	b.Begin("admin")
	ctx := With(t.Context(), &b)
	Say(ctx, "slice %d", 1)
	Say(ctx, "two\nlines\n")
	v := b.View(0)
	assert.True(t, v.Running)
	assert.Equal(t, int64(1), v.Run)
	assert.Equal(t, "admin", v.Doer)
	assert.Equal(t, []string{"slice 1", "two", "lines"}, v.Lines)
	assert.Equal(t, 3, v.Next)
	assert.GreaterOrEqual(t, v.Seconds, 0.0)

	// A reader asks for the lines after the ones it has.
	Say(ctx, "three")
	v = b.View(3)
	assert.Equal(t, 3, v.From)
	assert.Equal(t, []string{"three"}, v.Lines)
	assert.Empty(t, b.View(99).Lines, "a line number past the end gives nothing")
	assert.Equal(t, 4, b.View(99).From)

	b.End(errors.New("boom"))
	v = b.View(0)
	assert.False(t, v.Running)
	assert.True(t, v.Failed)
	assert.Equal(t, "error: boom", v.Lines[len(v.Lines)-1])
	Say(ctx, "after the end")
	assert.Len(t, b.View(0).Lines, 5, "a finished run takes no more lines")

	// The next run starts empty.
	b.Begin("")
	v = b.View(0)
	assert.Equal(t, int64(2), v.Run)
	assert.Empty(t, v.Lines)
	assert.False(t, v.Failed)
	b.End(nil)
	assert.False(t, b.View(0).Failed)
}

func TestBufferKeepsTheLastLines(t *testing.T) {
	var b Buffer
	b.Begin("")
	ctx := With(t.Context(), &b)
	for i := range MaxOutputLines + 20 {
		Say(ctx, "line %d", i)
	}
	v := b.View(0)
	assert.Equal(t, 20, v.Dropped)
	assert.Equal(t, 20, v.From, "the dropped lines are skipped")
	require.Len(t, v.Lines, MaxOutputLines)
	assert.Equal(t, "line 20", v.Lines[0])
	assert.Equal(t, fmt.Sprintf("line %d", MaxOutputLines+19), v.Lines[len(v.Lines)-1])
	assert.Equal(t, MaxOutputLines+20, v.Next)
}

func TestClipCleansALine(t *testing.T) {
	assert.Equal(t, "a?b\tc", clip("a\x1bb\tc"))
	assert.Equal(t, "bad ? byte", clip("bad \xff byte"))
	long := clip(strings.Repeat("é", maxLineBytes))
	assert.LessOrEqual(t, len(long), maxLineBytes+len("…"))
	assert.True(t, strings.HasSuffix(long, "…"))
	assert.True(t, strings.HasPrefix(long, "é"))
}
