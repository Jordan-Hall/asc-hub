// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"fmt"
	"time"

	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/google/uuid"
	"google.golang.org/grpc"
)

// The operations behind the pages. Each one makes the calls, checks every answer, and returns a cleaned view or an
// error that Classify understands. A context carries the credential (SystemContext or UserCall); the operations
// never pick one, so the caller decides whose rights apply.

// PolicyPage is what the policy page needs.
type PolicyPage struct {
	View *PolicyView
	// Base is the trunk changeset the file was read from. A proposal starts from it, so a trunk that moved is a
	// conflict and never a silent overwrite. Empty when the repository has no trunk yet.
	Base []byte
}

// LoadPolicyPage reads the policy in force and what the page draws from it. Only the first read is required; the
// land requirements, the secret rules and the signed-in person's access are asked for and left out when they fail,
// and the page then says less.
func LoadPolicyPage(ctx context.Context, conn grpc.ClientConnInterface, repoName string) (*PolicyPage, error) {
	policy := ascv1.NewPolicyClient(conn)
	cctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	src, err := policy.GetSource(cctx, &ascv1.GetPolicySourceRequest{Repo: RepoRef(repoName)})
	if err != nil {
		return nil, err
	}
	if len(src.GetText()) > MaxPolicyBytes {
		return nil, fmt.Errorf("%w: policy text", ErrBadAnswer)
	}
	if src.GetPresent() && len(src.GetSource()) != 0 && len(src.GetSource()) != 32 {
		return nil, fmt.Errorf("%w: policy source", ErrBadAnswer)
	}
	in := PolicyInput{Source: src}

	if src.GetPresent() && src.GetParsed().GetError() == "" {
		if land, err := policy.Evaluate(cctx, &ascv1.EvaluatePolicyRequest{Repo: RepoRef(repoName), Action: ascv1.PolicyAction_POLICY_ACTION_LAND}); err == nil {
			in.Land = land.GetLand()
		}
		probes := ProbePaths(src.GetParsed())
		req := &ascv1.EvaluatePolicyRequest{Repo: RepoRef(repoName), Action: ascv1.PolicyAction_POLICY_ACTION_WRITE}
		for _, p := range probes {
			req.Paths = append(req.Paths, []byte(p))
		}
		if you, err := policy.Evaluate(cctx, req); err == nil {
			in.You, in.YouPaths = you, probes
		}
	}
	if rules, err := ascv1.NewSecretScanClient(conn).ListRules(cctx, &ascv1.ListSecretRulesRequest{Repo: RepoRef(repoName)}); err == nil {
		in.Secrets = rules
	}
	return &PolicyPage{View: BuildPolicyView(in), Base: src.GetSource()}, nil
}

// Preview is the answer to "check this draft".
type Preview struct {
	Valid       bool
	Diagnostics []Diagnostic
	Meaning     []string
	// StaysMaintainer is false when the draft would leave the signed-in person unable to change the policy again.
	StaysMaintainer bool
}

// CheckText checks the size and the encoding of a policy text before it is sent.
func CheckText(text string) error {
	if len(text) == 0 || len(text) > MaxPolicyBytes {
		return fmt.Errorf("%w: the policy is 1 to %d bytes", ErrBadInput, MaxPolicyBytes)
	}
	if CleanBlock(text, MaxPolicyBytes+1) == "" {
		return fmt.Errorf("%w: the policy is empty", ErrBadInput)
	}
	for _, r := range text {
		if r == 0 || r == 0xFFFD {
			return fmt.Errorf("%w: the policy is not valid UTF-8 text", ErrBadInput)
		}
	}
	return nil
}

