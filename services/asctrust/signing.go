// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"crypto/rsa"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"golang.org/x/crypto/ssh"
	"google.golang.org/grpc"
)

// Signed changes (docs/BUILDOUT.md section 5): the keys of a person, and the Verified badge of a change.

const (
	maxKeyLineBytes   = 4096
	maxKeyTitleBytes  = 100
	maxRevokeReason   = 256
	maxKeyExpiryYears = 10
	maxVerifyBatch    = 100
	minRSABits        = 3072
	sshSigNamespace   = "asc-changeset"
)

// SigningNamespace is the namespace of an asc signature, for the instructions on the key page.
const SigningNamespace = sshSigNamespace

// KeyCheck is a public key line that passed the local checks.
type KeyCheck struct {
	// Line is the one authorized_keys line to send, without a comment that asc would store as it is.
	Line        string
	Kind        string
	Fingerprint string
}

// CheckPublicKey checks a public key line the way asc will: one line of at most 4 KiB, a key and nothing else (no
// `command=` or `from=` option), of a kind asc accepts (ed25519, ecdsa-p256, rsa of 3072 bits or more). asc checks
// again; this only gives the person the answer without a round trip and shows the fingerprint to compare.
func CheckPublicKey(text string) (KeyCheck, error) {
	line := strings.TrimSpace(text)
	if line == "" || len(line) > maxKeyLineBytes || !utf8.ValidString(line) {
		return KeyCheck{}, fmt.Errorf("%w: the key must be one line of at most %d bytes", ErrBadInput, maxKeyLineBytes)
	}
	if strings.ContainsAny(line, "\r\n\x00") {
		return KeyCheck{}, fmt.Errorf("%w: the key must be one line", ErrBadInput)
	}
	key, comment, options, rest, err := ssh.ParseAuthorizedKey([]byte(line))
	if err != nil {
		return KeyCheck{}, fmt.Errorf("%w: that is not a public key in authorized_keys form", ErrBadInput)
	}
	if len(options) > 0 {
		return KeyCheck{}, fmt.Errorf("%w: options such as command= or from= are not accepted", ErrBadInput)
	}
	if len(strings.TrimSpace(string(rest))) > 0 {
		return KeyCheck{}, fmt.Errorf("%w: send one key", ErrBadInput)
	}
	kind := ""
	switch key.Type() {
	case ssh.KeyAlgoED25519:
		kind = "ed25519"
	case ssh.KeyAlgoECDSA256:
		kind = "ecdsa-p256"
	case ssh.KeyAlgoRSA:
		kind = "rsa"
		pk, ok := key.(ssh.CryptoPublicKey)
		if !ok {
			return KeyCheck{}, fmt.Errorf("%w: that RSA key cannot be read", ErrBadInput)
		}
		rk, ok := pk.CryptoPublicKey().(*rsa.PublicKey)
		if !ok || rk.N.BitLen() < minRSABits {
			return KeyCheck{}, fmt.Errorf("%w: an RSA key needs %d bits or more", ErrBadInput, minRSABits)
		}
	default:
		return KeyCheck{}, fmt.Errorf("%w: use an ed25519, ecdsa-p256 or rsa key", ErrBadInput)
	}
	// Send the key without its comment: the title names it.
	_ = comment
	return KeyCheck{
		Line:        key.Type() + " " + strings.Fields(line)[1],
		Kind:        kind,
		Fingerprint: ssh.FingerprintSHA256(key),
	}, nil
}

// CheckKeyTitle checks the title of a key: 1 to 100 bytes of valid UTF-8 without a control character.
func CheckKeyTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" || len(title) > maxKeyTitleBytes || !utf8.ValidString(title) {
		return "", fmt.Errorf("%w: the title is 1 to %d bytes", ErrBadInput, maxKeyTitleBytes)
	}
	if CleanText(title, maxKeyTitleBytes) != title {
		return "", fmt.Errorf("%w: the title holds a character that cannot be shown", ErrBadInput)
	}
	return title, nil
}

// CheckRevokeReason checks the reason of a revocation: at most 256 bytes, no control character. It may be empty.
func CheckRevokeReason(reason string) (string, error) {
	reason = strings.TrimSpace(reason)
	if len(reason) > maxRevokeReason || CleanText(reason, maxRevokeReason) != reason {
		return "", fmt.Errorf("%w: the reason is at most %d bytes of plain text", ErrBadInput, maxRevokeReason)
	}
	return reason, nil
}

// ExpiryOf reads the expiry date of a key (year-month-day, empty for never) and returns unix seconds at the end of
// that day in UTC, or 0. The date must be ahead of now and at most ten years ahead.
func ExpiryOf(date string, now time.Time) (int64, error) {
	date = strings.TrimSpace(date)
	if date == "" {
		return 0, nil
	}
	t, err := time.Parse(time.DateOnly, date)
	if err != nil {
		return 0, fmt.Errorf("%w: the expiry is year-month-day", ErrBadInput)
	}
	end := t.AddDate(0, 0, 1).Add(-time.Second)
	if !end.After(now) {
		return 0, fmt.Errorf("%w: the expiry is in the past", ErrBadInput)
	}
	if end.After(now.AddDate(maxKeyExpiryYears, 0, 0)) {
		return 0, fmt.Errorf("%w: the expiry is at most %d years ahead", ErrBadInput, maxKeyExpiryYears)
	}
	return end.Unix(), nil
}

