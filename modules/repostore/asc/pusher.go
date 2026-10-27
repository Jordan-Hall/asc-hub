// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asc

import (
	"context"
	"strconv"
	"strings"
)

// envPusherID is the variable the hub's services put the person who makes a change in (modules/repository,
// EnvPusherID). A git store hands it to its hooks as the environment; asc runs the hub's pre-receive and post-receive
// hooks itself, as the hub's service credential, and names no person to them.
const (
	envPusherID = "GITEA_PUSHER_ID"
	// envPRID names the pull request a merge belongs to (0 for none); the hook uses it to let a merge into a
	// protected branch through when the pull request may be merged.
	envPRID = "GITEA_PR_ID"
)

// PushedRef is one ref change of a write, as the hub's hooks will see it: the full ref name and the git id the ref
// is given ("" for a deletion). The id the ref has before is not known here (the write expects any), so the hooks
// match on the name and the new id.
type PushedRef struct {
	Name string
	New  string
}

// NotePusher is set by the hub (services/ascsync). It records that pusherID is writing refs of the repository called
// repo ("owner/name"), before the write is sent to asc, so that asc's pre-receive and post-receive calls, which come
// back to the hub without a person, can be attributed to that person and checked as that person. It returns the
// function that forgets the record; the writer calls it when the write fails. Nil does nothing.
var NotePusher func(ctx context.Context, repo string, pusherID, pullRequestID int64, refs []PushedRef) (forget func())

// pusherOf reads the person and the pull request from a hook environment: 0 when it names none.
func pusherOf(env []string) (pusher, pullRequest int64) {
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, envPusherID+"="); ok {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
				pusher = n
			}
		}
		if v, ok := strings.CutPrefix(kv, envPRID+"="); ok {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
				pullRequest = n
			}
		}
	}
	return pusher, pullRequest
}

// notePusher records the pusher of a write, when the environment names one, and returns the function that forgets it.
func (s *session) notePusher(repo string, env []string, refs []PushedRef) func() {
	id, pr := pusherOf(env)
	if id == 0 || NotePusher == nil || len(refs) == 0 {
		return func() {}
	}
	if forget := NotePusher(s.ctx, repo, id, pr, refs); forget != nil {
		return forget
	}
	return func() {}
}
