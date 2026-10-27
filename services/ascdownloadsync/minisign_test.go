// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascdownloadsync

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMinisignPrehashedAndLegacy(t *testing.T) {
	sg := newSigner(t, 1)
	pub, err := ParsePublicKey("untrusted comment: minisign public key\n" + sg.publicKey() + "\n")
	require.NoError(t, err, "a whole .pub file is accepted")
	data := []byte("the content of a release file")

	for _, prehashed := range []bool{true, false} {
		sig, err := ParseSignature(sg.sign(data, prehashed))
		require.NoError(t, err)
		assert.Equal(t, prehashed, sig.Prehashed())
		require.NoError(t, pub.VerifyBytes(sig, data))
		assert.ErrorIs(t, pub.VerifyBytes(sig, append([]byte("x"), data...)), ErrBadSignature, "another content")
	}
}

func TestMinisignRefusals(t *testing.T) {
	sg, other := newSigner(t, 1), newSigner(t, 2)
	pub, err := ParsePublicKey(sg.publicKey())
	require.NoError(t, err)
	data := []byte("data")

	sig, err := ParseSignature(other.sign(data, true))
	require.NoError(t, err)
	assert.ErrorIs(t, pub.VerifyBytes(sig, data), ErrBadSignature, "another key")

	// The same id with another key: the signature does not verify.
	forged := newSigner(t, 1)
	sig, err = ParseSignature(forged.sign(data, true))
	require.NoError(t, err)
	assert.ErrorIs(t, pub.VerifyBytes(sig, data), ErrBadSignature)

	// The trusted comment is covered by the global signature.
	text := sg.sign(data, true)
	changed := strings.Replace(text, "timestamp:1", "timestamp:2", 1)
	sig, err = ParseSignature(changed)
	require.NoError(t, err)
	assert.ErrorIs(t, pub.VerifyBytes(sig, data), ErrBadSignature)

	for _, bad := range []string{"", "RWQ", "not base64!", strings.Repeat("A", 56)} {
		_, err := ParsePublicKey(bad)
		assert.ErrorIs(t, err, ErrBadKey, bad)
	}
	for _, bad := range []string{"", "untrusted comment: x\nAAAA\ntrusted comment: y\nAAAA", "a\nb\nc\nd"} {
		_, err := ParseSignature(bad)
		assert.ErrorIs(t, err, ErrBadSignature, bad)
	}
}

func TestParseSums(t *testing.T) {
	h := strings.Repeat("a", 64)
	got, err := parseSums([]byte(h + "  one.tar.gz\n" + strings.ToUpper(h) + " *two.zip\n\n# comment\n"))
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"one.tar.gz": h, "two.zip": h}, got)
	for _, bad := range []string{"short  x\n", h + "  \n", h + "  a/b\n", strings.Repeat("g", 64) + "  x\n", h + "  x\n" + strings.Repeat("b", 64) + "  x\n"} {
		_, err := parseSums([]byte(bad))
		assert.Error(t, err, bad)
	}
}
