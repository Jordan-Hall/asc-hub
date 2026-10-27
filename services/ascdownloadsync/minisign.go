// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascdownloadsync

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/blake2b"
)

// A reader of minisign public keys and signatures, enough to check a release (https://jedisct1.github.io/minisign/).
//
//	public key   base64( "Ed" | key id (8) | ed25519 key (32) )
//	signature    "untrusted comment: ..." / base64( alg (2) | key id (8) | signature (64) ) /
//	             "trusted comment: ..." / base64( global signature (64) )
//
// alg "ED" signs the BLAKE2b-512 digest of the file, alg "Ed" the file itself. The global signature covers the
// signature and the trusted comment, so the comment cannot be changed. A signature is accepted only when the key id
// is the key's, the signature verifies, and the global signature verifies.
var (
	// ErrBadKey is returned for a public key that is not a minisign key.
	ErrBadKey = errors.New("ascdownloadsync: the release public key is not a minisign key")
	// ErrBadSignature is returned for a signature that is malformed or does not verify.
	ErrBadSignature = errors.New("ascdownloadsync: the signature does not verify with the release public key")
)

// PublicKey is a minisign public key.
type PublicKey struct {
	id  [8]byte
	key ed25519.PublicKey
}

// ParsePublicKey reads the key line of a .pub file ("RWQ..."), or the whole file.
func ParsePublicKey(text string) (PublicKey, error) {
	line := ""
	for _, l := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "untrusted comment:") {
			line = l
			break
		}
	}
	raw, err := base64.StdEncoding.DecodeString(line)
	if err != nil || len(raw) != 42 || raw[0] != 'E' || raw[1] != 'd' {
		return PublicKey{}, ErrBadKey
	}
	var pk PublicKey
	copy(pk.id[:], raw[2:10])
	pk.key = ed25519.PublicKey(append([]byte(nil), raw[10:]...))
	return pk, nil
}

// Signature is a parsed .minisig file.
type Signature struct {
	prehashed bool
	id        [8]byte
	sig       []byte
	comment   string
	global    []byte
}

// Prehashed reports whether the signature is over the BLAKE2b-512 digest of the file.
func (s Signature) Prehashed() bool { return s.prehashed }

// ParseSignature reads a .minisig file.
func ParseSignature(text string) (Signature, error) {
	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(text), "\r\n", "\n"), "\n")
	if len(lines) < 4 || !strings.HasPrefix(lines[0], "untrusted comment:") || !strings.HasPrefix(lines[2], "trusted comment:") {
		return Signature{}, ErrBadSignature
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[1]))
	if err != nil || len(raw) != 74 || raw[0] != 'E' || (raw[1] != 'd' && raw[1] != 'D') {
		return Signature{}, ErrBadSignature
	}
	global, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[3]))
	if err != nil || len(global) != 64 {
		return Signature{}, ErrBadSignature
	}
	s := Signature{prehashed: raw[1] == 'D', sig: raw[10:], comment: strings.TrimPrefix(lines[2], "trusted comment: "), global: global}
	copy(s.id[:], raw[2:10])
	return s, nil
}

// Verify checks sig over a file given its content (a legacy "Ed" signature) or, for a prehashed one, its
// BLAKE2b-512 digest. Exactly one of content and digest is used, as sig.Prehashed says.
func (k PublicKey) Verify(sig Signature, content []byte, digest []byte) error {
	if !bytes.Equal(sig.id[:], k.id[:]) {
		return fmt.Errorf("%w: another key id", ErrBadSignature)
	}
	msg := content
	if sig.prehashed {
		if len(digest) != blake2b.Size {
			return ErrBadSignature
		}
		msg = digest
	}
	if !ed25519.Verify(k.key, msg, sig.sig) {
		return ErrBadSignature
	}
	if !ed25519.Verify(k.key, append(append([]byte(nil), sig.sig...), []byte(sig.comment)...), sig.global) {
		return fmt.Errorf("%w: the trusted comment", ErrBadSignature)
	}
	return nil
}

// VerifyBytes checks sig over data held in memory.
func (k PublicKey) VerifyBytes(sig Signature, data []byte) error {
	if sig.prehashed {
		d := blake2b.Sum512(data)
		return k.Verify(sig, nil, d[:])
	}
	return k.Verify(sig, data, nil)
}
