// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package auth

import "context"

// ProbeClaim is one fact a source gave back about a person during a test sign-in.
type ProbeClaim struct {
	Name  string
	Value string
}

// Prober is a password source that can check a name and password without signing anyone in, creating an account
// or changing one. The admin's Test sign-in uses it to show what the source says about a person.
type Prober interface {
	Probe(ctx context.Context, login, password string) ([]ProbeClaim, error)
}

// The session keys of an OAuth test sign-in: the source an admin is testing, and what came back from it
// (a ProbeResult as JSON).
const (
	ProbeOAuthSessionKey  = "asc_oauth_test"
	ProbeResultSessionKey = "asc_oauth_test_result"
)

// ProbeResult is what an OAuth test sign-in brought back, kept in the session until the admin page shows it.
type ProbeResult struct {
	SourceID int64
	Claims   []ProbeClaim
	Error    string
}
