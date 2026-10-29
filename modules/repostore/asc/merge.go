// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package asc

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git"
	"forgejo.org/modules/repostore"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func hexString(b []byte) string { return hex.EncodeToString(b) }

// wireStyle maps a merge style onto the asc style. The rebase-update-only style computes the rebase and then
// publishes the rebased head, which is the head branch rather than the base.
func wireStyle(style repostore.MergeStyle) (ascv1.MergeStyle, bool) {
	switch style {
	case repostore.MergeStyleMerge:
		return ascv1.MergeStyle_MERGE_STYLE_MERGE, true
	case repostore.MergeStyleSquash:
		return ascv1.MergeStyle_MERGE_STYLE_SQUASH, true
	case repostore.MergeStyleRebase, repostore.MergeStyleRebaseUpdate:
		return ascv1.MergeStyle_MERGE_STYLE_REBASE, true
	case repostore.MergeStyleRebaseMerge:
		return ascv1.MergeStyle_MERGE_STYLE_REBASE_MERGE, true
	case repostore.MergeStyleFastForwardOnly:
		return ascv1.MergeStyle_MERGE_STYLE_FAST_FORWARD_ONLY, true
	}
	return 0, false
}

// Merge merges Head into Base in one of the MergeStyles, or, with DryRun, reports whether it would merge and
// what conflicts. A head in another repository is copied into the base repository first (CopyClosure).
func (w *Writer) Merge(ctx context.Context, req repostore.MergeRequest) (*repostore.MergeResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	base, err := handle(req.Base)
	if err != nil {
		return nil, err
	}
	head, err := handle(req.Head)
	if err != nil {
		return nil, err
	}
	s, err := w.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer s.close()

	baseTip, err := s.mustResolve(base, req.BaseRef)
	if err != nil {
		return nil, err
	}
	headTip, err := s.mustResolve(head, req.HeadRev)
	if err != nil {
		return nil, err
	}
	if !req.DryRun && req.ExpectHead != nil && req.ExpectHead.String() != headTip.String() {
		return nil, &repostore.HeadMismatchError{Given: req.ExpectHead.String(), Current: headTip.String()}
	}

	if req.DivergenceOnly {
		return s.divergence(base, head, baseTip, headTip, req)
	}
	bp, err := s.pair(base, baseTip)
	if err != nil {
		return nil, err
	}
	hp, err := s.pair(head, headTip)
	if err != nil {
		return nil, err
	}
	if err := s.copyClosure(head, base, hp.asc); err != nil {
		return nil, err
	}
	if req.DryRun {
		return s.dryRun(base, bp, hp, req)
	}
	return s.merge(base, bp, hp, req)
}

// mergeBase returns the best common ancestor in the base repository, or nil when the histories are unrelated.
func (s *session) mergeBase(h *repoHandle, a, b git.ObjectID) (git.ObjectID, error) {
	resp, err := s.api.MergeBase(s.ctx, &ascv1.MergeBaseRequest{Repo: h.gitRef(), A: a.RawValue(), B: [][]byte{b.RawValue()}})
	if err != nil {
		return nil, callErr("MergeBase", err)
	}
	if len(resp.GetBases()) == 0 {
		return nil, nil
	}
	return idFromWire(resp.GetBases()[0])
}

// count counts the commits reachable from include and not from exclude. It asks asc to count (count_only); an
// asc server that does not offer that yet answers Unimplemented, and the commits are then listed and counted
// here, up to MaxLogCommits. Without this fallback no pull request could be opened against such a server.
func (s *session) count(h *repoHandle, include, exclude git.ObjectID) (int, error) {
	if !s.noCountOnly {
		n, err := s.countLog(h, include, exclude, true)
		if err == nil || !isUnimplemented(err) {
			return n, err
		}
		s.noCountOnly = true
	}
	return s.countLog(h, include, exclude, false)
}

