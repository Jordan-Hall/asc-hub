// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascbyo

import (
	"context"
	"slices"
	"strings"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
)

// ListKeys returns the AI keys of the scope, oldest first.
func (s *Service) ListKeys(ctx context.Context, sc Scope) ([]*ascv1.AiKey, error) {
	if !sc.Valid() {
		return nil, failure(FailInvalid)
	}
	cctx, cancel := s.call(ctx, sc, "ListKeys", "", false)
	defer cancel()
	resp, err := s.api.AI.ListKeys(cctx, &ascv1.ListAiKeysRequest{Owner: sc.Owner()})
	if err != nil {
		return nil, done("list ai keys", sc, err)
	}
	out := make([]*ascv1.AiKey, 0, len(resp.GetKeys()))
	for _, k := range resp.GetKeys() {
		if k != nil && sc.Owns(k.GetOwner()) && ValidKeyID(k.GetId()) {
			out = append(out, k)
		}
	}
	slices.SortStableFunc(out, func(a, b *ascv1.AiKey) int { return int(a.GetCreated() - b.GetCreated()) })
	return out, nil
}

// GetKey returns one key and refuses it unless it belongs to the scope.
func (s *Service) GetKey(ctx context.Context, sc Scope, id string) (*ascv1.AiKey, error) {
	if !sc.Valid() || !ValidKeyID(id) {
		return nil, failure(FailNotFound)
	}
	cctx, cancel := s.call(ctx, sc, "GetKey", "", false)
	defer cancel()
	resp, err := s.api.AI.GetKey(cctx, &ascv1.GetAiKeyRequest{Id: id})
	if err != nil {
		return nil, done("get ai key", sc, err)
	}
	k := resp.GetKey()
	if k == nil || k.GetId() != id || !sc.Owns(k.GetOwner()) {
		return nil, failure(FailNotFound)
	}
	return k, nil
}

// CreateKey validates the input and stores a new, UNTESTED key.
func (s *Service) CreateKey(ctx context.Context, sc Scope, in *AIKeyInput, op string) (*ascv1.AiKey, FieldErrors, error) {
	if !sc.Valid() {
		return nil, nil, failure(FailInvalid)
	}
	if fe := in.Validate(true); len(fe) > 0 {
		return nil, fe, nil
	}
	provider, _ := ProviderFromForm(in.Provider)
	cctx, cancel := s.call(ctx, sc, "CreateKey", op, false)
	defer cancel()
	resp, err := s.api.AI.CreateKey(cctx, &ascv1.CreateAiKeyRequest{
		Owner:              sc.Owner(),
		Label:              in.Label,
		Provider:           provider,
		ApiKey:             in.APIKey,
		BaseUrl:            in.BaseURL,
		DefaultModel:       in.DefaultModel,
		MonthlyCapMicroUsd: in.CapMicro,
	})
	if err != nil {
		return nil, nil, done("create ai key", sc, err, in.secrets()...)
	}
	k := resp.GetKey()
	if k == nil || !ValidKeyID(k.GetId()) || !sc.Owns(k.GetOwner()) {
		// An answer about no key, or about another owner's, is a server bug; nothing of it is shown.
		return nil, nil, failure(FailInternal)
	}
	return k, nil, nil
}

// ownedKey checks the key that asc returned from a change of key id: it has to be there, be that key and
// belong to the scope. Anything else is not returned.
func ownedKey(sc Scope, id string, k *ascv1.AiKey) (*ascv1.AiKey, error) {
	if k == nil {
		return nil, failure(FailInternal)
	}
	if k.GetId() != id || !sc.Owns(k.GetOwner()) {
		return nil, failure(FailNotFound)
	}
	return k, nil
}

// UpdateKey changes the fields that differ from the stored key and replaces the secret when the input
// carries one. It reports changed=false when there was nothing to send.
func (s *Service) UpdateKey(ctx context.Context, sc Scope, id string, in *AIKeyInput, op string) (k *ascv1.AiKey, changed bool, fe FieldErrors, err error) {
	cur, err := s.GetKey(ctx, sc, id)
	if err != nil {
		return nil, false, nil, err
	}
	in.Provider = string(ProviderToForm(cur.GetProvider()))
	verr := in.Validate(false)
	// A key goes to the address it was stored for. Pointing a stored key at a new address (or at one for
	// the first time) would send it to whoever runs that address, so the form has to carry the key again.
	if in.BaseURL != cur.GetBaseUrl() && in.APIKey == "" {
		verr.Add("api_key", "ascbyo.ai.err.api_key")
	}
	if len(verr) > 0 {
		return cur, false, verr, nil
	}
	req := &ascv1.UpdateAiKeyRequest{Id: id}
	if in.Label != cur.GetLabel() {
		req.Label, changed = &in.Label, true
	}
	if in.APIKey != "" {
		req.ApiKey, changed = &in.APIKey, true
	}
	if in.BaseURL != cur.GetBaseUrl() {
		req.BaseUrl, changed = &in.BaseURL, true
	}
	if in.DefaultModel != cur.GetDefaultModel() {
		req.DefaultModel, changed = &in.DefaultModel, true
	}
	if in.CapMicro != cur.GetMonthlyCapMicroUsd() {
		req.MonthlyCapMicroUsd, changed = &in.CapMicro, true
	}
	if !changed {
		return cur, false, nil, nil
	}
	cctx, cancel := s.call(ctx, sc, "UpdateKey", op, false)
	defer cancel()
	resp, err := s.api.AI.UpdateKey(cctx, req)
	if err != nil {
		return nil, false, nil, done("update ai key", sc, err, in.secrets()...)
	}
	k, err = ownedKey(sc, id, resp.GetKey())
	if err != nil {
		return nil, false, nil, err
	}
	return k, true, nil, nil
}