// KeyRow is one signing key, cleaned for drawing.
type KeyRow struct {
	ID            string
	Title         string
	Kind          string
	Fingerprint   string
	Created       time.Time
	Expires       time.Time
	Revoked       time.Time
	RevokedReason string
	LastUsed      time.Time
	// State is live, expired or revoked.
	State string
}

func kindLabel(k ascv1.SigningKeyKind) string {
	switch k {
	case ascv1.SigningKeyKind_SIGNING_KEY_KIND_SSH_ED25519:
		return "ed25519"
	case ascv1.SigningKeyKind_SIGNING_KEY_KIND_SSH_ECDSA_P256:
		return "ecdsa-p256"
	case ascv1.SigningKeyKind_SIGNING_KEY_KIND_SSH_RSA:
		return "rsa"
	}
	return "unknown"
}

var fingerprintOK = func(s string) bool {
	if !strings.HasPrefix(s, "SHA256:") || len(s) > 64 {
		return false
	}
	for _, c := range s[len("SHA256:"):] {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '+' || c == '/' || c == '=') {
			return false
		}
	}
	return true
}

// KeyOf checks and cleans one key of an answer.
func KeyOf(k *ascv1.SigningKey, now time.Time) (KeyRow, error) {
	if k == nil || k.GetId() == "" || len(k.GetId()) > 64 || !fingerprintOK(k.GetFingerprint()) {
		return KeyRow{}, fmt.Errorf("%w: signing key", ErrBadAnswer)
	}
	row := KeyRow{
		ID:            CleanText(k.GetId(), 64),
		Title:         CleanText(k.GetTitle(), maxKeyTitleBytes),
		Kind:          kindLabel(k.GetKind()),
		Fingerprint:   k.GetFingerprint(),
		Created:       ts(k.GetCreated()),
		Expires:       ts(k.GetExpiresAt()),
		Revoked:       ts(k.GetRevokedAt()),
		RevokedReason: CleanText(k.GetRevokedReason(), maxRevokeReason),
		LastUsed:      ts(k.GetLastUsed()),
		State:         "live",
	}
	switch {
	case !row.Revoked.IsZero():
		row.State = "revoked"
	case !row.Expires.IsZero() && !row.Expires.After(now):
		row.State = "expired"
	}
	return row, nil
}

// KeysOf checks and cleans an answer of Signing.ListKeys: live keys first, then the others, each newest first.
func KeysOf(resp *ascv1.ListSigningKeysResponse, now time.Time) ([]KeyRow, error) {
	if len(resp.GetKeys()) > MaxListItems {
		return nil, fmt.Errorf("%w: too many keys", ErrBadAnswer)
	}
	var live, other []KeyRow
	for _, k := range resp.GetKeys() {
		row, err := KeyOf(k, now)
		if err != nil {
			return nil, err
		}
		if row.State == "live" {
			live = append(live, row)
		} else {
			other = append(other, row)
		}
	}
	byNewest := func(rows []KeyRow) {
		for i := 1; i < len(rows); i++ {
			for j := i; j > 0 && rows[j].Created.After(rows[j-1].Created); j-- {
				rows[j], rows[j-1] = rows[j-1], rows[j]
			}
		}
	}
	byNewest(live)
	byNewest(other)
	return append(live, other...), nil
}

// ServerKey is the key that signs the commits the server makes.
type ServerKey struct {
	Configured  bool
	Kind        string
	Fingerprint string
	PublicKey   string
}

// ServerKeyOf cleans an answer of Signing.GetServerKey.
func ServerKeyOf(resp *ascv1.GetServerKeyResponse) ServerKey {
	if !resp.GetConfigured() || !fingerprintOK(resp.GetFingerprint()) {
		return ServerKey{}
	}
	return ServerKey{
		Configured:  true,
		Kind:        kindLabel(resp.GetKind()),
		Fingerprint: resp.GetFingerprint(),
		PublicKey:   CleanText(resp.GetPublicKey(), maxKeyLineBytes),
	}
}

// Badge is the signature badge of a change version.
type Badge struct {
	// Label is Verified, Unverified or Unsigned; empty means no badge is drawn.
	Label string
	// Tone is ok, wn or no.
	Tone string
	// Title explains the label; the page puts it in the tooltip and in text for screen readers.
	Title string
	// Signer is the handle of the key owner, for a verified signature.
	Signer      string
	Fingerprint string
}

