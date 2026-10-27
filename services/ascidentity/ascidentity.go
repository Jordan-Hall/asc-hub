// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package ascidentity is the hub's claim about the signed-in person, for the calls the hub makes to `asc serve` on
// their behalf from its web pages.
//
// The hub does not issue access tokens for these calls. A call from a web page is made with the hub's own service
// credential and carries a claim: who the person is and what the hub decided that person may do on the subject of the
// call. The hub's permission decision stands; asc serve does not decide again. The claim is the one asc defines in
// asc-rpc (src/forwarded.rs), four metadata entries:
//
//	x-asc-forwarded-user   "<user id>:<login>"
//	x-asc-forwarded-role   what the hub decided: "admin", "write" or "read" (asc accepts "admin" today; the other two
//	                       are the hub's proposal, see docs/asc/ARCHITECTURE.md)
//	x-asc-forwarded-ts     Unix seconds when the hub made the claim
//	x-asc-forwarded-mac    lower-case hex HMAC-SHA256, under the key only the hub and the server share, of
//	                       "asc-forwarded-v1\n<user>\n<role>\n<subject>\n<ts>"
//
// The subject is the repository ("owner/name", lower case), "org:<name>" for the log of an organisation,
// "user:<login>" for the account of the person and "instance" for the whole server. A claim made for one subject is no
// claim for another, and one older than MaxSkew is no claim. The key is [asc] FORWARDED_KEY (32 bytes, 64 hex digits).
//
// Platform access tokens are not involved: the asc command line, agents and API clients present the platform's own
// personal access tokens, which asc serve validates by asking the hub's authz endpoint.
package ascidentity

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"

	"google.golang.org/grpc/metadata"
)

// The metadata keys of the claim.
const (
	HeaderUser = "x-asc-forwarded-user"
	HeaderRole = "x-asc-forwarded-role"
	HeaderTS   = "x-asc-forwarded-ts"
	HeaderMAC  = "x-asc-forwarded-mac"

	// Context is the first line of the signed text; it names the version of the format.
	Context = "asc-forwarded-v1"
	// MaxSkew is how far the time of a claim may be from the clock of the verifier.
	MaxSkew = 60 * time.Second
	// MinKey is the length of the key in bytes.
	MinKey = 32
	// MaxUserBytes bounds the user entry.
	MaxUserBytes = 300
)

// Roles a claim can carry.
const (
	RoleAdmin = "admin"
	RoleWrite = "write"
	RoleRead  = "read"
)

var headers = [...]string{HeaderUser, HeaderRole, HeaderTS, HeaderMAC}

var (
	// ErrNoKey is returned when the hub has no (or a short) forwarded key: a claim cannot be signed, so the call is
	// not made.
	ErrNoKey = errors.New("ascidentity: no forwarded key")
	// ErrBadClaim is returned for a claim that cannot be signed, or by Verify for one that does not verify.
	ErrBadClaim = errors.New("ascidentity: bad claim")
)

// Claim is what the hub says about a person.
type Claim struct {
	// ID and Login name the person.
	ID    int64
	Login string
	// Role is what the hub decided the person may do on Subject.
	Role string
	// Subject is what the claim is about (see the package documentation).
	Subject string
}

// User is the first entry of the claim, "<id>:<login>".
func (c Claim) User() string { return strconv.FormatInt(c.ID, 10) + ":" + c.Login }

// Validate checks that the claim can be signed: the entries are text a header and a line-oriented payload can carry.
func (c Claim) Validate() error {
	if c.ID <= 0 || c.Login == "" || len(c.User()) > MaxUserBytes {
		return fmt.Errorf("%w: user", ErrBadClaim)
	}
	for i := range len(c.Login) {
		if b := c.Login[i]; b < 0x21 || b > 0x7e {
			return fmt.Errorf("%w: login", ErrBadClaim)
		}
	}
	switch c.Role {
	case RoleAdmin, RoleWrite, RoleRead:
	default:
		return fmt.Errorf("%w: role", ErrBadClaim)
	}
	if c.Subject == "" || len(c.Subject) > 300 {
		return fmt.Errorf("%w: subject", ErrBadClaim)
	}
	for i := range len(c.Subject) {
		if b := c.Subject[i]; b < 0x21 || b > 0x7e {
			return fmt.Errorf("%w: subject", ErrBadClaim)
		}
	}
	return nil
}

func payload(user, role, subject string, ts int64) []byte {
	return []byte(Context + "\n" + user + "\n" + role + "\n" + subject + "\n" + strconv.FormatInt(ts, 10))
}

func mac(key []byte, user, role, subject string, ts int64) string {
	m := hmac.New(sha256.New, key)
	m.Write(payload(user, role, subject, ts))
	return hex.EncodeToString(m.Sum(nil))
}

// Sign returns the four entries of the claim made at the time at.
func Sign(key []byte, c Claim, at time.Time) (map[string]string, error) {
	if len(key) < MinKey {
		return nil, ErrNoKey
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	ts := at.Unix()
	return map[string]string{
		HeaderUser: c.User(),
		HeaderRole: c.Role,
		HeaderTS:   strconv.FormatInt(ts, 10),
		HeaderMAC:  mac(key, c.User(), c.Role, c.Subject, ts),
	}, nil
}

// Verify is the check a server makes: get returns the value of a metadata key ("" when absent). It returns the user
// and the role of a claim that is signed under key, made for subject and inside the clock window. It is the reference
// of the format, for tests; the server has its own implementation.
func Verify(key []byte, get func(string) string, subject string, now time.Time) (user, role string, err error) {
	if len(key) < MinKey {
		return "", "", ErrNoKey
	}
	user, role, ts, given := get(HeaderUser), get(HeaderRole), get(HeaderTS), get(HeaderMAC)
	if user == "" || len(user) > MaxUserBytes || strings.ContainsAny(user, "\r\n") {
		return "", "", ErrBadClaim
	}
	n, perr := strconv.ParseInt(ts, 10, 64)
	if perr != nil || strconv.FormatInt(n, 10) != ts {
		return "", "", ErrBadClaim
	}
	if d := now.Sub(time.Unix(n, 0)); d > MaxSkew || d < -MaxSkew {
		return "", "", fmt.Errorf("%w: outside the clock window", ErrBadClaim)
	}
	want := mac(key, user, role, subject, n)
	if !hmac.Equal([]byte(want), []byte(given)) {
		return "", "", ErrBadClaim
	}
	return user, role, nil
}

// Attach returns a context whose outgoing calls carry the claim. Entries of the claim that the context already holds
// are replaced, never kept, so a caller cannot choose another person.
func Attach(ctx context.Context, key []byte, c Claim, at time.Time) (context.Context, error) {
	signed, err := Sign(key, c, at)
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

// AttachDoer signs the claim of doer with the key of the hub ([asc] FORWARDED_KEY). Without a key the call is not made:
// the claim is the only thing that says who the person is, and the hub does not fall back to a call without it.
func AttachDoer(ctx context.Context, doer *user_model.User, role, subject string) (context.Context, error) {
	if doer == nil {
		return ctx, fmt.Errorf("%w: no user", ErrBadClaim)
	}
	return Attach(ctx, setting.Asc.ForwardedKey, Claim{ID: doer.ID, Login: doer.Name, Role: role, Subject: subject}, time.Now())
}
