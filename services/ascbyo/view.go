// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascbyo

import (
	"strconv"
	"strings"
	"time"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/timeutil"
)

// Tone is the colour family of a state badge. The templates map it to a class; the badge always carries
// the state name as text as well, so colour is never the only signal.
type Tone string

const (
	ToneNeutral Tone = "neutral"
	ToneGood    Tone = "good"
	ToneWarn    Tone = "warn"
	ToneBad     Tone = "bad"
)

// TargetView is a storage target as a page shows it. It holds nothing secret: asc never returns one.
type TargetView struct {
	ID          string
	Name        string
	Endpoint    string
	Region      string
	Bucket      string
	Prefix      string
	Addressing  string // form value
	KeyHint     string
	HasSecret   bool
	StateKey    string
	StateName   string // slug, for tests and CSS
	Tone        Tone
	LastError   string
	Created     timeutil.TimeStamp
	Updated     timeutil.TimeStamp
	VerifiedAt  timeutil.TimeStamp
	Repos       uint32
	KeyVersion  uint32
	CanVerify   bool
	CanEnable   bool
	CanDisable  bool
	IsEnabled   bool
	NeedsVerify bool
}

// maxHintBytes bounds a hint taken from asc.
const maxHintBytes = 16

// TargetViewOf converts a target.
func TargetViewOf(t *ascv1.StorageTarget) TargetView {
	v := TargetView{
		ID: t.GetId(), Name: CleanText(t.GetName(), 64), Endpoint: CleanText(t.GetEndpoint(), MaxEndpointBytes),
		Region: CleanText(t.GetRegion(), MaxRegionBytes), Bucket: CleanText(t.GetBucket(), 64), Prefix: CleanText(t.GetPrefix(), MaxPrefixBytes),
		Addressing: AddressingToForm(t.GetAddressing()), KeyHint: CleanText(t.GetAccessKeyHint(), maxHintBytes), HasSecret: t.GetHasSecret(),
		LastError: CleanText(t.GetLastError(), 300), Repos: t.GetRepos(), KeyVersion: t.GetKeyVersion(),
		Created: timeutil.TimeStamp(t.GetCreated()), Updated: timeutil.TimeStamp(t.GetUpdated()), VerifiedAt: timeutil.TimeStamp(t.GetVerifiedAt()),
	}
	switch t.GetState() {
	case ascv1.StorageTargetState_STORAGE_TARGET_STATE_PENDING:
		v.StateName, v.StateKey, v.Tone, v.NeedsVerify = "pending", "ascbyo.storage.state.pending", ToneWarn, true
	case ascv1.StorageTargetState_STORAGE_TARGET_STATE_VERIFIED:
		v.StateName, v.StateKey, v.Tone, v.CanEnable = "verified", "ascbyo.storage.state.verified", ToneNeutral, true
	case ascv1.StorageTargetState_STORAGE_TARGET_STATE_ENABLED:
		v.StateName, v.StateKey, v.Tone, v.CanDisable, v.IsEnabled = "enabled", "ascbyo.storage.state.enabled", ToneGood, true, true
	case ascv1.StorageTargetState_STORAGE_TARGET_STATE_DISABLED:
		v.StateName, v.StateKey, v.Tone, v.CanEnable = "disabled", "ascbyo.storage.state.disabled", ToneNeutral, true
	case ascv1.StorageTargetState_STORAGE_TARGET_STATE_FAILED:
		v.StateName, v.StateKey, v.Tone, v.NeedsVerify = "failed", "ascbyo.storage.state.failed", ToneBad, true
	default:
		v.StateName, v.StateKey, v.Tone = "unknown", "ascbyo.storage.state.unknown", ToneNeutral
	}
	v.CanVerify = true
	return v
}

// Location is where a target keeps objects, as its card shows it: the host of the endpoint, then the bucket and the
// folder ("s3.example.com / bucket/folder").
func (v TargetView) Location() string {
	host := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(v.Endpoint, "https://"), "http://"), "/")
	where := v.Bucket
	if v.Prefix != "" {
		where += "/" + v.Prefix
	}
	if host == "" {
		return where
	}
	return host + " / " + where
}

// ChipClass is the colour of the state chip of a target card: in use is ok, verified is info, not verified is warn and
// failed is err; the others are plain.
func (v TargetView) ChipClass() string {
	switch v.StateName {
	case "enabled":
		return "ds-chip-ok"
	case "verified":
		return "ds-chip-info"
	case "pending":
		return "ds-chip-warn"
	case "failed":
		return "ds-chip-err"
	}
	return ""
}

// RepoCount is Repos as an int, the count the plural forms of the locale take.
func (v TargetView) RepoCount() int {
	return int(v.Repos)
}

// TargetViews converts a list.
func TargetViews(ts []*ascv1.StorageTarget) []TargetView {
	out := make([]TargetView, 0, len(ts))
	for _, t := range ts {
		out = append(out, TargetViewOf(t))
	}
	return out
}

