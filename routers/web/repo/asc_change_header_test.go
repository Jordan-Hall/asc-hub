// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"strings"
	"testing"

	"forgejo.org/models/unittest"
	"forgejo.org/services/asctrust"
	pull_service "forgejo.org/services/pull"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscChangeState(t *testing.T) {
	// merged, closed, draft, changesRequested, approved, askedViewer
	cases := []struct {
		name string
		args [6]bool
		want string
	}{
		{"merged beats everything", [6]bool{true, true, true, true, true, true}, ascChangeMerged},
		{"closed", [6]bool{false, true, false, false, false, false}, ascChangeClosed},
		{"draft", [6]bool{false, false, true, false, false, true}, ascChangeDraft},
		{"changes requested", [6]bool{false, false, false, true, true, true}, ascChangeChangesRequested},
		{"asked of the viewer", [6]bool{false, false, false, false, true, true}, ascChangeWaitingOnYou},
		{"approved", [6]bool{false, false, false, false, true, false}, ascChangeApproved},
		{"nothing yet", [6]bool{}, ascChangeWaitingForReview},
	}
	for _, c := range cases {
		a := c.args
		assert.Equal(t, c.want, ascChangeState(a[0], a[1], a[2], a[3], a[4], a[5]), c.name)
	}
}

func render(t *testing.T, h *AscChangeHeader) string {
	t.Helper()
	return renderPart(t, "repo/pulls/asc_change_header", func(d map[string]any) {
		if h != nil {
			d["AscChangeHeader"] = h
		}
	})
}

func TestAscChangeHeaderTemplate(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	run := &AscRunPull{RunID: 7, Agent: "coder-2", URL: "/o/r/asc/agents/runs/7"}
	h := &AscChangeHeader{
		ChangeID: "zyxwvutsrqpo", Commit: "0123abcd", Version: 4,
		Signature: asctrust.Badge{Label: "Verified", Tone: "ok", Title: "signed by a key of the author"},
		BasedOn:   &pull_service.AscStackEntry{Index: 2, Link: "/o/r/pulls/2"},
		Run:       run, State: ascChangeWaitingOnYou, ChecksPass: 42, ChecksTotal: 42,
	}
	out := render(t, h)
	for _, want := range []string{
		`data-testid="asc-change-header"`, `data-state="waiting_on_you"`,
		`zyxwvutsrqpo`, `0123abcd`, `data-testid="asc-signature"`, `data-state="Verified"`,
		`href="/o/r/pulls/2"`, `href="/o/r/asc/agents/runs/7"`, `data-testid="asc-change-agent"`,
		`asc_review.change_state_waiting_on_you`, `asc_review.change_header_checks`,
	} {
		assert.Contains(t, out, want)
	}

	// A change a person made has no agent banner; the viewer without access to runs gets text, not a link.
	h.Run, h.BasedOn = nil, nil
	out = render(t, h)
	assert.NotContains(t, out, "asc-change-agent")
	assert.NotContains(t, out, "asc-change-based-on")
	assert.Contains(t, out, `data-testid="asc-change-state"`)
	h.Run = &AscRunPull{RunID: 7, Agent: "coder-2"}
	out = render(t, h)
	assert.NotContains(t, out, "<a href=\"/o/r/asc/agents")
	assert.True(t, strings.Contains(out, "change_header_opened_for"))

	// Nothing in the data: nothing drawn.
	assert.Empty(t, strings.TrimSpace(render(t, nil)))
}
