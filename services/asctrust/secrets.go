// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"google.golang.org/grpc/status"
)

// The secret scanning surfaces (docs/BUILDOUT.md section 4): the rules, the findings and how a person works them
// off, and the text of a refused push. A finding never carries the value of a secret; what asc sends as a
// preview is cut again here to the two leading characters, so a server that sent more would still show no more.

const (
	maxNoteBytes     = 1024
	maxRuleIDLen     = 64
	maxFindingsPage  = 200
	previewKeepRunes = 2
)

var ruleIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// SecretStates lists the states in the order the page shows them, with the form value and the label.
var SecretStates = []struct {
	Key   string
	Label string
	State ascv1.SecretFindingState
}{
	{"open", "Open", ascv1.SecretFindingState_SECRET_FINDING_STATE_OPEN},
	{"revoked", "Revoked", ascv1.SecretFindingState_SECRET_FINDING_STATE_REVOKED},
	{"false_positive", "False positive", ascv1.SecretFindingState_SECRET_FINDING_STATE_FALSE_POSITIVE},
	{"accepted_risk", "Accepted risk", ascv1.SecretFindingState_SECRET_FINDING_STATE_ACCEPTED_RISK},
}

// SecretStateOf returns the state of a form value ("open", "revoked", "false_positive" or "accepted_risk").
func SecretStateOf(key string) (ascv1.SecretFindingState, bool) {
	for _, s := range SecretStates {
		if s.Key == key {
			return s.State, true
		}
	}
	return ascv1.SecretFindingState_SECRET_FINDING_STATE_UNSPECIFIED, false
}

func secretStateLabel(st ascv1.SecretFindingState) (key, label string) {
	for _, s := range SecretStates {
		if s.State == st {
			return s.Key, s.Label
		}
	}
	return "unknown", "Unknown"
}

// CheckNote validates the note of a finding update: valid UTF-8, at most 1,024 bytes, no control character but
// a line break.
func CheckNote(note string) (string, error) {
	note = strings.TrimSpace(note)
	if len(note) > maxNoteBytes || !utf8.ValidString(note) {
		return "", fmt.Errorf("%w: the note is longer than %d bytes", ErrBadInput, maxNoteBytes)
	}
	for _, r := range note {
		if (r < 0x20 && r != '\n') || r == 0x7f {
			return "", fmt.Errorf("%w: the note holds a control character", ErrBadInput)
		}
	}
	return note, nil
}

// SeverityOf returns the label and the tone of a severity.
func SeverityOf(s ascv1.SecretSeverity) (label, tone string) {
	switch s {
	case ascv1.SecretSeverity_SECRET_SEVERITY_LOW:
		return "Low", "ro"
	case ascv1.SecretSeverity_SECRET_SEVERITY_MEDIUM:
		return "Medium", "wn"
	case ascv1.SecretSeverity_SECRET_SEVERITY_HIGH:
		return "High", "no"
	case ascv1.SecretSeverity_SECRET_SEVERITY_CRITICAL:
		return "Critical", "no"
	}
	return "Unknown", "ro"
}

// RuleRow is one scanning rule.
type RuleRow struct {
	ID          string
	Name        string
	Description string
	Severity    string
	Tone        string
	Builtin     bool
	Enabled     bool
	Detector    string
}

// RulesView is the rules page content.
type RulesView struct {
	Rules       []RuleRow
	BlockOnPush bool
	AllowPaths  []string
}

