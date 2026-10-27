// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/google/uuid"
	"google.golang.org/grpc"
)

// The dependencies page: findings of known vulnerabilities, newer versions, and the settings of a repository
// (asc.v1.DepsService, docs/DEPENDENCIES.md on the asc side). The server reads the manifests, asks the advisory source
// and the registries, and records findings; nothing here runs code of the repository. Every text that arrives is
// data: advisory ids, summaries and links come from a public source, so they are cut, stripped of control characters
// and checked before a page draws them, and a link is drawn only when it is an https address.
const (
	// FeatureDeps is the Version.features string of the dependency service.
	FeatureDeps = "deps.v1"

	// MaxDepRows bounds the rows of any list of the page.
	MaxDepRows = 200
	// DepFindingsPageSize is the page size asked for.
	DepFindingsPageSize = 50
	// maxDepNote is the longest note of a dismissal (asc allows 1,024 bytes).
	maxDepNote = 1024
	// maxDepErrors bounds the lookup errors the page lists.
	maxDepErrors = 20
	maxDepURL    = 512
)

// DepSeverityKeys are the severities in the order the filter lists them, with the Tone of the pill of each.
var DepSeverityKeys = []struct {
	Key      string
	Severity ascv1.DepSeverity
	Tone     string
}{
	{"critical", ascv1.DepSeverity_DEP_SEVERITY_CRITICAL, "no"},
	{"high", ascv1.DepSeverity_DEP_SEVERITY_HIGH, "no"},
	{"moderate", ascv1.DepSeverity_DEP_SEVERITY_MODERATE, "wn"},
	{"low", ascv1.DepSeverity_DEP_SEVERITY_LOW, "info"},
}

func depSeverityOf(s ascv1.DepSeverity) (key, tone string) {
	for _, k := range DepSeverityKeys {
		if k.Severity == s {
			return k.Key, k.Tone
		}
	}
	return "unknown", "ro"
}

// DepSeverityFilter reads a severity key of a form; "" means any.
func DepSeverityFilter(key string) (ascv1.DepSeverity, bool) {
	if key == "" {
		return ascv1.DepSeverity_DEP_SEVERITY_UNSPECIFIED, true
	}
	for _, k := range DepSeverityKeys {
		if k.Key == key {
			return k.Severity, true
		}
	}
	return ascv1.DepSeverity_DEP_SEVERITY_UNSPECIFIED, false
}

// DepStateKeys are the states of a finding in the order the tabs list them.
var DepStateKeys = []struct {
	Key   string
	State ascv1.DepFindingState
}{
	{"open", ascv1.DepFindingState_DEP_FINDING_STATE_OPEN},
	{"dismissed", ascv1.DepFindingState_DEP_FINDING_STATE_DISMISSED},
	{"fixed", ascv1.DepFindingState_DEP_FINDING_STATE_FIXED},
}

func depStateKey(s ascv1.DepFindingState) string {
	for _, k := range DepStateKeys {
		if k.State == s {
			return k.Key
		}
	}
	return "unknown"
}

// DepDismissReasons are the reasons a finding can be dismissed with.
var DepDismissReasons = []struct {
	Key    string
	Reason ascv1.DepDismissReason
}{
	{"not_used", ascv1.DepDismissReason_DEP_DISMISS_REASON_NOT_USED},
	{"tolerable", ascv1.DepDismissReason_DEP_DISMISS_REASON_TOLERABLE},
	{"inaccurate", ascv1.DepDismissReason_DEP_DISMISS_REASON_INACCURATE},
	{"fix_started", ascv1.DepDismissReason_DEP_DISMISS_REASON_FIX_STARTED},
}

func depReasonKey(r ascv1.DepDismissReason) string {
	for _, k := range DepDismissReasons {
		if k.Reason == r {
			return k.Key
		}
	}
	return ""
}

func depLookupKey(l ascv1.DepLookupStatus) string {
	switch l {
	case ascv1.DepLookupStatus_DEP_LOOKUP_STATUS_COMPLETE:
		return "complete"
	case ascv1.DepLookupStatus_DEP_LOOKUP_STATUS_PARTIAL:
		return "partial"
	case ascv1.DepLookupStatus_DEP_LOOKUP_STATUS_FAILED:
		return "failed"
	}
	return "unknown"
}

