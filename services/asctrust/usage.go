// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/dustin/go-humanize"
	"google.golang.org/grpc"
)

// The Usage service of asc serve: four measures (object storage, agent minutes, AI spend, bandwidth) of an account, an
// organisation or the whole server for a calendar month, the AI spend grouped by key, person, repository or day, and the
// limits. Everything that comes back is data: texts are cleaned, numbers are bounded, a period must have the shape of one.

// FeatureUsage is what Version.features says when the server has the Usage service.
const FeatureUsage = "usage.v1"

const (
	gib = 1 << 30
	// MaxQuotaGiB, MaxQuotaMinutes and MaxQuotaUSD bound what a form may set (a typo of an order of magnitude is the
	// risk, not abuse: only a site administrator can set a limit).
	MaxQuotaGiB     = 1 << 20
	MaxQuotaMinutes = 100_000_000
	MaxQuotaUSD     = 10_000_000
	// MaxSpendDays is the longest range asc answers for.
	MaxSpendDays = 400
)

var (
	periodShape = regexp.MustCompile(`^[0-9]{4}-(0[1-9]|1[0-2])$`)
	ownerName   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
)

// CheckPeriod returns period when it is "YYYY-MM" or empty (this month).
func CheckPeriod(period string) (string, error) {
	if period != "" && !periodShape.MatchString(period) {
		return "", fmt.Errorf("%w: period", ErrBadInput)
	}
	return period, nil
}

// OwnerOf makes the owner of a request from the kind ("user" or "org") and a name, or the instance for an empty kind.
func OwnerOf(kind, name string) (*ascv1.OwnerRef, error) {
	switch kind {
	case "":
		if name != "" {
			return nil, fmt.Errorf("%w: owner", ErrBadInput)
		}
		return &ascv1.OwnerRef{Kind: ascv1.OwnerKind_OWNER_KIND_INSTANCE}, nil
	case "user", "org":
		if !ownerName.MatchString(name) {
			return nil, fmt.Errorf("%w: owner name", ErrBadInput)
		}
		k := ascv1.OwnerKind_OWNER_KIND_USER
		if kind == "org" {
			k = ascv1.OwnerKind_OWNER_KIND_ORG
		}
		return &ascv1.OwnerRef{Kind: k, Name: strings.ToLower(name)}, nil
	}
	return nil, fmt.Errorf("%w: owner kind", ErrBadInput)
}

// FormatBytes writes a size: "1.5 GiB".
func FormatBytes(n uint64) string { return humanize.IBytes(n) }

// FormatUSD writes micro-dollars as dollars: two decimals, or four below one cent so that a small spend is not "$0.00".
func FormatUSD(micro uint64) string {
	whole, frac := micro/1_000_000, micro%1_000_000
	if micro != 0 && micro < 10_000 {
		return fmt.Sprintf("$%d.%04d", whole, frac/100)
	}
	// Round half up to cents.
	cents := (frac + 5_000) / 10_000
	if cents == 100 {
		whole, cents = whole+1, 0
	}
	return fmt.Sprintf("$%s.%02d", groupDigits(whole), cents)
}

