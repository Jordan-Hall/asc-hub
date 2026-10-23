// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package dual

import (
	"context"
	"crypto/sha256"
	"errors"
	"hash"
	"io"
	"iter"
	"slices"
	"strconv"
	"strings"

	"forgejo.org/modules/git"
)

// Every method below calls the git backend first and returns its answer unchanged. When the call is
// sampled it also hands a digest of that answer, taken before returning, to observe.

// shadowRead builds the shadow half of a comparison: repeat the read, then summarise the answer.
func shadowRead[T any](read func(ctx context.Context, sh git.Backend) (T, error), fp func(T) fingerprint) func(ctx context.Context, sh git.Backend) (fingerprint, error) {
	return func(ctx context.Context, sh git.Backend) (fingerprint, error) {
		v, err := read(ctx, sh)
		if err != nil {
			return fingerprint{}, err
		}
		return fp(v), nil
	}
}

// ---------------------------------------------------------------------------------------- objects

// ReadCommit reads a commit from git.
func (d *Backend) ReadCommit(ctx context.Context, id git.ObjectID) (*git.CommitData, error) {
	sampled := d.shouldSample()
	c, err := d.primary.ReadCommit(ctx, id)
	if sampled {
		d.observe(mReadCommit, primaryOutcome(err, func() fingerprint { return fpCommit(c) }), false,
			shadowRead(func(ctx context.Context, sh git.Backend) (*git.CommitData, error) { return sh.ReadCommit(ctx, id) }, fpCommit))
	}
	return c, err
}

// isTree reports whether git knows id as a tree. The shadow cannot find a tree it was not shown the
// commit of, so its "no such object" for a tree id is a known limit.
func (d *Backend) isTree(ctx context.Context, id git.ObjectID) bool {
	infos, err := d.primary.StatObjects(ctx, []git.ObjectID{id})
	return err != nil || (len(infos) == 1 && !infos[0].Missing && infos[0].Type == git.ObjectTree)
}

// ReadTree lists one tree level from git.
func (d *Backend) ReadTree(ctx context.Context, id git.ObjectID) ([]git.TreeEntryData, error) {
	sampled := d.shouldSample()
	entries, err := d.primary.ReadTree(ctx, id)
	if sampled {
		lenient := err == nil && d.isTree(ctx, id)
		d.observe(mReadTree, primaryOutcome(err, func() fingerprint { return fpTree(entries) }), lenient,
			shadowRead(func(ctx context.Context, sh git.Backend) ([]git.TreeEntryData, error) { return sh.ReadTree(ctx, id) }, fpTree))
	}
	return entries, err
}

// ReadTreeRecursive walks a tree in git. The walk is compared when the caller reads it to the end.
func (d *Backend) ReadTreeRecursive(ctx context.Context, id git.ObjectID, opt git.TreeWalkOpts) iter.Seq2[git.TreeEntryData, error] {
	seq := d.primary.ReadTreeRecursive(ctx, id, opt)
	if !d.shouldSample() {
		return seq
	}
	return func(yield func(git.TreeEntryData, error) bool) {
		var rows []string
		var failed, over bool
		for e, err := range seq {
			switch {
			case err != nil:
				failed = true
			case len(rows) >= maxCompareItems:
				over = true
			default:
				rows = append(rows, treeRow(e))
			}
			if !yield(e, err) {
				countSkip(skipPartial)
				return
			}
		}
		want := outcome{fp: fpRows(rows)}
		switch {
		case failed:
			want = skipped(skipPrimaryError)
		case over:
			want = skipped(skipTooLarge)
		}
		lenient := !failed && d.isTree(ctx, id)
		d.observe(mReadTreeRecursive, want, lenient, func(ctx context.Context, sh git.Backend) (fingerprint, error) {
			return fpWalk(sh.ReadTreeRecursive(ctx, id, opt))
		})
	}
}

