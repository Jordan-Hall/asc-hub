// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTeamAnswersAndRequestsAreChecked(t *testing.T) {
	ctx := context.Background()

	teams, err := ListTeams(ctx, &answerConn{answer: &ascv1.ListTeamsResponse{Teams: []*ascv1.Team{
		{Name: "team1", Members: []string{"user4", "agent:bot", "user2"}},
	}}}, "org3")
	require.NoError(t, err)
	assert.Equal(t, []AscTeam{{Name: "team1", Members: []string{"agent:bot", "user2", "user4"}}}, teams)

	for _, bad := range []*ascv1.Team{
		{Name: "a b"},
		{Name: "team1", Members: []string{"bad member"}},
		{Name: "team1", Members: make([]string, MaxTeamMembers+1)},
	} {
		_, err := ListTeams(ctx, &answerConn{answer: &ascv1.ListTeamsResponse{Teams: []*ascv1.Team{bad}}}, "org3")
		require.ErrorIs(t, err, ErrBadAnswer, "%v", bad)
	}

	c := &answerConn{answer: &ascv1.SetTeamMembersResponse{}}
	require.NoError(t, SetTeamMembers(ctx, c, "org3", "team1", []string{"user2"}, uuid.NewString()))
	require.NoError(t, SetTeamMembers(ctx, c, "org3", "team1", nil, uuid.NewString()), "no members clears the team")
	assert.Equal(t, 2, c.calls)
	for _, bad := range [][]any{
		{"a b", []string{"user2"}, uuid.NewString()},
		{"team1", []string{"x y"}, uuid.NewString()},
		{"team1", make([]string, MaxTeamMembers+1), uuid.NewString()},
		{"team1", []string{"user2"}, "not-an-op"},
	} {
		err := SetTeamMembers(ctx, c, "org3", bad[0].(string), bad[1].([]string), bad[2].(string))
		require.ErrorIs(t, err, ErrBadInput)
	}
	assert.Equal(t, 2, c.calls, "a refused request is not sent")
}
