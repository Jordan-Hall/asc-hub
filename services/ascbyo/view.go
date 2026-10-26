// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascbyo

import (
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
