// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package webpush sends Web Push messages: the content encoding of RFC 8291 (aes128gcm) and the VAPID identification of
// RFC 8292, with the standard library only. It knows nothing of the hub; the caller decides who gets a message.
package webpush

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/url"
	"strings"
	"time"
)

const (
	// RecordSize is the record size written in the header; one record holds a whole message.
	RecordSize = 4096
	// MaxPayload is the longest plaintext: a record, less the header of 86 bytes, 16 bytes of tag and 1 of padding mark.
	MaxPayload = RecordSize - 86 - 16 - 1
)

var b64 = base64.RawURLEncoding

// EncodeKey writes bytes the way browsers and RFC 8292 do: URL-safe base64 without padding.
func EncodeKey(b []byte) string { return b64.EncodeToString(b) }

// DecodeKey reads URL-safe base64, with or without padding.
func DecodeKey(s string) ([]byte, error) {
	return b64.DecodeString(strings.TrimRight(s, "="))
}

// ValidateSubscriptionKeys checks the two secrets a browser gives with a subscription: p256dh, an uncompressed point on
// P-256 (65 bytes), and auth, 16 random bytes.
func ValidateSubscriptionKeys(p256dh, auth string) error {
	pub, err := DecodeKey(p256dh)
	if err != nil {
		return errors.New("p256dh is not base64")
	}
	if _, err := ecdh.P256().NewPublicKey(pub); err != nil {
		return errors.New("p256dh is not a P-256 public key")
	}
	a, err := DecodeKey(auth)
	if err != nil || len(a) != 16 {
		return errors.New("auth must be 16 bytes")
	}
	return nil
}

type contentKeys struct {
	cek, nonce []byte
}

// deriveKeys is the key schedule of RFC 8291 section 3.4 and 3.3: the same on both sides.
func deriveKeys(shared, authSecret, uaPublic, asPublic, salt []byte) (*contentKeys, error) {
	prkKey, err := hkdf.Extract(sha256.New, shared, authSecret)
	if err != nil {
		return nil, err
	}
	keyInfo := string(append(append([]byte("WebPush: info\x00"), uaPublic...), asPublic...))
	ikm, err := hkdf.Expand(sha256.New, prkKey, keyInfo, 32)
	if err != nil {
		return nil, err
	}
	prk, err := hkdf.Extract(sha256.New, ikm, salt)
	if err != nil {
		return nil, err
	}
	cek, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, err
	}
	nonce, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, err
	}
	return &contentKeys{cek: cek, nonce: nonce}, nil
}

