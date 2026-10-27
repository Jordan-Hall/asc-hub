// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"errors"
	"regexp"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Kind groups the failures of an asc call by what a person can do about them.
type Kind int

const (
	// KindOther is a failure nobody can act on from the page: a bad answer, an internal error.
	KindOther Kind = iota
	// KindUnavailable is a server that cannot be reached, a call that timed out, or a request cancelled.
	KindUnavailable
	// KindNotFound is a resource that does not exist or that the caller may not see.
	KindNotFound
	// KindDenied is a caller who may not do this.
	KindDenied
	// KindInvalid is an input that asc refused.
	KindInvalid
	// KindConflict is a state that does not allow this now.
	KindConflict
	// KindLimit is a quota or a rate limit.
	KindLimit
	// KindUnimplemented is a server that lacks the feature.
	KindUnimplemented
)

// Error is a failed asc call, reduced to what a page may show. The text asc sent is dropped: it can name
// internals, and nothing here is built from it. Code is the stable slug of ErrorDetail.code, when asc sent one.
type Error struct {
	Kind Kind
	Code string
}

func (e *Error) Error() string {
	if e.Code != "" {
		return "asc call failed: " + e.Code
	}
	return "asc call failed"
}

var codePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

// Classify turns the error of an asc call into an *Error. It returns nil for nil. A context that ended is
// KindUnavailable. An error that is not a gRPC status is KindOther.
func Classify(err error) *Error {
	if err == nil {
		return nil
	}
	var already *Error
	if errors.As(err, &already) {
		return already
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return &Error{Kind: KindUnavailable}
	}
	st, ok := status.FromError(err)
	if !ok {
		return &Error{Kind: KindOther}
	}
	e := &Error{Kind: kindOfCode(st.Code())}
	for _, d := range st.Details() {
		if detail, ok := d.(*ascv1.ErrorDetail); ok {
			if c := detail.GetCode(); codePattern.MatchString(c) {
				e.Code = c
			}
			break
		}
	}
	return e
}

func kindOfCode(c codes.Code) Kind {
	switch c {
	case codes.NotFound:
		return KindNotFound
	case codes.PermissionDenied, codes.Unauthenticated:
		return KindDenied
	case codes.InvalidArgument, codes.OutOfRange:
		return KindInvalid
	case codes.FailedPrecondition, codes.Aborted, codes.AlreadyExists:
		return KindConflict
	case codes.ResourceExhausted:
		return KindLimit
	case codes.Unavailable, codes.DeadlineExceeded, codes.Canceled:
		return KindUnavailable
	case codes.Unimplemented:
		return KindUnimplemented
	}
	return KindOther
}

// Status is the HTTP status a page answers with for the failure.
func (e *Error) Status() int {
	switch e.Kind {
	case KindNotFound:
		return 404
	case KindDenied:
		return 403
	case KindInvalid:
		return 400
	case KindConflict:
		return 409
	case KindLimit:
		return 429
	case KindUnavailable, KindUnimplemented:
		return 503
	}
	return 502
}

// messages holds the fixed texts by ErrorDetail.code. They tell the person what to do, never what asc said.
var messages = map[string]string{
	"POLICY_INVALID":      "The policy file is not valid. Check it and try again.",
	"POLICY_LOCKOUT":      "That policy would leave nobody who could change it again, so it was not accepted.",
	"POLICY_DENIED":       "The repository policy does not allow this.",
	"VERSION_CONFLICT":    "The trunk moved while you were editing. Reload the page and apply your change again.",
	"SECRET_DETECTED":     "Secret scanning found something that looks like a secret.",
	"UNSIGNED_CHANGE":     "This change is not signed.",
	"SIGNATURE_INVALID":   "That signature is not valid.",
	"SIGNING_KEY_LIMIT":   "You have reached the limit of signing keys. Revoke one you no longer use.",
	"SEARCH_TOO_BROAD":    "That search is too broad.",
	"RATE_LIMITED":        "Too many requests. Try again in a moment.",
	"CHANGE_NOT_ALLOWED":  "You may not change this.",
	"ALREADY_EXISTS":      "That already exists.",
	"QUARANTINE_DISABLED": "The policy of this repository does not allow quarantining tests.",
	"REF_UNDO_PROTECTED":  "That branch is protected, so it cannot be undone here. Revert the landed changes instead.",
	"REF_LOG_EXPIRED":     "That operation is too old to undo.",
}

// Message is the text for the person. It depends only on the code and the kind of the failure.
func (e *Error) Message() string {
	if m, ok := messages[e.Code]; ok {
		return m
	}
	switch e.Kind {
	case KindNotFound:
		return "That was not found, or you may not see it."
	case KindDenied:
		return "You do not have the right to do this."
	case KindInvalid:
		return "ASC refused the request. Check what you entered and try again."
	case KindConflict:
		return "That is not possible in the current state. Reload the page and try again."
	case KindLimit:
		return "Too many requests. Try again in a moment."
	case KindUnavailable:
		return "The ASC server did not answer in time. Try again in a moment."
	case KindUnimplemented:
		return "This server does not have that feature yet."
	}
	return "ASC could not do this right now."
}

// codeKeys maps ErrorDetail.code to the locale key (under "asc_trust.err.") of its text. Every entry of messages has
// one, so the English text above and the locale file say the same.
var codeKeys = map[string]string{
	"POLICY_INVALID":      "policy_invalid",
	"POLICY_LOCKOUT":      "policy_lockout",
	"POLICY_DENIED":       "policy_denied",
	"VERSION_CONFLICT":    "version_conflict",
	"SECRET_DETECTED":     "secret_detected",
	"UNSIGNED_CHANGE":     "unsigned_change",
	"SIGNATURE_INVALID":   "signature_invalid",
	"SIGNING_KEY_LIMIT":   "signing_key_limit",
	"SEARCH_TOO_BROAD":    "search_too_broad",
	"RATE_LIMITED":        "rate_limited",
	"CHANGE_NOT_ALLOWED":  "change_not_allowed",
	"ALREADY_EXISTS":      "already_exists",
	"QUARANTINE_DISABLED": "quarantine_disabled",
	"REF_UNDO_PROTECTED":  "ref_undo_protected",
	"REF_LOG_EXPIRED":     "ref_log_expired",
}

// kindKeys is the locale key of the text of a kind of failure that has no code of its own.
var kindKeys = map[Kind]string{
	KindNotFound:      "not_found",
	KindDenied:        "denied",
	KindInvalid:       "invalid",
	KindConflict:      "conflict",
	KindLimit:         "limit",
	KindUnavailable:   "unavailable",
	KindUnimplemented: "unimplemented",
	KindOther:         "other",
}

// MessageKey is the full locale key ("asc_trust.err.<name>") of the text for the person. It depends only on the code
// and the kind of the failure, like Message.
func (e *Error) MessageKey() string {
	if k, ok := codeKeys[e.Code]; ok {
		return "asc_trust.err." + k
	}
	if k, ok := kindKeys[e.Kind]; ok {
		return "asc_trust.err." + k
	}
	return "asc_trust.err.other"
}
