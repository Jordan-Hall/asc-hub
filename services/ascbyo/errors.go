// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascbyo

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// FailureKind classifies what went wrong, for the person at the page. The page shows a fixed, translated
// text for the kind; it never shows an error string from the transport.
type FailureKind string

const (
	FailInvalid         FailureKind = "invalid"
	FailNotFound        FailureKind = "not_found"
	FailExists          FailureKind = "exists"
	FailDenied          FailureKind = "denied"
	FailEndpointBlocked FailureKind = "endpoint_blocked"
	FailVerifyFailed    FailureKind = "verify_failed"
	FailNotReady        FailureKind = "not_ready"
	FailInUse           FailureKind = "in_use"
	FailMigrationActive FailureKind = "migration_active"
	FailQuota           FailureKind = "quota"
	FailRateLimited     FailureKind = "rate_limited"
	FailUnavailable     FailureKind = "unavailable"
	FailUnimplemented   FailureKind = "unimplemented"
	FailKeyUntested     FailureKind = "key_untested"
	FailCapReached      FailureKind = "cap_reached"
	FailInternal        FailureKind = "internal"
)

const (
	failDetailMaxBytes   = 300
	failReasonMaxBytes   = 32
	failMessageSecretMin = 4
)

// blockedReasons are the slugs asc puts in ErrorDetail.paths[0] for STORAGE_ENDPOINT_BLOCKED and
// AI_ENDPOINT_BLOCKED (BUILDOUT section 2). Anything else is shown as "other".
var blockedReasons = map[string]bool{
	"loopback": true, "private": true, "link_local": true, "metadata": true, "not_https": true,
	"credentials_in_url": true, "unresolvable": true, "port": true, "reserved": true,
}

// Failure is the error type of this package. It never holds a secret: Detail is asc's message for a
// rejected request after the checks of cleanText, and empty for every other kind.
type Failure struct {
	Kind FailureKind
	// Reason is a slug from blockedReasons (or "other"), set for FailEndpointBlocked.
	Reason string
	// Detail is the cleaned message asc wrote for a rejected request (FailInvalid and FailNotReady only).
	Detail string
	Code   codes.Code
}

func (f *Failure) Error() string {
	s := "ascbyo: " + string(f.Kind)
	if f.Reason != "" {
		s += " (" + f.Reason + ")"
	}
	return s
}

// LocaleKey is the translation key of the fixed text for the failure.
func (f *Failure) LocaleKey() string { return "ascbyo.error." + string(f.Kind) }

// ReasonLocaleKey is the translation key of the blocked-endpoint reason, or "" when there is none.
func (f *Failure) ReasonLocaleKey() string {
	if f.Reason == "" {
		return ""
	}
	return "ascbyo.blocked." + f.Reason
}

// AsFailure returns the Failure in err's chain.
func AsFailure(err error) (*Failure, bool) {
	var f *Failure
	if errors.As(err, &f) {
		return f, true
	}
	return nil, false
}

// IsKind reports whether err is a Failure of kind k.
func IsKind(err error, k FailureKind) bool {
	f, ok := AsFailure(err)
	return ok && f.Kind == k
}

func failure(kind FailureKind) *Failure { return &Failure{Kind: kind} }

// classify turns an error of a call to asc into a Failure. secrets are the values that were submitted with
// the call: a message that repeats one is dropped whole. The function logs nothing; the caller logs the
// kind and the gRPC code.
func classify(err error, secrets ...string) *Failure {
	if err == nil {
		return nil
	}
	if f, ok := AsFailure(err); ok {
		return f
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return &Failure{Kind: FailUnavailable, Code: codes.DeadlineExceeded}
	}
	st, ok := status.FromError(err)
	if !ok {
		return &Failure{Kind: FailUnavailable, Code: codes.Unknown}
	}
	var detail *ascv1.ErrorDetail
	for _, d := range st.Details() {
		if ed, ok := d.(*ascv1.ErrorDetail); ok {
			detail = ed
			break
		}
	}
	f := &Failure{Code: st.Code()}
	switch detail.GetCode() {
	case "STORAGE_ENDPOINT_BLOCKED", "AI_ENDPOINT_BLOCKED":
		f.Kind = FailEndpointBlocked
		f.Reason = "other"
		if p := detail.GetPaths(); len(p) > 0 && len(p[0]) <= failReasonMaxBytes && blockedReasons[p[0]] {
			f.Reason = p[0]
		}
		return f
	case "STORAGE_VERIFY_FAILED":
		f.Kind = FailVerifyFailed
		return f
	case "STORAGE_IN_USE":
		f.Kind = FailInUse
		return f
	case "MIGRATION_ACTIVE":
		f.Kind = FailMigrationActive
		return f
	case "QUOTA_EXCEEDED":
		f.Kind = FailQuota
		return f
	case "RATE_LIMITED":
		f.Kind = FailRateLimited
		return f
	case "AI_KEY_UNTESTED":
		f.Kind = FailKeyUntested
		return f
	case "AI_CAP_REACHED":
		f.Kind = FailCapReached
		return f
	}
	switch st.Code() {
	case codes.InvalidArgument, codes.OutOfRange:
		f.Kind = FailInvalid
		f.Detail = cleanText(st.Message(), failDetailMaxBytes, secrets)
	case codes.NotFound:
		// asc answers NOT_FOUND for another owner's resource as well, so this is also what a probe sees.
		f.Kind = FailNotFound
	case codes.AlreadyExists:
		f.Kind = FailExists
	case codes.PermissionDenied, codes.Unauthenticated:
		f.Kind = FailDenied
	case codes.FailedPrecondition:
		f.Kind = FailNotReady
		f.Detail = cleanText(st.Message(), failDetailMaxBytes, secrets)
	case codes.Aborted:
		f.Kind = FailMigrationActive
	case codes.ResourceExhausted:
		f.Kind = FailRateLimited
	case codes.Unimplemented:
		f.Kind = FailUnimplemented
	case codes.Unavailable, codes.DeadlineExceeded:
		f.Kind = FailUnavailable
	default:
		f.Kind = FailInternal
	}
	return f
}

// cleanText makes s one line of printable text of at most limit bytes. Control characters become spaces,
// invalid UTF-8 is dropped and a cut never splits a rune. A text that contains any of secrets (4 bytes or
// longer) is replaced by "" because it may be an echo of a credential.
func cleanText(s string, limit int, secrets []string) string {
	for _, sec := range secrets {
		if len(sec) >= failMessageSecretMin && strings.Contains(s, sec) {
			return ""
		}
	}
	var b strings.Builder
	for _, r := range strings.ToValidUTF8(s, "") {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			r = ' '
		}
		if b.Len()+utf8.RuneLen(r) > limit {
			break
		}
		b.WriteRune(r)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// CleanText is cleanText for text that asc returned inside a message (a last error, a check message).
func CleanText(s string, limit int) string { return cleanText(s, limit, nil) }
