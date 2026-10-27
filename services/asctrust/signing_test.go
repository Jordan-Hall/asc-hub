// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"strings"
	"testing"
	"time"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

func authorizedKey(t *testing.T, pub any) string {
	t.Helper()
	k, err := ssh.NewPublicKey(pub)
	require.NoError(t, err)
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(k)))
}

func TestCheckPublicKeyAcceptsTheKindsAscAccepts(t *testing.T) {
	edPub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	rsaKey, err := rsa.GenerateKey(rand.Reader, 3072)
	require.NoError(t, err)

	cases := map[string]struct {
		pub  any
		kind string
	}{
		"ed25519":    {edPub, "ed25519"},
		"ecdsa-p256": {&ecKey.PublicKey, "ecdsa-p256"},
		"rsa-3072":   {&rsaKey.PublicKey, "rsa"},
	}
	for name, c := range cases {
		line := authorizedKey(t, c.pub)
		got, err := CheckPublicKey(line + " me@laptop")
		require.NoError(t, err, name)
		assert.Equal(t, c.kind, got.Kind, name)
		assert.True(t, strings.HasPrefix(got.Fingerprint, "SHA256:"), name)
		assert.Equal(t, line, got.Line, "%s: the comment is not sent", name)
		assert.NotContains(t, got.Line, "me@laptop")
	}
}

func TestCheckPublicKeyRefuses(t *testing.T) {
	edPub, edPriv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	good := authorizedKey(t, edPub)
	weakRSA, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	ec384, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	require.NoError(t, err)

	block, err := ssh.MarshalPrivateKey(edPriv, "")
	require.NoError(t, err)
	privText := string(pem.EncodeToMemory(block))

	bad := map[string]string{
		"empty":             "",
		"spaces":            "   \n ",
		"not a key":         "hello world",
		"options":           `command="rm -rf /" ` + good,
		"from option":       `from="10.0.0.1" ` + good,
		"two lines":         good + "\n" + good,
		"line break inside": strings.Replace(good, " ", "\n", 1),
		"private key":       privText,
		"weak rsa":          authorizedKey(t, &weakRSA.PublicKey),
		"p384":              authorizedKey(t, &ec384.PublicKey),
		"too long":          good + " " + strings.Repeat("c", 5000),
		"nul":               good + "\x00",
		"invalid utf8":      good + " \xff\xfe",
	}
	for name, in := range bad {
		_, err := CheckPublicKey(in)
		require.Error(t, err, name)
		assert.ErrorIs(t, err, ErrBadInput, name)
		assert.NotContains(t, err.Error(), "BEGIN", "%s: the input is never echoed in the error", name)
	}
}

func TestCheckKeyTitleAndReason(t *testing.T) {
	got, err := CheckKeyTitle("  Laptop  ")
	require.NoError(t, err)
	assert.Equal(t, "Laptop", got)
	for _, bad := range []string{"", "   ", strings.Repeat("t", 101), "a\x00b", "bi\u202ediagram", "new\nline"} {
		_, err := CheckKeyTitle(bad)
		assert.ErrorIs(t, err, ErrBadInput, "%q", bad)
	}
	r, err := CheckRevokeReason("")
	require.NoError(t, err)
	assert.Empty(t, r)
	_, err = CheckRevokeReason(strings.Repeat("r", 257))
	assert.ErrorIs(t, err, ErrBadInput)
	_, err = CheckRevokeReason("lost\nlaptop")
	assert.ErrorIs(t, err, ErrBadInput)
}

func TestExpiryOf(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	got, err := ExpiryOf("", now)
	require.NoError(t, err)
	assert.Zero(t, got)

	got, err = ExpiryOf("2026-06-02", now)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 6, 2, 23, 59, 59, 0, time.UTC).Unix(), got, "the end of that day")

	got, err = ExpiryOf("2026-06-01", now)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 6, 1, 23, 59, 59, 0, time.UTC).Unix(), got, "today still counts")

	for _, bad := range []string{"2026-05-31", "tomorrow", "2026-02-30", "2036-06-02", "2026-6-2x"} {
		_, err := ExpiryOf(bad, now)
		assert.ErrorIs(t, err, ErrBadInput, bad)
	}
	_, err = ExpiryOf("2036-05-31", now)
	assert.NoError(t, err, "just under ten years ahead is accepted")
}