// PreviewPolicy validates a draft and explains it.
func PreviewPolicy(ctx context.Context, conn grpc.ClientConnInterface, repoName, text string) (*Preview, error) {
	if err := CheckText(text); err != nil {
		return nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	resp, err := ascv1.NewPolicyClient(conn).Validate(cctx, &ascv1.ValidatePolicyRequest{Repo: RepoRef(repoName), Text: []byte(text)})
	if err != nil {
		return nil, err
	}
	p := &Preview{Valid: resp.GetValid(), Diagnostics: DiagnosticsOf(resp), StaysMaintainer: resp.GetCallerStaysMaintainer()}
	if resp.GetValid() && resp.GetParsed() != nil {
		p.Meaning = Meaning(resp.GetParsed(), text, nil, nil, true)
	}
	return p, nil
}

// Proposal is the result of a proposed change to the policy.
type Proposal struct {
	// Change is the 32-letter text form of the new change; Short its first 12 letters.
	Change    string
	Short     string
	Changeset []byte
}

// ChangeText returns the 32-letter text form of a 16-byte change id: letter c is the hex digit 'z' - c, high
// digit first. It returns "" for any other length.
func ChangeText(b []byte) string {
	if len(b) != 16 {
		return ""
	}
	out := make([]byte, 32)
	for i, v := range b {
		out[2*i] = 'z' - (v >> 4)
		out[2*i+1] = 'z' - (v & 0xf)
	}
	return string(out)
}

// ProposePolicy turns a draft into a change under review. It never writes trunk: asc makes a change on base, in
// the name of the credential in ctx, and a maintainer lands it like any other. op, a UUID that the form carried,
// makes a double submit return the first answer instead of a second change. The draft is validated first, here
// and by asc, and a draft with an error is never proposed.
func ProposePolicy(ctx context.Context, conn grpc.ClientConnInterface, repoName string, base []byte, text, message, op string) (*Proposal, error) {
	if err := CheckText(text); err != nil {
		return nil, err
	}
	if len(base) != 0 && len(base) != 32 {
		return nil, fmt.Errorf("%w: base", ErrBadInput)
	}
	id, err := uuid.Parse(op)
	if err != nil {
		return nil, fmt.Errorf("%w: operation id", ErrBadInput)
	}
	if message != "" {
		if len(message) > 1024 || CleanBlock(message, 1024) == "" {
			return nil, fmt.Errorf("%w: the message is plain text of at most 1,024 bytes", ErrBadInput)
		}
	}
	cctx, cancel := context.WithTimeout(ascclient.WithOpID(ctx, id), CallTimeout)
	defer cancel()
	resp, err := ascv1.NewPolicyClient(conn).Propose(cctx, &ascv1.ProposePolicyRequest{
		Repo: RepoRef(repoName), Base: base, Text: []byte(text), Message: []byte(message),
	})
	if err != nil {
		return nil, err
	}
	change := ChangeText(resp.GetChange())
	if change == "" || len(resp.GetChangeset()) != 32 {
		return nil, fmt.Errorf("%w: proposal", ErrBadAnswer)
	}
	return &Proposal{Change: change, Short: change[:12], Changeset: resp.GetChangeset()}, nil
}

// NewOpID returns a fresh operation id for a form.
func NewOpID() string {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.NewString()
	}
	return id.String()
}

// ------------------------------------------------------------------------------------------------ audit

// AuditResult is a page of the audit log with the catalogue for the filter.
type AuditResult struct {
	Page    *AuditPage
	Actions []ActionInfo
}

// ListAudit asks for one page of the audit log of scope.
func ListAudit(ctx context.Context, conn grpc.ClientConnInterface, scope *ascv1.OwnerRef, q AuditQuery, filter *ascv1.AuditFilter) (*AuditResult, error) {
	client := ascv1.NewAuditClient(conn)
	cctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	resp, err := client.List(cctx, &ascv1.ListAuditRequest{Scope: scope, Filter: filter, OldestFirst: q.OldestFirst, Limit: q.Limit, PageToken: q.PageToken})
	if err != nil {
		return nil, err
	}
	var catalogue []ActionInfo
	if acts, err := client.ListActions(cctx, &ascv1.ListAuditActionsRequest{}); err == nil {
		catalogue = ActionsOf(acts)
	}
	page, err := AuditPageOf(resp, catalogue)
	if err != nil {
		return nil, err
	}
	return &AuditResult{Page: page, Actions: catalogue}, nil
}

// StartAuditExport opens the export stream of scope. The caller passes the stream to CopyExport.
func StartAuditExport(ctx context.Context, conn grpc.ClientConnInterface, scope *ascv1.OwnerRef, filter *ascv1.AuditFilter, csv bool) (ExportStream, error) {
	format := ascv1.AuditExportFormat_AUDIT_EXPORT_FORMAT_JSONL
	if csv {
		format = ascv1.AuditExportFormat_AUDIT_EXPORT_FORMAT_CSV
	}
	return ascv1.NewAuditClient(conn).Export(ctx, &ascv1.ExportAuditRequest{Scope: scope, Filter: filter, Format: format})
}

// VerifyAuditChain asks asc to verify the hash chain of scope.
func VerifyAuditChain(ctx context.Context, conn grpc.ClientConnInterface, scope *ascv1.OwnerRef) (*ChainResult, error) {
	cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	resp, err := ascv1.NewAuditClient(conn).VerifyChain(cctx, &ascv1.VerifyAuditChainRequest{Scope: scope})
	if err != nil {
		return nil, err
	}
	return &ChainResult{OK: resp.GetOk(), Checked: resp.GetChecked(), FirstBad: resp.GetFirstBadSeq(), Reason: CleanText(resp.GetReason(), MaxTextBytes)}, nil
}

