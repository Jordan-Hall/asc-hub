// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"errors"
	"strings"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func TestCheckPathRules(t *testing.T) {
	ok := []AccessRule{{Dir: "", Subject: "*", Level: "read"}, {Dir: "services/pay", Subject: "team:payments", Level: "write"}, {Dir: "services/pay", Subject: "ci:build", Level: "build"}}
	require.NoError(t, CheckPathRules(ok))

	for name, tc := range map[string]struct {
		rules []AccessRule
		want  string
	}{
		"a leading slash":     {[]AccessRule{{Dir: "/a", Subject: "*", Level: "read"}}, "row 1"},
		"a dot-dot segment":   {[]AccessRule{ok[0], {Dir: "a/../b", Subject: "*", Level: "read"}}, "row 2"},
		"a glob":              {[]AccessRule{{Dir: "a/*", Subject: "*", Level: "read"}}, "row 1"},
		"a control character": {[]AccessRule{{Dir: "a\nb", Subject: "*", Level: "read"}}, "row 1"},
		"an unknown subject":  {[]AccessRule{{Dir: "a", Subject: "group:x", Level: "read"}}, "subject"},
		"an unknown level":    {[]AccessRule{{Dir: "a", Subject: "*", Level: "owner"}}, "level"},
		"a duplicate":         {[]AccessRule{ok[1], ok[0], ok[1]}, "row 3 names the same directory and subject as row 1"},
		"a long directory":    {[]AccessRule{{Dir: strings.Repeat("a", MaxPathRuleDir+1), Subject: "*", Level: "read"}}, "row 1"},
	} {
		err := CheckPathRules(tc.rules)
		require.Error(t, err, name)
		assert.ErrorIs(t, err, ErrBadInput, name)
		assert.Contains(t, err.Error(), tc.want, name)
	}
	assert.Error(t, CheckPathRules(make([]AccessRule, MaxPathRules+1)))
	assert.Equal(t, "a/b", CleanRuleDir("  /a/b/ "))
	assert.Empty(t, CleanRuleDir("/"))
}

// answerConn answers every call with one fixed response.
type answerConn struct {
	answer any
	calls  int
}

func (c *answerConn) Invoke(_ context.Context, _ string, _, reply any, _ ...grpc.CallOption) error {
	c.calls++
	switch r := reply.(type) {
	case *ascv1.ListPathRulesResponse:
		a := c.answer.(*ascv1.ListPathRulesResponse)
		r.Rules, r.Revision, r.Updated, r.UpdatedBy = a.GetRules(), a.GetRevision(), a.GetUpdated(), a.GetUpdatedBy()
	case *ascv1.SetPathRulesResponse:
		r.Revision = c.answer.(*ascv1.SetPathRulesResponse).GetRevision()
	case *ascv1.CheckPathAccessResponse:
		a := c.answer.(*ascv1.CheckPathAccessResponse)
		r.Access, r.Teams = a.GetAccess(), a.GetTeams()
	default:
		return errors.New("unexpected call")
	}
	return nil
}

func (c *answerConn) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, errors.New("no streams")
}

func TestPathRulesAnswersAreChecked(t *testing.T) {
	ctx := context.Background()

	_, err := ListPathRules(ctx, &answerConn{answer: &ascv1.ListPathRulesResponse{Rules: []*ascv1.PathRule{{Dir: "../x", Subject: "*", Level: "read"}}}}, "o/r")
	assert.ErrorIs(t, err, ErrBadAnswer, "a rule asc would refuse")
	set, err := ListPathRules(ctx, &answerConn{answer: &ascv1.ListPathRulesResponse{Rules: []*ascv1.PathRule{{Dir: "x", Subject: "*", Level: "none"}}, Revision: 4, Updated: 1_700_000_000, UpdatedBy: "user2"}}, "o/r")
	require.NoError(t, err)
	assert.Equal(t, []AccessRule{{Dir: "x", Subject: "*", Level: "none"}}, set.Rules)
	assert.Equal(t, uint64(4), set.Revision)

	_, err = SetPathRules(ctx, &answerConn{answer: &ascv1.SetPathRulesResponse{Revision: 4}}, "o/r", nil, 4, uuid.NewString())
	assert.ErrorIs(t, err, ErrBadAnswer, "the revision did not move")
	c := &answerConn{answer: &ascv1.SetPathRulesResponse{Revision: 5}}
	_, err = SetPathRules(ctx, c, "o/r", nil, 4, "not-an-op")
	assert.ErrorIs(t, err, ErrBadInput)
	assert.Zero(t, c.calls)
	rev, err := SetPathRules(ctx, c, "o/r", nil, 4, uuid.NewString())
	require.NoError(t, err)
	assert.Equal(t, uint64(5), rev)

	_, err = PreviewPathAccess(ctx, &answerConn{answer: &ascv1.CheckPathAccessResponse{Access: []*ascv1.PathAccess{{Path: "b", Level: "read"}}}}, "o/r", "user:a", "read", []string{"a"})
	assert.ErrorIs(t, err, ErrBadAnswer, "an answer for another path")
	for _, bad := range [][3]any{{"team:a", "read", []string{"a"}}, {"user:a", "build", []string{"a"}}, {"user:a", "read", []string{}}, {"user:a", "read", []string{"../a"}}} {
		_, err = PreviewPathAccess(ctx, &answerConn{}, "o/r", bad[0].(string), bad[1].(string), bad[2].([]string))
		assert.ErrorIs(t, err, ErrBadInput)
	}
	p, err := PreviewPathAccess(ctx, &answerConn{answer: &ascv1.CheckPathAccessResponse{Access: []*ascv1.PathAccess{{Path: "a", Level: "none", DecidedBy: "a"}}, Teams: []string{"backend"}}}, "o/r", "user:a", "read", []string{"a"})
	require.NoError(t, err)
	assert.Equal(t, []PathAccess{{Path: "a", Level: "none", DecidedBy: "a"}}, p.Access)
	assert.Equal(t, []string{"backend"}, p.Teams)
}