// OpenBlob opens a blob from git. The blob is compared when the caller reads all of it.
func (d *Backend) OpenBlob(ctx context.Context, id git.ObjectID, limit int64) (io.ReadCloser, int64, error) {
	sampled := d.shouldSample()
	rc, size, err := d.primary.OpenBlob(ctx, id, limit)
	if !sampled {
		return rc, size, err
	}
	if err != nil {
		d.observe(mOpenBlob, primaryOutcome(err, nil), false, shadowBlob(id, limit))
		return rc, size, err
	}
	expect := size
	if limit > 0 && limit < expect {
		expect = limit
	}
	if expect < 0 || expect > maxCompareBytes {
		countSkip(skipTooLarge)
		return rc, size, nil
	}
	return &blobTee{rc: rc, h: sha256.New(), expect: expect, size: size, finish: func(read int64, sum []byte) {
		d.observe(mOpenBlob, outcome{fp: blobFP(size, read, sum)}, false, shadowBlob(id, limit))
	}}, size, nil
}

func blobFP(size, read int64, sum []byte) fingerprint {
	return newFP().ident(strconv.FormatInt(size, 10)).num(read).text(string(sum)).item().done()
}

// shadowBlob reads the same blob from the shadow, up to the comparison cap.
func shadowBlob(id git.ObjectID, limit int64) func(ctx context.Context, sh git.Backend) (fingerprint, error) {
	return func(ctx context.Context, sh git.Backend) (fingerprint, error) {
		rc, size, err := sh.OpenBlob(ctx, id, limit)
		if err != nil {
			return fingerprint{}, err
		}
		defer rc.Close()
		h := sha256.New()
		n, err := io.Copy(h, io.LimitReader(rc, maxCompareBytes+1))
		if err != nil {
			return fingerprint{}, err
		}
		return blobFP(size, n, h.Sum(nil)), nil
	}
}

// blobTee passes a blob through unchanged and digests what the caller reads.
type blobTee struct {
	rc       io.ReadCloser
	h        hash.Hash
	expect   int64 // the bytes a complete read returns
	size     int64
	read     int64
	failed   bool
	finished bool
	finish   func(read int64, sum []byte)
}

func (t *blobTee) Read(p []byte) (int, error) {
	n, err := t.rc.Read(p)
	if n > 0 {
		t.h.Write(p[:n])
		t.read += int64(n)
	}
	if err != nil && !errors.Is(err, io.EOF) {
		t.failed = true
	}
	if err != nil || t.read >= t.expect {
		t.complete()
	}
	return n, err
}

func (t *blobTee) Close() error {
	t.complete()
	return t.rc.Close()
}

func (t *blobTee) complete() {
	if t.finished {
		return
	}
	t.finished = true
	if t.failed || t.read != t.expect {
		countSkip(skipPartial)
		return
	}
	t.finish(t.read, t.h.Sum(nil))
}

// StatObjects stats objects in git.
func (d *Backend) StatObjects(ctx context.Context, ids []git.ObjectID) ([]git.ObjectInfo, error) {
	sampled := d.shouldSample()
	infos, err := d.primary.StatObjects(ctx, ids)
	if sampled {
		ids := slices.Clone(ids)
		var skip []bool
		for i, in := range infos {
			if in.Type == git.ObjectTag {
				if skip == nil {
					skip = make([]bool, len(infos))
				}
				skip[i] = true
			}
		}
		d.observe(mStatObjects, primaryOutcome(err, func() fingerprint { return fpStat(infos, skip) }), false,
			shadowRead(func(ctx context.Context, sh git.Backend) ([]git.ObjectInfo, error) { return sh.StatObjects(ctx, ids) },
				func(v []git.ObjectInfo) fingerprint { return fpStat(v, skip) }))
	}
	return infos, err
}

// ReadTag reads a tag object from git. It is never sampled: asc does not serve tag objects.
func (d *Backend) ReadTag(ctx context.Context, id git.ObjectID) (*git.TagData, error) {
	return d.primary.ReadTag(ctx, id)
}

// resolveUncomparable reports whether asc is documented to answer rev differently from git.
func resolveUncomparable(rev string, typ git.ObjectType) bool {
	if typ == git.ObjectTag || strings.HasPrefix(rev, ":/") || strings.Contains(rev, "^{") {
		return true
	}
	if len(rev) < 40 && len(rev) >= 4 {
		for i := 0; i < len(rev); i++ {
			c := rev[i]
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
				return false
			}
		}
		return true // an abbreviated id
	}
	return false
}

type resolved struct {
	id  git.ObjectID
	typ git.ObjectType
}

func fpResolved(r resolved) fingerprint {
	return newFP().ident(idString(r.id)).ident(string(r.typ)).item().done()
}

