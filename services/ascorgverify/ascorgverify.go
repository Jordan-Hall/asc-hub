// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package ascorgverify proves that an organization controls the domain it claims: the hub makes a token, the
// organization puts it in a DNS TXT record under _asc-hub.<domain>, and the hub reads that record. A verified
// organization gets the badge on its page and in the lists; the proof is looked at again every day, so a domain that is
// sold or a record that is removed takes the badge away.
package ascorgverify

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"

	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/modules/timeutil"
)

const (
	recordPrefix = "asc-hub-verify="
	lookupTime   = 8 * time.Second
	recheckAfter = 24 * time.Hour
)

// lookupTXT reads the TXT records of a name; tests replace it.
var lookupTXT = func(ctx context.Context, name string) ([]string, error) {
	return (&net.Resolver{}).LookupTXT(ctx, name)
}

// UseLookup replaces the DNS lookup and returns the function that restores it.
func UseLookup(f func(ctx context.Context, name string) ([]string, error)) func() {
	prev := lookupTXT
	lookupTXT = f
	return func() { lookupTXT = prev }
}

// RecordName is the DNS name the record goes under.
func RecordName(domain string) string { return "_asc-hub." + domain }

// RecordValue is the text of the record.
func RecordValue(d *user_model.AscOrgDomain) string { return recordPrefix + d.Token }

// ErrNotFound means the record is not there (yet); the person can try again after the DNS has caught up.
var ErrNotFound = errors.New("the DNS record was not found")

// Check reads the record and stores the result: verified when the record holds the token, else not.
func Check(ctx context.Context, d *user_model.AscOrgDomain) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, lookupTime)
	defer cancel()
	records, err := lookupTXT(ctx, RecordName(d.Domain))
	found := false
	if err == nil {
		want := RecordValue(d)
		for _, r := range records {
			if strings.TrimSpace(r) == want {
				found = true
				break
			}
		}
	}
	if err != nil {
		var dnsErr *net.DNSError
		if !(errors.As(err, &dnsErr) && dnsErr.IsNotFound) {
			return false, err // the lookup itself failed: nothing is concluded, the old state stays
		}
	}
	if err := user_model.MarkAscOrgDomainChecked(ctx, d, found); err != nil {
		return false, err
	}
	if !found {
		return false, ErrNotFound
	}
	return true, nil
}

// RecheckDue looks again at the verified claims that were last checked more than a day ago. A failing lookup keeps the
// badge until the next try.
func RecheckDue(ctx context.Context) (int, error) {
	due, err := user_model.AscOrgDomainsToCheck(ctx, timeutil.TimeStamp(time.Now().Add(-recheckAfter).Unix()))
	if err != nil {
		return 0, err
	}
	n := 0
	for _, d := range due {
		if _, err := Check(ctx, d); err != nil && !errors.Is(err, ErrNotFound) {
			log.Warn("asc org domain: %s of organization %d: %v", d.Domain, d.OrgID, err)
			continue
		}
		n++
	}
	return n, nil
}