// DepKindKeys are the sizes of an update.
var DepKindKeys = []struct {
	Key  string
	Kind ascv1.DepUpdateKind
}{
	{"patch", ascv1.DepUpdateKind_DEP_UPDATE_KIND_PATCH},
	{"minor", ascv1.DepUpdateKind_DEP_UPDATE_KIND_MINOR},
	{"major", ascv1.DepUpdateKind_DEP_UPDATE_KIND_MAJOR},
}

// DepKindOf reads an update kind key of a form; "" means the repository's own limit.
func DepKindOf(key string) (ascv1.DepUpdateKind, bool) {
	if key == "" {
		return ascv1.DepUpdateKind_DEP_UPDATE_KIND_UNSPECIFIED, true
	}
	for _, k := range DepKindKeys {
		if k.Key == key {
			return k.Kind, true
		}
	}
	return ascv1.DepUpdateKind_DEP_UPDATE_KIND_UNSPECIFIED, false
}

func depKindKey(k ascv1.DepUpdateKind) string {
	for _, e := range DepKindKeys {
		if e.Kind == k {
			return e.Key
		}
	}
	return "unknown"
}

// DepTone is the pill tone of an update of that size: a larger one needs more care.
func depKindTone(k ascv1.DepUpdateKind) string {
	switch k {
	case ascv1.DepUpdateKind_DEP_UPDATE_KIND_PATCH:
		return "ok"
	case ascv1.DepUpdateKind_DEP_UPDATE_KIND_MINOR:
		return "info"
	case ascv1.DepUpdateKind_DEP_UPDATE_KIND_MAJOR:
		return "wn"
	}
	return "ro"
}

func depText(s string, max int) string { return CleanText(s, max) }

func depPath(b []byte) string { return CleanText(string(b), 1024) }

// httpsAdvisoryURL returns u when it is an https address without credentials, else "". An advisory link comes from a public
// source and is drawn as a link only after this check.
func httpsAdvisoryURL(u string) string {
	if u == "" || len(u) > maxDepURL || CleanText(u, maxDepURL) != u {
		return ""
	}
	p, err := url.Parse(u)
	if err != nil || p.Scheme != "https" || p.Host == "" || p.User != nil {
		return ""
	}
	return p.String()
}

func depList(in []string, n, max int) []string {
	var out []string
	for _, s := range in {
		if c := CleanText(s, max); c != "" {
			out = append(out, c)
		}
		if len(out) == n {
			break
		}
	}
	return out
}

// ---------------------------------------------------------------------------------------------------- findings

// DepFindingRow is one finding.
type DepFindingRow struct {
	ID          uint64
	Ecosystem   string
	Name        string
	Version     string
	Direct      bool
	Path        string
	AdvisoryID  string
	Aliases     []string
	Summary     string
	SeverityKey string
	Tone        string
	CVSS        string
	FixedIn     string
	FixedAll    []string
	URL         string
	StateKey    string
	FirstSeen   time.Time
	LastSeen    time.Time
	DismissedBy string
	DismissedAt time.Time
	ReasonKey   string
	Note        string
}

func depFindingRow(f *ascv1.DepFinding) (DepFindingRow, error) {
	if f == nil || f.GetId() == 0 || f.GetName() == "" || f.GetAdvisory().GetId() == "" {
		return DepFindingRow{}, fmt.Errorf("%w: dependency finding", ErrBadAnswer)
	}
	sev, tone := depSeverityOf(f.GetAdvisory().GetSeverity())
	cvss := f.GetAdvisory().GetCvss()
	if cvss < 0 || cvss > 10 || cvss != cvss {
		return DepFindingRow{}, fmt.Errorf("%w: cvss", ErrBadAnswer)
	}
	row := DepFindingRow{
		ID: f.GetId(), Ecosystem: depText(f.GetEcosystem(), 64), Name: depText(f.GetName(), maxNameBytes), Version: depText(f.GetVersion(), 128),
		Direct: f.GetDirect(), Path: depPath(f.GetPath()), AdvisoryID: depText(f.GetAdvisory().GetId(), 128),
		Aliases: depList(f.GetAdvisory().GetAliases(), 5, 128), Summary: depText(f.GetAdvisory().GetSummary(), MaxTextBytes),
		SeverityKey: sev, Tone: tone, FixedIn: depText(f.GetFixedIn(), 128), FixedAll: depList(f.GetAdvisory().GetFixedVersions(), 5, 128),
		URL: httpsAdvisoryURL(f.GetAdvisory().GetUrl()), StateKey: depStateKey(f.GetState()), FirstSeen: ts(f.GetFirstSeen()), LastSeen: ts(f.GetLastSeen()),
		DismissedBy: depText(f.GetDismissedBy(), 128), ReasonKey: depReasonKey(f.GetDismissReason()), Note: depText(f.GetNote(), maxDepNote),
	}
	if cvss > 0 {
		row.CVSS = fmt.Sprintf("%.1f", cvss)
	}
	if f.GetDismissedAt() > 0 {
		row.DismissedAt = ts(f.GetDismissedAt())
	}
	return row, nil
}

