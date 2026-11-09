// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascscope

import "slices"

// Scope says which runs receive a secret or a variable. A run has exactly one scope: Agent runs when an agent
// started it or opened the pull request it runs for, else Releases when it runs for a release or a tag, else
// Checks. A secret or variable is handed to a run only when its scopes include the run's.
type Scope int

const (
	// ScopeChecks is every run that is neither an agent run nor a release run: pushes, pull requests, schedules,
	// manual runs.
	ScopeChecks Scope = 1 << iota
	// ScopeReleases is a run for a release event or a pushed tag.
	ScopeReleases
	// ScopeAgents is a run an agent started, or one for a pull request an agent opened.
	ScopeAgents
)

// DefaultScope is the scope of a new secret or variable, and of one made before scopes existed: agents receive a
// value only once someone ticks Agent runs.
const DefaultScope = ScopeChecks | ScopeReleases

// AllScopes has every scope.
const AllScopes = ScopeChecks | ScopeReleases | ScopeAgents

// Scopes are the scopes in the order the pages show them.
var Scopes = []Scope{ScopeChecks, ScopeAgents, ScopeReleases}

var scopeWords = map[Scope]string{ScopeChecks: "checks", ScopeAgents: "agents", ScopeReleases: "releases"}

// Word is the form value and locale key part of a single scope.
func (s Scope) Word() string { return scopeWords[s] }

// Has reports whether s includes every scope of x.
func (s Scope) Has(x Scope) bool { return x != 0 && s&x == x }

// List is the single scopes s includes, in page order.
func (s Scope) List() []Scope {
	out := []Scope{}
	for _, x := range Scopes {
		if s.Has(x) {
			out = append(out, x)
		}
	}
	return out
}

// ScopeOfWords is the scope of the ticked form values; an unknown word is ignored.
func ScopeOfWords(words []string) Scope {
	var s Scope
	for _, x := range Scopes {
		if slices.Contains(words, x.Word()) {
			s |= x
		}
	}
	return s
}

// Valid reports whether s has at least one scope and no unknown bit.
func (s Scope) Valid() bool { return s != 0 && s&^AllScopes == 0 }

// OfForm reads the scope boxes of a form. sent says the form had the boxes (its `scopes_sent` field); without
// them the scope is 0, which keeps the scopes a value has or gives a new one DefaultScope. With them, ok is false
// when no box is ticked.
func OfForm(words []string, sent bool) (s Scope, ok bool) {
	if !sent {
		return 0, true
	}
	s = ScopeOfWords(words)
	return s, s.Valid()
}
