// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user_test

import (
	"testing"

	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeAscDomain(t *testing.T) {
	for in, want := range map[string]string{"Acme.DEV": "acme.dev", " acme.co.uk. ": "acme.co.uk", "a-b.acme.com": "a-b.acme.com"} {
		got, err := user_model.NormalizeAscDomain(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got)
	}
	for _, bad := range []string{"", "acme", "10.0.0.1", "::1", "localhost", "printer.local", "db.internal", "foo.test", "a b.org", "-a.org", "a..org", "ex_ample.org", "x.org/path", "x.org:80", "foo.example"} {
		_, err := user_model.NormalizeAscDomain(bad)
		assert.Error(t, err, bad)
	}
}

func TestAscOrgDomainClaimAndProof(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	d, err := user_model.SetAscOrgDomain(ctx, 3, "Acme.dev")
	require.NoError(t, err)
	assert.Equal(t, "acme.dev", d.Domain)
	assert.Len(t, d.Token, 32)
	none, _ := user_model.VerifiedAscOrgDomains(ctx, []int64{3})
	assert.Empty(t, none, "a claim is not a proof")

	require.NoError(t, user_model.MarkAscOrgDomainChecked(ctx, d, true))
	got, err := user_model.VerifiedAscOrgDomains(ctx, []int64{3, 6})
	require.NoError(t, err)
	assert.Equal(t, map[int64]string{3: "acme.dev"}, got)

	// another organization cannot prove the same domain
	d2, err := user_model.SetAscOrgDomain(ctx, 6, "acme.dev")
	require.NoError(t, err)
	assert.ErrorIs(t, user_model.MarkAscOrgDomainChecked(ctx, d2, true), user_model.ErrAscDomainTaken)

	// a new claim of another domain starts again, with a new token
	d3, err := user_model.SetAscOrgDomain(ctx, 3, "labs.acme.dev")
	require.NoError(t, err)
	assert.NotEqual(t, d.Token, d3.Token)
	got, _ = user_model.VerifiedAscOrgDomains(ctx, []int64{3})
	assert.Empty(t, got)

	// a proof that stops holding takes the badge away
	require.NoError(t, user_model.MarkAscOrgDomainChecked(ctx, d3, true))
	require.NoError(t, user_model.MarkAscOrgDomainChecked(ctx, d3, false))
	got, _ = user_model.VerifiedAscOrgDomains(ctx, []int64{3})
	assert.Empty(t, got)
	require.NoError(t, user_model.ClearAscOrgDomain(ctx, 3))
	cl, _ := user_model.GetAscOrgDomain(ctx, 3)
	assert.Nil(t, cl)
}