// DepFindingsQuery selects findings.
type DepFindingsQuery struct {
	State     string // a key of DepStateKeys; "" means open
	Severity  string // the lowest severity key; "" means any
	Ecosystem string
	PageToken string
}

// DepFindingsPage is a page of findings with the numbers for the tabs.
type DepFindingsPage struct {
	Rows   []DepFindingRow
	Next   string
	Counts map[string]uint32
	// LastScan is when the repository was last scanned, zero when it never was.
	LastScan     time.Time
	LookupKey    string
	NeverScanned bool
}

// CheckEcosystem checks an ecosystem name taken from a form: the OSV spelling, short plain text.
func CheckEcosystem(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	if len(s) > 64 || CleanText(s, 64) != s || strings.ContainsAny(s, " \\\"'<>") {
		return "", fmt.Errorf("%w: ecosystem", ErrBadInput)
	}
	return s, nil
}

// LoadDepFindings reads one page of findings.
func LoadDepFindings(ctx context.Context, conn grpc.ClientConnInterface, repoName string, q DepFindingsQuery) (*DepFindingsPage, error) {
	state := ascv1.DepFindingState_DEP_FINDING_STATE_OPEN
	if q.State != "" {
		found := false
		for _, k := range DepStateKeys {
			if k.Key == q.State {
				state, found = k.State, true
			}
		}
		if !found {
			return nil, fmt.Errorf("%w: state", ErrBadInput)
		}
	}
	sev, ok := DepSeverityFilter(q.Severity)
	if !ok {
		return nil, fmt.Errorf("%w: severity", ErrBadInput)
	}
	eco, err := CheckEcosystem(q.Ecosystem)
	if err != nil {
		return nil, err
	}
	if q.PageToken != "" && !tokenPattern.MatchString(q.PageToken) {
		return nil, fmt.Errorf("%w: page token", ErrBadInput)
	}
	req := &ascv1.ListDepFindingsRequest{Repo: RepoRef(repoName), States: []ascv1.DepFindingState{state}, MinSeverity: sev, Limit: DepFindingsPageSize, PageToken: q.PageToken}
	if eco != "" {
		req.Ecosystems = []string{eco}
	}
	cctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	resp, err := ascv1.NewDepsServiceClient(conn).ListFindings(cctx, req)
	if err != nil {
		return nil, err
	}
	if len(resp.GetFindings()) > MaxDepRows || (resp.GetNextPageToken() != "" && !tokenPattern.MatchString(resp.GetNextPageToken())) {
		return nil, fmt.Errorf("%w: dependency findings page", ErrBadAnswer)
	}
	page := &DepFindingsPage{Next: resp.GetNextPageToken(), Counts: map[string]uint32{}, LookupKey: depLookupKey(resp.GetLastLookup())}
	for _, k := range DepStateKeys {
		page.Counts[k.Key] = resp.GetCountsByState()[k.Key]
	}
	if resp.GetLastScan() > 0 {
		page.LastScan = ts(resp.GetLastScan())
	} else {
		page.NeverScanned = true
	}
	for _, f := range resp.GetFindings() {
		row, err := depFindingRow(f)
		if err != nil {
			return nil, err
		}
		page.Rows = append(page.Rows, row)
	}
	return page, nil
}

// ---------------------------------------------------------------------------------------------------- updates

// DepUpdateRow is one newer version.
type DepUpdateRow struct {
	Ecosystem string
	Name      string
	Current   string
	Latest    string
	Target    string
	KindKey   string
	KindTone  string
	Direct    bool
	Path      string
	Fixes     []string
	Unfixed   []string
	Group     string
	Ignored   bool
	Error     string
}