func (k *contentKeys) aead() (cipher.AEAD, error) {
	block, err := aes.NewCipher(k.cek)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Encrypt makes the body of a push message for the browser with the public key uaPublic (65 bytes) and the secret
// authSecret (16 bytes). salt and sender may be nil: they are then made fresh, as they must be for every message.
func Encrypt(plaintext, uaPublic, authSecret, salt []byte, sender *ecdh.PrivateKey) ([]byte, error) {
	if len(plaintext) > MaxPayload {
		return nil, fmt.Errorf("message of %d bytes is longer than %d", len(plaintext), MaxPayload)
	}
	if len(authSecret) != 16 {
		return nil, errors.New("auth secret must be 16 bytes")
	}
	curve := ecdh.P256()
	ua, err := curve.NewPublicKey(uaPublic)
	if err != nil {
		return nil, fmt.Errorf("browser key: %w", err)
	}
	if sender == nil {
		if sender, err = curve.GenerateKey(rand.Reader); err != nil {
			return nil, err
		}
	}
	if salt == nil {
		salt = make([]byte, 16)
		if _, err := io.ReadFull(rand.Reader, salt); err != nil {
			return nil, err
		}
	}
	if len(salt) != 16 {
		return nil, errors.New("salt must be 16 bytes")
	}
	shared, err := sender.ECDH(ua)
	if err != nil {
		return nil, err
	}
	asPublic := sender.PublicKey().Bytes()
	keys, err := deriveKeys(shared, authSecret, uaPublic, asPublic, salt)
	if err != nil {
		return nil, err
	}
	gcm, err := keys.aead()
	if err != nil {
		return nil, err
	}
	record := append(append([]byte(nil), plaintext...), 0x02) // 0x02 marks the last (here the only) record
	header := make([]byte, 0, 86+len(record)+16)
	header = append(header, salt...)
	header = binary.BigEndian.AppendUint32(header, RecordSize)
	header = append(header, byte(len(asPublic)))
	header = append(header, asPublic...)
	return gcm.Seal(header, keys.nonce, record, nil), nil
}

// Decrypt reads a body made by Encrypt with the private key of the browser; the hub never needs it, tests do.
func Decrypt(body []byte, uaPrivate *ecdh.PrivateKey, authSecret []byte) ([]byte, error) {
	if len(body) < 86+16 || body[20] != 65 {
		return nil, errors.New("bad header")
	}
	salt, asPublic := body[:16], body[21:86]
	as, err := ecdh.P256().NewPublicKey(asPublic)
	if err != nil {
		return nil, err
	}
	shared, err := uaPrivate.ECDH(as)
	if err != nil {
		return nil, err
	}
	keys, err := deriveKeys(shared, authSecret, uaPrivate.PublicKey().Bytes(), asPublic, salt)
	if err != nil {
		return nil, err
	}
	gcm, err := keys.aead()
	if err != nil {
		return nil, err
	}
	record, err := gcm.Open(nil, keys.nonce, body[86:], nil)
	if err != nil {
		return nil, err
	}
	record = []byte(strings.TrimRight(string(record), "\x00")) // padding
	if len(record) == 0 || record[len(record)-1] != 0x02 {
		return nil, errors.New("missing delimiter")
	}
	return record[:len(record)-1], nil
}

// Keys is the VAPID key pair of the hub (RFC 8292).
type Keys struct {
	priv *ecdsa.PrivateKey
}

// NewKeys makes a fresh pair.
func NewKeys() (*Keys, error) {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	return &Keys{priv: k}, nil
}

// KeysFromPrivate reads a pair from the 32 bytes of the private key.
func KeysFromPrivate(d []byte) (*Keys, error) {
	if len(d) != 32 {
		return nil, errors.New("a VAPID private key is 32 bytes")
	}
	k := new(ecdsa.PrivateKey)
	k.Curve = elliptic.P256()
	k.D = new(big.Int).SetBytes(d)
	if k.D.Sign() == 0 || k.D.Cmp(k.Curve.Params().N) >= 0 {
		return nil, errors.New("not a valid P-256 private key")
	}
	k.PublicKey.X, k.PublicKey.Y = k.Curve.ScalarBaseMult(d)
	return &Keys{priv: k}, nil
}

// Private returns the 32 bytes of the private key.
func (k *Keys) Private() []byte { return k.priv.D.FillBytes(make([]byte, 32)) }

// Public returns the uncompressed public key (65 bytes), which browsers take as applicationServerKey.
func (k *Keys) Public() []byte {
	out := make([]byte, 65)
	out[0] = 4
	k.priv.PublicKey.X.FillBytes(out[1:33])
	k.priv.PublicKey.Y.FillBytes(out[33:65])
	return out
}

// Authorization makes the Authorization header for the push service of endpoint: the JWT of RFC 8292 for its origin,
// valid for 12 hours, and the public key.
func (k *Keys) Authorization(endpoint, subject string, now time.Time) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return "", errors.New("endpoint is not an https URL")
	}
	claims, err := json.Marshal(map[string]any{"aud": u.Scheme + "://" + u.Host, "exp": now.Add(12 * time.Hour).Unix(), "sub": subject})
	if err != nil {
		return "", err
	}
	signing := b64.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`)) + "." + b64.EncodeToString(claims)
	sum := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, k.priv, sum[:])
	if err != nil {
		return "", err
	}
	sig := append(r.FillBytes(make([]byte, 32)), s.FillBytes(make([]byte, 32))...)
	return "vapid t=" + signing + "." + b64.EncodeToString(sig) + ", k=" + b64.EncodeToString(k.Public()), nil
}

// VerifyAuthorization checks a header made by Authorization against the public key; push services do this, tests too.
func VerifyAuthorization(header string, public []byte, audience string, now time.Time) error {
	rest, ok := strings.CutPrefix(header, "vapid t=")
	if !ok {
		return errors.New("not a vapid header")
	}
	jwt, key, ok := strings.Cut(rest, ", k=")
	if !ok || key != b64.EncodeToString(public) {
		return errors.New("key does not match")
	}
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		return errors.New("not a JWT")
	}
	sig, err := b64.DecodeString(parts[2])
	if err != nil || len(sig) != 64 || len(public) != 65 {
		return errors.New("bad signature")
	}
	pub := &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(public[1:33]), Y: new(big.Int).SetBytes(public[33:])}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if !ecdsa.Verify(pub, sum[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		return errors.New("signature does not verify")
	}
	raw, err := b64.DecodeString(parts[1])
	if err != nil {
		return err
	}
	var claims struct {
		Aud string `json:"aud"`
		Exp int64  `json:"exp"`
	}
	if err := json.Unmarshal(raw, &claims); err != nil {
		return err
	}
	if claims.Aud != audience {
		return errors.New("wrong audience")
	}
	if now.Unix() > claims.Exp {
		return errors.New("expired")
	}
	return nil
}
