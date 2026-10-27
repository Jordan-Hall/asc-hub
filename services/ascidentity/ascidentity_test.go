// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascidentity

import (
	"strings"
	"testing"
	"time"

	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
)

var (
	key = []byte("0123456789abcdef0123456789abcdef")
	now = time.Unix(1_800_000_000, 0)
)

func getter(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

var alice = Claim{ID: 7, Login: "alice", Role: RoleAdmin, Subject: "acme/payments"}

func TestSignedClaimVerifies(t *testing.T) {
	md, err := Sign(key, alice, now)
	require.NoError(t, err)
	assert.Equal(t, "7:alice", md[HeaderUser])
	assert.Equal(t, "admin", md[HeaderRole])
	assert.Equal(t, "1800000000", md[HeaderTS])
	assert.Len(t, md[HeaderMAC], 64)

	user, role, err := Verify(key, getter(md), "acme/payments", now.Add(MaxSkew))
	require.NoError(t, err)
	assert.Equal(t, "7:alice", user)
	assert.Equal(t, RoleAdmin, role)
}

// The signed text is pinned so that the server's implementation can be checked against this one.
func TestSignedTextIsPinned(t *testing.T) {
	assert.Equal(t, "asc-forwarded-v1\n7:alice\nadmin\nacme/payments\n1800000000", string(payload("7:alice", RoleAdmin, "acme/payments", 1_800_000_000)))
	md, err := Sign(key, alice, now)
	require.NoError(t, err)
	assert.Equal(t, mac(key, "7:alice", RoleAdmin, "acme/payments", 1_800_000_000), md[HeaderMAC])
	other, err := Sign([]byte("fedcba9876543210fedcba9876543210"), alice, now)
	require.NoError(t, err)
	assert.NotEqual(t, md[HeaderMAC], other[HeaderMAC])
}

func TestEveryFieldAndTheSubjectAreCoveredByTheSignature(t *testing.T) {
	good, err := Sign(key, Claim{ID: 7, Login: "alice", Role: RoleRead, Subject: "acme/payments"}, now)
	require.NoError(t, err)
	for name, change := range map[string]struct{ key, value string }{
		"user": {HeaderUser, "8:alice"},
		"name": {HeaderUser, "7:bob"},
		"role": {HeaderRole, RoleAdmin},
		"time": {HeaderTS, "1800000001"},
	} {
		forged := map[string]string{}
		for k, v := range good {
			forged[k] = v
		}
		forged[change.key] = change.value
		_, _, err := Verify(key, getter(forged), "acme/payments", now)
		assert.ErrorIs(t, err, ErrBadClaim, name)
	}
	// A claim made for one subject is no claim for another.
	for _, subject := range []string{"acme/other", "org:acme", "user:alice", "instance", ""} {
		_, _, err := Verify(key, getter(good), subject, now)
		assert.ErrorIs(t, err, ErrBadClaim, subject)
	}
	_, _, err = Verify([]byte("fedcba9876543210fedcba9876543210"), getter(good), "acme/payments", now)
	assert.ErrorIs(t, err, ErrBadClaim, "another key")
}

func TestVerifyRefusesMissingMalformedAndStale(t *testing.T) {
	good, err := Sign(key, alice, now)
	require.NoError(t, err)
	_, _, err = Verify(key, getter(nil), "acme/payments", now)
	assert.ErrorIs(t, err, ErrBadClaim, "nothing sent")

	for name, at := range map[string]time.Time{"future": now.Add(-MaxSkew - time.Second), "past": now.Add(MaxSkew + time.Second)} {
		_, _, err := Verify(key, getter(good), "acme/payments", at)
		assert.ErrorIs(t, err, ErrBadClaim, name)
	}

	// A signature over a non-canonical spelling of the same number is refused.
	forged := map[string]string{HeaderUser: "7:alice", HeaderRole: RoleAdmin, HeaderTS: "01800000000"}
	forged[HeaderMAC] = mac(key, "7:alice", RoleAdmin, "acme/payments", 1_800_000_000)
	_, _, err = Verify(key, getter(forged), "acme/payments", now)
	assert.ErrorIs(t, err, ErrBadClaim)

	_, _, err = Verify([]byte("short"), getter(good), "acme/payments", now)
	assert.ErrorIs(t, err, ErrNoKey)
}

func TestSignRefusesWhatCannotBeSigned(t *testing.T) {
	_, err := Sign(nil, alice, now)
	assert.ErrorIs(t, err, ErrNoKey)
	_, err = Sign([]byte("short"), alice, now)
	assert.ErrorIs(t, err, ErrNoKey)
	for name, c := range map[string]Claim{
		"no id":         {Login: "a", Role: RoleRead, Subject: "x/y"},
		"negative id":   {ID: -1, Login: "a", Role: RoleRead, Subject: "x/y"},
		"no login":      {ID: 1, Role: RoleRead, Subject: "x/y"},
		"line feed":     {ID: 1, Login: "a\nb", Role: RoleRead, Subject: "x/y"},
		"space":         {ID: 1, Login: "a b", Role: RoleRead, Subject: "x/y"},
		"non ascii":     {ID: 1, Login: "café", Role: RoleRead, Subject: "x/y"},
		"control char":  {ID: 1, Login: "a\x00", Role: RoleRead, Subject: "x/y"},
		"no role":       {ID: 1, Login: "a", Subject: "x/y"},
		"unknown role":  {ID: 1, Login: "a", Role: "root", Subject: "x/y"},
		"no subject":    {ID: 1, Login: "a", Role: RoleRead},
		"subject lines": {ID: 1, Login: "a", Role: RoleRead, Subject: "x/y\nz"},
		"long subject":  {ID: 1, Login: "a", Role: RoleRead, Subject: strings.Repeat("a", 301)},
		"long login":    {ID: 1, Login: strings.Repeat("a", MaxUserBytes), Role: RoleRead, Subject: "x/y"},
	} {
		_, err := Sign(key, c, now)
		assert.ErrorIs(t, err, ErrBadClaim, name)
	}
}

func TestAttachReplacesWhatTheContextHolds(t *testing.T) {
	base := metadata.AppendToOutgoingContext(t.Context(),
		HeaderUser, "1:root", HeaderRole, RoleAdmin, HeaderMAC, "forged", "traceparent", "00-abc")
	c := Claim{ID: 7, Login: "alice", Role: RoleRead, Subject: "acme/payments"}
	ctx, err := Attach(base, key, c, now)
	require.NoError(t, err)
	md, ok := metadata.FromOutgoingContext(ctx)
	require.True(t, ok)
	for _, h := range headers {
		assert.Len(t, md.Get(h), 1, h)
	}
	assert.Equal(t, []string{"7:alice"}, md.Get(HeaderUser))
	assert.Equal(t, []string{RoleRead}, md.Get(HeaderRole), "a smuggled administrator claim is replaced")
	assert.Equal(t, []string{"00-abc"}, md.Get("traceparent"))
	user, role, err := Verify(key, func(k string) string { return strings.Join(md.Get(k), ",") }, "acme/payments", now)
	require.NoError(t, err)
	assert.Equal(t, "7:alice", user)
	assert.Equal(t, RoleRead, role)

	// The caller's own context is untouched.
	old, _ := metadata.FromOutgoingContext(base)
	assert.Equal(t, []string{"1:root"}, old.Get(HeaderUser))

	_, err = Attach(base, nil, c, now)
	assert.ErrorIs(t, err, ErrNoKey)
}

func TestAttachDoerUsesTheKeyOfTheHub(t *testing.T) {
	doer := &user_model.User{ID: 9, Name: "carol"}
	t.Cleanup(test.MockVariableValue(&setting.Asc.ForwardedKey, []byte(nil)))
	_, err := AttachDoer(t.Context(), doer, RoleWrite, "acme/payments")
	assert.ErrorIs(t, err, ErrNoKey, "without a key no call is made")

	setting.Asc.ForwardedKey = key
	ctx, err := AttachDoer(t.Context(), doer, RoleWrite, "acme/payments")
	require.NoError(t, err)
	md, _ := metadata.FromOutgoingContext(ctx)
	user, role, err := Verify(key, func(k string) string { return strings.Join(md.Get(k), ",") }, "acme/payments", time.Now())
	require.NoError(t, err)
	assert.Equal(t, "9:carol", user)
	assert.Equal(t, RoleWrite, role)

	_, err = AttachDoer(t.Context(), nil, RoleWrite, "acme/payments")
	assert.ErrorIs(t, err, ErrBadClaim)
}