// KeyView is an AI key as a page shows it. It holds nothing secret: asc returns a hint and a flag only.
type KeyView struct {
	ID           string
	Label        string
	Provider     Provider
	ProviderKey  string // translation key of the provider name
	BaseURL      string
	DefaultModel string
	KeyHint      string
	HasKey       bool
	StateKey     string
	StateName    string
	Tone         Tone
	LastError    string
	Created      timeutil.TimeStamp
	Updated      timeutil.TimeStamp
	TestedAt     timeutil.TimeStamp
	KeyVersion   uint32
	// CapMicro and SpentMicro are the monthly cap and this month's spend; Cap and Spent are the same
	// formatted. Percent is 0..100 and only meaningful when HasCap.
	CapMicro   uint64
	SpentMicro uint64
	Cap        string
	CapInput   string
	Spent      string
	HasCap     bool
	Percent    int
	Disabled   bool
	CanDisable bool
	CanEnable  bool
	NeedsTest  bool
	// AddedByID and AddedBy are the hub account that added the key and its login then; 0 and "" when asc
	// does not know (a key added before it kept this, or by a command line client).
	AddedByID int64
	AddedBy   string
}

// KeyViewOf converts a key.
func KeyViewOf(k *ascv1.AiKey) KeyView {
	v := KeyView{
		ID: k.GetId(), Label: CleanText(k.GetLabel(), MaxLabelBytes), Provider: ProviderToForm(k.GetProvider()),
		BaseURL: CleanText(k.GetBaseUrl(), MaxEndpointBytes), DefaultModel: CleanText(k.GetDefaultModel(), MaxModelBytes),
		KeyHint: CleanText(k.GetKeyHint(), maxHintBytes), HasKey: k.GetHasKey(), LastError: CleanText(k.GetLastError(), 300),
		Created: timeutil.TimeStamp(k.GetCreated()), Updated: timeutil.TimeStamp(k.GetUpdated()), TestedAt: timeutil.TimeStamp(k.GetTestedAt()),
		KeyVersion: k.GetKeyVersion(), CapMicro: k.GetMonthlyCapMicroUsd(), SpentMicro: k.GetMonthSpentMicroUsd(),
	}
	v.AddedByID, v.AddedBy = AddedByOf(k.GetCreatedBy())
	v.ProviderKey = "ascbyo.ai.provider." + string(v.Provider)
	if v.Provider == "" {
		v.ProviderKey = "ascbyo.ai.provider.unknown"
	}
	v.Spent = FormatUSD(v.SpentMicro)
	v.CapInput = CapUSDInput(v.CapMicro)
	if v.CapMicro > 0 {
		v.HasCap = true
		v.Cap = FormatUSD(v.CapMicro)
		v.Percent = int(min(v.SpentMicro, v.CapMicro) * 100 / v.CapMicro)
	}
	switch k.GetState() {
	case ascv1.AiKeyState_AI_KEY_STATE_UNTESTED:
		v.StateName, v.StateKey, v.Tone, v.NeedsTest, v.CanDisable = "untested", "ascbyo.ai.state.untested", ToneWarn, true, true
	case ascv1.AiKeyState_AI_KEY_STATE_ACTIVE:
		v.StateName, v.StateKey, v.Tone, v.CanDisable = "active", "ascbyo.ai.state.active", ToneGood, true
	case ascv1.AiKeyState_AI_KEY_STATE_FAILED:
		v.StateName, v.StateKey, v.Tone, v.NeedsTest, v.CanDisable = "failed", "ascbyo.ai.state.failed", ToneBad, true, true
	case ascv1.AiKeyState_AI_KEY_STATE_CAPPED:
		v.StateName, v.StateKey, v.Tone, v.CanDisable = "capped", "ascbyo.ai.state.capped", ToneWarn, true
	case ascv1.AiKeyState_AI_KEY_STATE_DISABLED:
		v.StateName, v.StateKey, v.Tone, v.Disabled, v.CanEnable = "disabled", "ascbyo.ai.state.disabled", ToneNeutral, true, true
	default:
		v.StateName, v.StateKey, v.Tone = "unknown", "ascbyo.ai.state.unknown", ToneNeutral
	}
	return v
}

// AddedByOf reads the person asc recorded as having added a key. A call of the hub made for a person is recorded
// as "<account id>:<login>"; anything else (the hub's own credential, a command line token) names no account here.
func AddedByOf(createdBy string) (int64, string) {
	id, login, ok := strings.Cut(createdBy, ":")
	if !ok || id == "" || len(id) > 19 || login == "" || len(login) > MaxScopeNameBytes || hasControl(login) {
		return 0, ""
	}
	for i := range len(id) {
		if c := id[i]; c < '0' || c > '9' {
			return 0, ""
		}
	}
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n <= 0 {
		return 0, ""
	}
	return n, login
}

// MeterClass is the colour of a spend meter: amber from 80 percent of the cap, red at the cap.
func MeterClass(percent int) string {
	switch {
	case percent >= 100:
		return "asc-meter-bad"
	case percent >= 80:
		return "asc-meter-warn"
	}
	return ""
}

// MeterClass is the colour of the key's spend meter.
func (v KeyView) MeterClass() string { return MeterClass(v.Percent) }

