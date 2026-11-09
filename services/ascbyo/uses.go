// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascbyo

import (
	"context"
	"regexp"
	"time"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
)

// KeyUsesPage is how many uses of a key one page of its Activity section lists, newest first.
const KeyUsesPage = 50

// usesTokenRE is the page token asc hands out for the uses of a key: "o" and an offset. Anything else is refused
// before asc is asked.
var usesTokenRE = regexp.MustCompile(`^o[1-9][0-9]{0,5}$`)

// ValidUsesToken reports whether t is empty (the first page) or a page token of the uses of a key.
func ValidUsesToken(t string) bool { return t == "" || usesTokenRE.MatchString(t) }

// KeyUse is one use of a key as asc summed it: an agent run, one person's review summaries of one change, the
// tests of one day, or other calls. Every text is cleaned.
type KeyUse struct {
	Kind ascv1.AiActivityKind
	// Subject is the run id, the 32 letter change id or the UTC day, by kind; "" for other calls.
	Subject string
	// Actor is the handle of the person who started the run or asked, "" when none.
	Actor, Repo, Agent, Model string
	FirstAt, LastAt           time.Time
	Requests, InputTokens     uint64
	OutputTokens, CostMicro   uint64
	Unpriced                  uint64
}

// KeyUses is one page of the uses of a key. Next is the token of the next page, "" on the last.
type KeyUses struct {
	Rows []KeyUse
	Next string
}

var (
	runSubjectRE = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
	daySubjectRE = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
	changeRE     = regexp.MustCompile(`^[k-z]{32}$`)
	handleRE     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	repoNameRE   = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}/[A-Za-z0-9._-]{1,100}$`)
)

// useOf cleans one row from asc. A subject that does not match its kind is dropped, and a kind the hub does not
// know is shown as other calls, so a newer asc never puts text the page would link.
func useOf(a *ascv1.AiActivity) KeyUse {
	u := KeyUse{
		Kind: a.GetKind(), Agent: CleanText(a.GetAgent(), 64),
		Model: CleanText(a.GetModel(), 120), Requests: a.GetRequests(), InputTokens: a.GetInputTokens(),
		OutputTokens: a.GetOutputTokens(), CostMicro: a.GetCostMicroUsd(), Unpriced: a.GetUnpricedRequests(),
	}
	if handleRE.MatchString(a.GetActor()) {
		u.Actor = a.GetActor()
	}
	if repoNameRE.MatchString(a.GetRepo()) {
		u.Repo = a.GetRepo()
	}
	if a.GetFirstAt() > 0 {
		u.FirstAt = time.Unix(a.GetFirstAt(), 0).UTC()
	}
	if a.GetLastAt() > 0 {
		u.LastAt = time.Unix(a.GetLastAt(), 0).UTC()
	}
	s := a.GetSubject()
	switch u.Kind {
	case ascv1.AiActivityKind_AI_ACTIVITY_KIND_AGENT_RUN:
		if runSubjectRE.MatchString(s) {
			u.Subject = s
		}
	case ascv1.AiActivityKind_AI_ACTIVITY_KIND_REVIEW_SUMMARY:
		if changeRE.MatchString(s) {
			u.Subject = s
		}
	case ascv1.AiActivityKind_AI_ACTIVITY_KIND_KEY_TEST:
		if daySubjectRE.MatchString(s) {
			u.Subject = s
		}
	default:
		u.Kind = ascv1.AiActivityKind_AI_ACTIVITY_KIND_OTHER
	}
	return u
}

// KeyUses returns one page of the uses of a key of the scope: who used it, for which run, change or test, and what
// it cost. The key is checked to be the scope's first, so a key id of another owner reads nothing. An asc without
// the call answers FailUnimplemented.
func (s *Service) KeyUses(ctx context.Context, sc Scope, keyID, token string) (*KeyUses, error) {
	if !sc.Valid() || !ValidKeyID(keyID) {
		return nil, failure(FailNotFound)
	}
	if !ValidUsesToken(token) {
		return nil, failure(FailInvalid)
	}
	if _, err := s.GetKey(ctx, sc, keyID); err != nil {
		return nil, err
	}
	cctx, cancel := s.call(ctx, sc, "KeyUses", "", false)
	defer cancel()
	resp, err := s.api.AI.ListActivity(cctx, &ascv1.ListAiActivityRequest{
		Owner: sc.Owner(), KeyId: keyID, PageSize: KeyUsesPage, PageToken: token,
	})
	if err != nil {
		return nil, done("ai key uses", sc, err)
	}
	out := &KeyUses{Rows: make([]KeyUse, 0, min(len(resp.GetItems()), KeyUsesPage))}
	for _, a := range resp.GetItems() {
		if len(out.Rows) >= KeyUsesPage {
			break
		}
		out.Rows = append(out.Rows, useOf(a))
	}
	if ValidUsesToken(resp.GetNextPageToken()) {
		out.Next = resp.GetNextPageToken()
	}
	return out, nil
}
