// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package asc_test

import (
	"context"
	"errors"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git"
	"forgejo.org/modules/repostore"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
)

// mergeFixture is a base at 0x11 on refs/heads/main and a head at 0x22 on refs/heads/feature, both in o/r,
// with 0x10 as their merge base.
func mergeFixture() *fake {
	f := newFake()
	f.refs["o/r|refs/heads/main"] = gid(0x11)
	f.refs["o/r|refs/heads/feature"] = gid(0x22)
	f.mergeBase[hexPair(0x11, 0x22)] = gid(0x10)
	return f
}

// hexPair is the key the fake's MergeBase looks up: the raw bytes of a and b, in hex.
func hexPair(a, b byte) string {
	return gid(a).String() + "|" + gid(b).String()
}

func mergeReq(style repostore.MergeStyle) repostore.MergeRequest {
	r := ref("o", "r")
	return repostore.MergeRequest{
		Base: r, BaseRef: "refs/heads/main", Head: r, HeadRev: "refs/heads/feature", Style: style,
		Meta:    repostore.CommitMeta{Author: sig("Ann"), Committer: sig("Bob"), Message: "Merge PR"},
		Publish: &repostore.RefUpdate{Repo: r, Ref: "refs/heads/main"},
	}
}

func TestMergeStyles(t *testing.T) {
	for style, want := range map[repostore.MergeStyle]ascv1.MergeStyle{
		repostore.MergeStyleMerge:           ascv1.MergeStyle_MERGE_STYLE_MERGE,
		repostore.MergeStyleSquash:          ascv1.MergeStyle_MERGE_STYLE_SQUASH,
		repostore.MergeStyleRebase:          ascv1.MergeStyle_MERGE_STYLE_REBASE,
		repostore.MergeStyleRebaseMerge:     ascv1.MergeStyle_MERGE_STYLE_REBASE_MERGE,
		repostore.MergeStyleFastForwardOnly: ascv1.MergeStyle_MERGE_STYLE_FAST_FORWARD_ONLY,
	} {
		t.Run(string(style), func(t *testing.T) {
			f := mergeFixture()
			res, err := newWriter(t, f).Merge(t.Context(), mergeReq(style))
			require.NoError(t, err)
			assert.Equal(t, gid(0x70).String(), res.Commit.String())
			assert.Equal(t, gid(0x11).String(), res.BaseBefore.String())

			m := f.last("Merge").(*ascv1.MergeRequest)
			assert.Equal(t, want, m.GetStyle())
			assert.Equal(t, hexOf(0x11), m.GetBase())
			assert.Equal(t, hexOf(0x22), m.GetHead())
			assert.Empty(t, m.GetUpdateRef(), "the result is published by UpdateRefs")
			assert.False(t, m.GetDryRun())
			assert.Empty(t, m.GetProtectedGlobs())
			assert.Equal(t, "Merge PR\n", string(m.GetMessage()))
			assert.Equal(t, "Bob", string(m.GetCommitter().GetName()))

			u := f.last("UpdateRefs").(*ascv1.UpdateRefsRequest)
			assert.Equal(t, "refs/heads/main", u.GetUpdates()[0].GetName())
			assert.Equal(t, ascv1.UpdateMode_UPDATE_MODE_FAST_FORWARD, u.GetMode())
			assert.Zero(t, f.called("CopyClosure"))
		})
	}
}

func TestMergeHeadMismatch(t *testing.T) {
	f := mergeFixture()
	req := mergeReq(repostore.MergeStyleMerge)
	req.ExpectHead = gid(0x99)
	_, err := newWriter(t, f).Merge(t.Context(), req)
	var mismatch *repostore.HeadMismatchError
	require.ErrorAs(t, err, &mismatch)
	assert.Equal(t, gid(0x99).String(), mismatch.Given)
	assert.Equal(t, gid(0x22).String(), mismatch.Current)
	assert.Zero(t, f.called("Merge"))

	req.ExpectHead = gid(0x22)
	_, err = newWriter(t, f).Merge(t.Context(), req)
	require.NoError(t, err)
}

