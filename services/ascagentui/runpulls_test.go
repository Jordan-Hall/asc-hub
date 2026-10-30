// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascagentui

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func changeID(b byte) []byte { return bytes.Repeat([]byte{b}, 16) }

func gitIDOf(b byte) []byte { return bytes.Repeat([]byte{b}, 20) }

type runPullLog struct {
	staged, unstaged []string
	created          []string
	parents          []int64
	posters          []string
	infos            map[string]*ascv1.ChangeInfo
	message          string
	next             []int64 // the pull requests (fixture ids) Create hands out, in turn
}

func (l *runPullLog) seams() RunPullSeams {
	return RunPullSeams{
		Resolve: func(_ context.Context, _ grpc.ClientConnInterface, _ string, changes [][]byte) ([]*ascv1.ChangeInfo, error) {
			out := make([]*ascv1.ChangeInfo, len(changes))
			for i, c := range changes {
				out[i] = l.infos[string(c)]
			}
			return out, nil
		},
		Stage: func(_ context.Context, _ *repo_model.Repository, ref, commit string) error {
			l.staged = append(l.staged, ref+"|"+commit)
			return nil
		},
		Unstage: func(_ context.Context, _ *repo_model.Repository, ref string) error {
			l.unstaged = append(l.unstaged, ref)
			return nil
		},
		Commit: func(context.Context, *repo_model.Repository, string) (string, error) { return l.message, nil },
		Create: func(ctx context.Context, _ *repo_model.Repository, bot *user_model.User, change, commit, title, body string, parent int64) (*issues_model.PullRequest, error) {
			pr, err := issues_model.GetPullRequestByID(ctx, l.next[len(l.created)])
			if err != nil {
				return nil, err
			}
			l.created = append(l.created, change+"|"+commit+"|"+title+"|"+body)
			l.parents = append(l.parents, parent)
			l.posters = append(l.posters, bot.Name)
			return pr, issues_model.SetPullRequestAscStack(ctx, pr, change, parent)
		},
	}
}

func ascRepoForRuns(t *testing.T) *repo_model.Repository {
	t.Helper()
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	_, err := db.GetEngine(t.Context()).Exec("UPDATE repository SET storage_backend = ? WHERE id = 1", string(gitrepo.StorageBackendAsc))
	require.NoError(t, err)
	return unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
}

func TestOpenRunPullsOpensTheStackAsTheAgent(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	repo := ascRepoForRuns(t)
	a, b := changeID(0x11), changeID(0x22)
	l := &runPullLog{
		message: "Add the user page\n\nThe route and its test.",
		next:    []int64{2, 5}, // open pull requests of the fixtures
		infos: map[string]*ascv1.ChangeInfo{
			string(a): {Change: a, Git: gitIDOf(0xa1)},
			string(b): {Change: b, Git: gitIDOf(0xb2), ParentChange: a},
		},
	}
	run := RunPullRun{ID: 7, Agent: "coder-2", Title: "users page", CreatedBy: "user2", Changes: [][]byte{a, b}, RunURL: "/user2/repo1/asc/agents/runs/7"}

	got, err := OpenRunPulls(t.Context(), nil, repo, run, l.seams())
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, RunPullOpened, got[0].State)
	assert.Equal(t, RunPullOpened, got[1].State)

	// The commits were published under branches of the run, and the branches removed again.
	require.Len(t, l.staged, 2)
	first := "refs/heads/asc/agent/coder-2/run-7-" + ShortChange(ChangeText(a))
	assert.Equal(t, first+"|"+strings.Repeat("a1", 20), l.staged[0])
	assert.Len(t, l.unstaged, 2)
	assert.Equal(t, first, l.unstaged[0])

	// The poster is the agent's own bot account, the second change sits on the first, the description says where it
	// came from, and the run is remembered.
	assert.Equal(t, []string{"agent-coder-2", "agent-coder-2"}, l.posters)
	assert.Equal(t, []int64{0, 2}, l.parents)
	assert.Contains(t, l.created[0], "|Add the user page|")
	assert.Contains(t, l.created[0], "[agent run #7](/user2/repo1/asc/agents/runs/7)")
	assert.Contains(t, l.created[0], "started by @user2")
	assert.Contains(t, l.created[0], "The route and its test.")
	bot, err := user_model.GetUserByName(t.Context(), "agent-coder-2")
	require.NoError(t, err)
	assert.True(t, bot.IsBot())
	assert.True(t, bot.ProhibitLogin, "a bot cannot sign in")
	for _, id := range []int64{2, 5} {
		link, err := issues_model.GetPullRequestAscRun(t.Context(), id)
		require.NoError(t, err)
		require.NotNil(t, link, "pull request %d", id)
		assert.Equal(t, uint64(7), link.RunID)
		assert.Equal(t, "coder-2", link.Agent)
		assert.Equal(t, "user2", link.StartedBy)
	}
}