// SecretRulesOf checks and cleans an answer of SecretScan.ListRules.
func SecretRulesOf(resp *ascv1.ListSecretRulesResponse) (*RulesView, error) {
	if len(resp.GetRules()) > MaxListItems || len(resp.GetAllowPaths()) > MaxGlobs*4 {
		return nil, fmt.Errorf("%w: too many rules", ErrBadAnswer)
	}
	v := &RulesView{BlockOnPush: resp.GetBlockOnPush()}
	for _, r := range resp.GetRules() {
		if len(r.GetId()) > maxRuleIDLen || !ruleIDPattern.MatchString(strings.TrimPrefix(r.GetId(), "custom.")) {
			return nil, fmt.Errorf("%w: rule id", ErrBadAnswer)
		}
		sev, tone := SeverityOf(r.GetSeverity())
		v.Rules = append(v.Rules, RuleRow{
			ID: r.GetId(), Name: CleanText(r.GetName(), MaxTextBytes), Description: CleanText(r.GetDescription(), MaxTextBytes),
			Severity: sev, Tone: tone, Builtin: r.GetBuiltin(), Enabled: r.GetEnabled(), Detector: CleanText(r.GetDetector(), 16),
		})
	}
	for _, p := range resp.GetAllowPaths() {
		v.AllowPaths = append(v.AllowPaths, CleanText(p, MaxGlobLen))
	}
	return v, nil
}

// FindingRow is one finding, cleaned for drawing. There is no field for a value.
type FindingRow struct {
	ID         uint64
	Rule       string
	Severity   string
	Tone       string
	StateKey   string
	State      string
	Path       string
	Line       uint32
	Location   string
	Preview    string
	Change     string
	FirstSeen  time.Time
	LastSeen   time.Time
	ResolvedBy string
	ResolvedAt time.Time
	Note       string
}

// StateCount is a count of findings in one state.
type StateCount struct {
	Key   string
	Label string
	Count uint32
}

// FindingsPage is one page of findings.
type FindingsPage struct {
	Rows   []FindingRow
	Next   string
	Counts []StateCount
}

// MaskPreview keeps at most the first two characters of a preview and ends it with ***, whatever the server sent.
func MaskPreview(p string) string {
	p = strings.ToValidUTF8(p, "")
	if i := strings.IndexByte(p, '*'); i >= 0 {
		p = p[:i]
	}
	out := make([]rune, 0, previewKeepRunes)
	for _, r := range p {
		if len(out) == previewKeepRunes {
			break
		}
		if r < 0x20 || r == 0x7f || isFormatRune(r) {
			continue
		}
		out = append(out, r)
	}
	return string(out) + "***"
}

func ts(sec int64) time.Time {
	if sec <= 0 || sec > 32503680000 {
		return time.Time{}
	}
	return time.Unix(sec, 0).UTC()
}

// changeText is the short form (12 letters) of a change id, as the pages show it.
func changeText(b []byte) string {
	t := ChangeText(b)
	if t == "" {
		return ""
	}
	return t[:12]
}

// FindingsPageOf checks and cleans an answer of SecretScan.ListFindings.
func FindingsPageOf(resp *ascv1.ListSecretFindingsResponse) (*FindingsPage, error) {
	if len(resp.GetFindings()) > maxFindingsPage {
		return nil, fmt.Errorf("%w: too many findings", ErrBadAnswer)
	}
	if tok := resp.GetNextPageToken(); tok != "" && !tokenPattern.MatchString(tok) {
		return nil, fmt.Errorf("%w: page token", ErrBadAnswer)
	}
	page := &FindingsPage{Next: resp.GetNextPageToken()}
	for _, f := range resp.GetFindings() {
		row, err := findingRow(f)
		if err != nil {
			return nil, err
		}
		page.Rows = append(page.Rows, row)
	}
	for _, s := range SecretStates {
		page.Counts = append(page.Counts, StateCount{Key: s.Key, Label: s.Label, Count: resp.GetCountsByState()[s.Key]})
	}
	return page, nil
}

