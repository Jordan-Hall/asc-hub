// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/netip"
	"strings"

	"forgejo.org/models/db"
	"forgejo.org/modules/timeutil"
)

// AscOrgDomain is the domain an organization claims. It is verified when a DNS TXT record under that domain holds the
// token the hub made: the organization proves it controls the domain, and gets the verified badge. Only one organization
// can hold a verified domain.
type AscOrgDomain struct {
	ID           int64              `xorm:"pk autoincr"`
	OrgID        int64              `xorm:"UNIQUE NOT NULL"`
	Domain       string             `xorm:"VARCHAR(253) INDEX NOT NULL"`
	Token        string             `xorm:"VARCHAR(64) NOT NULL"`
	VerifiedUnix timeutil.TimeStamp `xorm:"NOT NULL DEFAULT 0"`
	CheckedUnix  timeutil.TimeStamp `xorm:"NOT NULL DEFAULT 0"`
}

func init() {
	db.RegisterModel(new(AscOrgDomain))
}

// TableName is the table of the model.
func (AscOrgDomain) TableName() string { return "asc_org_domain" }

// ErrAscDomainTaken is returned when another organization already proved the domain.
var ErrAscDomainTaken = errors.New("another organization verified this domain")

// NormalizeAscDomain lowercases a domain and refuses what cannot be a public domain: an address, a name without a dot,
// a name of this machine or network, or a name with characters that do not belong in one.
func NormalizeAscDomain(s string) (string, error) {
	d := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), ".")
	if len(d) < 4 || len(d) > 253 || !strings.Contains(d, ".") {
		return "", errors.New("enter a domain such as example.org")
	}
	if _, err := netip.ParseAddr(d); err == nil {
		return "", errors.New("enter a domain name, not an address")
	}
	for _, bad := range []string{"localhost", "local", "internal", "test", "invalid", "example"} {
		if d == bad || strings.HasSuffix(d, "."+bad) {
			return "", errors.New("this is not a public domain")
		}
	}
	for _, label := range strings.Split(d, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", errors.New("enter a domain such as example.org")
		}
		for _, c := range label {
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return "", errors.New("use letters, digits and dashes only")
			}
		}
	}
	return d, nil
}

// GetAscOrgDomain returns the claim of the organization, or nil.
func GetAscOrgDomain(ctx context.Context, orgID int64) (*AscOrgDomain, error) {
	d := new(AscOrgDomain)
	has, err := db.GetEngine(ctx).Where("org_id = ?", orgID).Get(d)
	if err != nil || !has {
		return nil, err
	}
	return d, nil
}

// SetAscOrgDomain claims a domain for the organization with a new token; a claim of another domain replaces the old one.
func SetAscOrgDomain(ctx context.Context, orgID int64, domain string) (*AscOrgDomain, error) {
	domain, err := NormalizeAscDomain(domain)
	if err != nil {
		return nil, err
	}
	var tok [16]byte
	if _, err := rand.Read(tok[:]); err != nil {
		return nil, err
	}
	d := &AscOrgDomain{OrgID: orgID, Domain: domain, Token: hex.EncodeToString(tok[:])}
	err = db.WithTx(ctx, func(ctx context.Context) error {
		if _, err := db.GetEngine(ctx).Where("org_id = ?", orgID).Delete(new(AscOrgDomain)); err != nil {
			return err
		}
		_, err := db.GetEngine(ctx).Insert(d)
		return err
	})
	return d, err
}

// ClearAscOrgDomain removes the claim.
func ClearAscOrgDomain(ctx context.Context, orgID int64) error {
	_, err := db.GetEngine(ctx).Where("org_id = ?", orgID).Delete(new(AscOrgDomain))
	return err
}

// MarkAscOrgDomainChecked records a check; verified true sets the badge. A domain that another organization already
// holds verified is refused.
func MarkAscOrgDomainChecked(ctx context.Context, d *AscOrgDomain, verified bool) error {
	now := timeutil.TimeStampNow()
	d.CheckedUnix = now
	if verified {
		taken, err := db.GetEngine(ctx).Where("domain = ? AND org_id != ? AND verified_unix > 0", d.Domain, d.OrgID).Exist(new(AscOrgDomain))
		if err != nil {
			return err
		}
		if taken {
			return ErrAscDomainTaken
		}
		if d.VerifiedUnix == 0 {
			d.VerifiedUnix = now
		}
	} else {
		d.VerifiedUnix = 0
	}
	_, err := db.GetEngine(ctx).ID(d.ID).Cols("checked_unix", "verified_unix").Update(d)
	return err
}

// VerifiedAscOrgDomains returns, for the organizations given, the domain each one proved.
func VerifiedAscOrgDomains(ctx context.Context, orgIDs []int64) (map[int64]string, error) {
	out := map[int64]string{}
	if len(orgIDs) == 0 {
		return out, nil
	}
	var rows []*AscOrgDomain
	if err := db.GetEngine(ctx).Where("verified_unix > 0").In("org_id", orgIDs).Find(&rows); err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.OrgID] = r.Domain
	}
	return out, nil
}

// AscOrgDomainsToCheck lists the verified claims that are due for a new look.
func AscOrgDomainsToCheck(ctx context.Context, olderThan timeutil.TimeStamp) ([]*AscOrgDomain, error) {
	var rows []*AscOrgDomain
	return rows, db.GetEngine(ctx).Where("verified_unix > 0 AND checked_unix < ?", olderThan).Limit(200).Find(&rows)
}
