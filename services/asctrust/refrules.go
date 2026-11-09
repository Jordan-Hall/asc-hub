// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"fmt"
	"slices"
	"strings"
)

// The `[refs."<pattern>"]` rules of an asc policy protect branches and tags other than trunk:
//
//	[refs."release/*"]
//	land = ["queue", "maintainers", "release-key"]
//	signed = true
//	force = false
//	delete = false
//
// The pattern is a branch name glob or `refs/tags/<glob>` for tags. `land` says who may move a ref the pattern
// matches: the merge queue, maintainers pushing directly, CI and deploy keys (a release key), and whether the queue
// may land changes an agent wrote. asc (asc-core policy.rs) parses and enforces them; this file reads and edits
// the text for the Rules page, and asc checks the result again before it can land.

// The words of a `land` list, in the order the page offers them.
const (
	LandQueue       = "queue"
	LandMaintainers = "maintainers"
	LandReleaseKey  = "release-key"
	LandAgents      = "agents"
)

// LandWords are the words `land` takes.
var LandWords = []string{LandQueue, LandMaintainers, LandReleaseKey, LandAgents}

// MaxRefRules is asc-core MAX_REF_RULES.
const MaxRefRules = 64

const refSectionPrefix = `refs."`

func refSection(pattern string) string { return refSectionPrefix + pattern + `"` }

// RefRule is one `[refs."<pattern>"]` table.
type RefRule struct {
	Pattern string
	// Land is in the order of LandWords, without repeats.
	Land   []string
	Signed bool
	Force  bool
	Delete bool
}

// IsTags reports whether the rule is for tags.
func (r RefRule) IsTags() bool { return strings.HasPrefix(r.Pattern, "refs/tags/") }

// Lets reports whether the rule's `land` list has word.
func (r RefRule) Lets(word string) bool { return slices.Contains(r.Land, word) }

// Name is the pattern without `refs/tags/`, as the page shows it.
func (r RefRule) Name() string { return strings.TrimPrefix(r.Pattern, "refs/tags/") }

// RefRulesOfText reads the `[refs]` rules of a policy text, in file order.
func RefRulesOfText(text string) []RefRule {
	var out []RefRule
	for _, h := range parseToml(text).headers {
		rest, ok := strings.CutPrefix(h.name, refSectionPrefix)
		if !ok || !strings.HasSuffix(rest, `"`) {
			continue
		}
		r := RefRule{Pattern: strings.TrimSuffix(rest, `"`)}
		land, _ := TomlStrings(text, h.name, "land")
		r.Land = orderLand(land)
		r.Signed, _ = TomlBool(text, h.name, "signed")
		r.Force, _ = TomlBool(text, h.name, "force")
		r.Delete, _ = TomlBool(text, h.name, "delete")
		out = append(out, r)
	}
	return out
}

// orderLand keeps the known words of land in the order of LandWords, once each.
func orderLand(land []string) []string {
	out := []string{}
	for _, w := range LandWords {
		if slices.Contains(land, w) {
			out = append(out, w)
		}
	}
	return out
}

// ValidateRefPattern checks a pattern by the rules of asc-core's valid_ref_pattern: a branch name glob or
// `refs/tags/<glob>`, no other `refs/` prefix, no leading or trailing `/`, no `//`, `..`, `@{` or `.lock` end, no
// space, control character, quote or one of `~^:?[\`, and at most MaxGlobLen bytes.
func ValidateRefPattern(pattern string) error {
	name := strings.TrimPrefix(pattern, "refs/tags/")
	switch {
	case name == "":
		return fmt.Errorf("%w: the pattern is empty", ErrBadInput)
	case len(pattern) > MaxGlobLen:
		return fmt.Errorf("%w: the pattern is longer than %d bytes", ErrBadInput, MaxGlobLen)
	case strings.HasPrefix(name, "refs/"):
		return fmt.Errorf("%w: write a branch pattern without refs/heads/, or refs/tags/<pattern> for tags", ErrBadInput)
	case strings.HasPrefix(name, "/"), strings.HasSuffix(name, "/"), strings.Contains(name, "//"):
		return fmt.Errorf("%w: the pattern has an empty part", ErrBadInput)
	case strings.HasSuffix(name, ".lock"), strings.Contains(name, ".."), strings.Contains(name, "@{"):
		return fmt.Errorf("%w: the pattern is not a valid ref name", ErrBadInput)
	}
	for _, c := range name {
		if c < 0x20 || c == 0x7f || c == ' ' || strings.ContainsRune("~^:?[\\\"", c) {
			return fmt.Errorf("%w: the pattern has a character a ref name cannot have", ErrBadInput)
		}
	}
	return nil
}

// ErrRefRuleExists is wrapped by the error for a new or renamed rule whose pattern another rule has.
var ErrRefRuleExists = fmt.Errorf("%w: a rule with this pattern exists", ErrBadInput)

// ErrNoRefRule is wrapped by the error for an edit or removal of a rule the text does not have.
var ErrNoRefRule = fmt.Errorf("%w: no rule with this pattern", ErrBadInput)

// ApplyRefRule returns the text with the rule r set. was is the pattern of the rule being edited, empty for a new
// rule; a changed pattern moves the rule. Only the four keys of the rule are written; a key that is false is
// taken out, so the text says only what the rule allows. Every other byte stays.
func ApplyRefRule(text, was string, r RefRule) (string, error) {
	if err := ValidateRefPattern(r.Pattern); err != nil {
		return "", err
	}
	for _, w := range r.Land {
		if !slices.Contains(LandWords, w) {
			return "", fmt.Errorf("%w: who can land takes queue, maintainers, release-key and agents", ErrBadInput)
		}
	}
	rules := RefRulesOfText(text)
	has := func(p string) bool {
		return slices.ContainsFunc(rules, func(x RefRule) bool { return x.Pattern == p })
	}
	if was != "" && !has(was) {
		return "", fmt.Errorf("%w: %s", ErrNoRefRule, was)
	}
	if r.Pattern != was && has(r.Pattern) {
		return "", fmt.Errorf("%w: %s", ErrRefRuleExists, r.Pattern)
	}
	if was == "" && len(rules) >= MaxRefRules {
		return "", fmt.Errorf("%w: at most %d ref rules", ErrBadInput, MaxRefRules)
	}
	if was != "" && was != r.Pattern {
		text = tomlRemoveSection(text, refSection(was))
	}
	section := refSection(r.Pattern)
	text = TomlSetStrings(text, section, "land", orderLand(r.Land))
	for _, kv := range []struct {
		key string
		on  bool
	}{{"signed", r.Signed}, {"force", r.Force}, {"delete", r.Delete}} {
		if kv.on {
			text = TomlSetBool(text, section, kv.key, true)
		} else {
			text = removeKey(text, section, kv.key)
		}
	}
	return text, nil
}

// RemoveRefRule returns the text without the rule for pattern.
func RemoveRefRule(text, pattern string) (string, error) {
	if !slices.ContainsFunc(RefRulesOfText(text), func(x RefRule) bool { return x.Pattern == pattern }) {
		return "", fmt.Errorf("%w: %s", ErrNoRefRule, pattern)
	}
	return tomlRemoveSection(text, refSection(pattern)), nil
}
