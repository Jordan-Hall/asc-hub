// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package asc

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git"
	"forgejo.org/modules/repostore"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// repoNotFoundMessage is what asc serve says when it has no such repository.
const repoNotFoundMessage = "no such repository"

// Error is a failed asc call that has no more specific type. It never carries a credential: the message
// is the server's status text, reduced to printable ASCII and cut short.
type Error struct {
	Op   string
	Code codes.Code
	// Detail is the ErrorDetail.code of the answer (REF_CONFLICT, HOOK_REJECTED, ...), when it has one.
	Detail string
	Msg    string
}

func (e *Error) Error() string {
	detail := ""
	if e.Detail != "" {
		detail = " (" + e.Detail + ")"
	}
	if e.Msg == "" {
		return fmt.Sprintf("repostore/asc: %s: asc server answered %s%s", e.Op, e.Code, detail)
	}
	return fmt.Sprintf("repostore/asc: %s: asc server answered %s%s: %s", e.Op, e.Code, detail, e.Msg)
}

// Is maps status codes onto the repostore errors.
func (e *Error) Is(target error) bool {
	switch target {
	case repostore.ErrUnsupported:
		return e.Code == codes.Unimplemented
	case ErrTooLarge:
		return e.Code == codes.ResourceExhausted || e.Code == codes.OutOfRange
	}
	return false
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		if b.Len() >= MaxMessageEcho {
			b.WriteString("...")
			break
		}
		if r >= 0x20 && r < 0x7f || r == '\n' {
			b.WriteRune(r)
		} else {
			b.WriteByte('?')
		}
	}
	return b.String()
}

// detailOf returns the ErrorDetail an answer carries, or nil.
func detailOf(st *status.Status) *ascv1.ErrorDetail {
	for _, d := range st.Details() {
		if ed, ok := d.(*ascv1.ErrorDetail); ok {
			return ed
		}
	}
	return nil
}

// callErr turns the error of an asc call into an error of this package. Local errors (a cancelled
// context, a missing credential) pass through unchanged.
func callErr(op string, err error) error {
	if err == nil {
		return nil
	}
	st, ok := status.FromError(err)
	if !ok || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	msg := sanitize(st.Message())
	detail := detailOf(st)
	code := ""
	if detail != nil {
		code = detail.GetCode()
	}
	switch {
	case code == "PATH_POLICY" && detail != nil && len(detail.GetPaths()) > 0:
		return &repostore.InvalidPathError{Path: sanitize(detail.GetPaths()[0])}
	case st.Code() == codes.AlreadyExists:
		return repostore.ErrExists
	case st.Code() == codes.InvalidArgument:
		return fmt.Errorf("%w: %s: %s", repostore.ErrInvalidRequest, op, msg)
	case st.Code() == codes.NotFound && st.Message() != repoNotFoundMessage:
		if path, ok := strings.CutSuffix(st.Message(), " does not exist"); ok && !strings.HasPrefix(path, "ref ") {
			return &repostore.FileNotFoundError{Path: sanitize(path)}
		}
		return &repostore.RefNotFoundError{Ref: msg}
	}
	return &Error{Op: op, Code: st.Code(), Detail: code, Msg: msg}
}

// publishErr is callErr for the call that moves a ref. A refusal that git reports as a rejected or
// out-of-date push becomes the types the callers already handle.
func publishErr(op string, err error) error {
	if err == nil {
		return nil
	}
	st, ok := status.FromError(err)
	if !ok || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	msg := sanitize(st.Message())
	code := ""
	stCode := st.Code()
	shown := ""
	if r, ok := ascclient.RefusalOf(err); ok {
		// The typed refusal (native Any in the status details, or the text form of an older asc). The text of asc
		// stays as it is for the codes this function knows; any other gate refusal is shown with its code and files.
		code, stCode, msg, shown = r.Code, r.Status, sanitize(r.Message), sanitize(r.Text())
	} else if d := detailOf(st); d != nil {
		code = d.GetCode()
	}
	switch code {
	case "NOT_FAST_FORWARD", "REF_CONFLICT":
		return &git.ErrPushOutOfDate{StdErr: msg, Err: err}
	case "HOOK_REJECTED", "REF_NOT_SHARED", "QUOTA_EXCEEDED", "USER_REFS_LIMIT", "MIRROR_MODE", "PATH_POLICY":
		return &git.ErrPushRejected{Message: msg, StdErr: "remote: " + msg, Err: err}
	}
	if code != "" && (stCode == codes.FailedPrecondition || stCode == codes.PermissionDenied) {
		// Any other typed refusal of the gate (SECRET_DETECTED, a missing review, a signature, ...) is a rejected
		// push too, with its reason.
		return &git.ErrPushRejected{Message: shown, StdErr: "remote: " + shown, Err: err}
	}
	if st.Code() == codes.FailedPrecondition && strings.Contains(strings.ToLower(st.Message()), "fast-forward") {
		return &git.ErrPushOutOfDate{StdErr: msg, Err: err}
	}
	return callErr(op, err)
}

// refMissing is the error for a revision that asc does not have.
func refMissing(rev string) error {
	return &repostore.RefNotFoundError{Ref: rev}
}

// PolicyRefusal is the sentence of asc's refusal of a write because of the repository's policy (the approvals and
// checks a landing needs, a change that is not allowed), when err holds one; it is what a person is told.
func PolicyRefusal(err error) (string, bool) {
	var e *Error
	if !errors.As(err, &e) {
		return "", false
	}
	switch e.Code {
	case codes.PermissionDenied, codes.FailedPrecondition:
		if e.Msg != "" {
			return e.Msg, true
		}
	}
	return "", false
}
