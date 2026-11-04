// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascbyo

import (
	"context"
	"strings"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
)

// ReplaceResult is the answer of a key replacement.
type ReplaceResult struct {
	// Replaced is false when the provider refused the new key; the old one is then kept and still works.
	Replaced  bool
	LatencyMS uint32
	// Status is one of the fixed provider statuses; anything else is "bad_response".
	Status  string
	Message string // cleaned, and dropped whole when it repeats the new key
	Key     *ascv1.AiKey
}

// ReplaceKey has asc try the new secret with the provider first and swap it in only when the provider accepts it.
func (s *Service) ReplaceKey(ctx context.Context, sc Scope, id, apiKey, op string) (*ReplaceResult, FieldErrors, error) {
	if _, err := s.GetKey(ctx, sc, id); err != nil {
		return nil, nil, err
	}
	apiKey = TrimSecret(apiKey)
	if len(apiKey) < MinAPIKeyBytes || len(apiKey) > MaxAPIKeyBytes || !visibleASCII(apiKey) {
		return nil, FieldErrors{"api_key": "ascbyo.ai.err.api_key"}, nil
	}
	cctx, cancel := s.call(ctx, sc, "ReplaceKey", op, true)
	defer cancel()
	resp, err := s.api.AI.ReplaceKey(cctx, &ascv1.ReplaceAiKeyRequest{Id: id, ApiKey: apiKey})
	if err != nil {
		return nil, nil, done("replace ai key", sc, err, apiKey)
	}
	st := strings.ToLower(resp.GetProviderStatus())
	if !testStatuses[st] {
		st = "bad_response"
	}
	res := &ReplaceResult{
		Replaced: resp.GetReplaced(), LatencyMS: resp.GetLatencyMs(), Status: st,
		Message: cleanText(resp.GetMessage(), 200, []string{apiKey}),
	}
	if k := resp.GetKey(); k != nil && k.GetId() == id && sc.Owns(k.GetOwner()) {
		res.Key = k
	}
	return res, nil, nil
}

// Feature is a feature that uses AI, by the name of its form and route.
type Feature string

const FeatureAgentRuns Feature = "agent_runs"

// Features lists every feature, in the order the page shows them.
var Features = []Feature{FeatureAgentRuns}

// FeatureFromForm maps a route or form value to the wire feature.
func FeatureFromForm(v string) (ascv1.AiFeature, bool) {
	switch Feature(v) {
	case FeatureAgentRuns:
		return ascv1.AiFeature_AI_FEATURE_AGENT_RUNS, true
	}
	return ascv1.AiFeature_AI_FEATURE_UNSPECIFIED, false
}

// FeatureToForm maps a wire feature to its form value, "" for one this hub does not know.
func FeatureToForm(f ascv1.AiFeature) Feature {
	switch f {
	case ascv1.AiFeature_AI_FEATURE_AGENT_RUNS:
		return FeatureAgentRuns
	}
	return ""
}

// ListFeatures returns the features this hub knows, with the setting that applies to the scope. A feature asc does
// not know yet is left out; one this hub does not know is dropped.
func (s *Service) ListFeatures(ctx context.Context, sc Scope) ([]*ascv1.AiFeatureSetting, error) {
	if !sc.Valid() {
		return nil, failure(FailInvalid)
	}
	cctx, cancel := s.call(ctx, sc, "ListFeatures", "", false)
	defer cancel()
	resp, err := s.api.AI.ListFeatures(cctx, &ascv1.ListAiFeaturesRequest{Owner: sc.Owner()})
	if err != nil {
		return nil, done("list ai features", sc, err)
	}
	byName := map[Feature]*ascv1.AiFeatureSetting{}
	for _, f := range resp.GetFeatures() {
		if n := FeatureToForm(f.GetFeature()); n != "" && byName[n] == nil {
			byName[n] = f
		}
	}
	out := make([]*ascv1.AiFeatureSetting, 0, len(byName))
	for _, n := range Features {
		if f := byName[n]; f != nil {
			out = append(out, f)
		}
	}
	return out, nil
}

// FeatureInput is the form of a feature's setting.
type FeatureInput struct {
	// KeyID is a key of the scope, or "" for the first usable key.
	KeyID    string
	CapUSD   string
	CapMicro uint64
}

// Validate checks the form and parses the cap.
func (in *FeatureInput) Validate() FieldErrors {
	fe := FieldErrors{}
	in.KeyID = strings.TrimSpace(in.KeyID)
	if in.KeyID != "" && !ValidKeyID(in.KeyID) {
		fe.Add("key_id", "ascbyo.ai.err.feature_key")
	}
	if m, ok := ParseUSD(in.CapUSD); ok {
		in.CapMicro = m
	} else {
		fe.Add("monthly_cap", "ascbyo.ai.err.cap")
	}
	return fe
}

// SetFeature sets the scope's own setting of a feature, or with clear removes it so the setting of the owner above
// applies. A key named must be one of the scope's own: the page offers no other, and asc checks again that the owner
// may use it.
func (s *Service) SetFeature(ctx context.Context, sc Scope, feature string, in *FeatureInput, clear bool, op string) (*ascv1.AiFeatureSetting, FieldErrors, error) {
	if !sc.Valid() {
		return nil, nil, failure(FailInvalid)
	}
	wf, ok := FeatureFromForm(feature)
	if !ok {
		return nil, nil, failure(FailNotFound)
	}
	req := &ascv1.SetAiFeatureRequest{Owner: sc.Owner(), Feature: wf, Clear: clear}
	if !clear {
		if fe := in.Validate(); len(fe) > 0 {
			return nil, fe, nil
		}
		if in.KeyID != "" {
			if _, err := s.GetKey(ctx, sc, in.KeyID); err != nil {
				if IsKind(err, FailNotFound) {
					return nil, FieldErrors{"key_id": "ascbyo.ai.err.feature_key"}, nil
				}
				return nil, nil, err
			}
		}
		req.KeyId, req.MonthlyCapMicroUsd = in.KeyID, in.CapMicro
	}
	cctx, cancel := s.call(ctx, sc, "SetFeature", op, false)
	defer cancel()
	resp, err := s.api.AI.SetFeature(cctx, req)
	if err != nil {
		return nil, nil, done("set ai feature", sc, err)
	}
	f := resp.GetFeature()
	if f == nil || f.GetFeature() != wf {
		return nil, nil, failure(FailInternal)
	}
	return f, nil, nil
}