// ChainResult is the result of a chain verification.
type ChainResult struct {
	OK       bool
	Checked  uint64
	FirstBad uint64
	Reason   string
}

// ------------------------------------------------------------------------------------------------ secrets

// SecretsPage is the security page content.
type SecretsPage struct {
	Rules    *RulesView
	Findings *FindingsPage
}

// SecretsQuery selects findings.
type SecretsQuery struct {
	State     string // a key of SecretStates, or "" for open
	PageToken string
}

// LoadSecrets reads the rules and one page of findings.
func LoadSecrets(ctx context.Context, conn grpc.ClientConnInterface, repoName string, q SecretsQuery) (*SecretsPage, error) {
	client := ascv1.NewSecretScanClient(conn)
	cctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	rules, err := client.ListRules(cctx, &ascv1.ListSecretRulesRequest{Repo: RepoRef(repoName)})
	if err != nil {
		return nil, err
	}
	rv, err := SecretRulesOf(rules)
	if err != nil {
		return nil, err
	}
	state := ascv1.SecretFindingState_SECRET_FINDING_STATE_OPEN
	if q.State != "" {
		s, ok := SecretStateOf(q.State)
		if !ok {
			return nil, fmt.Errorf("%w: state", ErrBadInput)
		}
		state = s
	}
	if q.PageToken != "" && !tokenPattern.MatchString(q.PageToken) {
		return nil, fmt.Errorf("%w: page token", ErrBadInput)
	}
	fresp, err := client.ListFindings(cctx, &ascv1.ListSecretFindingsRequest{
		Repo: RepoRef(repoName), States: []ascv1.SecretFindingState{state}, Limit: 50, PageToken: q.PageToken,
	})
	if err != nil {
		return nil, err
	}
	fp, err := FindingsPageOf(fresp)
	if err != nil {
		return nil, err
	}
	return &SecretsPage{Rules: rv, Findings: fp}, nil
}

