// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascagentui

import (
	"context"
	"errors"
	"net/http"
	"strings"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ErrBadAnswer marks an answer of asc that failed the hub's checks (a run of another repository, a sequence that
// does not rise, an id of the wrong shape). Nothing of such an answer is used.
var ErrBadAnswer = errors.New("asc answered with something the hub cannot use")

// Failure is what a person is told when a call to asc failed. Only the gRPC status code and the ErrorDetail code
// of asc's answer are used; the text asc sent is never shown, logged by this package or passed on.
type Failure struct {
	// HTTP is the status for a page that cannot be shown or an action that was refused.
	HTTP int
	// Code is the ErrorDetail code of asc, or "" when it sent none or a code that is not a plain slug.
	Code string
	// Message is a fixed sentence.
	Message string
	// Details are values asc attached to the code (refused path globs, the lines of a workflow diagnosis), cleaned
	// and cut. They are untrusted text.
	Details []string
	// Disabled is true when the feature is off for the repository or the server: a state to explain, not a fault.
	Disabled bool
	// Unavailable is true when asc could not be reached or answered with something unusable. Log it.
	Unavailable bool
}

const maxFailureDetails = 8

// Classify turns an error from a call to asc into a Failure. A nil error returns nil.
func Classify(err error) *Failure {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrBadAnswer) {
		return &Failure{HTTP: http.StatusBadGateway, Message: "asc gave an answer the hub cannot use.", Unavailable: true}
	}
	if errors.Is(err, context.Canceled) {
		return &Failure{HTTP: http.StatusRequestTimeout, Message: "The request was cancelled."}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &Failure{HTTP: http.StatusGatewayTimeout, Message: "asc did not answer in time. Try again.", Unavailable: true}
	}
	st, ok := status.FromError(err)
	if !ok {
		return &Failure{HTTP: http.StatusBadGateway, Message: "asc could not be reached.", Unavailable: true}
	}
	f := &Failure{}
	for _, d := range st.Details() {
		if ed, ok := d.(*ascv1.ErrorDetail); ok {
			f.Code = SafeCode(ed.GetCode())
			for i, p := range ed.GetPaths() {
				if i >= maxFailureDetails {
					break
				}
				if c := CleanLine(p, MaxGlobBytes); c != "" {
					f.Details = append(f.Details, c)
				}
			}
			break
		}
	}
	if known, ok := failureByCode[f.Code]; ok {
		f.HTTP, f.Message, f.Disabled = known.http, known.message, known.disabled
		if f.Code == "BUDGET_EXCEEDED" {
			f.Message = budgetMessage(f.Details)
			f.Details = nil
		}
		if f.Code != "AGENT_SCOPE_DENIED" && f.Code != "WORKFLOW_INVALID" {
			f.Details = nil
		}
		return f
	}
	f.Details = nil
	switch st.Code() {
	case codes.NotFound:
		f.HTTP, f.Message = http.StatusNotFound, "That does not exist, or you may not see it."
	case codes.InvalidArgument:
		f.HTTP, f.Message = http.StatusBadRequest, "asc did not accept those values."
	case codes.PermissionDenied, codes.Unauthenticated:
		f.HTTP, f.Message = http.StatusForbidden, "You may not do that."
	case codes.AlreadyExists, codes.Aborted:
		f.HTTP, f.Message = http.StatusConflict, "That changed while you were looking. Reload and try again."
	case codes.FailedPrecondition:
		f.HTTP, f.Message = http.StatusConflict, "That cannot be done in the current state."
	case codes.ResourceExhausted:
		f.HTTP, f.Message = http.StatusTooManyRequests, "A limit is reached. Try again later."
	case codes.DeadlineExceeded:
		f.HTTP, f.Message, f.Unavailable = http.StatusGatewayTimeout, "asc did not answer in time. Try again.", true
	case codes.Canceled:
		f.HTTP, f.Message = http.StatusRequestTimeout, "The request was cancelled."
	case codes.Unimplemented:
		f.HTTP, f.Message, f.Disabled = http.StatusNotImplemented, "This asc server does not offer this feature.", true
	default:
		f.HTTP, f.Message, f.Unavailable = http.StatusBadGateway, "asc could not be reached or gave an unusable answer.", true
	}
	return f
}

type knownFailure struct {
	http     int
	message  string
	disabled bool
}

var failureByCode = map[string]knownFailure{
	"AGENTS_DISABLED":     {http.StatusConflict, "Agents are not turned on for this repository. A repository admin turns them on in the repository policy, and the server must allow them.", true},
	"WORKFLOW_DISABLED":   {http.StatusConflict, "This workflow is switched off.", true},
	"WORKFLOW_INVALID":    {http.StatusBadRequest, "The workflow definition is not valid.", false},
	"AGENT_TOKEN_EXPIRED": {http.StatusForbidden, "The agent token has expired.", false},
	"AGENT_TOKEN_REVOKED": {http.StatusForbidden, "The agent token was revoked.", false},
	"AGENT_SCOPE_DENIED":  {http.StatusForbidden, "The repository policy does not let an agent write those paths.", false},
	"BUDGET_EXCEEDED":     {http.StatusTooManyRequests, "A budget is reached.", false},
	"RUN_NOT_ACTIVE":      {http.StatusConflict, "That run has already finished.", false},
	"AI_KEY_UNTESTED":     {http.StatusConflict, "Test that AI key first. An untested key cannot start a run.", false},
	"AI_CAP_REACHED":      {http.StatusTooManyRequests, "The monthly cap of that AI key is reached.", false},
	"AI_ENDPOINT_BLOCKED": {http.StatusBadRequest, "The AI key points at an address that is not allowed.", false},
}

func budgetMessage(details []string) string {
	if len(details) > 0 {
		switch strings.ToLower(details[0]) {
		case "changes":
			return "The limit on changes is reached."
		case "tokens":
			return "The limit on model tokens is reached."
		case "cost":
			return "The cost limit is reached."
		case "paths":
			return "The limit on written paths is reached."
		case "runtime":
			return "The time limit is reached."
		}
	}
	return "A budget is reached."
}