func TestKeysOfStatesAndOrder(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	fp := "SHA256:abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"
	resp := &ascv1.ListSigningKeysResponse{Keys: []*ascv1.SigningKey{
		{Id: "k1", Title: "old laptop", Kind: ascv1.SigningKeyKind_SIGNING_KEY_KIND_SSH_ED25519, Fingerprint: fp, Created: now.AddDate(0, -3, 0).Unix(), RevokedAt: now.AddDate(0, -1, 0).Unix(), RevokedReason: "lost\nit"},
		{Id: "k2", Title: "new laptop", Kind: ascv1.SigningKeyKind_SIGNING_KEY_KIND_SSH_ED25519, Fingerprint: fp, Created: now.AddDate(0, -1, 0).Unix()},
		{Id: "k3", Title: "yubikey", Kind: ascv1.SigningKeyKind_SIGNING_KEY_KIND_SSH_ECDSA_P256, Fingerprint: fp, Created: now.AddDate(0, -2, 0).Unix(), ExpiresAt: now.AddDate(0, 0, -1).Unix()},
		{Id: "k4", Title: "ci", Kind: ascv1.SigningKeyKind_SIGNING_KEY_KIND_SSH_RSA, Fingerprint: fp, Created: now.AddDate(0, -2, 0).Unix(), ExpiresAt: now.AddDate(1, 0, 0).Unix(), LastUsed: now.Unix()},
	}}
	rows, err := KeysOf(resp, now)
	require.NoError(t, err)
	require.Len(t, rows, 4)
	assert.Equal(t, []string{"k2", "k4", "k3", "k1"}, []string{rows[0].ID, rows[1].ID, rows[2].ID, rows[3].ID}, "live keys first, newest first; then the others")
	assert.Equal(t, "live", rows[0].State)
	assert.Equal(t, "rsa", rows[1].Kind)
	assert.Equal(t, "expired", rows[2].State)
	assert.Equal(t, "revoked", rows[3].State)
	assert.Equal(t, "lost it", rows[3].RevokedReason)
}

func TestKeysOfRefusesBadAnswers(t *testing.T) {
	now := time.Now()
	for _, k := range []*ascv1.SigningKey{
		{Id: "", Fingerprint: "SHA256:abc"},
		{Id: "k", Fingerprint: "MD5:aa:bb"},
		{Id: "k", Fingerprint: "SHA256:has space"},
		{Id: strings.Repeat("k", 65), Fingerprint: "SHA256:abc"},
	} {
		_, err := KeysOf(&ascv1.ListSigningKeysResponse{Keys: []*ascv1.SigningKey{k}}, now)
		assert.ErrorIs(t, err, ErrBadAnswer, "%+v", k)
	}
}

func TestServerKeyOf(t *testing.T) {
	assert.False(t, ServerKeyOf(&ascv1.GetServerKeyResponse{}).Configured)
	assert.False(t, ServerKeyOf(&ascv1.GetServerKeyResponse{Configured: true, Fingerprint: "nope"}).Configured, "an answer that fails a check is not drawn")
	k := ServerKeyOf(&ascv1.GetServerKeyResponse{Configured: true, Kind: ascv1.SigningKeyKind_SIGNING_KEY_KIND_SSH_ED25519, Fingerprint: "SHA256:abcDEF123+/=", PublicKey: "ssh-ed25519 AAAA"})
	assert.True(t, k.Configured)
	assert.Equal(t, "ed25519", k.Kind)
}