// Resolve resolves a revision in git.
func (d *Backend) Resolve(ctx context.Context, rev string) (git.ObjectID, git.ObjectType, error) {
	sampled := d.shouldSample()
	id, typ, err := d.primary.Resolve(ctx, rev)
	if sampled {
		want := primaryOutcome(err, func() fingerprint { return fpResolved(resolved{id, typ}) })
		if err == nil && resolveUncomparable(rev, typ) {
			want = skipped(skipUncomparable)
		}
		d.observe(mResolve, want, false, shadowRead(func(ctx context.Context, sh git.Backend) (resolved, error) {
			id, typ, err := sh.Resolve(ctx, rev)
			return resolved{id, typ}, err
		}, fpResolved))
	}
	return id, typ, err
}

// ---------------------------------------------------------------------------------------- refs

// ListRefs lists references in git.
func (d *Backend) ListRefs(ctx context.Context, q git.RefQuery) ([]git.Reference, error) {
	sampled := d.shouldSample()
	refs, err := d.primary.ListRefs(ctx, q)
	if sampled {
		annotated := annotatedTags(refs)
		d.observe(mListRefs, primaryOutcome(err, func() fingerprint { return fpRefs(refs, annotated) }), false,
			shadowRead(func(ctx context.Context, sh git.Backend) ([]git.Reference, error) { return sh.ListRefs(ctx, q) },
				func(v []git.Reference) fingerprint { return fpRefs(v, annotated) }))
	}
	return refs, err
}

// Head returns the HEAD reference of git.
func (d *Backend) Head(ctx context.Context) (git.RefName, error) {
	sampled := d.shouldSample()
	head, err := d.primary.Head(ctx)
	if sampled {
		d.observe(mHead, primaryOutcome(err, func() fingerprint { return fpString(string(head)) }), false,
			shadowRead(func(ctx context.Context, sh git.Backend) (git.RefName, error) { return sh.Head(ctx) },
				func(v git.RefName) fingerprint { return fpString(string(v)) }))
	}
	return head, err
}

// SetHead writes to git only. asc is never written from here.
func (d *Backend) SetHead(ctx context.Context, branch git.RefName) error {
	return d.primary.SetHead(ctx, branch)
}

// ---------------------------------------------------------------------------------------- history

// cloneQuery copies the slices of q, which the caller may reuse once the call returns.
func cloneQuery(q git.LogQuery) git.LogQuery {
	q.From = slices.Clone(q.From)
	q.Not = slices.Clone(q.Not)
	q.Paths = slices.Clone(q.Paths)
	return q
}

// Log lists commits from git. The list is compared when the caller reads it to the end.
func (d *Backend) Log(ctx context.Context, q git.LogQuery) iter.Seq2[*git.CommitData, error] {
	seq := d.primary.Log(ctx, q)
	if !d.shouldSample() {
		return seq
	}
	q = cloneQuery(q)
	return func(yield func(*git.CommitData, error) bool) {
		b := newFP()
		var failed, over bool
		for c, err := range seq {
			switch {
			case err != nil:
				failed = true
			case b.items >= maxCompareItems:
				over = true
			case !over:
				func() {
					defer func() {
						if recover() != nil {
							failed = true
						}
					}()
					b.commit(c)
					b.item()
				}()
			}
			if !yield(c, err) {
				countSkip(skipPartial)
				return
			}
		}
		want := outcome{fp: b.done()}
		switch {
		case failed:
			want = skipped(skipPrimaryError)
		case over:
			want = skipped(skipTooLarge)
		}
		d.observe(mLog, want, false, func(ctx context.Context, sh git.Backend) (fingerprint, error) {
			return drain(sh.Log(ctx, q), func(b *fpBuilder, c *git.CommitData) { b.commit(c) })
		})
	}
}

// Count counts commits in git.
func (d *Backend) Count(ctx context.Context, q git.LogQuery) (int64, error) {
	sampled := d.shouldSample()
	n, err := d.primary.Count(ctx, q)
	if sampled {
		q := cloneQuery(q)
		d.observe(mCount, primaryOutcome(err, func() fingerprint { return fpInt(n) }), false,
			shadowRead(func(ctx context.Context, sh git.Backend) (int64, error) { return sh.Count(ctx, q) }, fpInt))
	}
	return n, err
}

