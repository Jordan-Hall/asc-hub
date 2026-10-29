// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package ascclient

import (
	"regexp"
	"strings"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Refusal is a refusal of asc (a gate, a policy, a hook, a conflict) as the caller can show it: the status, the
// typed code of asc's ErrorDetail and the files or refs it is about.
type Refusal struct {
	// Status is the gRPC status asc meant.
	Status codes.Code
	// Code is the typed code (SECRET_DETECTED, PATH_POLICY, REF_CONFLICT, HOOK_REJECTED, ...), when there is one.
	Code string
	// Paths are what the refusal is about, as asc sent them.
	Paths []string
	// Message is asc's text.
	Message string
}

// RefusalOf reads the refusal out of the error of an asc call. asc sends the typed ErrorDetail as an Any inside a
// google.rpc.Status (grpc-status-details-bin), which grpc-go gives as st.Details(). Older asc servers sent the detail
// in a form a Go client cannot read: the call then failed as Internal with "grpc-status-details-bin mismatch:
// grpc-status=<Code>, grpc-message=<text>" as its text, and the status and text are read from that. It reports
// false for an error that is no refusal (a transport error, an internal error without a typed detail).
func RefusalOf(err error) (Refusal, bool) {
	st, ok := status.FromError(err)
	if !ok {
		return Refusal{}, false
	}
	r := Refusal{Status: st.Code(), Message: st.Message()}
	found := false
	for _, d := range st.Details() {
		if ed, ok := d.(*ascv1.ErrorDetail); ok {
			r.Code, r.Paths, found = ed.GetCode(), ed.GetPaths(), true
			break
		}
	}
	if c, m, ok := unwrapStatusMismatch(st.Code(), st.Message()); ok {
		r.Status, r.Message, found = c, m, true
		r.Code = codeInText(m)
	}
	if !found {
		return Refusal{}, false
	}
	return r, true
}

// Text is the refusal as one line: the text of asc, with the typed code in front and the files after it where
// the text does not name them.
func (r Refusal) Text() string {
	msg := r.Message
	if r.Code != "" && !strings.Contains(msg, r.Code) {
		msg = r.Code + ": " + msg
	}
	var missing []string
	for i, p := range r.Paths {
		if i == 8 {
			break
		}
		if p != "" && !strings.Contains(msg, p) {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		msg += " (" + strings.Join(missing, ", ") + ")"
	}
	return msg
}

var statusMismatch = regexp.MustCompile(`grpc-status=(\w+), grpc-message="((?:[^"\]|\.)*)"`)

func unwrapStatusMismatch(code codes.Code, text string) (codes.Code, string, bool) {
	if code != codes.Internal || !strings.Contains(text, "grpc-status-details-bin mismatch") {
		return code, text, false
	}
	m := statusMismatch.FindStringSubmatch(text)
	if m == nil {
		return code, text, false
	}
	for c := codes.Canceled; c <= codes.Unauthenticated; c++ {
		if c.String() == m[1] {
			return c, strings.NewReplacer(`\"`, `"`, `\`, `\`).Replace(m[2]), true
		}
	}
	return code, text, false
}

// codeInText reads a leading typed code ("SECRET_DETECTED: ...") out of a refusal text.
func codeInText(msg string) string {
	code, _, ok := strings.Cut(msg, ":")
	if !ok || code == "" || len(code) > 64 {
		return ""
	}
	for _, r := range code {
		if (r < 'A' || r > 'Z') && r != '_' && (r < '0' || r > '9') {
			return ""
		}
	}
	return code
}
