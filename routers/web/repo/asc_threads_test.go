// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	pull_service "forgejo.org/services/pull"

	"github.com/stretchr/testify/assert"
)

// Only the threads that started in asc are listed; what the hub wrote is already in the conversation.
func TestAscThreadsKeepOnlyWhatStartedInAsc(t *testing.T) {
	threads := []*ascv1.ReviewThread{
		{Id: 1, Comments: []*ascv1.ReviewComment{{Author: "maya", Body: "from the hub", ExternalId: pull_service.AscExternalCommentPrefix + "7"}}},
		{
			Id: 2, Status: ascv1.ThreadStatus_THREAD_STATUS_RESOLVED, OpenedBy: "coder-2",
			Anchor: &ascv1.ThreadAnchor{Path: []byte("src/client.rs"), Version: 3, StartLine: 14, EndLine: 16},
			Comments: []*ascv1.ReviewComment{
				{Author: "coder-2", Body: "<script>alert(1)</script> use retry", Suggestion: "retry(policy, send)", AppliedVersion: 4},
				{Author: "maya", Body: "ok"},
			},
		},
		{Id: 3, Comments: []*ascv1.ReviewComment{{Author: "a", Body: "whole change"}}},
		{Id: 4}, // no comment: nothing to show
	}
	got := ascThreadsOf(threads)
	assert.Len(t, got, 2)
	assert.EqualValues(t, 2, got[0].ID)
	assert.True(t, got[0].Resolved)
	assert.Equal(t, "src/client.rs", got[0].Path)
	assert.Equal(t, "14-16", got[0].Lines)
	assert.EqualValues(t, 3, got[0].Version)
	assert.Len(t, got[0].Comments, 2)
	assert.Equal(t, "retry(policy, send)", got[0].Comments[0].Suggestion)
	assert.EqualValues(t, 4, got[0].Comments[0].Applied)
	assert.Empty(t, got[1].Path, "a thread on the change as a whole")
	assert.Empty(t, got[1].Lines)
}

// The where-to-look lines of the brief form.
func TestAscBriefWhereOf(t *testing.T) {
	w := ascBriefWhereOf("src/client.rs:42 the retry loop\n\nREADME.md\nsrc/a.rs:notanumber some note\n  docs/x.md:7  \n:3 no path")
	if assert.Len(t, w, 4) {
		assert.Equal(t, "src/client.rs", string(w[0].GetPath()))
		assert.EqualValues(t, 42, w[0].GetLine())
		assert.Equal(t, "the retry loop", w[0].GetNote())
		assert.Equal(t, "README.md", string(w[1].GetPath()))
		assert.Zero(t, w[1].GetLine())
		assert.Equal(t, "src/a.rs", string(w[2].GetPath()), "a line that is not a number is part of the note")
		assert.Contains(t, w[2].GetNote(), "notanumber")
		assert.EqualValues(t, 7, w[3].GetLine())
	}
	many := ""
	for range 30 {
		many += "a.go:1\n"
	}
	assert.Len(t, ascBriefWhereOf(many), ascBriefFormMaxWhere)
}