// UpdateFinding sets the state of a finding. op makes a repeated submit return the first answer.
func UpdateFinding(ctx context.Context, conn grpc.ClientConnInterface, repoName string, id uint64, stateKey, note, op string) (*FindingRow, error) {
	state, ok := SecretStateOf(stateKey)
	if !ok || id == 0 {
		return nil, fmt.Errorf("%w: finding", ErrBadInput)
	}
	note, err := CheckNote(note)
	if err != nil {
		return nil, err
	}
	opID, err := uuid.Parse(op)
	if err != nil {
		return nil, fmt.Errorf("%w: operation id", ErrBadInput)
	}
	cctx, cancel := context.WithTimeout(ascclient.WithOpID(ctx, opID), CallTimeout)
	defer cancel()
	resp, err := ascv1.NewSecretScanClient(conn).UpdateFinding(cctx, &ascv1.UpdateSecretFindingRequest{Repo: RepoRef(repoName), Id: id, State: state, Note: note})
	if err != nil {
		return nil, err
	}
	row, err := FindingOf(resp.GetFinding())
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// ScanSummary is the result of a scan on demand.
type ScanSummary struct {
	Findings     int
	FilesScanned uint32
	Truncated    bool
}

// ScanRev scans a revision (a ref name, empty for the trunk tip).
func ScanRev(ctx context.Context, conn grpc.ClientConnInterface, repoName, rev string) (*ScanSummary, error) {
	if len(rev) > 255 || CleanText(rev, 255) != rev {
		return nil, fmt.Errorf("%w: revision", ErrBadInput)
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	resp, err := ascv1.NewSecretScanClient(conn).Scan(cctx, &ascv1.ScanSecretsRequest{Repo: RepoRef(repoName), At: &ascv1.ScanSecretsRequest_Rev{Rev: rev}})
	if err != nil {
		return nil, err
	}
	return &ScanSummary{Findings: len(resp.GetFindings()), FilesScanned: resp.GetFilesScanned(), Truncated: resp.GetTruncated()}, nil
}

// ------------------------------------------------------------------------------------------------ signing keys

// KeysPage is the signing key page content.
type KeysPage struct {
	Keys   []KeyRow
	Server ServerKey
}

// LoadKeys lists the keys of the credential's owner and reads the server key.
func LoadKeys(ctx context.Context, conn grpc.ClientConnInterface, now time.Time) (*KeysPage, error) {
	client := ascv1.NewSigningClient(conn)
	cctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	resp, err := client.ListKeys(cctx, &ascv1.ListSigningKeysRequest{})
	if err != nil {
		return nil, err
	}
	keys, err := KeysOf(resp, now)
	if err != nil {
		return nil, err
	}
	page := &KeysPage{Keys: keys}
	if sk, err := client.GetServerKey(cctx, &ascv1.GetServerKeyRequest{}); err == nil {
		page.Server = ServerKeyOf(sk)
	}
	return page, nil
}

// AddKey registers a signing key.
func AddKey(ctx context.Context, conn grpc.ClientConnInterface, title, publicKey string, expiresAt int64, op string) (*KeyRow, error) {
	title, err := CheckKeyTitle(title)
	if err != nil {
		return nil, err
	}
	kc, err := CheckPublicKey(publicKey)
	if err != nil {
		return nil, err
	}
	opID, err := uuid.Parse(op)
	if err != nil {
		return nil, fmt.Errorf("%w: operation id", ErrBadInput)
	}
	cctx, cancel := context.WithTimeout(ascclient.WithOpID(ctx, opID), CallTimeout)
	defer cancel()
	resp, err := ascv1.NewSigningClient(conn).AddKey(cctx, &ascv1.AddSigningKeyRequest{Title: title, PublicKey: kc.Line, ExpiresAt: expiresAt})
	if err != nil {
		return nil, err
	}
	row, err := KeyOf(resp.GetKey(), time.Now())
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// RevokeKey revokes a signing key. The call is idempotent.
func RevokeKey(ctx context.Context, conn grpc.ClientConnInterface, id, reason, op string) (*KeyRow, error) {
	if id == "" || len(id) > 64 || CleanText(id, 64) != id {
		return nil, fmt.Errorf("%w: key id", ErrBadInput)
	}
	reason, err := CheckRevokeReason(reason)
	if err != nil {
		return nil, err
	}
	opID, err := uuid.Parse(op)
	if err != nil {
		return nil, fmt.Errorf("%w: operation id", ErrBadInput)
	}
	cctx, cancel := context.WithTimeout(ascclient.WithOpID(ctx, opID), CallTimeout)
	defer cancel()
	resp, err := ascv1.NewSigningClient(conn).RevokeKey(cctx, &ascv1.RevokeSigningKeyRequest{Id: id, Reason: reason})
	if err != nil {
		return nil, err
	}
	row, err := KeyOf(resp.GetKey(), time.Now())
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// ------------------------------------------------------------------------------------------------ checks

// ChecksQuery selects failure groups.
type ChecksQuery struct {
	State     string // a key of GroupStates; "" for open and acknowledged
	FlakyOnly bool
	Job       string
	Test      string
	PageToken string
	// SkipFlaky leaves out the flaky list, for a view that does not draw it.
	SkipFlaky bool
}

// ChecksPage is the checks page content.
type ChecksPage struct {
	Groups *GroupsPage
	Flaky  []FlakyRow
}

// LoadChecks reads one page of failure groups and the flaky list.
func LoadChecks(ctx context.Context, conn grpc.ClientConnInterface, repoName string, q ChecksQuery, windowDays uint32) (*ChecksPage, error) {
	client := ascv1.NewCiInsightsClient(conn)
	cctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	req := &ascv1.ListFailureGroupsRequest{Repo: RepoRef(repoName), FlakyOnly: q.FlakyOnly, Limit: 30}
	if q.State == "" {
		req.States = []ascv1.FailureGroupState{ascv1.FailureGroupState_FAILURE_GROUP_STATE_OPEN, ascv1.FailureGroupState_FAILURE_GROUP_STATE_ACKNOWLEDGED}
	} else {
		s, ok := GroupStateOf(q.State)
		if !ok {
			return nil, fmt.Errorf("%w: state", ErrBadInput)
		}
		req.States = []ascv1.FailureGroupState{s}
	}
	if q.Job != "" {
		job, err := CheckName(q.Job)
		if err != nil {
			return nil, err
		}
		req.JobName = job
	}
	if q.Test != "" {
		test, err := CheckName(q.Test)
		if err != nil {
			return nil, err
		}
		req.TestName = test
	}
	if q.PageToken != "" {
		if !tokenPattern.MatchString(q.PageToken) {
			return nil, fmt.Errorf("%w: page token", ErrBadInput)
		}
		req.PageToken = q.PageToken
	}
	gresp, err := client.ListFailureGroups(cctx, req)
	if err != nil {
		return nil, err
	}
	groups, err := GroupsPageOf(gresp)
	if err != nil {
		return nil, err
	}
	page := &ChecksPage{Groups: groups}
	if q.PageToken == "" && !q.SkipFlaky {
		if fresp, err := client.ListFlakyTests(cctx, &ascv1.ListFlakyTestsRequest{Repo: RepoRef(repoName), WindowDays: windowDays, Limit: 15}); err == nil {
			if rows, _, err := FlakyOf(fresp); err == nil {
				page.Flaky = rows
			}
		}
	}
	return page, nil
}

// LoadGroup reads one failure group with its occurrences and, for a group of one test, the history strip.
func LoadGroup(ctx context.Context, conn grpc.ClientConnInterface, repoName string, id uint64, pageToken string, windowDays uint32) (*GroupDetail, *History, error) {
	if id == 0 {
		return nil, nil, fmt.Errorf("%w: group", ErrBadInput)
	}
	if pageToken != "" && !tokenPattern.MatchString(pageToken) {
		return nil, nil, fmt.Errorf("%w: page token", ErrBadInput)
	}
	client := ascv1.NewCiInsightsClient(conn)
	cctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	resp, err := client.GetFailureGroup(cctx, &ascv1.GetFailureGroupRequest{Repo: RepoRef(repoName), Id: id, Limit: 20, PageToken: pageToken})
	if err != nil {
		return nil, nil, err
	}
	detail, err := GroupDetailOf(resp)
	if err != nil {
		return nil, nil, err
	}
	if detail.Group.ID != id {
		return nil, nil, fmt.Errorf("%w: group id", ErrBadAnswer)
	}
	if detail.Group.TestName == "" && detail.Group.JobName == "" {
		return detail, nil, nil
	}
	hreq := &ascv1.FlakyHistoryRequest{Repo: RepoRef(repoName), TestName: detail.Group.TestName, JobName: detail.Group.JobName, WindowDays: windowDays}
	hresp, err := client.FlakyHistory(cctx, hreq)
	if err != nil {
		return detail, nil, nil // the strip is advice; the group is still shown
	}
	hist, err := HistoryOf(hresp)
	if err != nil {
		return detail, nil, nil
	}
	return detail, hist, nil
}

// SetGroupState sets the state of a failure group (acknowledge, quarantine, resolve, reopen). asc decides whether
// the caller may: quarantine needs a maintainer and a policy that allows it.
func SetGroupState(ctx context.Context, conn grpc.ClientConnInterface, repoName string, id uint64, stateKey, note, op string) (*GroupRow, error) {
	state, ok := GroupStateOf(stateKey)
	if !ok || id == 0 {
		return nil, fmt.Errorf("%w: group", ErrBadInput)
	}
	note, err := CheckGroupNote(note)
	if err != nil {
		return nil, err
	}
	opID, err := uuid.Parse(op)
	if err != nil {
		return nil, fmt.Errorf("%w: operation id", ErrBadInput)
	}
	cctx, cancel := context.WithTimeout(ascclient.WithOpID(ctx, opID), CallTimeout)
	defer cancel()
	resp, err := ascv1.NewCiInsightsClient(conn).SetFailureGroupState(cctx, &ascv1.SetFailureGroupStateRequest{Repo: RepoRef(repoName), Id: id, State: state, Note: note})
	if err != nil {
		return nil, err
	}
	row, err := GroupOf(resp.GetGroup())
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// RequiredChecks returns the checks that the policy in force needs on every change that lands on trunk.
func RequiredChecks(ctx context.Context, conn grpc.ClientConnInterface, repoName string) ([]string, error) {
	cctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	src, err := ascv1.NewPolicyClient(conn).GetSource(cctx, &ascv1.GetPolicySourceRequest{Repo: RepoRef(repoName)})
	if err != nil {
		return nil, err
	}
	if !src.GetPresent() || src.GetParsed().GetError() != "" {
		return nil, nil
	}
	checks := src.GetParsed().GetTrunk().GetRequireChecks()
	if len(checks) > 64 {
		return nil, fmt.Errorf("%w: required checks", ErrBadAnswer)
	}
	return append([]string(nil), checks...), nil
}

// RequireSigned reports whether the policy in force makes a signature necessary to land (`[signing] require_signed`).
// It is false when the policy cannot be read: the badge then marks only what is wrong with a signature that exists.
func RequireSigned(ctx context.Context, conn grpc.ClientConnInterface, repoName string) bool {
	cctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	src, err := ascv1.NewPolicyClient(conn).GetSource(cctx, &ascv1.GetPolicySourceRequest{Repo: RepoRef(repoName)})
	if err != nil || !src.GetPresent() || len(src.GetText()) > MaxPolicyBytes {
		return false
	}
	signed, _ := TomlBool(string(src.GetText()), "signing", "require_signed")
	return signed
}
