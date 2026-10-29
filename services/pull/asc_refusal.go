// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"regexp"
	"strings"
	"unicode"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// AscRefusal is a refusal of asc that a person can act on: the policy or the gate said no. Code is the machine word
// ("POLICY_DENIED", "LAND_NEEDS_CONTENT_MERGE", ...) and Message is asc's sentence ("landing on trunk needs the
// approvals of the policy"), cleaned. The page shows the message, so nobody has to read a gRPC error.
type AscRefusal struct {
	Code    string
	Message string
}

func (e *AscRefusal) Error() string { return "asc refused: " + e.Code + ": " + e.Message }

// maxRefusalBytes bounds what is kept of a refusal.
const maxRefusalBytes = 400

// refusalInDetails finds the refusal in the text of an Internal error that asc sent with a typed-error encoding the
// client could not read ("grpc-status-details-bin mismatch: grpc-status=FailedPrecondition, grpc-message=\"...\""):
// the status and the message are still in the text.
var refusalInDetails = regexp.MustCompile(`grpc-status=(FailedPrecondition|PermissionDenied|InvalidArgument|Aborted), grpc-message="((?:[^"\\]|\\.)*)"`)

// ascRefusalOf extracts the refusal from err, or nil when err is not one. A refusal is a gRPC error whose code is
// FailedPrecondition, PermissionDenied, InvalidArgument or Aborted and whose message is "CODE: sentence", or the
// Internal error described at refusalInDetails.
func ascRefusalOf(err error) *AscRefusal {
	st, ok := status.FromError(err)
	if !ok {
		return nil
	}
	msg := st.Message()
	switch st.Code() {
	case codes.FailedPrecondition, codes.PermissionDenied, codes.InvalidArgument, codes.Aborted:
	case codes.Internal:
		m := refusalInDetails.FindStringSubmatch(msg)
		if m == nil {
			return nil
		}
		msg = strings.NewReplacer(`\"`, `"`, `\\`, `\`).Replace(m[2])
	default:
		return nil
	}
	code, text, found := strings.Cut(msg, ": ")
	if !found || code == "" || strings.ContainsAny(code, " \t") || strings.ToUpper(code) != code {
		code, text = "", msg
	}
	text = cleanRefusalText(text)
	if text == "" {
		return nil
	}
	return &AscRefusal{Code: code, Message: text}
}

func cleanRefusalText(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == unicode.ReplacementChar {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > maxRefusalBytes {
		s = strings.ToValidUTF8(s[:maxRefusalBytes], "") + "..."
	}
	return s
}