// isUnimplemented reports whether err is asc's answer that the call or an option of it is not available.
func isUnimplemented(err error) bool {
	var ae *Error
	if errors.As(err, &ae) {
		return ae.Code == codes.Unimplemented
	}
	return status.Code(err) == codes.Unimplemented
}

func (s *session) countLog(h *repoHandle, include, exclude git.ObjectID, countOnly bool) (int, error) {
	req := &ascv1.LogRequest{
		Repo: h.gitRef(), Include: []string{include.String()}, Exclude: []string{exclude.String()}, CountOnly: countOnly,
	}
	if !countOnly {
		req.Limit = MaxLogCommits + 1
	}
	st, err := s.api.Log(s.ctx, req)
	if err != nil {
		return 0, callErr("Log", err)
	}
	var counted, entries uint64
	for {
		msg, err := st.Recv()
		if errors.Is(err, io.EOF) {
			n := max(counted, entries)
			if n > MaxLogCommits {
				return 0, fmt.Errorf("%w: more than %d commits", ErrTooLarge, MaxLogCommits)
			}
			return int(n), nil
		}
		if err != nil {
			return 0, callErr("Log", err)
		}
		counted = max(counted, msg.GetCount())
		if entries += uint64(len(msg.GetEntries())); entries > MaxLogCommits {
			return 0, fmt.Errorf("%w: more than %d commits", ErrTooLarge, MaxLogCommits)
		}
	}
}

// divergence answers a DivergenceOnly dry run. The head has to be in the base repository for the counts, so a
// head in another repository is copied first.
func (s *session) divergence(base, head *repoHandle, baseTip, headTip git.ObjectID, req repostore.MergeRequest) (*repostore.MergeResult, error) {
	if !same(base, head) {
		hp, err := s.pair(head, headTip)
		if err != nil {
			return nil, err
		}
		if err := s.copyClosure(head, base, hp.asc); err != nil {
			return nil, err
		}
	}
	res := &repostore.MergeResult{Head: headTip, MergeBase: baseTip}
	mb, err := s.mergeBase(base, baseTip, headTip)
	if err != nil {
		return nil, err
	}
	if mb != nil {
		res.MergeBase = mb
	}
	if err := s.fillDivergence(base, baseTip, headTip, res); err != nil {
		return nil, err
	}
	return res, nil
}

func (s *session) fillDivergence(base *repoHandle, baseTip, headTip git.ObjectID, res *repostore.MergeResult) error {
	var err error
	if res.Ahead, err = s.count(base, headTip, baseTip); err != nil {
		return err
	}
	if res.Behind, err = s.count(base, baseTip, headTip); err != nil {
		return err
	}
	return nil
}

func (s *session) dryRun(base *repoHandle, bp, hp gitAndAsc, req repostore.MergeRequest) (*repostore.MergeResult, error) {
	res := &repostore.MergeResult{Head: hp.git, BaseBefore: bp.git, MergeBase: bp.git}
	mb, err := s.mergeBase(base, bp.git, hp.git)
	if err != nil {
		return nil, err
	}
	if mb != nil {
		res.MergeBase = mb
	}
	if req.WantDivergence {
		if err := s.fillDivergence(base, bp.git, hp.git, res); err != nil {
			return nil, err
		}
	}
	// A head that equals the merge base is part of the base already.
	if hp.git.String() == res.MergeBase.String() {
		res.Status = repostore.DryRunAncestor
		return res, nil
	}

	resp, err := s.api.Merge(s.ctx, &ascv1.MergeRequest{
		Repo: base.ascRef(), Base: hexString(bp.asc), Head: hexString(hp.asc), Style: ascv1.MergeStyle_MERGE_STYLE_MERGE,
		DryRun: true, ProtectedGlobs: req.ProtectedGlobs,
	})
	if err != nil {
		return nil, mergeErr(repostore.MergeStyleMerge, err)
	}
	switch {
	case len(resp.GetConflicts()) > 0:
		res.Status = repostore.DryRunConflict
		res.Conflicts = conflictPaths(resp.GetConflicts())
	case resp.GetIsEmpty():
		res.Status = repostore.DryRunEmpty
	default:
		res.Status = repostore.DryRunMergeable
		for _, p := range resp.GetChangedProtected() {
			if len(res.ProtectedFiles) >= MaxProtected {
				break
			}
			res.ProtectedFiles = append(res.ProtectedFiles, strings.ToLower(string(p)))
		}
	}
	return res, nil
}