func groupDigits(n uint64) string {
	s := strconv.FormatUint(n, 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// ParseUSD reads a dollar amount a person typed ("12", "12.5", "$1,200.25") as micro-dollars. Empty is 0.
func ParseUSD(s string) (uint64, error) {
	s = strings.TrimSpace(strings.ReplaceAll(strings.TrimPrefix(strings.TrimSpace(s), "$"), ",", ""))
	if s == "" {
		return 0, nil
	}
	whole, frac, _ := strings.Cut(s, ".")
	if whole == "" || len(whole) > 8 || len(frac) > 6 || !digits(whole) || (frac != "" && !digits(frac)) {
		return 0, fmt.Errorf("%w: amount", ErrBadInput)
	}
	w, _ := strconv.ParseUint(whole, 10, 64)
	if w > MaxQuotaUSD {
		return 0, fmt.Errorf("%w: amount", ErrBadInput)
	}
	f := uint64(0)
	if frac != "" {
		f, _ = strconv.ParseUint(frac+strings.Repeat("0", 6-len(frac)), 10, 64)
	}
	return w*1_000_000 + f, nil
}

func digits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// ParseCount reads a whole number a person typed. Empty is 0. max bounds it.
func ParseCount(s string, max uint64) (uint64, error) {
	s = strings.TrimSpace(strings.ReplaceAll(s, ",", ""))
	if s == "" {
		return 0, nil
	}
	if !digits(s) || len(s) > 12 {
		return 0, fmt.Errorf("%w: number", ErrBadInput)
	}
	n, _ := strconv.ParseUint(s, 10, 64)
	if n > max {
		return 0, fmt.Errorf("%w: number", ErrBadInput)
	}
	return n, nil
}

// ------------------------------------------------------------------------------------------------ usage

// Meter is one measure with its limit. Pct is 0..100 and 0 without a limit.
type Meter struct {
	Used  uint64
	Limit uint64
	Over  bool
	Pct   int
	kind  string
}

func (m Meter) text(n uint64) string {
	switch m.kind {
	case "bytes":
		return FormatBytes(n)
	case "usd":
		return FormatUSD(n)
	}
	return groupDigits(n)
}

// UsedText and LimitText write the figures of the meter in its unit (a size, dollars or minutes).
func (m Meter) UsedText() string  { return m.text(m.Used) }
func (m Meter) LimitText() string { return m.text(m.Limit) }

func meterOf(m *ascv1.UsageMeter, kind string) Meter {
	out := Meter{Used: m.GetUsed(), Limit: m.GetLimit(), Over: m.GetOver(), kind: kind}
	if out.Limit > 0 {
		switch {
		case out.Used >= out.Limit:
			out.Pct = 100
		case out.Used > math.MaxUint64/100:
			out.Pct = int(out.Used / (out.Limit / 100))
		default:
			out.Pct = int(out.Used * 100 / out.Limit)
		}
		if out.Pct > 100 {
			out.Pct = 100
		}
	}
	return out
}

// UsageRepoRow is one repository of the usage table.
type UsageRepoRow struct {
	Repo      string
	Storage   uint64
	Minutes   uint64
	Spend     uint64
	Bandwidth uint64
}

// StorageText, SpendText and BandwidthText write the figures of the row.
func (r UsageRepoRow) StorageText() string   { return FormatBytes(r.Storage) }
func (r UsageRepoRow) SpendText() string     { return FormatUSD(r.Spend) }
func (r UsageRepoRow) BandwidthText() string { return FormatBytes(r.Bandwidth) }

// BYOSpendText writes the spend on the owner's own keys.
func (p *UsagePage) BYOSpendText() string { return FormatUSD(p.BYOSpend) }

// UsagePage is the usage board of an owner for one month.
type UsagePage struct {
	Period      string
	Start       time.Time
	ResetsAt    time.Time
	Storage     Meter
	Minutes     Meter
	AISpend     Meter
	Bandwidth   Meter
	AIKeys      uint32
	BYOSpend    uint64
	BYOStorage  bool
	Empty       bool
	OverQuota   bool
	Repos       []UsageRepoRow
	ReposCapped bool
}

// LoadUsage asks for the usage of owner in period ("" is this month).
func LoadUsage(ctx context.Context, conn grpc.ClientConnInterface, owner *ascv1.OwnerRef, period string) (*UsagePage, error) {
	period, err := CheckPeriod(period)
	if err != nil {
		return nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	resp, err := ascv1.NewUsageClient(conn).GetUsage(cctx, &ascv1.GetUsageRequest{Owner: owner, Period: period})
	if err != nil {
		return nil, err
	}
	if !periodShape.MatchString(resp.GetPeriod()) || len(resp.GetRepos()) > MaxListItems {
		return nil, fmt.Errorf("%w: usage", ErrBadAnswer)
	}
	p := &UsagePage{
		Period: resp.GetPeriod(), Start: unixTime(resp.GetPeriodStart()), ResetsAt: unixTime(resp.GetResetsAt()),
		Storage: meterOf(resp.GetStorage(), "bytes"), Minutes: meterOf(resp.GetAgentMinutes(), "count"), AISpend: meterOf(resp.GetAiSpend(), "usd"),
		Bandwidth: meterOf(resp.GetBandwidth(), "bytes"), AIKeys: resp.GetAiKeys(), BYOSpend: resp.GetByoAiSpendMicroUsd(),
		BYOStorage: resp.GetByoStorage(), Empty: resp.GetEmpty(), OverQuota: resp.GetOverQuota(),
		ReposCapped: len(resp.GetRepos()) >= 200,
	}
	for _, r := range resp.GetRepos() {
		name := CleanText(r.GetRepo(), MaxTextBytes)
		if name == "" {
			return nil, fmt.Errorf("%w: usage repository", ErrBadAnswer)
		}
		p.Repos = append(p.Repos, UsageRepoRow{Repo: name, Storage: r.GetStorageBytes(), Minutes: r.GetAgentMinutes(), Spend: r.GetAiSpendMicroUsd(), Bandwidth: r.GetBandwidthBytes()})
	}
	return p, nil
}

// ------------------------------------------------------------------------------------------------ spend

// SpendGroups are the groupings a page offers, in the order it draws them.
var SpendGroups = []string{"key", "user", "repo", "day", "month"}

// SpendGroupOf reads the name of a grouping; an unknown or empty name is "key".
func SpendGroupOf(name string) (string, ascv1.SpendGroup) {
	switch name {
	case "user":
		return name, ascv1.SpendGroup_SPEND_GROUP_USER
	case "repo":
		return name, ascv1.SpendGroup_SPEND_GROUP_REPO
	case "day":
		return name, ascv1.SpendGroup_SPEND_GROUP_DAY
	case "month":
		return name, ascv1.SpendGroup_SPEND_GROUP_MONTH
	}
	return "key", ascv1.SpendGroup_SPEND_GROUP_KEY
}

// SpendRow is one row of the spend table.
type SpendRow struct {
	Group    string
	Label    string
	Requests uint64
	Input    uint64
	Output   uint64
	Cost     uint64
}

// CostText writes the cost of the row in dollars.
func (r SpendRow) CostText() string { return FormatUSD(r.Cost) }

// SpendTable is the AI spend of an owner, grouped.
type SpendTable struct {
	GroupBy string
	Rows    []SpendRow
	Total   SpendRow
}

func spendRowOf(r *ascv1.SpendRow) SpendRow {
	return SpendRow{
		Group: CleanText(r.GetGroup(), MaxTextBytes), Label: CleanText(r.GetLabel(), MaxTextBytes), Requests: r.GetRequests(),
		Input: r.GetInputTokens(), Output: r.GetOutputTokens(), Cost: r.GetCostMicroUsd(),
	}
}

// LoadSpend asks for the AI spend of owner between since and until (unix seconds, 0 is this month so far).
func LoadSpend(ctx context.Context, conn grpc.ClientConnInterface, owner *ascv1.OwnerRef, groupBy string, since, until int64) (*SpendTable, error) {
	name, group := SpendGroupOf(groupBy)
	if since < 0 || until < 0 || (since != 0 && until != 0 && until-since > MaxSpendDays*86400) {
		return nil, fmt.Errorf("%w: range", ErrBadInput)
	}
	cctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	resp, err := ascv1.NewUsageClient(conn).ListSpend(cctx, &ascv1.ListSpendRequest{Owner: owner, GroupBy: group, Since: since, Until: until})
	if err != nil {
		return nil, err
	}
	if len(resp.GetRows()) > 2*MaxListItems {
		return nil, fmt.Errorf("%w: spend", ErrBadAnswer)
	}
	t := &SpendTable{GroupBy: name, Total: spendRowOf(resp.GetTotal())}
	for _, r := range resp.GetRows() {
		t.Rows = append(t.Rows, spendRowOf(r))
	}
	return t, nil
}

// ------------------------------------------------------------------------------------------------ quota

// Quota is a set of limits. 0 is unlimited, or, in an override, inherits.
type Quota struct {
	StorageBytes   uint64
	AgentMinutes   uint64
	AISpendMicro   uint64
	BandwidthBytes uint64
}

func quotaOf(q *ascv1.UsageQuota) Quota {
	return Quota{StorageBytes: q.GetStorageBytes(), AgentMinutes: q.GetAgentMinutes(), AISpendMicro: q.GetAiSpendMicroUsd(), BandwidthBytes: q.GetBandwidthBytes()}
}

// StorageText, SpendText and BandwidthText write a limit, or "" for none.
func (q Quota) StorageText() string {
	if q.StorageBytes == 0 {
		return ""
	}
	return FormatBytes(q.StorageBytes)
}
func (q Quota) SpendText() string {
	if q.AISpendMicro == 0 {
		return ""
	}
	return FormatUSD(q.AISpendMicro)
}
func (q Quota) BandwidthText() string {
	if q.BandwidthBytes == 0 {
		return ""
	}
	return FormatBytes(q.BandwidthBytes)
}

func (q Quota) wire() *ascv1.UsageQuota {
	return &ascv1.UsageQuota{StorageBytes: q.StorageBytes, AgentMinutes: q.AgentMinutes, AiSpendMicroUsd: q.AISpendMicro, BandwidthBytes: q.BandwidthBytes}
}

// GiB is a size in whole GiB for a form field, rounded up; 0 for none.
func GiB(bytes uint64) uint64 { return (bytes + gib - 1) / gib }

// USDField writes micro-dollars the way a form field takes them: no sign, no grouping, no trailing zeros.
func USDField(micro uint64) string {
	if micro == 0 {
		return ""
	}
	s := fmt.Sprintf("%d.%06d", micro/1_000_000, micro%1_000_000)
	return strings.TrimRight(strings.TrimRight(s, "0"), ".")
}

// QuotaPage is what applies to an owner, the owner's own limits and the default of the server.
type QuotaPage struct {
	Effective Quota
	Override  Quota
	Default   Quota
}

func quotaPageOf(r *ascv1.GetQuotaResponse) *QuotaPage {
	return &QuotaPage{Effective: quotaOf(r.GetEffective()), Override: quotaOf(r.GetOverride()), Default: quotaOf(r.GetInstanceDefault())}
}

// LoadQuota asks for the limits of owner.
func LoadQuota(ctx context.Context, conn grpc.ClientConnInterface, owner *ascv1.OwnerRef) (*QuotaPage, error) {
	cctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	resp, err := ascv1.NewUsageClient(conn).GetQuota(cctx, &ascv1.GetQuotaRequest{Owner: owner})
	if err != nil {
		return nil, err
	}
	return quotaPageOf(resp), nil
}

// QuotaFromForm reads the four limit fields of a form (GiB, minutes, USD, GiB). An empty field is 0.
func QuotaFromForm(get func(string) string) (Quota, error) {
	var q Quota
	st, err := ParseCount(get("storage_gib"), MaxQuotaGiB)
	if err != nil {
		return q, err
	}
	mn, err := ParseCount(get("agent_minutes"), MaxQuotaMinutes)
	if err != nil {
		return q, err
	}
	sp, err := ParseUSD(get("ai_spend_usd"))
	if err != nil {
		return q, err
	}
	bw, err := ParseCount(get("bandwidth_gib"), MaxQuotaGiB)
	if err != nil {
		return q, err
	}
	return Quota{StorageBytes: st * gib, AgentMinutes: mn, AISpendMicro: sp, BandwidthBytes: bw * gib}, nil
}

// SetQuota sets the limits of owner (the default of the server for the instance). The same op id is the same change.
func SetQuota(ctx context.Context, conn grpc.ClientConnInterface, owner *ascv1.OwnerRef, q Quota, op string) (*QuotaPage, error) {
	cctx, cancel, err := opCtx(ctx, op)
	if err != nil {
		return nil, err
	}
	defer cancel()
	resp, err := ascv1.NewUsageClient(conn).SetQuota(cctx, &ascv1.SetQuotaRequest{Owner: owner, Quota: q.wire()})
	if err != nil {
		return nil, err
	}
	return quotaPageOf(resp.GetQuota()), nil
}