func TestBadgeOfEveryStatus(t *testing.T) {
	fp := "SHA256:abc123"
	cases := []struct {
		status        ascv1.VerificationStatus
		requireSigned bool
		label, tone   string
	}{
		{ascv1.VerificationStatus_VERIFICATION_STATUS_VALID, false, "Verified", "ok"},
		{ascv1.VerificationStatus_VERIFICATION_STATUS_UNSIGNED, false, "", ""},
		{ascv1.VerificationStatus_VERIFICATION_STATUS_UNSIGNED, true, "Unsigned", "no"},
		{ascv1.VerificationStatus_VERIFICATION_STATUS_UNKNOWN_KEY, false, "Unverified", "wn"},
		{ascv1.VerificationStatus_VERIFICATION_STATUS_KEY_NOT_AUTHOR, false, "Unverified", "wn"},
		{ascv1.VerificationStatus_VERIFICATION_STATUS_KEY_EXPIRED, false, "Unverified", "wn"},
		{ascv1.VerificationStatus_VERIFICATION_STATUS_KEY_REVOKED, false, "Unverified", "no"},
		{ascv1.VerificationStatus_VERIFICATION_STATUS_BAD_SIGNATURE, false, "Unverified", "no"},
		{ascv1.VerificationStatus_VERIFICATION_STATUS_UNSUPPORTED, false, "Unverified", "wn"},
		{ascv1.VerificationStatus_VERIFICATION_STATUS_UNSPECIFIED, false, "Unverified", "wn"},
	}
	for _, c := range cases {
		b := BadgeOf(&ascv1.SignatureVerification{Status: c.status, Fingerprint: fp, Signer: "alice"}, c.requireSigned)
		assert.Equal(t, c.label, b.Label, c.status.String())
		assert.Equal(t, c.tone, b.Tone, c.status.String())
		if c.label != "" {
			assert.NotEmpty(t, b.Title, "every badge says why: %s", c.status)
		}
	}
	valid := BadgeOf(&ascv1.SignatureVerification{Status: ascv1.VerificationStatus_VERIFICATION_STATUS_VALID, Signer: "ali\u202ece", Fingerprint: "bad fingerprint"}, false)
	assert.Equal(t, "alice", valid.Signer)
	assert.Empty(t, valid.Fingerprint, "a fingerprint that fails the check is not passed on")
	assert.Contains(t, valid.Title, "alice")
}

func TestVerifyCommitsMapsCommitsToChangesetsAndBack(t *testing.T) {
	f, c := startFake(t)
	known := strings.Repeat("a1", 20)
	unknown := strings.Repeat("b2", 20)
	cs := make([]byte, 32)
	cs[0] = 7
	f.gitMap = func(req *ascv1.GitMapRequest) (*ascv1.GitMapResponse, error) {
		resp := &ascv1.GitMapResponse{}
		for _, g := range req.GetGit() {
			if g[0] == 0xa1 {
				resp.GitToAsc = append(resp.GitToAsc, cs)
			} else {
				resp.GitToAsc = append(resp.GitToAsc, nil)
			}
		}
		return resp, nil
	}
	f.verify = func(req *ascv1.VerifySignaturesRequest) (*ascv1.VerifySignaturesResponse, error) {
		require.Len(t, req.GetChangesets(), 1, "only the commit asc knows is verified")
		return &ascv1.VerifySignaturesResponse{Results: []*ascv1.SignatureVerification{
			{Changeset: cs, Status: ascv1.VerificationStatus_VERIFICATION_STATUS_VALID, Signer: "alice", Fingerprint: "SHA256:abc"},
		}}, nil
	}
	badges, err := VerifyCommits(userCtx(t, "t"), c, "acme/payments", []string{known, unknown}, false)
	require.NoError(t, err)
	assert.Equal(t, "Verified", badges[known].Label)
	_, has := badges[unknown]
	assert.False(t, has, "a commit asc does not know has no badge")

	// An answer in the wrong order or of the wrong size fails the whole call.
	f.verify = func(*ascv1.VerifySignaturesRequest) (*ascv1.VerifySignaturesResponse, error) {
		other := make([]byte, 32)
		other[0] = 9
		return &ascv1.VerifySignaturesResponse{Results: []*ascv1.SignatureVerification{{Changeset: other, Status: ascv1.VerificationStatus_VERIFICATION_STATUS_VALID}}}, nil
	}
	_, err = VerifyCommits(userCtx(t, "t"), c, "acme/payments", []string{known}, false)
	assert.ErrorIs(t, err, ErrBadAnswer)
	f.verify = func(*ascv1.VerifySignaturesRequest) (*ascv1.VerifySignaturesResponse, error) {
		return &ascv1.VerifySignaturesResponse{}, nil
	}
	_, err = VerifyCommits(userCtx(t, "t"), c, "acme/payments", []string{known}, false)
	assert.ErrorIs(t, err, ErrBadAnswer)

	_, err = VerifyCommits(userCtx(t, "t"), c, "acme/payments", []string{"not hex"}, false)
	assert.ErrorIs(t, err, ErrBadInput)
	out, err := VerifyCommits(userCtx(t, "t"), c, "acme/payments", nil, false)
	require.NoError(t, err)
	assert.Empty(t, out)
}

