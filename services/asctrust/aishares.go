// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"google.golang.org/grpc"
)

// The AiKeyShares service of asc serve: sharing an AI key with an organization, a repository or a person, what each sharer
// spent, and the warning and stop rules of the monthly cap of a key. The owner of a key sees shares and use; the grantee
// sees a label, the provider and whether the key works, never the key. Everything a form sends is checked here first;
// everything that comes back is cleaned.

// FeatureAiKeyShares is what Version.features says when the server has the AiKeyShares service.
const FeatureAiKeyShares = "aikeyshares.v1"

// MaxShares bounds the shares one answer may list.
const MaxShares = 500

var (
	shareIDShape = regexp.MustCompile(`^as_[0-9A-Za-z]{20}$`)
	aiKeyIDShape = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
)

// GranteeKinds are what a key can be shared with.
var GranteeKinds = []string{"user", "org", "repo"}

func granteeOf(kind, name string) (*ascv1.OwnerRef, error) {
	switch kind {
	case "user", "org":
		if !ownerName.MatchString(name) {
			return nil, fmt.Errorf("%w: grantee", ErrBadInput)
		}
		k := ascv1.OwnerKind_OWNER_KIND_USER
		if kind == "org" {
			k = ascv1.OwnerKind_OWNER_KIND_ORG
		}
		return &ascv1.OwnerRef{Kind: k, Name: strings.ToLower(name)}, nil
	case "repo":
		if !repoNameShape.MatchString(name) {
			return nil, fmt.Errorf("%w: grantee repository", ErrBadInput)
		}
		return &ascv1.OwnerRef{Kind: ascv1.OwnerKind_OWNER_KIND_REPO, Name: strings.ToLower(name)}, nil
	}
	return nil, fmt.Errorf("%w: grantee kind", ErrBadInput)
}

func ownerKindName(k ascv1.OwnerKind) (string, bool) {
	switch k {
	case ascv1.OwnerKind_OWNER_KIND_USER:
		return "user", true
	case ascv1.OwnerKind_OWNER_KIND_ORG:
		return "org", true
	case ascv1.OwnerKind_OWNER_KIND_REPO:
		return "repo", true
	case ascv1.OwnerKind_OWNER_KIND_UNSPECIFIED, ascv1.OwnerKind_OWNER_KIND_INSTANCE:
	}
	return "", false
}

func checkKeyID(id string) error {
	if !aiKeyIDShape.MatchString(id) {
		return fmt.Errorf("%w: key id", ErrBadInput)
	}
	return nil
}

// ShareRow is one share of a key.
type ShareRow struct {
	ID          string
	GranteeKind string
	Grantee     string
	AllowUse    bool
	SeeSpend    bool
	Created     time.Time
	CreatedBy   string
	// The use of the grantee this period, from ShareUsage; zero without it.
	Requests uint64
	Cost     uint64
}

// CostText writes what the grantee's use cost.
func (s ShareRow) CostText() string { return FormatUSD(s.Cost) }

func shareOf(s *ascv1.AiKeyShare) (ShareRow, error) {
	kind, ok := ownerKindName(s.GetGrantee().GetKind())
	if !ok || !shareIDShape.MatchString(s.GetId()) {
		return ShareRow{}, fmt.Errorf("%w: share", ErrBadAnswer)
	}
	return ShareRow{
		ID: s.GetId(), GranteeKind: kind, Grantee: CleanText(s.GetGrantee().GetName(), MaxTextBytes), AllowUse: s.GetAllowUse(),
		SeeSpend: s.GetSeeSpend(), Created: unixTime(s.GetCreated()), CreatedBy: CleanText(s.GetCreatedBy(), 64),
	}, nil
}

// KeySharing is the sharing of one key: its shares, with what each used this month, and the use of the owner.
type KeySharing struct {
	Shares   []ShareRow
	OwnRows  ShareRow // the owner's own use; the last row of the answer without a share id
	HasOwn   bool
	Caps     KeyCaps
	CapsSeen bool
}

// KeyCaps are the warning and stop rules of the monthly cap of a key.
type KeyCaps struct {
	WarnPercent uint32
	StopAtCap   bool
	CapMicro    uint64
	SpentMicro  uint64
	Warned      bool
}

// CapText and SpentText write the cap and the spend, or "" for no cap.
func (c KeyCaps) CapText() string {
	if c.CapMicro == 0 {
		return ""
	}
	return FormatUSD(c.CapMicro)
}
func (c KeyCaps) SpentText() string { return FormatUSD(c.SpentMicro) }

func capsOf(c *ascv1.AiKeyCaps) KeyCaps {
	return KeyCaps{WarnPercent: c.GetWarnPercent(), StopAtCap: c.GetStopAtCap(), CapMicro: c.GetMonthlyCapMicroUsd(), SpentMicro: c.GetMonthSpentMicroUsd(), Warned: c.GetWarned()}
}

