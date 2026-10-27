// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascidentity

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
)

const secret = "a-shared-service-secret"

var now = time.Unix(1_800_000_000, 0)

func getter(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestSignedIdentityVerifies(t *testing.T) {
	md, err := Sign(secret, Identity{ID: 7, Login: "alice", Admin: true}, now)
	require.NoError(t, err)
	assert.Equal(t, "7", md[HeaderUser])
	assert.Equal(t, "alice", md[HeaderLogin])
	assert.Equal(t, "1", md[HeaderAdmin])
	assert.Equal(t, "1800000000", md[HeaderTimestamp])
	assert.Len(t, md[HeaderSignature], 64)

	id, err := Verify(secret, getter(md), now.Add(MaxSkew))
	require.NoError(t, err)
	assert.Equal(t, Identity{ID: 7, Login: "alice", Admin: true}, id)
}

// The signature is pinned to a fixed vector so the server's implementation can be checked against this one.
func TestSignatureVector(t *testing.T) {
	md, err := Sign(secret, Identity{ID: 7, Login: "alice", Admin: true}, now)
	require.NoError(t, err)
	assert.Equal(t, mac(secret, "asc-hub-identity-v1\n7\nalice\n1\n1800000000\n"), md[HeaderSignature])
	other, err := Sign("another-shared-secret", Identity{ID: 7, Login: "alice", Admin: true}, now)
	require.NoError(t, err)
	assert.NotEqual(t, md[HeaderSignature], other[HeaderSignature])
}

func TestEveryFieldIsCoveredByTheSignature(t *testing.T) {
	good, err := Sign(secret, Identity{ID: 7, Login: "alice"}, now)
	require.NoError(t, err)
	for name, change := range map[string]struct{ key, value string }{
		"user":  {HeaderUser, "8"},
		"login": {HeaderLogin, "bob"},
		"admin": {HeaderAdmin, "1"},
		"time":  {HeaderTimestamp, "1800000001"},
	} {
		forged := map[string]string{}
		for k, v := range good {
			forged[k] = v
		}
		forged[change.key] = change.value
		_, err := Verify(secret, getter(forged), now)
		assert.ErrorIs(t, err, ErrBadSignature, name)
	}
	_, err = Verify("not-the-shared-secret", getter(good), now)
	assert.ErrorIs(t, err, ErrBadSignature, "another secret")
}

func TestVerifyRefusesMissingMalformedAndStale(t *testing.T) {
	good, err := Sign(secret, Identity{ID: 7, Login: "alice"}, now)
	require.NoError(t, err)
	_, err = Verify(secret, getter(nil), now)
	assert.ErrorIs(t, err, ErrBadSignature, "nothing sent")

	for name, at := range map[string]time.Time{"future": now.Add(-MaxSkew - time.Second), "past": now.Add(MaxSkew + time.Second)} {
		_, err := Verify(secret, getter(good), at)
		assert.ErrorIs(t, err, ErrBadSignature, name)
	}

	// A signature over a non-canonical spelling of the same number is refused.
	forged := map[string]string{HeaderUser: "07", HeaderLogin: "alice", HeaderAdmin: "0", HeaderTimestamp: "1800000000"}
	forged[HeaderSignature] = mac(secret, canonical("07", "alice", "0", "1800000000"))
	_, err = Verify(secret, getter(forged), now)
	assert.ErrorIs(t, err, ErrBadSignature)

	_, err = Verify("short", getter(good), now)
	assert.ErrorIs(t, err, ErrNoSecret)
}

func TestSignRefusesWhatCannotBeSigned(t *testing.T) {
	_, err := Sign("", Identity{ID: 1, Login: "a"}, now)
	assert.ErrorIs(t, err, ErrNoSecret)
	_, err = Sign("short", Identity{ID: 1, Login: "a"}, now)
	assert.ErrorIs(t, err, ErrNoSecret)
	for name, id := range map[string]Identity{
		"no id":        {Login: "a"},
		"negative id":  {ID: -1, Login: "a"},
		"no login":     {ID: 1},
		"line feed":    {ID: 1, Login: "a\nb"},
		"space":        {ID: 1, Login: "a b"},
		"non ascii":    {ID: 1, Login: "café"},
		"long login":   {ID: 1, Login: strings.Repeat("a", MaxLoginBytes+1)},
		"control char": {ID: 1, Login: "a\x00"},
	} {
		_, err := Sign(secret, id, now)
		assert.ErrorIs(t, err, ErrBadIdentity, name)
	}
}

func TestAttachReplacesWhatTheContextHolds(t *testing.T) {
	base := metadata.AppendToOutgoingContext(t.Context(),
		HeaderUser, "1", HeaderAdmin, "1", HeaderLogin, "root", "traceparent", "00-abc")
	ctx, err := Attach(base, secret, Identity{ID: 7, Login: "alice"}, now)
	require.NoError(t, err)
	md, ok := metadata.FromOutgoingContext(ctx)
	require.True(t, ok)
	for _, h := range headers {
		assert.Len(t, md.Get(h), 1, h)
	}
	assert.Equal(t, []string{"7"}, md.Get(HeaderUser))
	assert.Equal(t, []string{"0"}, md.Get(HeaderAdmin), "a smuggled administrator flag is replaced")
	assert.Equal(t, []string{"00-abc"}, md.Get("traceparent"))
	id, err := Verify(secret, func(k string) string { return strings.Join(md.Get(k), ",") }, now)
	require.NoError(t, err)
	assert.Equal(t, int64(7), id.ID)

	// The caller's own context is untouched.
	old, _ := metadata.FromOutgoingContext(base)
	assert.Equal(t, []string{"1"}, old.Get(HeaderUser))

	_, err = Attach(base, "", Identity{ID: 7, Login: "alice"}, now)
	assert.ErrorIs(t, err, ErrNoSecret)
}