func TestOpenRunPullsIsIdempotentAndReportsWhatIsNotReady(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	repo := ascRepoForRuns(t)
	a, b, c, d := changeID(0x11), changeID(0x22), changeID(0x33), changeID(0x44)
	l := &runPullLog{
		message: "x",
		next:    []int64{2},
		infos: map[string]*ascv1.ChangeInfo{
			string(a): {Change: a, Git: gitIDOf(0xa1)},
			string(b): {Change: b},                                           // asc has no git id yet
			string(c): {Change: c, Git: gitIDOf(0xc3), LandedAs: gitIDOf(1)}, // landed
			// d is unknown to asc
		},
	}
	got, err := OpenRunPulls(t.Context(), nil, repo, RunPullRun{ID: 9, Agent: "coder-2", Changes: [][]byte{a, b, c, d}}, l.seams())
	require.NoError(t, err)
	states := make([]string, len(got))
	for i, r := range got {
		states[i] = r.State
	}
	assert.Equal(t, []string{RunPullOpened, RunPullWaiting, RunPullLanded, RunPullWaiting}, states)

	// The second time the first change has its pull request: it is linked, nothing is published or opened again.
	staged := len(l.staged)
	got, err = OpenRunPulls(t.Context(), nil, repo, RunPullRun{ID: 9, Agent: "coder-2", Changes: [][]byte{a}}, l.seams())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, RunPullLinked, got[0].State)
	assert.Equal(t, staged, len(l.staged))
	assert.Len(t, l.created, 1)
}

func TestOpenRunPullsFailsOneChangeNotTheRest(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	repo := ascRepoForRuns(t)
	a, b := changeID(0x11), changeID(0x22)
	l := &runPullLog{
		message: "x", next: []int64{5},
		infos: map[string]*ascv1.ChangeInfo{string(a): {Change: a, Git: gitIDOf(0x01)}, string(b): {Change: b, Git: gitIDOf(0x02), ParentChange: a}},
	}
	seams := l.seams()
	stage := seams.Stage
	seams.Stage = func(ctx context.Context, r *repo_model.Repository, ref, commit string) error {
		if strings.HasPrefix(commit, "0101") {
			return errors.New("asc said no")
		}
		return stage(ctx, r, ref, commit)
	}
	got, err := OpenRunPulls(t.Context(), nil, repo, RunPullRun{ID: 3, Agent: "coder-2", Changes: [][]byte{a, b}}, seams)
	require.NoError(t, err)
	assert.Equal(t, RunPullFailed, got[0].State)
	assert.Equal(t, RunPullOpened, got[1].State)
	assert.Equal(t, []int64{0}, l.parents, "a change whose parent has no pull request is a bottom")
	assert.Len(t, l.unstaged, 2, "the branch is removed whether the pull request was opened or not")
}

func TestOpenRunPullsRefusesWhatItCannotTrust(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	repo := ascRepoForRuns(t)
	l := &runPullLog{}
	for name, run := range map[string]RunPullRun{
		"no run":         {Agent: "coder-2", Changes: [][]byte{changeID(1)}},
		"a bad agent":    {ID: 1, Agent: "Coder 2", Changes: [][]byte{changeID(1)}},
		"a bad change":   {ID: 1, Agent: "coder-2", Changes: [][]byte{{1, 2, 3}}},
		"a path as name": {ID: 1, Agent: "../x", Changes: [][]byte{changeID(1)}},
	} {
		_, err := OpenRunPulls(t.Context(), nil, repo, run, l.seams())
		assert.Error(t, err, name)
	}
	got, err := OpenRunPulls(t.Context(), nil, repo, RunPullRun{ID: 1, Agent: "coder-2"}, l.seams())
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.Empty(t, l.staged)

	// A repository that is not in asc is none of this.
	git := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 2})
	_, err = OpenRunPulls(t.Context(), nil, git, RunPullRun{ID: 1, Agent: "coder-2", Changes: [][]byte{changeID(1)}}, l.seams())
	assert.Error(t, err)
}

func TestEnsureAgentUser(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	u, err := EnsureAgentUser(t.Context(), "coder-2")
	require.NoError(t, err)
	again, err := EnsureAgentUser(t.Context(), "coder-2")
	require.NoError(t, err)
	assert.Equal(t, u.ID, again.ID, "one account per agent")

	// A person who already has the name keeps it.
	person := &user_model.User{Name: "agent-taken", LowerName: "agent-taken", Email: "taken@example.com", Passwd: "xxxxxxxxxxxxxxxx", IsActive: true}
	require.NoError(t, user_model.AdminCreateUser(t.Context(), person))
	_, err = EnsureAgentUser(t.Context(), "taken")
	assert.ErrorIs(t, err, ErrAgentNameTaken)

	_, err = EnsureAgentUser(t.Context(), "Not Valid")
	assert.Error(t, err)

	name := AgentUserName(strings.Repeat("a", 40))
	assert.LessOrEqual(t, len(name), 40)
	assert.NotEqual(t, name, AgentUserName(strings.Repeat("a", 39)+"b"), "two long names stay two accounts")
	assert.True(t, strings.HasPrefix(name, "agent-"))
}
