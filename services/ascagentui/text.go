// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package ascagentui holds what the agents and workflows pages share: the checks that a value from a form or
// from asc is fit to be shown or sent, the budget arithmetic, the labels of the run states and the per-user
// "agent features" switch.
//
// Everything that comes back from asc about a run (task text, model output, tool output, error text) is
// untrusted. The functions here only make it safe to draw as text: the templates still escape it, and the
// script that updates a live page only ever assigns it to textContent.
package ascagentui

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Limits of the wire contract (proto/asc/v1/agents.proto and workflows.proto).
const (
	MaxTaskBytes     = 65536
	MaxTitleBytes    = 200
	MaxInputBytes    = 16384
	MaxReasonBytes   = 256
	MaxCommentBytes  = 1024
	MaxTokenName     = 64
	MaxGlobs         = 16
	MaxGlobBytes     = 256
	MaxModelBytes    = 128
	MaxInputValue    = 4096
	MaxWorkflowName  = 64
	MaxWorkflowParam = 32
	MaxEventText     = 8192
	MaxLineText      = 512
)

var (
	agentNameRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
	tokenIDRe      = regexp.MustCompile(`^at_[A-Za-z0-9]{20}$`)
	keyIDRe        = regexp.MustCompile(`^ak_[A-Za-z0-9]{20}$`)
	modelRe        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@-]{0,127}$`)
	workflowRe     = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	inputNameRe    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,63}$`)
	stepIDRe       = regexp.MustCompile(`^[a-z0-9_-]{1,64}$`)
	failureCodeRe  = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
	principalNameR = regexp.MustCompile(`^[A-Za-z0-9._@:-]{1,80}$`)
)

// ValidAgentName reports whether s is an agent name: 1 to 40 characters of a-z, 0-9 and "-", not starting with "-".
func ValidAgentName(s string) bool { return agentNameRe.MatchString(s) }

// ValidTokenID reports whether s has the form of an agent token id.
func ValidTokenID(s string) bool { return tokenIDRe.MatchString(s) }

// ValidKeyID reports whether s has the form of an AI key id.
func ValidKeyID(s string) bool { return keyIDRe.MatchString(s) }

// ValidModel reports whether s can be a model identifier. Providers use letters, digits and ". _ : / @ -".
func ValidModel(s string) bool { return modelRe.MatchString(s) }

// ValidWorkflowName reports whether s is the stem of a workflow file. "runs" is the history page under the same
// path, so a workflow of that name cannot be linked to and is refused.
func ValidWorkflowName(s string) bool { return s != "runs" && workflowRe.MatchString(s) }

// ValidInputName reports whether s can be the name of a workflow input.
func ValidInputName(s string) bool { return inputNameRe.MatchString(s) }

// ValidStepID reports whether s is a workflow step id.
func ValidStepID(s string) bool { return stepIDRe.MatchString(s) }

// SafeCode returns s when it is a short error-code slug, else "". asc sends codes such as BUDGET_EXCEEDED.
func SafeCode(s string) string {
	if failureCodeRe.MatchString(s) {
		return s
	}
	return ""
}

// SafeName returns s when it is a short name of a person, an agent or another principal, else "".
func SafeName(s string) string {
	if principalNameR.MatchString(s) {
		return s
	}
	return ""
}

// isHidden reports whether r must never reach a page: control characters, the bidirectional overrides and
// isolates, zero-width and joiner characters, the byte order mark and the replacement character.
func isHidden(r rune) bool {
	switch {
	case r == '\n' || r == '\t':
		return false
	case unicode.IsControl(r):
		return true
	case r == 0x061C, r == 0x200B, r == 0x200C, r == 0x200D, r == 0x200E, r == 0x200F, r == 0x2060, r == 0xFEFF, r == utf8.RuneError:
		return true
	case r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069:
		return true
	}
	return false
}

// CleanText makes untrusted text fit to draw: invalid UTF-8 and hidden characters are dropped, a carriage return
// becomes a line feed, and the result is cut at max bytes on a character boundary with a final "…" when it was cut.
func CleanText(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "")
	}
	var b strings.Builder
	b.Grow(min(len(s), max+4))
	cut := false
	for _, r := range s {
		if r == '\r' {
			r = '\n'
		}
		if isHidden(r) {
			continue
		}
		if b.Len()+utf8.RuneLen(r) > max {
			cut = true
			break
		}
		b.WriteRune(r)
	}
	out := b.String()
	if cut {
		out = strings.TrimRight(out, "\n\t ") + "…"
	}
	return out
}

// CleanLine is CleanText for a one-line value: line feeds and tabs become spaces.
func CleanLine(s string, max int) string {
	out := CleanText(s, max)
	if strings.ContainsAny(out, "\n\t") {
		out = strings.Map(func(r rune) rune {
			if r == '\n' || r == '\t' {
				return ' '
			}
			return r
		}, out)
	}
	return strings.TrimSpace(out)
}

// CheckFreeText validates text a person typed (a task, a steering message, a comment): non-empty after trimming,
// at most max bytes, valid UTF-8, no NUL and no other control character except line feed, carriage return and tab.
// It returns the trimmed text.
func CheckFreeText(s string, max int) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > max || !utf8.ValidString(s) {
		return "", false
	}
	for _, r := range s {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return "", false
		}
	}
	return s, true
}

// CheckLine validates a one-line value a person typed: at most max bytes, valid UTF-8, no control character. The
// empty string passes when allowEmpty is set. It returns the trimmed value.
func CheckLine(s string, max int, allowEmpty bool) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", allowEmpty
	}
	if len(s) > max || !utf8.ValidString(s) {
		return "", false
	}
	for _, r := range s {
		if unicode.IsControl(r) || isHidden(r) {
			return "", false
		}
	}
	return s, true
}

// ParseGlobs splits the paths field (one glob per line or separated by commas) into checked globs. A glob is a
// relative path pattern of 1 to 256 bytes with no NUL or control character, no backslash, no leading "/" and no
// ".." segment. At most 16 are accepted, repeats are dropped. ok is false when any entry is refused.
func ParseGlobs(raw string) (globs []string, ok bool) {
	if len(raw) > MaxGlobs*(MaxGlobBytes+2) {
		return nil, false
	}
	seen := map[string]bool{}
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == '\n' || r == '\r' || r == ',' }) {
		g := strings.TrimSpace(part)
		if g == "" {
			continue
		}
		if !ValidGlob(g) {
			return nil, false
		}
		if seen[g] {
			continue
		}
		seen[g] = true
		globs = append(globs, g)
		if len(globs) > MaxGlobs {
			return nil, false
		}
	}
	return globs, true
}

// ValidGlob reports whether g is an acceptable path glob.
func ValidGlob(g string) bool {
	if g == "" || len(g) > MaxGlobBytes || !utf8.ValidString(g) || strings.HasPrefix(g, "/") || strings.Contains(g, "\\") {
		return false
	}
	for _, r := range g {
		if unicode.IsControl(r) || isHidden(r) {
			return false
		}
	}
	for _, seg := range strings.Split(g, "/") {
		if seg == ".." {
			return false
		}
	}
	return true
}
