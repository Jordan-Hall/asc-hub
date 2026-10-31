// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	gocontext "context"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"

	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"
	"forgejo.org/services/gitdiff"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func railChange(c byte, git string) *ascv1.ChangeInfo {
	raw, _ := hex.DecodeString(git)
	return &ascv1.ChangeInfo{Change: ascChangeBytes(ascTestChange(c)), Git: raw}
}

const (
	railGitA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	railGitB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	railGitC = "cccccccccccccccccccccccccccccccccccccccc"
)

func TestAscRailStackListsTopFirstAndMarksTheCurrentChange(t *testing.T) {
	resp := &ascv1.StackResponse{Trunk: "main", Changes: []*ascv1.ChangeInfo{railChange('k', railGitA), railChange('l', railGitB), railChange('m', railGitC)}}
	list, more := ascRailStackOf(resp, map[string]string{railGitA: "Add the client", railGitC: "Retry\x00 on 503"}, railGitC)
	require.Len(t, list, 3)
	assert.Zero(t, more)
	assert.Equal(t, []string{"mmmmmmmmmmmm", "llllllllllll", "kkkkkkkkkkkk"}, []string{list[0].Change, list[1].Change, list[2].Change})
	assert.Equal(t, "Retry on 503", list[0].Title, "control characters are cleaned")
	assert.True(t, list[0].Current)
	assert.False(t, list[1].Current)
	assert.Equal(t, "", list[1].Title, "a commit the comparison does not hold has no title")
	assert.Equal(t, "bbbbbbbb", list[1].Commit)
}

func TestAscRailStackRefusesBadAnswersAndCapsTheList(t *testing.T) {
	list, _ := ascRailStackOf(&ascv1.StackResponse{}, nil, "")
	assert.Empty(t, list)
	list, _ = ascRailStackOf(&ascv1.StackResponse{Changes: []*ascv1.ChangeInfo{{Change: []byte{1}, Git: []byte{1}}}}, nil, "")
	assert.Empty(t, list, "a change id of the wrong size")
	list, _ = ascRailStackOf(&ascv1.StackResponse{Changes: []*ascv1.ChangeInfo{{Change: ascChangeBytes(ascTestChange('k'))}}}, nil, "")
	assert.Empty(t, list, "a change without a commit")

	var many []*ascv1.ChangeInfo
	for i := 0; i < ascRailStackMax+1; i++ {
		many = append(many, railChange('k', railGitA))
	}
	list, _ = ascRailStackOf(&ascv1.StackResponse{Changes: many}, nil, "")
	assert.Empty(t, list, "more changes than a stack may have")

	list, more := ascRailStackOf(&ascv1.StackResponse{Changes: many[:20]}, nil, "")
	assert.Len(t, list, ascRailStackShown)
	assert.Equal(t, 20-ascRailStackShown, more)
}

func TestAscRailPolicyIsCleanedAndCapped(t *testing.T) {
	assert.Nil(t, ascRailPolicyOf(nil))
	checks := []string{"ci/build", "", "ci/test\x00"}
	for i := 0; i < 30; i++ {
		checks = append(checks, fmt.Sprintf("c%d", i))
	}
	p := ascRailPolicyOf(&ascv1.PolicyLandRequirements{Approvals: 1 << 30, Checks: checks, Signed: true, MergeStrategy: strings.Repeat("s", 100)})
	assert.Equal(t, uint32(ascRailApprovalsMax), p.Approvals)
	assert.Len(t, p.Checks, ascRailChecksMax)
	assert.Equal(t, []string{"ci/build", "ci/test"}, p.Checks[:2])
	assert.True(t, p.Signed)
	assert.LessOrEqual(t, len(p.Strategy), ascRailStrategyBytes)
	assert.True(t, p.Any())
	assert.False(t, (&AscRailPolicy{}).Any())
	assert.False(t, (*AscRailPolicy)(nil).Any())
}

