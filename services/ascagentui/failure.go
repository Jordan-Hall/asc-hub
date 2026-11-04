// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascagentui

import (
	"context"
	"errors"
	"net/http"
	"strings"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/translation"

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
	// Message is a fixed sentence in English, for logs. Key is the locale key of the same sentence; a page shows
	// Text.
	Message string
	Key     string
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
		return &Failure{HTTP: http.StatusBadGateway, Message: "asc gave an answer the hub cannot use.", Key: "asc_agentui.fail.bad_answer", Unavailable: true}
	}
	if errors.Is(err, context.Canceled) {
		return &Failure{HTTP: http.StatusRequestTimeout, Message: "The request was cancelled.", Key: "asc_agentui.fail.cancelled"}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &Failure{HTTP: http.StatusGatewayTimeout, Message: "asc did not answer in time. Try again.", Key: "asc_agentui.fail.timeout", Unavailable: true}
	}
	st, ok := status.FromError(err)
	if !ok {
		return &Failure{HTTP: http.StatusBadGateway, Message: "asc could not be reached.", Key: "asc_agentui.fail.unreachable", Unavailable: true}
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
		f.HTTP, f.Message, f.Key, f.Disabled = known.http, known.message, known.key, known.disabled
		if f.Code == "BUDGET_EXCEEDED" {
			f.Message, f.Key = budgetMessage(f.Details)
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
		f.HTTP, f.Message, f.Key = http.StatusNotFound, "That does not exist, or you may not see it.", "asc_agentui.fail.not_found"
	case codes.InvalidArgument:
		f.HTTP, f.Message, f.Key = http.StatusBadRequest, "asc did not accept those values.", "asc_agentui.fail.invalid"
	case codes.PermissionDenied, codes.Unauthenticated:
		f.HTTP, f.Message, f.Key = http.StatusForbidden, "You may not do that.", "asc_agentui.fail.denied"
	case codes.AlreadyExists, codes.Aborted:
		f.HTTP, f.Message, f.Key = http.StatusConflict, "That changed while you were looking. Reload and try again.", "asc_agentui.fail.changed"
	case codes.FailedPrecondition:
		f.HTTP, f.Message, f.Key = http.StatusConflict, "That cannot be done in the current state.", "asc_agentui.fail.precondition"
	case codes.ResourceExhausted:
		f.HTTP, f.Message, f.Key = http.StatusTooManyRequests, "A limit is reached. Try again later.", "asc_agentui.fail.limit"
	case codes.DeadlineExceeded:
		f.HTTP, f.Message, f.Key, f.Unavailable = http.StatusGatewayTimeout, "asc did not answer in time. Try again.", "asc_agentui.fail.timeout", true
	case codes.Canceled:
		f.HTTP, f.Message, f.Key = http.StatusRequestTimeout, "The request was cancelled.", "asc_agentui.fail.cancelled"
	case codes.Unimplemented:
		f.HTTP, f.Message, f.Key, f.Disabled = http.StatusNotImplemented, "This asc server does not offer this feature.", "asc_agentui.fail.unimplemented", true
	default:
		f.HTTP, f.Message, f.Key, f.Unavailable = http.StatusBadGateway, "asc could not be reached or gave an unusable answer.", "asc_agentui.fail.unusable", true
	}
	return f
}

type knownFailure struct {
	http     int
	message  string
	disabled bool
	key      string
}

var failureByCode = map[string]knownFailure{
	"AGENTS_DISABLED":     {http.StatusConflict, "Agents are not turned on for this repository. A repository admin turns them on in the repository policy, and the server must allow them.", true, "asc_agentui.fail.agents_disabled"},
	"WORKFLOW_DISABLED":   {http.StatusConflict, "This workflow is switched off.", true, "asc_agentui.fail.workflow_disabled"},
	"WORKFLOW_INVALID":    {http.StatusBadRequest, "The workflow definition is not valid.", false, "asc_agentui.fail.workflow_invalid"},
	"AGENT_TOKEN_EXPIRED": {http.StatusForbidden, "The agent token has expired.", false, "asc_agentui.fail.agent_token_expired"},
	"AGENT_TOKEN_REVOKED": {http.StatusForbidden, "The agent token was revoked.", false, "asc_agentui.fail.agent_token_revoked"},
	"AGENT_SCOPE_DENIED":  {http.StatusForbidden, "The repository policy does not let an agent write those paths.", false, "asc_agentui.fail.agent_scope_denied"},
	"BUDGET_EXCEEDED":     {http.StatusTooManyRequests, "A budget is reached.", false, "asc_agentui.fail.budget_exceeded"},
	"RUN_NOT_ACTIVE":      {http.StatusConflict, "That run has already finished.", false, "asc_agentui.fail.run_not_active"},
	"AI_KEY_UNTESTED":     {http.StatusConflict, "Test that AI key first. An untested key cannot start a run.", false, "asc_agentui.fail.ai_key_untested"},
	"AI_CAP_REACHED":      {http.StatusTooManyRequests, "The monthly cap of that AI key is reached.", false, "asc_agentui.fail.ai_cap_reached"},
	"AI_ENDPOINT_BLOCKED": {http.StatusBadRequest, "The AI key points at an address that is not allowed.", false, "asc_agentui.fail.ai_endpoint_blocked"},
}

// Text is the sentence of f in the language of l. Without a locale it is the English Message.
func (f *Failure) Text(l translation.Locale) string {
	if f == nil {
		return ""
	}
	if f.Key == "" || l == nil {
		return f.Message
	}
	return l.TrString(f.Key)
}

// budgetMessage returns the sentence and its locale key for the budget named first in details.
func budgetMessage(details []string) (string, string) {
	if len(details) > 0 {
		switch strings.ToLower(details[0]) {
		case "changes":
			return "The limit on changes is reached.", "asc_agentui.fail.budget_changes"
		case "tokens":
			return "The limit on model tokens is reached.", "asc_agentui.fail.budget_tokens"
		case "cost":
			return "The cost limit is reached.", "asc_agentui.fail.budget_cost"
		case "paths":
			return "The limit on written paths is reached.", "asc_agentui.fail.budget_paths"
		case "runtime":
			return "The time limit is reached.", "asc_agentui.fail.budget_runtime"
		}
	}
	return "A budget is reached.", "asc_agentui.fail.budget"
}
