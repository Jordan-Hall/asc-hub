// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Branch protection of an asc repository is the asc policy.
//
// The rules of trunk live in `.asc/policy.toml`, reviewed like code and enforced by asc. The hub's branch protection
// settings and API are a second way to read and write the same file for an asc repository: reading derives the
// protection from the policy in force, writing edits the policy text and proposes it as a change under review.
// Nothing is stored in the hub's protected_branch table for an asc repository.
//
// The mapping:
//
//	required_approvals          [trunk] require_approvals
//	enable_status_check + contexts   [trunk] require_checks (an empty list when the check is off)
//	require_signed_commits      [signing] require_signed
//	enable_force_push           [trunk] allow_force
//	enable_push = false         [merge] queue = true (trunk takes landings only through the queue)
//	protected_file_patterns     one [paths."<glob>"] write = ["role:maintainer"] rule per pattern
//
// A setting with no asc equivalent (a whitelist of pushers, mergers or approvers, blocking on rejected reviews or
// an outdated branch, dismissing stale approvals, unprotected patterns, a rule for a branch other than trunk) is
// refused with ErrUnsupportedProtection and a sentence naming it. It is never dropped silently.

// ErrUnsupportedProtection is wrapped by the error for a protection setting asc cannot express.
var ErrUnsupportedProtection = fmt.Errorf("%w: unsupported protection setting", ErrBadInput)

// BranchRule is the part of a branch protection rule that the asc policy expresses.
type BranchRule struct {
	Branch            string
	RequiredApprovals int64
	StatusChecks      []string
	RequireSigned     bool
	AllowForcePush    bool
	QueueOnly         bool
	ProtectedPaths    []string
}

// maintainerPathRule is the writer list of a protected path pattern: only a maintainer may write it.
var maintainerPathRule = []string{"role:maintainer"}

const pathSectionPrefix = `paths."`

func pathSection(glob string) string { return pathSectionPrefix + glob + `"` }

// ProtectionOfText reads the protection of trunk from the text of the policy. The branch is the default branch of
// the repository, the only one asc protects.
func ProtectionOfText(text, branch string) BranchRule {
	p := BranchRule{Branch: branch}
	if n, ok := TomlInt(text, "trunk", "require_approvals"); ok {
		p.RequiredApprovals = int64(n)
	}
	p.StatusChecks, _ = TomlStrings(text, "trunk", "require_checks")
	if v, ok := TomlBool(text, "signing", "require_signed"); ok {
		p.RequireSigned = v
	} else if v, ok := TomlBool(text, "trunk", "require_signed"); ok {
		p.RequireSigned = v
	}
	p.AllowForcePush, _ = TomlBool(text, "trunk", "allow_force")
	p.QueueOnly, _ = TomlBool(text, "merge", "queue")
	for _, glob := range pathRuleGlobs(text) {
		if w, ok := TomlStrings(text, pathSection(glob), "write"); ok && slices.Equal(w, maintainerPathRule) {
			p.ProtectedPaths = append(p.ProtectedPaths, glob)
		}
	}
	return p
}

// pathRuleGlobs lists the globs of the `[paths."<glob>"]` sections, in file order.
func pathRuleGlobs(text string) []string {
	var out []string
	for _, h := range parseToml(text).headers {
		if rest, ok := strings.CutPrefix(h.name, pathSectionPrefix); ok && strings.HasSuffix(rest, `"`) {
			out = append(out, strings.TrimSuffix(rest, `"`))
		}
	}
	return out
}