// BadgeOf turns a verification into a badge. requireSigned says whether the policy needs a signature to land: an
// unsigned version is then marked, otherwise it has no badge (most changes are not signed, and a mark on all of
// them says nothing).
func BadgeOf(v *ascv1.SignatureVerification, requireSigned bool) Badge {
	reason := CleanText(v.GetReason(), MaxTextBytes)
	fp := v.GetFingerprint()
	if !fingerprintOK(fp) {
		fp = ""
	}
	signer := CleanText(v.GetSigner(), 128)
	switch v.GetStatus() {
	case ascv1.VerificationStatus_VERIFICATION_STATUS_VALID:
		title := "Signed with a key that belongs to the author."
		if signer != "" {
			title = "Signed by " + signer + " with a key that belongs to the author."
		}
		return Badge{Label: "Verified", Tone: "ok", Title: title, Signer: signer, Fingerprint: fp}
	case ascv1.VerificationStatus_VERIFICATION_STATUS_UNSIGNED:
		if requireSigned {
			return Badge{Label: "Unsigned", Tone: "no", Title: "This version is not signed, and the policy of this repository needs a signature to land."}
		}
		return Badge{}
	case ascv1.VerificationStatus_VERIFICATION_STATUS_UNKNOWN_KEY:
		return Badge{Label: "Unverified", Tone: "wn", Title: "The signature is good, but nobody has registered the key.", Fingerprint: fp}
	case ascv1.VerificationStatus_VERIFICATION_STATUS_KEY_NOT_AUTHOR:
		return Badge{Label: "Unverified", Tone: "wn", Title: "The signature is good, but the key belongs to someone other than the author.", Signer: signer, Fingerprint: fp}
	case ascv1.VerificationStatus_VERIFICATION_STATUS_KEY_EXPIRED:
		return Badge{Label: "Unverified", Tone: "wn", Title: "The key had expired when the signature was sent.", Signer: signer, Fingerprint: fp}
	case ascv1.VerificationStatus_VERIFICATION_STATUS_KEY_REVOKED:
		return Badge{Label: "Unverified", Tone: "no", Title: "The key was revoked before the signature was sent.", Signer: signer, Fingerprint: fp}
	case ascv1.VerificationStatus_VERIFICATION_STATUS_BAD_SIGNATURE:
		return Badge{Label: "Unverified", Tone: "no", Title: "The signature does not match this version.", Fingerprint: fp}
	case ascv1.VerificationStatus_VERIFICATION_STATUS_UNSUPPORTED:
		return Badge{Label: "Unverified", Tone: "wn", Title: "The signature is in a form that ASC does not accept.", Fingerprint: fp}
	}
	if reason != "" {
		return Badge{Label: "Unverified", Tone: "wn", Title: reason}
	}
	return Badge{Label: "Unverified", Tone: "wn", Title: "The signature could not be checked."}
}

// VerifyCommits returns the badge of each git commit, keyed by the commit id (40 lower-case hex digits). A commit
// asc does not know has no entry. It maps the commits to changesets (Repo.GitMap) and verifies them in one call
// (Signing.Verify, at most 100). The calls are the credential of ctx: the page has already checked that the viewer
// may read the repository, so the hub's own credential is enough.
func VerifyCommits(ctx context.Context, conn grpc.ClientConnInterface, repoName string, commits []string, requireSigned bool) (map[string]Badge, error) {
	out := map[string]Badge{}
	if len(commits) == 0 {
		return out, nil
	}
	if len(commits) > maxVerifyBatch {
		commits = commits[:maxVerifyBatch]
	}
	raw := make([][]byte, 0, len(commits))
	ids := make([]string, 0, len(commits))
	for _, c := range commits {
		b, err := hex.DecodeString(c)
		if err != nil || len(b) != 20 {
			return nil, fmt.Errorf("%w: commit id", ErrBadInput)
		}
		raw = append(raw, b)
		ids = append(ids, hex.EncodeToString(b))
	}
	mapped, err := ascv1.NewRepoClient(conn).GitMap(ctx, &ascv1.GitMapRequest{Repo: RepoRef(repoName), Git: raw})
	if err != nil {
		return nil, err
	}
	if len(mapped.GetGitToAsc()) != len(raw) {
		return nil, fmt.Errorf("%w: git map", ErrBadAnswer)
	}
	var sets [][]byte
	var setIDs []string
	for i, cs := range mapped.GetGitToAsc() {
		if len(cs) == 0 {
			continue
		}
		if len(cs) != 32 {
			return nil, fmt.Errorf("%w: git map", ErrBadAnswer)
		}
		sets = append(sets, cs)
		setIDs = append(setIDs, ids[i])
	}
	if len(sets) == 0 {
		return out, nil
	}
	ver, err := ascv1.NewSigningClient(conn).Verify(ctx, &ascv1.VerifySignaturesRequest{Repo: RepoRef(repoName), Changesets: sets})
	if err != nil {
		return nil, err
	}
	if len(ver.GetResults()) != len(sets) {
		return nil, fmt.Errorf("%w: verify", ErrBadAnswer)
	}
	for i, r := range ver.GetResults() {
		if len(r.GetChangeset()) != 0 && string(r.GetChangeset()) != string(sets[i]) {
			return nil, fmt.Errorf("%w: verify order", ErrBadAnswer)
		}
		out[setIDs[i]] = BadgeOf(r, requireSigned)
	}
	return out, nil
}