// SetKeyDisabled disables a key or enables it again. Enabling moves the key to UNTESTED in asc only if
// the secret or address changed; a disabled key that was tested before is usable again at once.
func (s *Service) SetKeyDisabled(ctx context.Context, sc Scope, id string, disabled bool, op string) (*ascv1.AiKey, error) {
	if _, err := s.GetKey(ctx, sc, id); err != nil {
		return nil, err
	}
	cctx, cancel := s.call(ctx, sc, "SetKeyDisabled", op, false)
	defer cancel()
	resp, err := s.api.AI.UpdateKey(cctx, &ascv1.UpdateAiKeyRequest{Id: id, Disabled: &disabled})
	if err != nil {
		return nil, done("set ai key disabled", sc, err)
	}
	return ownedKey(sc, id, resp.GetKey())
}

// DeleteKey removes a key. asc refuses while a live run or token uses it.
func (s *Service) DeleteKey(ctx context.Context, sc Scope, id, op string) error {
	if _, err := s.GetKey(ctx, sc, id); err != nil {
		return err
	}
	cctx, cancel := s.call(ctx, sc, "DeleteKey", op, false)
	defer cancel()
	_, err := s.api.AI.DeleteKey(cctx, &ascv1.DeleteAiKeyRequest{Id: id})
	return done("delete ai key", sc, err)
}

// TestResult is the answer of a key test.
type TestResult struct {
	OK        bool
	LatencyMS uint32
	// Status is one of the fixed provider statuses; anything else is "bad_response".
	Status  string
	Message string // cleaned
	Key     *ascv1.AiKey
}

var testStatuses = map[string]bool{
	"ok": true, "unauthorized": true, "rate_limited": true, "unreachable": true, "blocked_address": true, "bad_response": true,
}

// TestKey makes asc send one tiny request to the provider with the key.
func (s *Service) TestKey(ctx context.Context, sc Scope, id, op string) (*TestResult, error) {
	if _, err := s.GetKey(ctx, sc, id); err != nil {
		return nil, err
	}
	cctx, cancel := s.call(ctx, sc, "TestKey", op, true)
	defer cancel()
	resp, err := s.api.AI.TestKey(cctx, &ascv1.TestAiKeyRequest{Id: id})
	if err != nil {
		return nil, done("test ai key", sc, err)
	}
	st := strings.ToLower(resp.GetProviderStatus())
	if !testStatuses[st] {
		st = "bad_response"
	}
	res := &TestResult{OK: resp.GetOk(), LatencyMS: resp.GetLatencyMs(), Status: st, Message: CleanText(resp.GetMessage(), 200)}
	if k := resp.GetKey(); k != nil && k.GetId() == id && sc.Owns(k.GetOwner()) {
		res.Key = k
	}
	return res, nil
}

// Model is a model a tested key can use, as shown.
type Model struct {
	ID            string
	DisplayName   string
	ContextTokens uint32
}

// maxModelsShown bounds the models taken from an answer.
const maxModelsShown = 200

// ListModels returns the models of a key that has been tested.
func (s *Service) ListModels(ctx context.Context, sc Scope, id string) ([]Model, error) {
	if _, err := s.GetKey(ctx, sc, id); err != nil {
		return nil, err
	}
	cctx, cancel := s.call(ctx, sc, "ListModels", "", true)
	defer cancel()
	resp, err := s.api.AI.ListModels(cctx, &ascv1.ListAiModelsRequest{Id: id})
	if err != nil {
		return nil, done("list ai models", sc, err)
	}
	var out []Model
	for _, m := range resp.GetModels() {
		if !modelRE.MatchString(m.GetId()) {
			continue
		}
		out = append(out, Model{ID: m.GetId(), DisplayName: CleanText(m.GetDisplayName(), 100), ContextTokens: m.GetContextTokens()})
		if len(out) >= maxModelsShown {
			break
		}
	}
	return out, nil
}

// UsageRow is one line of the usage table.
type UsageRow struct {
	Group        string
	Requests     uint64
	InputTokens  uint64
	OutputTokens uint64
	CostMicro    uint64
}

// Usage is the usage of a key, or of every key of the scope when keyID is empty, this month so far.
type Usage struct {
	Rows  []UsageRow
	Total UsageRow
}

// maxUsageRows bounds the rows taken from an answer.
const maxUsageRows = 400

// Usage reads the usage grouped by group. keyID "" sums every key of the scope.
func (s *Service) Usage(ctx context.Context, sc Scope, keyID string, group ascv1.AiUsageGroup) (*Usage, error) {
	if !sc.Valid() {
		return nil, failure(FailInvalid)
	}
	if keyID != "" {
		if _, err := s.GetKey(ctx, sc, keyID); err != nil {
			return nil, err
		}
	}
	cctx, cancel := s.call(ctx, sc, "Usage", "", false)
	defer cancel()
	resp, err := s.api.AI.Usage(cctx, &ascv1.AiUsageRequest{Owner: sc.Owner(), KeyId: keyID, GroupBy: group})
	if err != nil {
		return nil, done("ai usage", sc, err)
	}
	conv := func(r *ascv1.AiUsageRow) UsageRow {
		return UsageRow{
			Group: CleanText(r.GetGroup(), 120), Requests: r.GetRequests(), InputTokens: r.GetInputTokens(),
			OutputTokens: r.GetOutputTokens(), CostMicro: r.GetCostMicroUsd(),
		}
	}
	u := &Usage{Total: conv(resp.GetTotal())}
	for _, r := range resp.GetRows() {
		if len(u.Rows) >= maxUsageRows {
			break
		}
		u.Rows = append(u.Rows, conv(r))
	}
	return u, nil
}