// ApplyProtection returns the text of the policy with the protection set. Only the keys of the mapping are touched;
// every other byte stays, comments and order included. Path rules this mapping made (a maintainer-only write list)
// that are not in p are removed; a path rule with another writer list is never touched.
func ApplyProtection(text string, p BranchRule) (string, error) {
	if p.RequiredApprovals < 0 || p.RequiredApprovals > 64 {
		return "", fmt.Errorf("%w: required approvals is 0 to 64", ErrBadInput)
	}
	if len(p.ProtectedPaths) > MaxGlobs {
		return "", fmt.Errorf("%w: at most %d protected patterns", ErrBadInput, MaxGlobs)
	}
	for _, g := range p.ProtectedPaths {
		if err := ValidatePathGlob(g); err != nil {
			return "", fmt.Errorf("%w: protected pattern %q: %v", ErrBadInput, g, err)
		}
	}
	text = setLine(text, "trunk", "require_approvals", strconv.FormatInt(p.RequiredApprovals, 10))
	checks := p.StatusChecks
	if checks == nil {
		checks = []string{}
	}
	text = TomlSetStrings(text, "trunk", "require_checks", checks)
	text = TomlSetBool(text, "signing", "require_signed", p.RequireSigned)
	text = TomlSetBool(text, "trunk", "allow_force", p.AllowForcePush)
	text = TomlSetBool(text, "merge", "queue", p.QueueOnly)
	want := map[string]bool{}
	for _, g := range p.ProtectedPaths {
		want[g] = true
		if cur, ok := TomlStrings(text, pathSection(g), "write"); ok && !slices.Equal(cur, maintainerPathRule) {
			return "", fmt.Errorf("%w: %q already has a path rule with other writers; edit it in the policy file", ErrUnsupportedProtection, g)
		}
		text = TomlSetStrings(text, pathSection(g), "write", maintainerPathRule)
	}
	for _, g := range pathRuleGlobs(text) {
		if want[g] {
			continue
		}
		if cur, ok := TomlStrings(text, pathSection(g), "write"); ok && slices.Equal(cur, maintainerPathRule) {
			text = tomlRemoveSection(text, pathSection(g))
		}
	}
	return text, nil
}

// tomlRemoveSection deletes a section with its keys, up to the next header.
func tomlRemoveSection(text, section string) string {
	d := parseToml(text)
	section = normalizeSection(section)
	for i, h := range d.headers {
		if h.name != section {
			continue
		}
		end := len(d.lines)
		if i+1 < len(d.headers) {
			end = d.headers[i+1].line
		}
		lines := append(append([]string{}, d.lines[:h.line]...), d.lines[end:]...)
		return strings.Join(lines, "\n")
	}
	return text
}

// ProtectionRequest is what an API call asks for, in the words of the host's protection rule.
type ProtectionRequest struct {
	RuleName                  string
	EnablePush                bool
	PushWhitelist             bool // a push whitelist of people, teams or deploy keys is set
	MergeWhitelist            bool
	ApprovalsWhitelist        bool
	EnableStatusCheck         bool
	StatusCheckContexts       []string
	RequiredApprovals         int64
	BlockOnRejectedReviews    bool
	BlockOnOfficialReviews    bool
	BlockOnOutdatedBranch     bool
	DismissStaleApprovals     bool
	RequireSignedCommits      bool
	ProtectedFilePatterns     string
	UnprotectedFilePatterns   string
	EnableForcePush           bool
	ForcePushWhitelistPresent bool
}

// ProtectionFromRequest checks a request against what asc can express and returns the protection. defaultBranch is
// the trunk of the repository. Every setting the policy has no key for is named in the error.
func ProtectionFromRequest(r ProtectionRequest, defaultBranch string) (BranchRule, error) {
	var no []string
	if r.RuleName != defaultBranch {
		no = append(no, fmt.Sprintf("a rule for %q (asc protects only the default branch %q)", r.RuleName, defaultBranch))
	}
	for name, set := range map[string]bool{
		"push whitelists":                   r.PushWhitelist,
		"merge whitelists":                  r.MergeWhitelist,
		"approvals whitelists":              r.ApprovalsWhitelist,
		"force push whitelists":             r.ForcePushWhitelistPresent,
		"block on rejected reviews":         r.BlockOnRejectedReviews,
		"block on official review requests": r.BlockOnOfficialReviews,
		"block on outdated branch":          r.BlockOnOutdatedBranch,
		"dismiss stale approvals":           r.DismissStaleApprovals,
		"unprotected file patterns":         strings.TrimSpace(r.UnprotectedFilePatterns) != "",
		"status checks without any context": r.EnableStatusCheck && len(r.StatusCheckContexts) == 0,
	} {
		if set {
			no = append(no, name)
		}
	}
	if len(no) > 0 {
		slices.Sort(no)
		return BranchRule{}, fmt.Errorf("%w: asc has no equivalent for %s", ErrUnsupportedProtection, strings.Join(no, ", "))
	}
	p := BranchRule{
		Branch: defaultBranch, RequiredApprovals: r.RequiredApprovals, RequireSigned: r.RequireSignedCommits,
		AllowForcePush: r.EnableForcePush, QueueOnly: !r.EnablePush,
	}
	if r.EnableStatusCheck {
		p.StatusChecks = r.StatusCheckContexts
	}
	for _, g := range strings.Split(r.ProtectedFilePatterns, ";") {
		if g = strings.TrimSpace(g); g != "" {
			p.ProtectedPaths = append(p.ProtectedPaths, g)
		}
	}
	return p, nil
}