// KeyViews converts a list.
func KeyViews(ks []*ascv1.AiKey) []KeyView {
	out := make([]KeyView, 0, len(ks))
	for _, k := range ks {
		out = append(out, KeyViewOf(k))
	}
	return out
}

// UsageRowView is a usage row with its numbers formatted.
type UsageRowView struct {
	Group        string
	Requests     string
	InputTokens  string
	OutputTokens string
	Cost         string
}

// UsageViews formats rows for the table.
func UsageViews(rows []UsageRow) []UsageRowView {
	out := make([]UsageRowView, 0, len(rows))
	for _, r := range rows {
		out = append(out, UsageRowViewOf(r))
	}
	return out
}

// UsageRowViewOf formats one row.
func UsageRowViewOf(r UsageRow) UsageRowView {
	return UsageRowView{
		Group: r.Group, Requests: FormatCount(r.Requests), InputTokens: FormatCount(r.InputTokens),
		OutputTokens: FormatCount(r.OutputTokens), Cost: FormatUSD(r.CostMicro),
	}
}

// FeatureView is a feature that uses AI as the page of a scope shows it.
type FeatureView struct {
	Name    Feature
	NameKey string // translation key of the name
	HintKey string // translation key of what it does
	// Own is whether the setting that applies is the scope's own; SetBy names the owner above whose setting
	// applies otherwise ("" when no owner set one).
	Own   bool
	SetBy string
	// KeyID is the key the setting chose, "" for the first usable key.
	KeyID string
	// Key is the key the feature uses now, nil when there is none; KeyOwned is whether it is one of the scope's own.
	Key      *KeyView
	KeyOwned bool
	// CapMicro and SpentMicro are the feature's monthly cap and this month's spend under the setting.
	CapMicro   uint64
	SpentMicro uint64
	Cap        string
	CapInput   string
	Spent      string
	HasCap     bool
	Percent    int
	Paused     bool
	// PausedBy names the owner above whose cap paused the feature ("instance" for the instance); "" when the
	// feature is not paused or the scope's own cap paused it.
	PausedBy  string
	ResumesAt timeutil.TimeStamp
	Updated   timeutil.TimeStamp
}

// OwnerLabel is how a page names an owner: the user or organisation name, "owner/repo", or "" for the instance.
func OwnerLabel(o *ascv1.OwnerRef) string {
	return CleanText(o.GetName(), MaxScopeNameBytes)
}

// FeatureViewOf converts a feature for the page of sc.
func FeatureViewOf(sc Scope, f *ascv1.AiFeatureSetting) FeatureView {
	v := FeatureView{
		Name: FeatureToForm(f.GetFeature()), KeyID: f.GetKeyId(), CapMicro: f.GetMonthlyCapMicroUsd(),
		SpentMicro: f.GetMonthSpentMicroUsd(), Paused: f.GetPaused(), ResumesAt: timeutil.TimeStamp(f.GetResumesAt()),
		Updated: timeutil.TimeStamp(f.GetUpdated()),
	}
	if !ValidKeyID(v.KeyID) {
		v.KeyID = ""
	}
	if v.Paused && v.ResumesAt == 0 {
		// asc says when; an answer without it resumes, like every cap, at the start of the next month (UTC).
		now := time.Now().UTC()
		v.ResumesAt = timeutil.TimeStamp(time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC).Unix())
	}
	v.NameKey = "ascbyo.ai.feature." + string(v.Name)
	v.HintKey = v.NameKey + ".hint"
	if by := f.GetSetBy(); by != nil {
		v.Own = sc.Owns(by)
		if !v.Own {
			v.SetBy = OwnerLabel(by)
			if by.GetKind() == ascv1.OwnerKind_OWNER_KIND_INSTANCE {
				v.SetBy = "instance"
			}
		}
	}
	if by := f.GetPausedBy(); v.Paused && by != nil && !sc.Owns(by) {
		v.PausedBy = OwnerLabel(by)
		if by.GetKind() == ascv1.OwnerKind_OWNER_KIND_INSTANCE || v.PausedBy == "" {
			v.PausedBy = "instance"
		}
	}
	if k := f.GetKey(); k != nil && ValidKeyID(k.GetId()) {
		kv := KeyViewOf(k)
		v.Key, v.KeyOwned = &kv, sc.Owns(k.GetOwner())
	}
	v.Spent = FormatUSD(v.SpentMicro)
	v.CapInput = CapUSDInput(v.CapMicro)
	if v.CapMicro > 0 {
		v.HasCap = true
		v.Cap = FormatUSD(v.CapMicro)
		v.Percent = int(min(v.SpentMicro, v.CapMicro) * 100 / v.CapMicro)
	}
	return v
}

// MeterClass is the colour of the feature's spend meter.
func (v FeatureView) MeterClass() string { return MeterClass(v.Percent) }

// FeatureViews converts a list.
func FeatureViews(sc Scope, fs []*ascv1.AiFeatureSetting) []FeatureView {
	out := make([]FeatureView, 0, len(fs))
	for _, f := range fs {
		out = append(out, FeatureViewOf(sc, f))
	}
	return out
}
