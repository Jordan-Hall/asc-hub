// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package ascidentity forwards the signed-in person to `asc serve` in a form the server can verify.
//
// A call made for a person carries that person's own token. This package adds the identity the hub vouches for,
// as five gRPC metadata entries (the header contract is in docs/asc/ARCHITECTURE.md, "Forwarded identity"):
//
//	x-asc-hub-user    the numeric id of the user on the hub
//	x-asc-hub-login   the login name
//	x-asc-hub-admin   "1" for a site administrator, "0" otherwise
//	x-asc-hub-ts      the time of signing, Unix seconds
//	x-asc-hub-sig     lowercase hex of HMAC-SHA256 over the canonical text of the four above
//
// The key is not the service secret itself: it is HMAC-SHA256(secret, "asc-hub-identity-v1 key"), so the bearer
// value that the hub and asc already share is never used as a signing key and a signature leaks nothing about it.
// The canonical text is the lines "asc-hub-identity-v1", user, login, admin and ts, each ended by a line feed.
// A server accepts a signature when it verifies in constant time and the time is at most MaxSkew from its clock.
//
// A signature says who the hub signed in. It does not grant anything: asc still authorises the call by the
// token, and a request that reaches asc without the secret cannot carry a valid signature.
package ascidentity

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"

	"google.golang.org/grpc/metadata"
)

// The metadata keys of the contract.
const (
	HeaderUser      = "x-asc-hub-user"
	HeaderLogin     = "x-asc-hub-login"
	HeaderAdmin     = "x-asc-hub-admin"
	HeaderTimestamp = "x-asc-hub-ts"
	HeaderSignature = "x-asc-hub-sig"

	// Version names the canonical text and the key derivation. A change of either is a new version.
	Version = "asc-hub-identity-v1"
	// MaxSkew is how far the time of a signature may be from the clock of the verifier.
	MaxSkew = 5 * time.Minute
	// MaxLoginBytes bounds the login name.
	MaxLoginBytes = 255
	// MinSecretBytes is the shortest secret that signs. A shorter one is a configuration error, not a key.
	MinSecretBytes = 16
)

var headers = [...]string{HeaderUser, HeaderLogin, HeaderAdmin, HeaderTimestamp, HeaderSignature}

var (
	// ErrNoSecret is returned when there is no shared secret to sign with.
	ErrNoSecret = errors.New("ascidentity: no shared secret")
	// ErrBadIdentity is returned for an identity that cannot be signed: no user, or a login that is not safe text.
	ErrBadIdentity = errors.New("ascidentity: bad identity")
	// ErrBadSignature is returned by Verify for a missing, malformed, expired or wrong signature.
	ErrBadSignature = errors.New("ascidentity: bad signature")
)

// Identity is the person the hub signed in.
type Identity struct {
	ID    int64
	Login string
	Admin bool
}

// Validate checks that the identity can be signed. A login is visible ASCII without a space, because it travels
// in a header and in a line-oriented text.
func (id Identity) Validate() error {
	if id.ID <= 0 {
		return fmt.Errorf("%w: no user id", ErrBadIdentity)
	}
	if id.Login == "" || len(id.Login) > MaxLoginBytes {
		return fmt.Errorf("%w: login length", ErrBadIdentity)
	}
	for i := range len(id.Login) {
		if c := id.Login[i]; c < 0x21 || c > 0x7e {
			return fmt.Errorf("%w: login character", ErrBadIdentity)
		}
	}
	return nil
}

func adminText(admin bool) string {
	if admin {
		return "1"
	}
	return "0"
}

// canonical is the text that is signed.
func canonical(id, login, admin, ts string) string {
	return Version + "\n" + id + "\n" + login + "\n" + admin + "\n" + ts + "\n"
}

func key(secret string) []byte {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(Version + " key"))
	return m.Sum(nil)
}

func mac(secret, text string) string {
	m := hmac.New(sha256.New, key(secret))
	m.Write([]byte(text))
	return hex.EncodeToString(m.Sum(nil))
}

// Sign returns the metadata entries that vouch for id at the time at.
func Sign(secret string, id Identity, at time.Time) (map[string]string, error) {
	if len(secret) < MinSecretBytes {
		return nil, ErrNoSecret
	}
	if err := id.Validate(); err != nil {
		return nil, err
	}
	user, admin, ts := strconv.FormatInt(id.ID, 10), adminText(id.Admin), strconv.FormatInt(at.Unix(), 10)
	return map[string]string{
		HeaderUser:      user,
		HeaderLogin:     id.Login,
		HeaderAdmin:     admin,
		HeaderTimestamp: ts,
		HeaderSignature: mac(secret, canonical(user, id.Login, admin, ts)),
	}, nil
}

// Verify is the check a server makes. get returns the value of a metadata key ("" when absent). It is here as the
// reference of the contract and for tests; the server has its own implementation.
func Verify(secret string, get func(string) string, now time.Time) (Identity, error) {
	if len(secret) < MinSecretBytes {
		return Identity{}, ErrNoSecret
	}
	user, login, admin, ts, sig := get(HeaderUser), get(HeaderLogin), get(HeaderAdmin), get(HeaderTimestamp), get(HeaderSignature)
	if admin != "0" && admin != "1" {
		return Identity{}, ErrBadSignature
	}
	want := mac(secret, canonical(user, login, admin, ts))
	if !hmac.Equal([]byte(want), []byte(sig)) {
		return Identity{}, ErrBadSignature
	}
	// Parse only after the signature held, and require the canonical decimal spelling so one identity has one text.
	uid, err := strconv.ParseInt(user, 10, 64)
	if err != nil || strconv.FormatInt(uid, 10) != user {
		return Identity{}, ErrBadSignature
	}
	at, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || strconv.FormatInt(at, 10) != ts {
		return Identity{}, ErrBadSignature
	}
	if d := now.Sub(time.Unix(at, 0)); d > MaxSkew || d < -MaxSkew {
		return Identity{}, fmt.Errorf("%w: expired", ErrBadSignature)
	}
	id := Identity{ID: uid, Login: login, Admin: admin == "1"}
	if err := id.Validate(); err != nil {
		return Identity{}, ErrBadSignature
	}
	return id, nil
}

// Attach returns a context whose outgoing calls carry the signed identity. Entries of the contract that the
// context already holds are replaced, never kept, so a caller cannot choose another person.
func Attach(ctx context.Context, secret string, id Identity, at time.Time) (context.Context, error) {
	signed, err := Sign(secret, id, at)
	if err != nil {
		return ctx, err
	}
	md, _ := metadata.FromOutgoingContext(ctx)
	md = md.Copy()
	for _, h := range headers {
		md.Set(h, signed[h])
	}
	return metadata.NewOutgoingContext(ctx, md), nil
}

// AttachDoer signs doer with the hub's internal token and attaches the entries. A hub with no internal token has no
// connection to asc at all (asctrust.Conn refuses to dial), so there nothing is added and the call carries the
// token of the person alone; a token that is set but too short is an error, never a weak key.
func AttachDoer(ctx context.Context, doer *user_model.User) (context.Context, error) {
	if doer == nil {
		return ctx, fmt.Errorf("%w: no user", ErrBadIdentity)
	}
	if setting.InternalToken == "" {
		return ctx, nil
	}
	return Attach(ctx, setting.InternalToken, Identity{ID: doer.ID, Login: doer.Name, Admin: doer.IsAdmin}, time.Now())
}
