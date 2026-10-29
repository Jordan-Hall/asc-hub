// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascorgverify

import (
	"context"
	"errors"
	"net"
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"

	_ "forgejo.org/modules/testimport"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) { unittest.MainTest(m) }

func TestTheRecordProvesTheDomainAndKeepsProvingIt(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	d, err := user_model.SetAscOrgDomain(ctx, 3, "acme.dev")
	require.NoError(t, err)
	assert.Equal(t, "_asc-hub.acme.dev", RecordName(d.Domain))

	records := []string{"v=spf1 -all", "  " + RecordValue(d) + " "}
	var asked string
	defer UseLookup(func(_ context.Context, name string) ([]string, error) { asked = name; return records, nil })()

	ok, err := Check(ctx, d)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "_asc-hub.acme.dev", asked)
	got, _ := user_model.VerifiedAscOrgDomains(ctx, []int64{3})
	assert.Equal(t, "acme.dev", got[3])

	// a record with another token is no proof, and the badge goes
	records = []string{recordPrefix + "0000"}
	ok, err = Check(ctx, d)
	assert.False(t, ok)
	assert.ErrorIs(t, err, ErrNotFound)
	got, _ = user_model.VerifiedAscOrgDomains(ctx, []int64{3})
	assert.Empty(t, got)

	// a name that does not exist is "not found", a lookup that failed concludes nothing
	_, err = user_model.SetAscOrgDomain(ctx, 3, "acme.dev")
	require.NoError(t, err)
	d, _ = user_model.GetAscOrgDomain(ctx, 3)
	records = []string{RecordValue(d)}
	_, err = Check(ctx, d)
	require.NoError(t, err)
	defer UseLookup(func(context.Context, string) ([]string, error) { return nil, errors.New("server failure") })()
	_, err = Check(ctx, d)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrNotFound)
	got, _ = user_model.VerifiedAscOrgDomains(ctx, []int64{3})
	assert.Equal(t, "acme.dev", got[3], "a failing resolver does not take a badge away")
	UseLookup(func(context.Context, string) ([]string, error) { return nil, &net.DNSError{IsNotFound: true} })
	_, err = Check(ctx, d)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestRecheckDueLooksAtOldProofsOnly(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	d, err := user_model.SetAscOrgDomain(ctx, 3, "acme.dev")
	require.NoError(t, err)
	var calls int
	defer UseLookup(func(context.Context, string) ([]string, error) { calls++; return []string{RecordValue(d)}, nil })()
	require.NoError(t, user_model.MarkAscOrgDomainChecked(ctx, d, true))

	n, err := RecheckDue(ctx)
	require.NoError(t, err)
	assert.Zero(t, n, "checked a moment ago")
	_, err = db.GetEngine(ctx).Exec("UPDATE `asc_org_domain` SET checked_unix = 1")
	require.NoError(t, err)
	n, err = RecheckDue(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, 1, calls)
}