func TestMergeMissingRefs(t *testing.T) {
	f := mergeFixture()
	req := mergeReq(repostore.MergeStyleMerge)
	req.HeadRev = "refs/heads/gone"
	_, err := newWriter(t, f).Merge(t.Context(), req)
	var missing *repostore.RefNotFoundError
	require.ErrorAs(t, err, &missing)
	assert.Equal(t, "refs/heads/gone", missing.Ref)

	req = mergeReq(repostore.MergeStyleMerge)
	req.BaseRef = "refs/heads/gone"
	_, err = newWriter(t, f).Merge(t.Context(), req)
	require.ErrorAs(t, err, &missing)
}

func TestMergeFromAnotherRepository(t *testing.T) {
	f := mergeFixture()
	f.refs["u/fork|refs/heads/feature"] = gid(0x22)
	req := mergeReq(repostore.MergeStyleSquash)
	req.Head = ref("u", "fork")
	_, err := newWriter(t, f).Merge(t.Context(), req)
	require.NoError(t, err)
	cc := f.last("CopyClosure").(*ascv1.CopyClosureRequest)
	assert.Equal(t, "u/fork", cc.GetFrom().GetName())
	assert.Equal(t, "o/r", cc.GetTo().GetName())
	assert.Equal(t, [][]byte{ascid(0x22)}, cc.GetChangesets())
	assert.Equal(t, 1, f.called("CopyClosure"))
	assert.Nil(t, f.last("Merge").(*ascv1.MergeRequest).GetHeadRepo(), "the closure is already copied")
	// The closure arrives before the merge reads the head.
	assert.Less(t, indexOf(f.calls, "CopyClosure"), indexOf(f.calls, "Merge"))
}

func indexOf(list []string, name string) int {
	for i, n := range list {
		if n == name {
			return i
		}
	}
	return -1
}