func depUpdateRow(u *ascv1.DepUpdate) (DepUpdateRow, error) {
	if u == nil || u.GetName() == "" {
		return DepUpdateRow{}, fmt.Errorf("%w: dependency update", ErrBadAnswer)
	}
	return DepUpdateRow{
		Ecosystem: depText(u.GetEcosystem(), 64), Name: depText(u.GetName(), maxNameBytes), Current: depText(u.GetCurrent(), 128),
		Latest: depText(u.GetLatest(), 128), Target: depText(u.GetTarget(), 128), KindKey: depKindKey(u.GetKind()), KindTone: depKindTone(u.GetKind()),
		Direct: u.GetDirect(), Path: depPath(u.GetPath()), Fixes: depList(u.GetFixes(), 10, 128), Unfixed: depList(u.GetUnfixed(), 10, 128),
		Group: depText(u.GetGroup(), 64), Ignored: u.GetIgnored(), Error: depText(u.GetError(), MaxTextBytes),
	}, nil
}

// DepUpdates is the answer to "what is newer".
type DepUpdates struct {
	Rows []DepUpdateRow
	// Packages is how many packages were looked at, Unknown how many registries could not be asked.
	Packages, Unknown uint32
	Errors            []string
}

// LoadDepUpdates asks for the newer versions. securityOnly keeps those that fix an open finding.
func LoadDepUpdates(ctx context.Context, conn grpc.ClientConnInterface, repoName string, securityOnly bool) (*DepUpdates, error) {
	cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	resp, err := ascv1.NewDepsServiceClient(conn).ListUpdates(cctx, &ascv1.ListDepUpdatesRequest{Repo: RepoRef(repoName), SecurityOnly: securityOnly})
	if err != nil {
		return nil, err
	}
	if len(resp.GetUpdates()) > 5000 {
		return nil, fmt.Errorf("%w: too many updates", ErrBadAnswer)
	}
	out := &DepUpdates{Packages: resp.GetPackages(), Unknown: resp.GetUnknownPackages(), Errors: depList(resp.GetLookupErrors(), maxDepErrors, MaxTextBytes)}
	for _, u := range resp.GetUpdates() {
		row, err := depUpdateRow(u)
		if err != nil {
			return nil, err
		}
		out.Rows = append(out.Rows, row)
		if len(out.Rows) == MaxDepRows {
			break
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------------------------------------- config

// DepsConfigView is the configuration of a repository as the page shows it.
type DepsConfigView struct {
	// Source is "file" for `.asc/deps.toml` on trunk, "default" for the built-in settings.
	Source       string
	Text         string
	Error        string
	Workflow     string
	Supported    []string
	Ecosystems   []string
	Schedule     string
	SecurityOnly bool
	SeverityKey  string
	MaxKindKey   string
	MaxProposals uint32
	Grouping     string
	Groups       []DepGroupView
	Ignores      []DepIgnoreView
}

// DepGroupView is a group of updates that go in one change.
type DepGroupView struct {
	Name, Ecosystem string
	Patterns        []string
	Kinds           []string
}

// DepIgnoreView is a rule that leaves a package or an advisory out.
type DepIgnoreView struct {
	Ecosystem, Name, Until, Reason string
	Kinds, Advisories              []string
}

func depKindKeys(kinds []ascv1.DepUpdateKind) []string {
	var out []string
	for _, k := range kinds {
		out = append(out, depKindKey(k))
	}
	return out
}

// LoadDepsConfig reads the settings in force.
func LoadDepsConfig(ctx context.Context, conn grpc.ClientConnInterface, repoName string) (*DepsConfigView, error) {
	cctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	resp, err := ascv1.NewDepsServiceClient(conn).GetConfig(cctx, &ascv1.GetDepsConfigRequest{Repo: RepoRef(repoName)})
	if err != nil {
		return nil, err
	}
	src := resp.GetSource()
	if src != "file" && src != "default" {
		return nil, fmt.Errorf("%w: config source", ErrBadAnswer)
	}
	c := resp.GetConfig()
	if len(c.GetGroups()) > 64 || len(c.GetIgnore()) > 256 || len(resp.GetText()) > MaxPolicyBytes {
		return nil, fmt.Errorf("%w: config size", ErrBadAnswer)
	}
	sev, _ := depSeverityOf(c.GetSeverityThreshold())
	v := &DepsConfigView{
		Source: src, Text: CleanBlock(resp.GetText(), MaxPolicyBytes), Error: depText(resp.GetError(), MaxTextBytes),
		Workflow: CleanBlock(resp.GetWorkflow(), MaxPolicyBytes), Supported: depList(resp.GetSupported(), 64, 64),
		Ecosystems: depList(c.GetEcosystems(), 64, 64), Schedule: depText(c.GetSchedule(), 64), SecurityOnly: c.GetSecurityOnly(),
		SeverityKey: sev, MaxKindKey: depKindKey(c.GetMaxKind()), MaxProposals: c.GetMaxProposals(), Grouping: depText(c.GetGrouping(), 16),
	}
	for _, g := range c.GetGroups() {
		v.Groups = append(v.Groups, DepGroupView{Name: depText(g.GetName(), 64), Ecosystem: depText(g.GetEcosystem(), 64), Patterns: depList(g.GetPatterns(), 20, 128), Kinds: depKindKeys(g.GetKinds())})
	}
	for _, i := range c.GetIgnore() {
		v.Ignores = append(v.Ignores, DepIgnoreView{
			Ecosystem: depText(i.GetEcosystem(), 64), Name: depText(i.GetName(), 128), Until: depText(i.GetUntil(), 16), Reason: depText(i.GetReason(), MaxTextBytes),
			Kinds: depKindKeys(i.GetKinds()), Advisories: depList(i.GetAdvisories(), 20, 128),
		})
	}
	return v, nil
}

// ---------------------------------------------------------------------------------------------------- scan

// DepFileRow is the result of reading one file.
type DepFileRow struct {
	Path, Format, Ecosystem string
	Packages, Direct        uint32
	Error                   string
	Notes                   []string
}

// DepScan is the result of a scan.
type DepScan struct {
	Files     []DepFileRow
	Packages  uint32
	Findings  int
	LookupKey string
	Unknown   uint32
	Errors    []string
	Truncated bool
	Source    string
}

// ScanDeps asks asc to scan trunk and record the findings. It is for a maintainer; asc decides again.
func ScanDeps(ctx context.Context, conn grpc.ClientConnInterface, repoName string, refresh bool) (*DepScan, error) {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	resp, err := ascv1.NewDepsServiceClient(conn).Scan(cctx, &ascv1.ScanDepsRequest{Repo: RepoRef(repoName), Refresh: refresh})
	if err != nil {
		return nil, err
	}
	if len(resp.GetFiles()) > 2000 || len(resp.GetFindings()) > 100000 {
		return nil, fmt.Errorf("%w: scan size", ErrBadAnswer)
	}
	out := &DepScan{
		Packages: resp.GetPackages(), Findings: len(resp.GetFindings()), LookupKey: depLookupKey(resp.GetLookup()), Unknown: resp.GetUnknownPackages(),
		Errors: depList(resp.GetLookupErrors(), maxDepErrors, MaxTextBytes), Truncated: resp.GetTruncated(), Source: depText(resp.GetSource(), 32),
	}
	for _, f := range resp.GetFiles() {
		if len(out.Files) == MaxDepRows {
			out.Truncated = true
			break
		}
		out.Files = append(out.Files, DepFileRow{
			Path: depPath(f.GetPath()), Format: depText(f.GetFormat(), 64), Ecosystem: depText(f.GetEcosystem(), 64), Packages: f.GetPackages(),
			Direct: f.GetDirect(), Error: depText(f.GetError(), MaxTextBytes), Notes: depList(f.GetNotes(), 20, MaxTextBytes),
		})
	}
	return out, nil
}

// ---------------------------------------------------------------------------------------------------- propose

// DepProposeQuery is what a proposal is asked for.
type DepProposeQuery struct {
	SecurityOnly bool
	MaxKind      string // a key of DepKindKeys; "" means the repository's own limit
	DryRun       bool
	Op           string
}

// DepProposal is one change that a proposal makes (or would make).
type DepProposal struct {
	Group   string
	Ref     string
	Change  string
	Updates []DepUpdateRow
	Files   []string
	Message string
	Notes   []string
}

// DepSkippedRow is an update a proposal left out, and why.
type DepSkippedRow struct{ Ecosystem, Name, Reason string }

// DepPlan is the answer to a proposal.
type DepPlan struct {
	Proposals []DepProposal
	Skipped   []DepSkippedRow
	DryRun    bool
}

// ProposeDeps asks asc to rewrite the pins of the allowed updates and record the result as changes on
// `agent/deps/<group>` under review. It never writes trunk. A dry run changes nothing.
func ProposeDeps(ctx context.Context, conn grpc.ClientConnInterface, repoName string, q DepProposeQuery) (*DepPlan, error) {
	kind, ok := DepKindOf(q.MaxKind)
	if !ok {
		return nil, fmt.Errorf("%w: update size", ErrBadInput)
	}
	callCtx := ctx
	if !q.DryRun {
		id, err := uuid.Parse(q.Op)
		if err != nil {
			return nil, fmt.Errorf("%w: operation id", ErrBadInput)
		}
		callCtx = ascclient.WithOpID(ctx, id)
	}
	cctx, cancel := context.WithTimeout(callCtx, 5*time.Minute)
	defer cancel()
	resp, err := ascv1.NewDepsServiceClient(conn).ProposeUpdate(cctx, &ascv1.ProposeDepUpdateRequest{
		Repo: RepoRef(repoName), SecurityOnly: q.SecurityOnly, MaxKind: kind, DryRun: q.DryRun,
	})
	if err != nil {
		return nil, err
	}
	if len(resp.GetProposals()) > 64 || len(resp.GetSkipped()) > 5000 {
		return nil, fmt.Errorf("%w: proposal size", ErrBadAnswer)
	}
	plan := &DepPlan{DryRun: q.DryRun}
	for _, p := range resp.GetProposals() {
		ref := depText(p.GetRef(), 255)
		if !strings.HasPrefix(ref, "refs/heads/agent/deps/") {
			return nil, fmt.Errorf("%w: proposal ref", ErrBadAnswer)
		}
		out := DepProposal{Group: depText(p.GetGroup(), 64), Ref: ref, Change: ChangeText(p.GetChange()), Message: depText(p.GetMessage(), MaxTextBytes), Notes: depList(p.GetNotes(), 20, MaxTextBytes)}
		for _, u := range p.GetUpdates() {
			row, err := depUpdateRow(u)
			if err != nil {
				return nil, err
			}
			out.Updates = append(out.Updates, row)
		}
		for _, f := range p.GetFiles() {
			out.Files = append(out.Files, depPath(f))
		}
		plan.Proposals = append(plan.Proposals, out)
	}
	for i, s := range resp.GetSkipped() {
		if i == MaxDepRows {
			break
		}
		plan.Skipped = append(plan.Skipped, DepSkippedRow{Ecosystem: depText(s.GetEcosystem(), 64), Name: depText(s.GetName(), maxNameBytes), Reason: depText(s.GetReason(), MaxTextBytes)})
	}
	return plan, nil
}

// ---------------------------------------------------------------------------------------------------- dismiss

// DismissDep dismisses a finding with a reason, or reopens it when reasonKey is "reopen". op makes a double submit
// one dismissal.
func DismissDep(ctx context.Context, conn grpc.ClientConnInterface, repoName string, id uint64, reasonKey, note, op string) (*DepFindingRow, error) {
	if id == 0 {
		return nil, fmt.Errorf("%w: finding", ErrBadInput)
	}
	reason := ascv1.DepDismissReason_DEP_DISMISS_REASON_UNSPECIFIED
	if reasonKey != "reopen" {
		found := false
		for _, r := range DepDismissReasons {
			if r.Key == reasonKey {
				reason, found = r.Reason, true
			}
		}
		if !found {
			return nil, fmt.Errorf("%w: reason", ErrBadInput)
		}
	}
	note = strings.TrimSpace(note)
	if len(note) > maxDepNote || CleanBlock(note, maxDepNote) != strings.ReplaceAll(note, "\r\n", "\n") {
		return nil, fmt.Errorf("%w: the note is at most %d bytes of plain text", ErrBadInput, maxDepNote)
	}
	opID, err := uuid.Parse(op)
	if err != nil {
		return nil, fmt.Errorf("%w: operation id", ErrBadInput)
	}
	cctx, cancel := context.WithTimeout(ascclient.WithOpID(ctx, opID), CallTimeout)
	defer cancel()
	resp, err := ascv1.NewDepsServiceClient(conn).Dismiss(cctx, &ascv1.DismissDepFindingRequest{Repo: RepoRef(repoName), Id: id, Reason: reason, Note: note})
	if err != nil {
		return nil, err
	}
	row, err := depFindingRow(resp.GetFinding())
	if err != nil {
		return nil, err
	}
	if row.ID != id {
		return nil, fmt.Errorf("%w: finding id", ErrBadAnswer)
	}
	return &row, nil
}