func findingRow(f *ascv1.SecretFinding) (FindingRow, error) {
	if f.GetId() == 0 || !ruleIDPattern.MatchString(strings.TrimPrefix(f.GetRuleId(), "custom.")) || len(f.GetRuleId()) > maxRuleIDLen {
		return FindingRow{}, fmt.Errorf("%w: finding", ErrBadAnswer)
	}
	sev, tone := SeverityOf(f.GetSeverity())
	key, label := secretStateLabel(f.GetState())
	path := CleanText(string(f.GetPath()), 1024)
	loc := path
	if f.GetLine() > 0 {
		loc = fmt.Sprintf("%s:%d", path, f.GetLine())
	}
	return FindingRow{
		ID: f.GetId(), Rule: f.GetRuleId(), Severity: sev, Tone: tone, StateKey: key, State: label,
		Path: path, Line: f.GetLine(), Location: loc, Preview: MaskPreview(f.GetPreview()), Change: changeText(f.GetChange()),
		FirstSeen: ts(f.GetFirstSeen()), LastSeen: ts(f.GetLastSeen()), ResolvedBy: CleanText(f.GetResolvedBy(), 128),
		ResolvedAt: ts(f.GetResolvedAt()), Note: CleanText(f.GetNote(), maxNoteBytes),
	}, nil
}

// FindingOf checks and cleans the finding of an UpdateFinding answer.
func FindingOf(f *ascv1.SecretFinding) (FindingRow, error) { return findingRow(f) }

// SecretHit is one place a refused push matched: a rule and where. It has no value.
type SecretHit struct {
	Path string
	Line int
	Rule string
}

// SecretHitsOf reads ErrorDetail.paths of a SECRET_DETECTED refusal: "<path>:<line> <rule id>" for each finding.
// An entry that does not have that form is dropped.
func SecretHitsOf(paths []string) []SecretHit {
	var out []SecretHit
	for _, p := range paths {
		if len(out) >= 50 {
			break
		}
		loc, rule, ok := cutLast(p, ' ')
		if !ok || !ruleIDPattern.MatchString(strings.TrimPrefix(rule, "custom.")) || len(rule) > maxRuleIDLen {
			continue
		}
		file, lineText, ok := cutLast(loc, ':')
		if !ok {
			continue
		}
		line := 0
		for _, c := range lineText {
			if c < '0' || c > '9' || line > 10_000_000 {
				line = -1
				break
			}
			line = line*10 + int(c-'0')
		}
		file = CleanText(file, 1024)
		if line < 0 || lineText == "" || file == "" {
			continue
		}
		out = append(out, SecretHit{Path: file, Line: line, Rule: rule})
	}
	return out
}

func cutLast(s string, sep byte) (before, after string, ok bool) {
	i := strings.LastIndexByte(s, sep)
	if i < 0 {
		return s, "", false
	}
	return s[:i], s[i+1:], true
}

// SecretBlockText is the text a pusher sees when err is a SECRET_DETECTED refusal, or "" when it is not. It names
// the rules and the places, tells what to do, and has no value. It is for the hook that relays a refusal.
func SecretBlockText(err error) string {
	st, ok := status.FromError(err)
	if !ok {
		return ""
	}
	var detail *ascv1.ErrorDetail
	for _, d := range st.Details() {
		if x, ok := d.(*ascv1.ErrorDetail); ok {
			detail = x
			break
		}
	}
	if detail == nil || detail.GetCode() != "SECRET_DETECTED" {
		return ""
	}
	hits := SecretHitsOf(detail.GetPaths())
	var b strings.Builder
	b.WriteString("Push refused: secret scanning found what looks like a secret.\n")
	for _, h := range hits {
		fmt.Fprintf(&b, "  %s:%d matched rule %s\n", h.Path, h.Line, h.Rule)
	}
	if len(hits) == 0 {
		b.WriteString("  The places are listed in the audit log of the repository.\n")
	}
	b.WriteString("Remove the secret from the change, revoke it at its issuer, and push again.\n")
	b.WriteString("If the match is wrong, a maintainer can list the path under [secrets] allow_paths in .asc/policy.toml through a reviewed change.")
	return b.String()
}
