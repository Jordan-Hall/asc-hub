// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package quota

import (
	"slices"

	"forgejo.org/modules/setting"
)

// Allowance is the limit that applies to a subject and the bytes counted against it.
type Allowance struct {
	Limit int64 // -1: no limit
	Used  int64
}

// covers reports whether r applies to subject: it names the subject or one of the subjects that contain it.
func (r Rule) covers(subject LimitSubject) bool {
	for s := subject; s != LimitSubjectNone; s = subjectToParent[s] {
		if slices.Contains(r.Subjects, s) {
			return true
		}
	}
	return false
}

// allowanceOf is the tightest rule of g that covers subject: the one with the least room left.
func (g *Group) allowanceOf(used Used, subject LimitSubject) (Allowance, bool) {
	var best Allowance
	found := false
	for _, r := range g.Rules {
		if !r.covers(subject) {
			continue
		}
		a := Allowance{Limit: r.Limit, Used: r.Sum(used)}
		if !found || a.tighter(best) {
			best, found = a, true
		}
	}
	return best, found
}

// room is how much more fits, or -1 for no limit.
func (a Allowance) room() int64 {
	if a.Limit < 0 {
		return -1
	}
	return max(a.Limit-a.Used, 0)
}

// tighter reports whether a leaves less room than b.
func (a Allowance) tighter(b Allowance) bool {
	ra, rb := a.room(), b.room()
	switch {
	case ra < 0:
		return false
	case rb < 0:
		return true
	default:
		return ra < rb
	}
}

// AllowanceFor is the allowance a user with the groups gl has for subject, the same way the groups are
// evaluated: a group allows what all its covering rules allow and the user may use the group that leaves the most
// room. Without groups the configured default applies. ok is false when no rule covers the subject.
func (gl *GroupList) AllowanceFor(used Used, subject LimitSubject) (Allowance, bool) {
	groups := GroupList{}
	if gl != nil {
		groups = *gl
	}
	if len(groups) == 0 {
		groups = GroupList{{Rules: []Rule{{Limit: setting.Quota.Default.Total, Subjects: LimitSubjects{LimitSubjectSizeAll}}}}}
	}
	var best Allowance
	found := false
	for _, g := range groups {
		a, ok := g.allowanceOf(used, subject)
		if !ok {
			continue
		}
		if !found || best.tighter(a) {
			best, found = a, true
		}
	}
	return best, found
}