func TestMergeConflictsAndRefusals(t *testing.T) {
	t.Run("conflicts", func(t *testing.T) {
		f := mergeFixture()
		f.mergeResp = &ascv1.MergeResponse{Conflicts: [][]byte{[]byte("a.txt"), []byte("b.txt")}}
		_, err := newWriter(t, f).Merge(t.Context(), mergeReq(repostore.MergeStyleMerge))
		var conflict *repostore.ConflictError
		require.ErrorAs(t, err, &conflict)
		assert.Equal(t, repostore.MergeStyleMerge, conflict.Style)
		assert.Equal(t, []string{"a.txt", "b.txt"}, conflict.Files)
		assert.False(t, conflict.Rebase)
		assert.Zero(t, f.called("UpdateRefs"))
	})
	t.Run("a rebase conflict names the commit", func(t *testing.T) {
		f := mergeFixture()
		f.mergeResp = &ascv1.MergeResponse{
			Conflicts:       [][]byte{[]byte("a.txt")},
			ConflictDetails: []*ascv1.Conflict{{Changeset: ascid(0x23), Path: []byte("a.txt")}},
		}
		_, err := newWriter(t, f).Merge(t.Context(), mergeReq(repostore.MergeStyleRebase))
		var conflict *repostore.ConflictError
		require.ErrorAs(t, err, &conflict)
		assert.True(t, conflict.Rebase)
		assert.Equal(t, gid(0x23).String(), conflict.CommitSHA)
	})
	t.Run("unrelated histories", func(t *testing.T) {
		f := mergeFixture()
		f.mergeErr = failure(codes.FailedPrecondition, "", "the histories have no common base")
		_, err := newWriter(t, f).Merge(t.Context(), mergeReq(repostore.MergeStyleMerge))
		var conflict *repostore.ConflictError
		require.ErrorAs(t, err, &conflict)
		assert.True(t, conflict.Unrelated)
	})
	t.Run("a fast-forward that is not one", func(t *testing.T) {
		f := mergeFixture()
		f.mergeErr = failure(codes.FailedPrecondition, "", "not a fast-forward")
		_, err := newWriter(t, f).Merge(t.Context(), mergeReq(repostore.MergeStyleFastForwardOnly))
		var conflict *repostore.ConflictError
		require.ErrorAs(t, err, &conflict)
		assert.True(t, conflict.Diverging)
	})
	t.Run("nothing to merge publishes nothing", func(t *testing.T) {
		f := mergeFixture()
		f.mergeResp = &ascv1.MergeResponse{IsEmpty: true}
		res, err := newWriter(t, f).Merge(t.Context(), mergeReq(repostore.MergeStyleMerge))
		require.NoError(t, err)
		assert.Equal(t, gid(0x11).String(), res.Commit.String())
		assert.Zero(t, f.called("UpdateRefs"))
	})
	t.Run("the base moved while the merge ran", func(t *testing.T) {
		f := mergeFixture()
		f.updateErr = failure(codes.Aborted, "REF_CONFLICT", "ref moved")
		_, err := newWriter(t, f).Merge(t.Context(), mergeReq(repostore.MergeStyleMerge))
		var stale *git.ErrPushOutOfDate
		require.ErrorAs(t, err, &stale)
	})
	t.Run("BeforePublish sees the merge and can stop it", func(t *testing.T) {
		f := mergeFixture()
		req := mergeReq(repostore.MergeStyleMerge)
		var got repostore.PublishInput
		stop := errors.New("stop")
		req.BeforePublish = func(_ context.Context, in repostore.PublishInput) error {
			got = in
			return stop
		}
		_, err := newWriter(t, f).Merge(t.Context(), req)
		require.ErrorIs(t, err, stop)
		assert.Equal(t, gid(0x70).String(), got.Commit.String())
		assert.Equal(t, gid(0x11).String(), got.Base.String())
		assert.Equal(t, gid(0x22).String(), got.Head.String())
		assert.Equal(t, gid(0x10).String(), got.MergeBase.String())
		assert.Zero(t, f.called("UpdateRefs"))
	})
}

func TestMergeSquashAuthorAndAmend(t *testing.T) {
	t.Run("squash author", func(t *testing.T) {
		f := mergeFixture()
		f.logEntries[gid(0x22).String()+"|"+gid(0x11).String()] = []*ascv1.LogEntry{
			{Author: &ascv1.Signature{Name: []byte("Cy"), Email: []byte("cy@example.com"), Time: 5}},
			{Author: &ascv1.Signature{Name: []byte("Di"), Email: []byte("di@example.com"), Time: 4}},
		}
		req := mergeReq(repostore.MergeStyleSquash)
		var seen []string
		req.SquashAuthor = func(_ context.Context, authors []*repostore.Signature) (*repostore.Signature, error) {
			for _, a := range authors {
				seen = append(seen, a.Name)
			}
			return authors[1], nil
		}
		_, err := newWriter(t, f).Merge(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, []string{"Cy", "Di"}, seen)
		assert.Equal(t, "Di", string(f.last("Merge").(*ascv1.MergeRequest).GetAuthor().GetName()))
	})
	t.Run("amend the tip of a rebase", func(t *testing.T) {
		f := mergeFixture()
		f.header = &ascv1.LogEntry{
			Message: []byte("old message\n"), Parents: [][]byte{gid(0x60).RawValue()},
			Author: &ascv1.Signature{Name: []byte("Cy"), Email: []byte("cy@example.com"), Time: 5},
		}
		req := mergeReq(repostore.MergeStyleRebase)
		req.AmendMessage = func(_ context.Context, last string) (string, error) {
			assert.Equal(t, "old message\n", last)
			return "new message", nil
		}
		f.applyResp = &ascv1.ApplyResponse{Result: ascid(0x71)}
		res, err := newWriter(t, f).Merge(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, gid(0x71).String(), res.Commit.String())
		a := f.last("Apply").(*ascv1.ApplyRequest)
		assert.Equal(t, ascv1.ApplyMode_APPLY_MODE_CHERRY_PICK, a.GetMode())
		assert.Equal(t, hexOf(0x60), a.GetOnto())
		assert.Equal(t, hexOf(0x70), a.GetChangeset())
		assert.Equal(t, "new message\n", string(a.GetMessage()))
		assert.Equal(t, "Cy", string(a.GetAuthor().GetName()), "the author is kept")
		assert.Equal(t, ascid(0x71), f.last("UpdateRefs").(*ascv1.UpdateRefsRequest).GetUpdates()[0].GetNew().GetChangeset())
	})
	t.Run("an empty amend message keeps the commits", func(t *testing.T) {
		f := mergeFixture()
		f.header = &ascv1.LogEntry{Message: []byte("m"), Parents: [][]byte{gid(0x60).RawValue()}}
		req := mergeReq(repostore.MergeStyleRebase)
		req.AmendMessage = func(context.Context, string) (string, error) { return "", nil }
		res, err := newWriter(t, f).Merge(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, gid(0x70).String(), res.Commit.String())
		assert.Zero(t, f.called("Apply"))
	})
}