func TestAscRailFilesOpenTheFirstAndFoldTheRest(t *testing.T) {
	d := &gitdiff.Diff{TotalAddition: 30, TotalDeletion: 9}
	for i := 0; i < 11; i++ {
		d.Files = append(d.Files, &gitdiff.DiffFile{Name: fmt.Sprintf("src/f%02d.go", i), Addition: 3, Deletion: 1})
	}
	files, shown, total := ascRailFilesOf(d)
	assert.Len(t, files, 11)
	assert.Equal(t, ascRailFilesShown, shown)
	assert.Equal(t, 11, total)
	assert.Equal(t, AscRailFile{Name: "src/f00.go", Add: 3, Del: 1}, files[0])
	files, shown, total = ascRailFilesOf(nil)
	assert.Empty(t, files)
	assert.Zero(t, shown+total)
}

type fakeRail struct {
	ascv1.UnimplementedChangesServer
	ascv1.UnimplementedPolicyServer
	mu     sync.Mutex
	stack  *ascv1.StackRequest
	eval   *ascv1.EvaluatePolicyRequest
	stackF func() (*ascv1.StackResponse, error)
	evalF  func() (*ascv1.EvaluatePolicyResponse, error)
}

func (f *fakeRail) Stack(_ gocontext.Context, r *ascv1.StackRequest) (*ascv1.StackResponse, error) {
	f.mu.Lock()
	f.stack = r
	f.mu.Unlock()
	return f.stackF()
}

func (f *fakeRail) Evaluate(_ gocontext.Context, r *ascv1.EvaluatePolicyRequest) (*ascv1.EvaluatePolicyResponse, error) {
	f.mu.Lock()
	f.eval = r
	f.mu.Unlock()
	return f.evalF()
}

func startFakeRail(t *testing.T) *fakeRail {
	t.Helper()
	f := &fakeRail{}
	srv := grpc.NewServer()
	ascv1.RegisterChangesServer(srv, f)
	ascv1.RegisterPolicyServer(srv, f)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	useAsc(t, l.Addr().String())
	return f
}

type ascRailEnv struct{ ctx *context.Context }

func railCtx(t *testing.T, signed bool) *ascRailEnv {
	t.Helper()
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	ctx, _ := contexttest.MockContext(t, "user2/repo1/compare/master...branch2")
	if signed {
		contexttest.LoadUser(t, ctx, 2)
		ctx.IsSigned = true
	}
	contexttest.LoadRepo(t, ctx, 1)
	ctx.Data["BaseBranch"] = "master"
	ctx.Data["AfterCommitID"] = railGitC
	d := &gitdiff.Diff{TotalAddition: 5, TotalDeletion: 2, Files: []*gitdiff.DiffFile{{Name: "a.go", Addition: 5, Deletion: 2}}}
	ctx.Data["Diff"] = d
	return &ascRailEnv{ctx: ctx}
}

func TestPrepareAscNewChangeRailReadsTheStackAndThePolicy(t *testing.T) {
	f := startFakeRail(t)
	f.stackF = func() (*ascv1.StackResponse, error) {
		return &ascv1.StackResponse{Trunk: "master", Changes: []*ascv1.ChangeInfo{railChange('k', railGitA), railChange('l', railGitC)}}, nil
	}
	f.evalF = func() (*ascv1.EvaluatePolicyResponse, error) {
		return &ascv1.EvaluatePolicyResponse{Land: &ascv1.PolicyLandRequirements{Approvals: 2, Checks: []string{"ci/build"}, Signed: true, MergeStrategy: "squash"}}, nil
	}
	env := railCtx(t, true)
	prepareAscNewChangeRail(env.ctx)
	rail, ok := env.ctx.Data["AscNewChangeRail"].(*AscNewChangeRail)
	require.True(t, ok)
	assert.Equal(t, "master", rail.Trunk)
	require.Len(t, rail.Stack, 2)
	assert.True(t, rail.Stack[0].Current, "the head of the comparison is the top of the stack")
	assert.Equal(t, uint32(2), rail.Policy.Approvals)
	require.Len(t, rail.Files, 1)
	assert.Equal(t, 5, rail.Add)

	assert.Equal(t, "master", f.stack.GetTarget())
	assert.Equal(t, railGitC, f.stack.GetRev())
	assert.Equal(t, ascv1.PolicyAction_POLICY_ACTION_LAND, f.eval.GetAction())
}