// LoadKeySharing asks for the shares of key, what each used this month, and the caps. A failure of the use or the caps
// leaves that part out: the shares are what the page is for.
func LoadKeySharing(ctx context.Context, conn grpc.ClientConnInterface, keyID string) (*KeySharing, error) {
	if err := checkKeyID(keyID); err != nil {
		return nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	client := ascv1.NewAiKeySharesClient(conn)
	list, err := client.ListShares(cctx, &ascv1.ListAiKeySharesRequest{KeyId: keyID})
	if err != nil {
		return nil, err
	}
	if len(list.GetShares()) > MaxShares {
		return nil, fmt.Errorf("%w: shares", ErrBadAnswer)
	}
	out := &KeySharing{}
	index := map[string]int{}
	for _, s := range list.GetShares() {
		row, err := shareOf(s)
		if err != nil {
			return nil, err
		}
		index[row.ID] = len(out.Shares)
		out.Shares = append(out.Shares, row)
	}
	if use, err := client.ShareUsage(cctx, &ascv1.AiKeyShareUsageRequest{KeyId: keyID}); err == nil {
		for _, u := range use.GetRows() {
			if i, ok := index[u.GetShareId()]; ok && u.GetShareId() != "" {
				out.Shares[i].Requests, out.Shares[i].Cost = u.GetRequests(), u.GetCostMicroUsd()
			} else if u.GetShareId() == "" {
				out.OwnRows, out.HasOwn = ShareRow{Requests: u.GetRequests(), Cost: u.GetCostMicroUsd()}, true
			}
		}
	}
	if caps, err := client.GetKeyCaps(cctx, &ascv1.GetAiKeyCapsRequest{KeyId: keyID}); err == nil {
		out.Caps, out.CapsSeen = capsOf(caps.GetCaps()), true
	}
	return out, nil
}

// ShareKey shares key with a grantee, or changes the permissions of a share it already has.
func ShareKey(ctx context.Context, conn grpc.ClientConnInterface, keyID, kind, name string, allowUse, seeSpend bool, op string) (*ShareRow, error) {
	if err := checkKeyID(keyID); err != nil {
		return nil, err
	}
	grantee, err := granteeOf(kind, name)
	if err != nil {
		return nil, err
	}
	cctx, cancel, err := opCtx(ctx, op)
	if err != nil {
		return nil, err
	}
	defer cancel()
	resp, err := ascv1.NewAiKeySharesClient(conn).ShareKey(cctx, &ascv1.ShareAiKeyRequest{KeyId: keyID, Grantee: grantee, AllowUse: allowUse, SeeSpend: seeSpend})
	if err != nil {
		return nil, err
	}
	row, err := shareOf(resp.GetShare())
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// RevokeShare removes a share. The call is idempotent.
func RevokeShare(ctx context.Context, conn grpc.ClientConnInterface, keyID, shareID, op string) error {
	if err := checkKeyID(keyID); err != nil {
		return err
	}
	if !shareIDShape.MatchString(shareID) {
		return fmt.Errorf("%w: share id", ErrBadInput)
	}
	cctx, cancel, err := opCtx(ctx, op)
	if err != nil {
		return err
	}
	defer cancel()
	_, err = ascv1.NewAiKeySharesClient(conn).RevokeShare(cctx, &ascv1.RevokeAiKeyShareRequest{KeyId: keyID, ShareId: shareID})
	return err
}

// SetKeyCaps sets the warning percentage (0 is none, 1 to 100) and the stop rule of key.
func SetKeyCaps(ctx context.Context, conn grpc.ClientConnInterface, keyID string, warnPercent string, stopAtCap bool, op string) (*KeyCaps, error) {
	if err := checkKeyID(keyID); err != nil {
		return nil, err
	}
	pct, err := ParseCount(warnPercent, 100)
	if err != nil {
		return nil, fmt.Errorf("%w: warning percentage", ErrBadInput)
	}
	cctx, cancel, err := opCtx(ctx, op)
	if err != nil {
		return nil, err
	}
	defer cancel()
	resp, err := ascv1.NewAiKeySharesClient(conn).SetKeyCaps(cctx, &ascv1.SetAiKeyCapsRequest{KeyId: keyID, WarnPercent: uint32(pct), StopAtCap: stopAtCap})
	if err != nil {
		return nil, err
	}
	c := capsOf(resp.GetCaps())
	return &c, nil
}

// SharedKeyRow is a key somebody shared with the owner of the page.
type SharedKeyRow struct {
	ShareID   string
	KeyID     string
	Label     string
	Provider  string
	OwnerKind string
	Owner     string
	Usable    bool
	AllowUse  bool
	SeeSpend  bool
	Spent     uint64
}

// SpentText writes what the grantee's own use cost this month.
func (r SharedKeyRow) SpentText() string { return FormatUSD(r.Spent) }

// ListSharedWithMe lists the keys shared with the account or organization named by kind ("user" or "org") and name.
func ListSharedWithMe(ctx context.Context, conn grpc.ClientConnInterface, kind, name string) ([]SharedKeyRow, error) {
	grantee, err := granteeOf(kind, name)
	if err != nil || kind == "repo" {
		return nil, fmt.Errorf("%w: grantee", ErrBadInput)
	}
	cctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	resp, err := ascv1.NewAiKeySharesClient(conn).ListSharedWithMe(cctx, &ascv1.ListSharedAiKeysRequest{Grantee: grantee})
	if err != nil {
		return nil, err
	}
	if len(resp.GetKeys()) > MaxShares {
		return nil, fmt.Errorf("%w: shared keys", ErrBadAnswer)
	}
	out := make([]SharedKeyRow, 0, len(resp.GetKeys()))
	for _, k := range resp.GetKeys() {
		ok, valid := ownerKindName(k.GetOwner().GetKind())
		if !valid || !shareIDShape.MatchString(k.GetShareId()) {
			return nil, fmt.Errorf("%w: shared key", ErrBadAnswer)
		}
		out = append(out, SharedKeyRow{
			ShareID: k.GetShareId(), KeyID: CleanText(k.GetKeyId(), 64), Label: CleanText(k.GetLabel(), MaxTextBytes),
			Provider: strings.ToLower(strings.TrimPrefix(k.GetProvider().String(), "AI_PROVIDER_")), OwnerKind: ok,
			Owner: CleanText(k.GetOwner().GetName(), MaxTextBytes), Usable: k.GetUsable(), AllowUse: k.GetAllowUse(), SeeSpend: k.GetSeeSpend(),
			Spent: k.GetMonthSpentMicroUsd(),
		})
	}
	return out, nil
}