func TestMergeRebaseUpdatePublishesTheHeadBranch(t *testing.T) {
	f := mergeFixture()
	f.refs["u/fork|refs/heads/feature"] = gid(0x22)
	req := mergeReq(repostore.MergeStyleRebaseUpdate)
	req.Head = ref("u", "fork")
	req.Publish = &repostore.RefUpdate{Repo: req.Head, Ref: "refs/heads/feature", Force: true}
	_, err := newWriter(t, f).Merge(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, ascv1.MergeStyle_MERGE_STYLE_REBASE, f.last("Merge").(*ascv1.MergeRequest).GetStyle())
	u := f.last("UpdateRefs").(*ascv1.UpdateRefsRequest)
	assert.Equal(t, "u/fork", u.GetRepo().GetName())
	assert.Equal(t, ascv1.UpdateMode_UPDATE_MODE_FORCE, u.GetMode())
	assert.Equal(t, "refs/heads/feature", u.GetUpdates()[0].GetName())
	// The rebased commit is made in the base repository, so it is copied to the head repository before the ref moves.
	cc := f.last("CopyClosure").(*ascv1.CopyClosureRequest)
	assert.Equal(t, "o/r", cc.GetFrom().GetName())
	assert.Equal(t, "u/fork", cc.GetTo().GetName())
	assert.Equal(t, [][]byte{ascid(0x70)}, cc.GetChangesets())
}

func TestMergeSignDecision(t *testing.T) {
	f := mergeFixture()
	req := mergeReq(repostore.MergeStyleMerge)
	var in repostore.SignInput
	req.Meta.Sign = func(_ context.Context, got repostore.SignInput) (*repostore.SignDecision, error) {
		in = got
		return &repostore.SignDecision{KeyID: "k"}, nil
	}
	_, err := newWriter(t, f).Merge(t.Context(), req)
	require.ErrorIs(t, err, repostore.ErrUnsupported)
	assert.Equal(t, gid(0x11).String(), in.Base.String())
	assert.Equal(t, gid(0x22).String(), in.Head.String())
	assert.Zero(t, f.called("Merge"))
}