// squashAuthor chooses the author of a squash commit.
func (s *session) squashAuthor(ctx context.Context, base *repoHandle, baseTip, headTip git.ObjectID, choose func(context.Context, []*repostore.Signature) (*repostore.Signature, error)) (*repostore.Signature, error) {
	st, err := s.api.Log(s.ctx, &ascv1.LogRequest{
		Repo: base.gitRef(), Include: []string{headTip.String()}, Exclude: []string{baseTip.String()},
	})
	if err != nil {
		return nil, callErr("Log", err)
	}
	var authors []*repostore.Signature
	for {
		msg, err := st.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, callErr("Log", err)
		}
		for _, e := range msg.GetEntries() {
			if authors = append(authors, fromWireSignature(e.GetAuthor())); len(authors) > MaxLogCommits {
				return nil, fmt.Errorf("%w: more than %d commits to squash", ErrTooLarge, MaxLogCommits)
			}
		}
	}
	return choose(ctx, authors)
}

func (s *session) merge(base *repoHandle, bp, hp gitAndAsc, req repostore.MergeRequest) (*repostore.MergeResult, error) {
	style, _ := wireStyle(req.Style)
	meta := req.Meta
	if req.Style == repostore.MergeStyleSquash && req.SquashAuthor != nil {
		author, err := s.squashAuthor(s.caller, base, bp.git, hp.git, req.SquashAuthor)
		if err != nil {
			return nil, err
		}
		meta.Author = author
	}
	mw, err := buildMeta(s.caller, req.Base, meta, bp.git, hp.git, false)
	if err != nil {
		return nil, err
	}
	resp, err := s.api.Merge(s.ctx, &ascv1.MergeRequest{
		Repo: base.ascRef(), Base: hexString(bp.asc), Head: hexString(hp.asc), Style: style,
		Message: mw.message, Author: mw.author, Committer: mw.committer,
	})
	if err != nil {
		return nil, mergeErr(req.Style, err)
	}
	if len(resp.GetConflicts()) > 0 {
		return nil, s.conflict(base, req.Style, resp)
	}
	result := &repostore.MergeResult{BaseBefore: bp.git}
	tip := resp.GetResult()
	if len(tip) == 0 {
		if !resp.GetIsEmpty() {
			return nil, fmt.Errorf("%w: Merge answered neither a result nor conflicts", ErrBadAnswer)
		}
		// The head is part of the base already, or the merge changes nothing: there is nothing to publish.
		result.Commit = bp.git
		if req.Style == repostore.MergeStyleRebaseUpdate {
			result.Commit = hp.git
		}
		return result, nil
	}
	commit, err := s.gitOf(base, tip, nil)
	if err != nil {
		return nil, err
	}
	if req.Style == repostore.MergeStyleRebase && req.AmendMessage != nil && commit.String() != bp.git.String() {
		if commit, tip, err = s.amend(base, commit, tip, mw, req.AmendMessage); err != nil {
			return nil, err
		}
	}
	result.Commit = commit

	in := repostore.PublishInput{Commit: commit, Base: bp.git, Head: hp.git}
	if req.BeforePublish != nil {
		mb, err := s.mergeBase(base, bp.git, hp.git)
		if err != nil {
			return nil, err
		}
		in.MergeBase = mb
	}
	if err := s.finish(base, req.Publish, req.BeforePublish, in, tip, req.HookEnv); err != nil {
		return nil, err
	}
	return result, nil
}

