// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package taskout keeps the output of a run of a scheduled task, so the monitor page can follow it while it runs.
package taskout

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	// MaxOutputLines is the most lines one run keeps; older lines are dropped and counted.
	MaxOutputLines = 500
	// maxLineBytes is the longest line kept.
	maxLineBytes = 1000
)

// Buffer is the output of one run of a task: the lines the task wrote with Say, then the error that ended it, if one
// did. Lines are numbered from 0 across the run, so a reader that asks for the lines after n gets each line once even
// when old lines were dropped.
type Buffer struct {
	mu       sync.Mutex
	run      int64
	lines    []string
	dropped  int
	running  bool
	failed   bool
	doer     string
	started  time.Time
	finished time.Time
}

// Output is a view of a run's output from line From on.
type Output struct {
	// Run counts the runs since start; 0 is no run yet.
	Run      int64     `json:"run"`
	Running  bool      `json:"running"`
	Failed   bool      `json:"failed"`
	Doer     string    `json:"doer"`
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished"`
	// Seconds is the time the run took, or has taken so far.
	Seconds float64 `json:"seconds"`
	// From is the number of the first line in Lines, Next the number after the last.
	From  int      `json:"from"`
	Next  int      `json:"next"`
	Lines []string `json:"lines"`
	// Dropped is how many old lines are no longer kept.
	Dropped int `json:"dropped"`
}

// Begin starts a new run by doer and forgets the lines of the last.
func (o *Buffer) Begin(doer string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.run++
	o.lines, o.dropped = o.lines[:0], 0
	o.running, o.failed = true, false
	o.doer = doer
	o.started, o.finished = time.Now(), time.Time{}
}

// End ends the run; an error is the last line.
func (o *Buffer) End(err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.running = false
	o.finished = time.Now()
	if err != nil {
		o.failed = true
		o.addLocked("error: " + err.Error())
	}
}

func (o *Buffer) add(line string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.running {
		o.addLocked(line)
	}
}

func (o *Buffer) addLocked(line string) {
	for _, l := range strings.Split(strings.TrimRight(line, "\n"), "\n") {
		o.lines = append(o.lines, clip(l))
	}
	if over := len(o.lines) - MaxOutputLines; over > 0 {
		o.lines = append(o.lines[:0], o.lines[over:]...)
		o.dropped += over
	}
}

// clip cuts a line to maxLineBytes on a rune boundary and replaces control characters but tab.
func clip(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\t' || (r >= 0x20 && r != 0x7f && !(r >= 0x80 && r < 0xa0)) {
			return r
		}
		return '?'
	}, strings.ToValidUTF8(s, "?"))
	if len(s) <= maxLineBytes {
		return s
	}
	cut := maxLineBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

// View returns the output from line from on.
func (o *Buffer) View(from int) Output {
	o.mu.Lock()
	defer o.mu.Unlock()
	from = max(from, o.dropped)
	next := o.dropped + len(o.lines)
	from = min(from, next)
	v := Output{
		Run: o.run, Running: o.running, Failed: o.failed, Doer: o.doer, Started: o.started, Finished: o.finished,
		From: from, Next: next, Lines: append([]string{}, o.lines[from-o.dropped:]...), Dropped: o.dropped,
	}
	switch {
	case o.running:
		v.Seconds = time.Since(o.started).Seconds()
	case o.run > 0:
		v.Seconds = o.finished.Sub(o.started).Seconds()
	}
	return v
}

type outputKey struct{}

// With returns ctx with o as the output Say writes to.
func With(ctx context.Context, o *Buffer) context.Context {
	return context.WithValue(ctx, outputKey{}, o)
}

// Say writes a line to the output of the task run that ctx belongs to, which the monitor page shows while the task
// runs. Outside a task run it does nothing.
func Say(ctx context.Context, format string, args ...any) {
	if o, ok := ctx.Value(outputKey{}).(*Buffer); ok {
		o.add(fmt.Sprintf(format, args...))
	}
}