func TestMergeDryRun(t *testing.T) {
	dry := func() repostore.MergeRequest {
		r := ref("o", "r")
		return repostore.MergeRequest{Base: r, BaseRef: "refs/heads/main", Head: r, HeadRev: "refs/heads/feature", DryRun: true}
	}
	t.Run("mergeable with protected files", func(t *testing.T) {
		f := mergeFixture()
		f.mergeResp = &ascv1.MergeResponse{ChangedProtected: [][]byte{[]byte("Docs/README.md")}}
		req := dry()
		req.ProtectedGlobs = []string{"docs/**"}
		req.WantDivergence = true
		f.logCount[gid(0x22).String()+"|"+gid(0x11).String()] = 3
		f.logCount[gid(0x11).String()+"|"+gid(0x22).String()] = 1
		res, err := newWriter(t, f).Merge(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, repostore.DryRunMergeable, res.Status)
		assert.Equal(t, gid(0x10).String(), res.MergeBase.String())
		assert.Equal(t, gid(0x22).String(), res.Head.String())
		assert.Equal(t, []string{"docs/readme.md"}, res.ProtectedFiles)
		assert.Equal(t, 3, res.Ahead)
		assert.Equal(t, 1, res.Behind)
		m := f.last("Merge").(*ascv1.MergeRequest)
		assert.True(t, m.GetDryRun())
		assert.Equal(t, []string{"docs/**"}, m.GetProtectedGlobs())
		assert.Zero(t, f.called("UpdateRefs"))
	})
	t.Run("divergence is counted by listing when asc has no count_only", func(t *testing.T) {
		f := mergeFixture()
		f.noCountOnly = true
		f.logEntries[gid(0x22).String()+"|"+gid(0x11).String()] = make([]*ascv1.LogEntry, 3)
		f.logEntries[gid(0x11).String()+"|"+gid(0x22).String()] = make([]*ascv1.LogEntry, 1)
		req := dry()
		req.WantDivergence = true
		res, err := newWriter(t, f).Merge(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, 3, res.Ahead)
		assert.Equal(t, 1, res.Behind)
		assert.Equal(t, 3, f.called("Log"), "one refused count_only call, then a listing for each side")
	})
	t.Run("conflict", func(t *testing.T) {
		f := mergeFixture()
		f.mergeResp = &ascv1.MergeResponse{Conflicts: [][]byte{[]byte("a.txt")}, ChangedProtected: [][]byte{[]byte("x")}}
		res, err := newWriter(t, f).Merge(t.Context(), dry())
		require.NoError(t, err)
		assert.Equal(t, repostore.DryRunConflict, res.Status)
		assert.Equal(t, []string{"a.txt"}, res.Conflicts)
		assert.Empty(t, res.ProtectedFiles)
	})
	t.Run("empty", func(t *testing.T) {
		f := mergeFixture()
		f.mergeResp = &ascv1.MergeResponse{IsEmpty: true}
		res, err := newWriter(t, f).Merge(t.Context(), dry())
		require.NoError(t, err)
		assert.Equal(t, repostore.DryRunEmpty, res.Status)
	})
	t.Run("head already in the base", func(t *testing.T) {
		f := mergeFixture()
		f.mergeBase[hexPair(0x11, 0x22)] = gid(0x22)
		res, err := newWriter(t, f).Merge(t.Context(), dry())
		require.NoError(t, err)
		assert.Equal(t, repostore.DryRunAncestor, res.Status)
		assert.Zero(t, f.called("Merge"))
	})
	t.Run("unrelated histories use the base tip as the merge base", func(t *testing.T) {
		f := mergeFixture()
		delete(f.mergeBase, hexPair(0x11, 0x22))
		res, err := newWriter(t, f).Merge(t.Context(), dry())
		require.NoError(t, err)
		assert.Equal(t, gid(0x11).String(), res.MergeBase.String())
	})
	t.Run("divergence only", func(t *testing.T) {
		f := mergeFixture()
		f.logCount[gid(0x22).String()+"|"+gid(0x11).String()] = 2
		req := dry()
		req.DivergenceOnly = true
		res, err := newWriter(t, f).Merge(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, 2, res.Ahead)
		assert.Zero(t, res.Status)
		assert.Zero(t, f.called("Merge"))
	})
}