// MergeBase finds the merge base in git.
func (d *Backend) MergeBase(ctx context.Context, a, b git.ObjectID) (git.ObjectID, error) {
	sampled := d.shouldSample()
	id, err := d.primary.MergeBase(ctx, a, b)
	if sampled {
		d.observe(mMergeBase, primaryOutcome(err, func() fingerprint { return fpString(idString(id)) }), false,
			shadowRead(func(ctx context.Context, sh git.Backend) (git.ObjectID, error) { return sh.MergeBase(ctx, a, b) },
				func(v git.ObjectID) fingerprint { return fpString(idString(v)) }))
	}
	return id, err
}

// IsAncestor answers from git.
func (d *Backend) IsAncestor(ctx context.Context, ancestor, desc git.ObjectID) (bool, error) {
	sampled := d.shouldSample()
	ok, err := d.primary.IsAncestor(ctx, ancestor, desc)
	if sampled {
		d.observe(mIsAncestor, primaryOutcome(err, func() fingerprint { return fpBool(ok) }), false,
			shadowRead(func(ctx context.Context, sh git.Backend) (bool, error) { return sh.IsAncestor(ctx, ancestor, desc) }, fpBool))
	}
	return ok, err
}

// LastCommits answers from git.
func (d *Backend) LastCommits(ctx context.Context, at git.ObjectID, dir string, names []string) (map[string]git.ObjectID, error) {
	sampled := d.shouldSample()
	m, err := d.primary.LastCommits(ctx, at, dir, names)
	if sampled {
		names := slices.Clone(names)
		d.observe(mLastCommits, primaryOutcome(err, func() fingerprint { return fpLastCommits(m) }), false,
			shadowRead(func(ctx context.Context, sh git.Backend) (map[string]git.ObjectID, error) {
				return sh.LastCommits(ctx, at, dir, names)
			}, fpLastCommits))
	}
	return m, err
}

// Blame blames a file in git. The blame is compared when the caller reads it to the end.
func (d *Backend) Blame(ctx context.Context, at git.ObjectID, path string, opt git.BlameOpts) iter.Seq2[git.BlamePart, error] {
	seq := d.primary.Blame(ctx, at, path, opt)
	if !d.shouldSample() {
		return seq
	}
	return func(yield func(git.BlamePart, error) bool) {
		b := newFP()
		var failed, over bool
		for p, err := range seq {
			switch {
			case err != nil:
				failed = true
			case b.items >= maxCompareItems:
				over = true
			case !over:
				b.blamePart(p)
				b.item()
			}
			if !yield(p, err) {
				countSkip(skipPartial)
				return
			}
		}
		want := outcome{fp: b.done()}
		switch {
		case failed:
			want = skipped(skipPrimaryError)
		case over:
			want = skipped(skipTooLarge)
		}
		d.observe(mBlame, want, false, func(ctx context.Context, sh git.Backend) (fingerprint, error) {
			return drain(sh.Blame(ctx, at, path, opt), (*fpBuilder).blamePart)
		})
	}
}

// ---------------------------------------------------------------------------------------- diff

// ChangedPaths lists changed paths from git.
func (d *Backend) ChangedPaths(ctx context.Context, base, head git.ObjectID, opt git.DiffOpts) ([]git.PathChange, error) {
	sampled := d.shouldSample()
	changes, err := d.primary.ChangedPaths(ctx, base, head, opt)
	if sampled {
		d.observe(mChangedPaths, primaryOutcome(err, func() fingerprint { return fpPaths(changes) }), false,
			shadowRead(func(ctx context.Context, sh git.Backend) ([]git.PathChange, error) {
				return sh.ChangedPaths(ctx, base, head, opt)
			}, fpPaths))
	}
	return changes, err
}

