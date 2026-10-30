// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"strings"
	"unicode"

	"forgejo.org/modules/ascclient"

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

// ascRefusalOf extracts the refusal from err, or nil when err is not one. A refusal is a gRPC error whose code is
// FailedPrecondition, PermissionDenied, InvalidArgument or Aborted. The typed code and the files come from asc's
// ErrorDetail (ascclient.RefusalOf reads it natively from the status details, or out of the text of an older asc);
// without one, the message is "CODE: sentence".
func ascRefusalOf(err error) *AscRefusal {
	st, ok := status.FromError(err)
	if !ok {
		return nil
	}
	code, msg := "", st.Message()
	if r, typed := ascclient.RefusalOf(err); typed {
		switch r.Status {
		case codes.FailedPrecondition, codes.PermissionDenied, codes.InvalidArgument, codes.Aborted:
		default:
			return nil
		}
		code = r.Code
		// The files the refusal is about, where its sentence does not name them.
		msg = ascclient.Refusal{Message: r.Message, Paths: r.Paths}.Text()
	} else {
		switch st.Code() {
		case codes.FailedPrecondition, codes.PermissionDenied, codes.InvalidArgument, codes.Aborted:
		default:
			return nil
		}
	}
	if code != "" {
		msg = strings.TrimPrefix(msg, code+": ") // the sentence of asc may carry its own code
	} else {
		c, text, found := strings.Cut(msg, ": ")
		if found && c != "" && !strings.ContainsAny(c, " 	") && strings.ToUpper(c) == c {
			code, msg = c, text
		}
	}
	text := cleanRefusalText(msg)
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