func TestAddAndRevokeKey(t *testing.T) {
	f, c := startFake(t)
	edPub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	line := authorizedKey(t, edPub)
	fp := "SHA256:abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"
	f.addKey = func(req *ascv1.AddSigningKeyRequest) (*ascv1.AddSigningKeyResponse, error) {
		return &ascv1.AddSigningKeyResponse{Key: &ascv1.SigningKey{Id: "k9", Title: req.GetTitle(), Kind: ascv1.SigningKeyKind_SIGNING_KEY_KIND_SSH_ED25519, Fingerprint: fp, Created: time.Now().Unix(), ExpiresAt: req.GetExpiresAt()}}, nil
	}
	f.revokeKey = func(req *ascv1.RevokeSigningKeyRequest) (*ascv1.RevokeSigningKeyResponse, error) {
		return &ascv1.RevokeSigningKeyResponse{Key: &ascv1.SigningKey{Id: req.GetId(), Fingerprint: fp, Created: 1, RevokedAt: time.Now().Unix(), RevokedReason: req.GetReason()}}, nil
	}
	ctx := userCtx(t, "tok-person")
	op := NewOpID()
	row, err := AddKey(ctx, c, "Laptop", line+" me@laptop", 0, op)
	require.NoError(t, err)
	assert.Equal(t, "k9", row.ID)
	call := f.last(t, "AddKey")
	assert.Equal(t, op, call.OpID)
	assert.Equal(t, "Bearer tok-person", call.Auth)
	assert.Equal(t, line, call.Req.(*ascv1.AddSigningKeyRequest).GetPublicKey(), "the comment is not sent")

	before := f.count("AddKey")
	for _, in := range []struct{ title, key, op string }{
		{"", line, op}, {"Laptop", "garbage", op}, {"Laptop", line, "not-a-uuid"}, {"Laptop", `command="x" ` + line, op},
	} {
		_, err := AddKey(ctx, c, in.title, in.key, 0, in.op)
		assert.ErrorIs(t, err, ErrBadInput, "%+v", in)
	}
	assert.Equal(t, before, f.count("AddKey"), "a bad input never reaches the server")

	rev, err := RevokeKey(ctx, c, "k9", "lost", NewOpID())
	require.NoError(t, err)
	assert.Equal(t, "revoked", rev.State)
	_, err = RevokeKey(ctx, c, "", "x", NewOpID())
	assert.ErrorIs(t, err, ErrBadInput)
	_, err = RevokeKey(ctx, c, "k9", strings.Repeat("r", 300), NewOpID())
	assert.ErrorIs(t, err, ErrBadInput)
}

func TestLoadKeysIncludesTheServerKeyWhenThereIsOne(t *testing.T) {
	f, c := startFake(t)
	fp := "SHA256:abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"
	f.listKeys = func(*ascv1.ListSigningKeysRequest) (*ascv1.ListSigningKeysResponse, error) {
		return &ascv1.ListSigningKeysResponse{Keys: []*ascv1.SigningKey{{Id: "k1", Fingerprint: fp, Created: 5}}}, nil
	}
	page, err := LoadKeys(userCtx(t, "t"), c, time.Now())
	require.NoError(t, err)
	require.Len(t, page.Keys, 1)
	assert.False(t, page.Server.Configured, "GetServerKey is unimplemented here, and the page goes on")

	f.serverKey = func() (*ascv1.GetServerKeyResponse, error) {
		return &ascv1.GetServerKeyResponse{Configured: true, Kind: ascv1.SigningKeyKind_SIGNING_KEY_KIND_SSH_ED25519, Fingerprint: fp}, nil
	}
	page, err = LoadKeys(userCtx(t, "t"), c, time.Now())
	require.NoError(t, err)
	assert.True(t, page.Server.Configured)
}