// amend rewrites the message of the tip of a rebase, as git commit --amend does: a cherry-pick of the tip onto
// its parent with the new message and the same author.
func (s *session) amend(base *repoHandle, tip git.ObjectID, tipAsc []byte, mw *metaWire, choose func(context.Context, string) (string, error)) (git.ObjectID, []byte, error) {
	resp, err := s.api.ReadChangeset(s.ctx, &ascv1.ReadChangesetRequest{Repo: base.gitRef(), Rev: tip.String()})
	if err != nil {
		return nil, nil, callErr("ReadChangeset", err)
	}
	header := resp.GetHeader()
	last := string(header.GetMessage())
	message, err := choose(s.caller, last)
	if err != nil {
		return nil, nil, err
	}
	if message == "" {
		return tip, tipAsc, nil
	}
	if len(header.GetParents()) != 1 {
		return nil, nil, fmt.Errorf("%w: the tip of a rebase has %d parents", ErrBadAnswer, len(header.GetParents()))
	}
	parent, err := idFromWire(header.GetParents()[0])
	if err != nil {
		return nil, nil, err
	}
	ids, err := s.gitToAsc(base, parent, tip)
	if err != nil {
		return nil, nil, err
	}
	areq := &ascv1.ApplyRequest{
		Repo: base.ascRef(), Onto: hexString(ids[0]), Changeset: hexString(ids[1]), Mode: ascv1.ApplyMode_APPLY_MODE_CHERRY_PICK,
		Message: append([]byte(strings.TrimRight(message, "\n")), '\n'), Author: header.GetAuthor(), Committer: mw.committer,
	}
	done, err := s.api.Apply(s.ctx, areq)
	if err != nil {
		return nil, nil, callErr("Apply", err)
	}
	if len(done.GetResult()) == 0 || len(done.GetConflicts()) > 0 {
		return nil, nil, fmt.Errorf("%w: amending the rebase tip did not give a result", ErrBadAnswer)
	}
	commit, err := s.gitOf(base, done.GetResult(), nil)
	if err != nil {
		return nil, nil, err
	}
	return commit, done.GetResult(), nil
}

// conflict builds the ConflictError of a merge answer that lists conflicts.
func (s *session) conflict(base *repoHandle, style repostore.MergeStyle, resp *ascv1.MergeResponse) error {
	ce := &repostore.ConflictError{Style: style, Files: conflictPaths(resp.GetConflicts())}
	if style == repostore.MergeStyleRebase || style == repostore.MergeStyleRebaseMerge || style == repostore.MergeStyleRebaseUpdate {
		ce.Rebase = true
		if details := resp.GetConflictDetails(); len(details) > 0 && len(details[0].GetChangeset()) == ascIDLen {
			if ids, err := s.ascToGit(base, details[0].GetChangeset()); err == nil {
				ce.CommitSHA = ids[0].String()
			}
		}
	}
	return ce
}

// mergeErr maps the refusals of Merge onto the conflict flags callers handle. A conflict itself is an answer,
// not an error (SPEC 8.7); these two are errors.
func mergeErr(style repostore.MergeStyle, err error) error {
	if st, ok := status.FromError(err); ok && st.Code() == codes.FailedPrecondition {
		msg := st.Message()
		switch {
		case strings.Contains(msg, "no common base"):
			return &repostore.ConflictError{Style: style, Unrelated: true, StdErr: sanitize(msg), Err: err}
		case strings.Contains(msg, "not a fast-forward"):
			return &repostore.ConflictError{Style: style, Diverging: true, StdErr: sanitize(msg), Err: err}
		}
	}
	// A typed refusal of the gate (a secret, a missing review, a signature) is a rejected push with its reason, as for
	// any other write.
	return publishErr("Merge", err)
}