// UnifiedDiff writes the diff from git. The text is compared except in format-patch mode.
func (d *Backend) UnifiedDiff(ctx context.Context, base, head git.ObjectID, opt git.DiffOpts, w io.Writer) error {
	if !d.shouldSample() {
		return d.primary.UnifiedDiff(ctx, base, head, opt, w)
	}
	if opt.Mode == git.DiffModePatch {
		countSkip(skipUncomparable)
		return d.primary.UnifiedDiff(ctx, base, head, opt, w)
	}
	tee := &hashWriter{w: w, h: sha256.New()}
	err := d.primary.UnifiedDiff(ctx, base, head, opt, tee)
	want := primaryOutcome(err, tee.fingerprint)
	if err == nil && tee.over {
		want = skipped(skipTooLarge)
	}
	d.observe(mUnifiedDiff, want, false, func(ctx context.Context, sh git.Backend) (fingerprint, error) {
		out := &hashWriter{h: sha256.New()}
		if err := sh.UnifiedDiff(ctx, base, head, opt, out); err != nil {
			return fingerprint{}, err
		}
		return out.fingerprint(), nil
	})
	return err
}

// hashWriter passes writes through to w (when set) and digests up to maxCompareBytes of what was written.
type hashWriter struct {
	w     io.Writer
	h     hash.Hash
	total int64
	over  bool
}

func (hw *hashWriter) Write(p []byte) (int, error) {
	n := len(p)
	var err error
	if hw.w != nil {
		n, err = hw.w.Write(p)
	}
	written := p[:n]
	if room := maxCompareBytes - hw.total; int64(len(written)) > room {
		written, hw.over = written[:max(room, 0)], true
	}
	hw.h.Write(written)
	hw.total += int64(n)
	return n, err
}

func (hw *hashWriter) fingerprint() fingerprint {
	return newFP().ident(strconv.FormatInt(hw.total, 10)).flag(hw.over).text(string(hw.h.Sum(nil))).item().done()
}

// DiffStat answers from git.
func (d *Backend) DiffStat(ctx context.Context, base, head git.ObjectID) (git.DiffStat, error) {
	sampled := d.shouldSample()
	s, err := d.primary.DiffStat(ctx, base, head)
	if sampled {
		d.observe(mDiffStat, primaryOutcome(err, func() fingerprint { return fpDiffStat(s) }), false,
			shadowRead(func(ctx context.Context, sh git.Backend) (git.DiffStat, error) { return sh.DiffStat(ctx, base, head) }, fpDiffStat))
	}
	return s, err
}

// ---------------------------------------------------------------------------------------- bulk

// Archive writes the archive from git. It is never sampled: the two backends write different bytes.
func (d *Backend) Archive(ctx context.Context, at git.ObjectID, format git.ArchiveType, prefix string, w io.Writer) error {
	return d.primary.Archive(ctx, at, format, prefix, w)
}

// Grep searches in git.
func (d *Backend) Grep(ctx context.Context, at git.ObjectID, q git.GrepQuery) ([]*git.GrepResult, error) {
	sampled := d.shouldSample()
	results, err := d.primary.Grep(ctx, at, q)
	if sampled {
		d.observe(mGrep, primaryOutcome(err, func() fingerprint { return fpGrep(results) }), false,
			shadowRead(func(ctx context.Context, sh git.Backend) ([]*git.GrepResult, error) { return sh.Grep(ctx, at, q) }, fpGrep))
	}
	return results, err
}

// Attributes answers from git.
func (d *Backend) Attributes(ctx context.Context, at git.ObjectID, paths, names []string) (map[string]map[string]string, error) {
	sampled := d.shouldSample()
	attrs, err := d.primary.Attributes(ctx, at, paths, names)
	if sampled {
		paths, names := slices.Clone(paths), slices.Clone(names)
		d.observe(mAttributes, primaryOutcome(err, func() fingerprint { return fpAttributes(attrs) }), false,
			shadowRead(func(ctx context.Context, sh git.Backend) (map[string]map[string]string, error) {
				return sh.Attributes(ctx, at, paths, names)
			}, fpAttributes))
	}
	return attrs, err
}

// LanguageStats answers from git.
func (d *Backend) LanguageStats(ctx context.Context, at git.ObjectID) (map[string]int64, error) {
	sampled := d.shouldSample()
	stats, err := d.primary.LanguageStats(ctx, at)
	if sampled {
		d.observe(mLanguageStats, primaryOutcome(err, func() fingerprint { return fpLanguages(stats) }), false,
			shadowRead(func(ctx context.Context, sh git.Backend) (map[string]int64, error) { return sh.LanguageStats(ctx, at) }, fpLanguages))
	}
	return stats, err
}
