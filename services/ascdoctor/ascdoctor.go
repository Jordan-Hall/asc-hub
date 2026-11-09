// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package ascdoctor keeps the last machine report of `asc doctor --report` for each person, which the health check
// of Get started shows (board GetStarted): Git found, the shell shim, the daemon, and the checks that warn or fail.
//
// The report is what the command line of the person says about their own machine: the hub shows it only to them and
// trusts it for nothing. It holds no path, no detail text and no secret: the name and level of each check, the
// version of asc and of git, and the operating system.
package ascdoctor

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"time"

	user_model "forgejo.org/models/user"
	"forgejo.org/modules/json"
)

// SettingKey is the user setting that holds the last report as JSON.
const SettingKey = "asc.doctor_report"

// The bounds of a report.
const (
	MaxChecks  = 64
	maxNameLen = 40
	maxTextLen = 80
)

// The levels of a check, as `asc doctor` prints them.
const (
	LevelOk   = "ok"
	LevelInfo = "info"
	LevelWarn = "warn"
	LevelFail = "fail"
)

// The checks the health card names.
const (
	CheckGit    = "real-git"
	CheckShim   = "shim-dir"
	CheckDaemon = "daemon"
)

// Check is one check of the report.
type Check struct {
	Name  string `json:"name"`
	Level string `json:"level"`
}

// Report is the last report of a person's machine.
type Report struct {
	Asc      string    `json:"asc"`
	Git      string    `json:"git,omitempty"`
	OS       string    `json:"os"`
	Arch     string    `json:"arch,omitempty"`
	Checks   []Check   `json:"checks"`
	Reported time.Time `json:"reported"`
}

var (
	nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	// textRe is the version, os and arch text: letters, digits, spaces and . _ - + ( )
	textRe = regexp.MustCompile(`^[A-Za-z0-9 ._+()-]*$`)

	ErrBadReport = errors.New("the report is not valid")
)

func validText(s string, required bool) bool {
	if s == "" {
		return !required
	}
	return len(s) <= maxTextLen && textRe.MatchString(s)
}

// Validate refuses a report that is not of the shape `asc doctor --report` sends.
func (r *Report) Validate() error {
	if r == nil || !validText(r.Asc, true) || !validText(r.OS, true) || !validText(r.Git, false) || !validText(r.Arch, false) {
		return ErrBadReport
	}
	if len(r.Checks) == 0 || len(r.Checks) > MaxChecks {
		return ErrBadReport
	}
	for _, c := range r.Checks {
		if len(c.Name) > maxNameLen || !nameRe.MatchString(c.Name) {
			return ErrBadReport
		}
		switch c.Level {
		case LevelOk, LevelInfo, LevelWarn, LevelFail:
		default:
			return ErrBadReport
		}
	}
	return nil
}

// Level returns the level of the check called name, or "" when the report has none.
func (r *Report) Level(name string) string {
	if r == nil {
		return ""
	}
	for _, c := range r.Checks {
		if c.Name == name {
			return c.Level
		}
	}
	return ""
}

// Problems returns the checks that warn or fail, failures first, in the order of the report.
func (r *Report) Problems() []Check {
	if r == nil {
		return nil
	}
	var fail, warn []Check
	for _, c := range r.Checks {
		switch c.Level {
		case LevelFail:
			fail = append(fail, c)
		case LevelWarn:
			warn = append(warn, c)
		}
	}
	return slices.Concat(fail, warn)
}

// Save validates r and keeps it as the last report of the person, reported now.
func Save(ctx context.Context, userID int64, r *Report, now time.Time) error {
	if err := r.Validate(); err != nil {
		return err
	}
	r.Reported = now.UTC().Truncate(time.Second)
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return user_model.SetUserSetting(ctx, userID, SettingKey, string(b))
}

// Load returns the last report of the person, or nil when there is none or it can no longer be read.
func Load(ctx context.Context, userID int64) (*Report, error) {
	v, err := user_model.GetUserSetting(ctx, userID, SettingKey)
	if err != nil || v == "" {
		return nil, err
	}
	r := &Report{}
	if json.Unmarshal([]byte(v), r) != nil || r.Validate() != nil {
		return nil, nil
	}
	return r, nil
}
