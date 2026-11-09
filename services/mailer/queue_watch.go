// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package mailer

import (
	"sync"
	"time"

	"forgejo.org/modules/setting"
)

// maxWatched bounds the distinct write times kept for the mail waiting in the queue. Past it, newer mail is not
// watched; the oldest stays known, which is what the dashboard shows.
const maxWatched = 10000

// queueWatch remembers when each mail waiting in the queue was written (Message.Date), and when mail was last sent
// and last failed, so the site administration can say how long mail waits. It knows only the mail this process
// queued: mail left in a persistent queue by an earlier run is counted by the queue but has no age here.
type queueWatch struct {
	mu         sync.Mutex
	waiting    map[int64]int // write time in Unix nanoseconds -> mail written then and not yet tried
	lastSent   time.Time
	lastFailed time.Time
}

var watch = newQueueWatch()

func newQueueWatch() *queueWatch { return &queueWatch{waiting: map[int64]int{}} }

func (w *queueWatch) pushed(m *Message) {
	if m.Date.IsZero() {
		return
	}
	k := m.Date.UnixNano()
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, ok := w.waiting[k]; !ok && len(w.waiting) >= maxWatched {
		return
	}
	w.waiting[k]++
}

// tried is called once a mail has left the queue: sent, failed, or not queued at all.
func (w *queueWatch) tried(m *Message, err error, now time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err != nil {
		w.lastFailed = now
	} else {
		w.lastSent = now
	}
	if m.Date.IsZero() {
		return
	}
	k := m.Date.UnixNano()
	if n, ok := w.waiting[k]; ok {
		if n <= 1 {
			delete(w.waiting, k)
		} else {
			w.waiting[k] = n - 1
		}
	}
}

func (w *queueWatch) oldest() time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	var first int64
	for k := range w.waiting {
		if first == 0 || k < first {
			first = k
		}
	}
	if first == 0 {
		return time.Time{}
	}
	return time.Unix(0, first)
}

// QueueState is how the outgoing mail queue is doing.
type QueueState struct {
	Enabled    bool      // mail is set up
	Waiting    int       // mail in the queue, including mail an earlier run left there
	Oldest     time.Time // when the oldest watched mail in the queue was written; zero when none is watched
	LastSent   time.Time // zero: none sent since the hub started
	LastFailed time.Time
}

// Failing says the last attempt to send failed: a failure with no success after it.
func (s QueueState) Failing() bool {
	return !s.LastFailed.IsZero() && !s.LastFailed.Before(s.LastSent)
}

// QueueStatus reports the outgoing mail queue.
func QueueStatus() QueueState {
	s := QueueState{Enabled: setting.MailService != nil && mailQueue != nil}
	if !s.Enabled {
		return s
	}
	s.Waiting = max(mailQueue.GetQueueItemNumber(), 0)
	if s.Waiting > 0 {
		s.Oldest = watch.oldest()
	}
	watch.mu.Lock()
	s.LastSent, s.LastFailed = watch.lastSent, watch.lastFailed
	watch.mu.Unlock()
	return s
}
