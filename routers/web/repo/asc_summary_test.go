// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func summaryWords(key string, _ ...any) string {
	return map[string]string{
		"asc_rp.intent_what": "What", "asc_rp.intent_why": "Why", "asc_rp.intent_how": "How to check", "asc_rp.intent_risk": "Risk",
		"asc_review.brief_where": "Where to look first", "asc_review.summary_truncated": "Cut short.",
	}[key]
}

func TestAscSummaryOfTheBrief(t *testing.T) {
	resp := &ascv1.SummarizeChangeResponse{Brief: &ascv1.ReviewBrief{
		What: "Adds subtraction.\n\nThe tests‮ cover it.", Why: "Callers asked", How: "cargo test", RiskNote: "",
		WhereToLook: []*ascv1.WhereToLook{
			{Path: []byte("src/lib.rs"), Line: 3, Note: "the new\nfunction"},
			{Path: []byte{0xff}, Note: "bad path"},
			{Path: []byte("a b.rs"), Note: "a space"},
			{Path: []byte("tests/sub.rs"), Line: 1 << 30},
		},
	}}
	s := ascSummaryOf(resp, summaryWords)
	assert.False(t, s.Nothing)
	assert.Equal(t, "Adds subtraction.\n\nThe tests cover it.", s.What)
	assert.Equal(t, "Callers asked", s.Why)
	assert.Empty(t, s.Risk)
	require.Len(t, s.Pointers, 2)
	assert.Equal(t, AscSummaryPointer{Path: "src/lib.rs", Line: 3, Note: "the new function"}, s.Pointers[0])
	assert.Equal(t, AscSummaryPointer{Path: "tests/sub.rs"}, s.Pointers[1])
	assert.Equal(t, "src/lib.rs:3 the new function\ntests/sub.rs\n", s.Where)
	assert.Equal(t, "### What\n\nAdds subtraction.\n\nThe tests cover it.\n\n### Why\n\nCallers asked\n\n### How to check\n\ncargo test\n\n"+
		"### Where to look first\n\n- `src/lib.rs:3` the new function\n- `tests/sub.rs`\n", s.Body)
	// the brief form reads the pointers back as they were written
	where := ascBriefWhereOf(s.Where)
	require.Len(t, where, 2)
	assert.Equal(t, uint32(3), where[0].GetLine())
	assert.Equal(t, "the new function", where[0].GetNote())
}

func TestAscSummaryOfNothingAndCut(t *testing.T) {
	assert.Equal(t, AscSummary{Nothing: true}, ascSummaryOf(&ascv1.SummarizeChangeResponse{NothingToCompare: true}, summaryWords))

	many := make([]*ascv1.WhereToLook, 9)
	for i := range many {
		many[i] = &ascv1.WhereToLook{Path: []byte("f.rs"), Line: uint32(i + 1)}
	}
	s := ascSummaryOf(&ascv1.SummarizeChangeResponse{Truncated: true, Brief: &ascv1.ReviewBrief{What: strings.Repeat("x", 5000), WhereToLook: many}}, summaryWords)
	assert.Len(t, s.Pointers, ascSummaryMaxWhere)
	assert.LessOrEqual(t, len(s.What), ascSummaryMaxText)
	assert.True(t, strings.HasSuffix(s.Body, "Cut short.\n"))
}

func TestAscSummaryRefusals(t *testing.T) {
	for _, c := range []struct {
		err    error
		code   int
		reason string
	}{
		{status.Error(codes.FailedPrecondition, "no AI key is available for this repository: add one"), http.StatusConflict, "no_key"},
		{status.Error(codes.Unimplemented, "this server has no AI features"), http.StatusNotImplemented, "off"},
		{status.Error(codes.ResourceExhausted, "cap"), http.StatusTooManyRequests, "paused"},
		{status.Error(codes.PermissionDenied, "no"), http.StatusForbidden, "denied"},
		{asctrust.ErrForbidden, http.StatusForbidden, "denied"},
		{status.Error(codes.NotFound, "unknown revision"), http.StatusNotFound, "unknown"},
		{status.Error(codes.Unavailable, "the model's answer could not be read as a brief"), http.StatusServiceUnavailable, "unavailable"},
		{errors.New("no connection"), http.StatusServiceUnavailable, "unavailable"},
	} {
		ctx, resp := contexttest.MockContext(t, "/user2/repo1/asc/summarize")
		ascSummaryRefuse(ctx, c.err)
		assert.Equal(t, c.code, resp.Code, c.reason)
		var got ascSummaryRefusal
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &got))
		assert.Equal(t, c.reason, got.Error)
		assert.NotEmpty(t, got.Message)
		assert.NotContains(t, got.Message, "model's answer", "asc's own words never reach the page")
		assert.Len(t, got.Next, 36)
	}
}