func TestPrepareAscNewChangeRailKeepsTheFilesWhenAscDoesNotAnswer(t *testing.T) {
	f := startFakeRail(t)
	f.stackF = func() (*ascv1.StackResponse, error) { return nil, fmt.Errorf("down") }
	f.evalF = func() (*ascv1.EvaluatePolicyResponse, error) { return nil, fmt.Errorf("down") }
	env := railCtx(t, true)
	prepareAscNewChangeRail(env.ctx)
	rail, ok := env.ctx.Data["AscNewChangeRail"].(*AscNewChangeRail)
	require.True(t, ok)
	assert.Empty(t, rail.Stack)
	assert.Nil(t, rail.Policy)
	assert.Len(t, rail.Files, 1)
}

func TestPrepareAscNewChangeRailOnlyForSignedPeopleInAscRepositories(t *testing.T) {
	f := startFakeRail(t)
	f.stackF = func() (*ascv1.StackResponse, error) { return &ascv1.StackResponse{}, nil }
	f.evalF = func() (*ascv1.EvaluatePolicyResponse, error) { return &ascv1.EvaluatePolicyResponse{}, nil }

	env := railCtx(t, false)
	prepareAscNewChangeRail(env.ctx)
	assert.Nil(t, env.ctx.Data["AscNewChangeRail"], "nobody is signed in")
	assert.Nil(t, f.stack)

	env = railCtx(t, true)
	setStorageBackend(t, 1, "")
	env.ctx.Repo.Repository.StorageBackend = ""
	prepareAscNewChangeRail(env.ctx)
	assert.Nil(t, env.ctx.Data["AscNewChangeRail"], "the repository is not kept in asc")
	assert.Nil(t, f.stack)
}

func TestAscNewChangeRailTemplate(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	var files []AscRailFile
	for i := 0; i < 11; i++ {
		files = append(files, AscRailFile{Name: fmt.Sprintf("f%02d.go", i), Add: 1, Del: 1})
	}
	rail := &AscNewChangeRail{
		Trunk: "master", Stack: []AscRailChange{{Change: "mmmmmmmmmmmm", Title: "Retry", Current: true}, {Change: "kkkkkkkkkkkk", Commit: "aaaaaaaa"}},
		Files: files, FilesShown: ascRailFilesShown, FilesTotal: 11, Add: 11, Del: 11,
		Policy: &AscRailPolicy{Approvals: 2, Checks: []string{"ci/build"}, Signed: true, Strategy: "squash"},
	}
	out := renderPart(t, "repo/issue/asc_newchange_rail", func(d map[string]any) { d["AscNewChangeRail"] = rail })
	for _, want := range []string{
		`data-testid="asc-newchange-rail"`, `data-testid="asc-rail-stack"`, `aria-current="true"`, "mmmmmmmmmmmm", "Retry", "aaaaaaaa",
		`data-testid="asc-rail-files"`, `data-testid="asc-rail-files-more"`, "asc_rp.rail_show_all",
		`data-testid="asc-rail-policy"`, `data-testid="asc-rail-approvals"`, `data-testid="asc-rail-check"`, `data-testid="asc-rail-signed"`, `data-testid="asc-rail-strategy"`,
	} {
		assert.Contains(t, out, want)
	}
	assert.Equal(t, 11, strings.Count(out, `class="rp-rail-file"`), "every file is in the page, the rest behind the disclosure")

	// a short list has no disclosure, an empty policy no section
	rail.Files, rail.FilesShown, rail.FilesTotal = files[:3], 3, 3
	rail.Policy = &AscRailPolicy{}
	out = renderPart(t, "repo/issue/asc_newchange_rail", func(d map[string]any) { d["AscNewChangeRail"] = rail })
	assert.NotContains(t, out, "asc-rail-files-more")
	assert.NotContains(t, out, "asc-rail-policy")

	assert.Empty(t, strings.TrimSpace(renderPart(t, "repo/issue/asc_newchange_rail", func(map[string]any) {})))
}
