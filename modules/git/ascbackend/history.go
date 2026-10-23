// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package ascbackend

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"iter"
	"math"
	"strings"
	"sync"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git"
	"forgejo.org/modules/util"
)

// logRequest builds the Log request of q. It reports ok = false when q selects nothing at all.
func (b *Backend) logRequest(ctx context.Context, q git.LogQuery) (req *ascv1.LogRequest, ok bool, err error) {
	if len(q.From) > maxQueryItems || len(q.Not) > maxQueryItems || len(q.Paths) > maxQueryItems {
		return nil, false, util.NewInvalidArgumentErrorf("log query has more than %d start points, exclusions or paths", maxQueryItems)
	}
	if q.Skip < 0 || q.Limit < 0 || q.Skip > math.MaxUint32 || q.Limit > math.MaxUint32 {
		return nil, false, util.NewInvalidArgumentErrorf("log query has a skip or limit out of range")
	}
	if q.Follow && (len(q.Paths) != 1 || strings.HasPrefix(q.Paths[0], ":")) {
		return nil, false, util.NewInvalidArgumentErrorf("follow needs exactly one plain path")
	}
	req = &ascv1.LogRequest{Repo: b.gitRef(), Follow: q.Follow, Author: q.Author, Grep: q.Grep}
	if !q.Since.IsZero() {
		req.Since = q.Since.Unix()
	}
	if !q.Until.IsZero() {
		req.Until = q.Until.Unix()
	}
	if !q.NoMerges {
		// With NoMerges the filter runs here, so skip and limit must too.
		req.Skip, req.Limit = uint32(q.Skip), uint32(q.Limit)
	}

	switch {
	case q.All:
		listed, err := b.collectRefs(ctx, "")
		if err != nil {
			return nil, false, err
		}
		for _, r := range listed {
			if strings.HasPrefix(r.name, git.RemotePrefix) || r.name == "refs/stash" {
				continue
			}
			req.Include = append(req.Include, r.name)
		}
		if len(req.Include) > maxQueryItems {
			return nil, false, fmt.Errorf("%w: %d references to start a log from", ErrTooLarge, len(req.Include))
		}
		if len(req.Include) == 0 {
			return nil, false, nil
		}
	case len(q.From) == 0:
		return nil, false, util.NewInvalidArgumentErrorf("log query has no start point")
	default:
		for _, id := range q.From {
			if id == nil {
				return nil, false, util.NewInvalidArgumentErrorf("log query has a nil start point")
			}
			if err := requireSha1(id); err != nil {
				return nil, false, err
			}
			req.Include = append(req.Include, id.String())
		}
	}
	for _, id := range q.Not {
		if id == nil {
			return nil, false, util.NewInvalidArgumentErrorf("log query has a nil exclusion")
		}
		if err := requireSha1(id); err != nil {
			return nil, false, err
		}
		req.Exclude = append(req.Exclude, id.String())
	}
	for _, p := range q.Paths {
		if !validRev(p) {
			return nil, false, util.NewInvalidArgumentErrorf("log query has an invalid path")
		}
		if q.Follow {
			req.Paths = append(req.Paths, p)
		} else {
			req.Paths = append(req.Paths, ":(literal)"+p)
		}
	}
	return req, true, nil
}

// logEntries streams the log entries q selects, newest first, checking each one.
func (b *Backend) logEntries(ctx context.Context, q git.LogQuery) iter.Seq2[*ascv1.LogEntry, error] {
	return func(yield func(*ascv1.LogEntry, error) bool) {
		ctx, cancel, err := b.stream(ctx)
		if err != nil {
			yield(nil, err)
			return
		}
		defer cancel()
		req, ok, err := b.logRequest(ctx, q)
		if err != nil {
			yield(nil, err)
			return
		}
		if !ok {
			return
		}
		st, err := b.api.Log(ctx, req)
		if err != nil {
			yield(nil, callErr("Log", "", err))
			return
		}
		scanned, skipped, emitted := 0, 0, 0
		for {
			msg, err := st.Recv()
			if errors.Is(err, io.EOF) {
				return
			}
			if err != nil {
				yield(nil, callErr("Log", "", err))
				return
			}
			if msg.GetTruncated() {
				yield(nil, badAnswer("Log stopped at its scan budget"))
				return
			}
			for _, e := range msg.GetEntries() {
				if scanned++; scanned > maxLogCommits {
					yield(nil, fmt.Errorf("%w: more than %d commits in a log", ErrTooLarge, maxLogCommits))
					return
				}
				if q.NoMerges {
					if len(e.GetParents()) > 1 {
						continue
					}
					if skipped < q.Skip {
						skipped++
						continue
					}
					if q.Limit > 0 && emitted >= q.Limit {
						return
					}
				}
				emitted++
				if !yield(e, nil) {
					return
				}
			}
		}
	}
}

// Count counts the commits q selects.
func (b *Backend) Count(ctx context.Context, q git.LogQuery) (int64, error) {
	if q.Follow {
		return 0, util.NewInvalidArgumentErrorf("follow needs exactly one plain path")
	}
	var n int64
	for _, err := range b.logEntries(ctx, q) {
		if err != nil {
			return 0, err
		}
		n++
	}
	return n, nil
}

// Log yields the commits q selects, newest first. Each commit is read in full (ReadChangeset) a few at a
// time, because a log entry carries neither the tree nor the signature of its commit.
func (b *Backend) Log(ctx context.Context, q git.LogQuery) iter.Seq2[*git.CommitData, error] {
	return func(yield func(*git.CommitData, error) bool) {
		var window []git.ObjectID
		flush := func() bool {
			results := make([]*git.CommitData, len(window))
			errs := make([]error, len(window))
			var wg sync.WaitGroup
			for i, id := range window {
				wg.Add(1)
				go func() {
					defer wg.Done()
					results[i], errs[i] = b.ReadCommit(ctx, id)
				}()
			}
			wg.Wait()
			window = window[:0]
			for i := range results {
				if errs[i] != nil {
					yield(nil, errs[i])
					return false
				}
				if !yield(results[i], nil) {
					return false
				}
			}
			return true
		}
		for e, err := range b.logEntries(ctx, q) {
			if err != nil {
				yield(nil, err)
				return
			}
			id, err := idFromWire(e.GetGit())
			if err != nil {
				yield(nil, err)
				return
			}
			b.facts.putCommit(id)
			window = append(window, id)
			if len(window) >= logPrefetch && !flush() {
				return
			}
		}
		if len(window) > 0 {
			flush()
		}
	}
}

// MergeBase returns the best common ancestor of a and c.
func (b *Backend) MergeBase(ctx context.Context, a, c git.ObjectID) (git.ObjectID, error) {
	if err := errors.Join(requireSha1(a), requireSha1(c)); err != nil {
		return nil, err
	}
	ctx, cancel, err := b.unary(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	resp, err := b.api.MergeBase(ctx, &ascv1.MergeBaseRequest{Repo: b.gitRef(), A: a.RawValue(), B: [][]byte{c.RawValue()}})
	if err != nil {
		return nil, callErr("MergeBase", a.String(), err)
	}
	if len(resp.GetBases()) == 0 {
		return nil, git.ErrNoMergebaseFound
	}
	return idFromWire(resp.GetBases()[0])
}

// IsAncestor reports whether ancestor is reachable from desc. A commit is its own ancestor.
func (b *Backend) IsAncestor(ctx context.Context, ancestor, desc git.ObjectID) (bool, error) {
	if err := errors.Join(requireSha1(ancestor), requireSha1(desc)); err != nil {
		return false, err
	}
	ctx, cancel, err := b.unary(ctx)
	if err != nil {
		return false, err
	}
	defer cancel()
	resp, err := b.api.IsAncestor(ctx, &ascv1.IsAncestorRequest{Repo: b.gitRef(), Ancestor: ancestor.RawValue(), Descendant: desc.RawValue()})
	if err != nil {
		return false, callErr("IsAncestor", ancestor.String(), err)
	}
	return resp.GetResult(), nil
}

// LastCommits finds the newest commit that touched each name in dir, at or below the commit at.
// A name that is not in dir at that commit is not in the result.
func (b *Backend) LastCommits(ctx context.Context, at git.ObjectID, dir string, names []string) (map[string]git.ObjectID, error) {
	if err := requireSha1(at); err != nil {
		return nil, err
	}
	if len(names) > maxLastCommitNames {
		return nil, util.NewInvalidArgumentErrorf("last commits asked for %d names, the limit is %d", len(names), maxLastCommitNames)
	}
	if len(names) == 0 {
		return map[string]git.ObjectID{}, nil
	}
	asked := make(map[string]bool, len(names))
	wire := make([][]byte, len(names))
	for i, n := range names {
		if n == "" || strings.ContainsAny(n, "/\x00") {
			return nil, util.NewInvalidArgumentErrorf("last commits asked for an invalid name")
		}
		asked[n] = true
		wire[i] = []byte(n)
	}
	ctx, cancel, err := b.unary(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	resp, err := b.api.LastCommits(ctx, &ascv1.LastCommitsRequest{
		Repo: b.gitRef(), Rev: at.String(), Dir: []byte(strings.Trim(dir, "/")), Names: wire,
	})
	if err != nil {
		return nil, callErr("LastCommits", at.String(), err)
	}
	res := make(map[string]git.ObjectID, len(resp.GetByName()))
	for name, entry := range resp.GetByName() {
		if !asked[name] {
			return nil, badAnswer("LastCommits answered for a name that was not asked")
		}
		id, err := idFromWire(entry.GetGit())
		if err != nil {
			return nil, err
		}
		res[name] = id
		b.facts.putCommit(id)
	}
	return res, nil
}

// splitLines cuts file content into lines the way git blame counts them: a final newline does not
// start another line.
func splitLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	data = bytes.TrimSuffix(data, []byte{'\n'})
	parts := bytes.Split(data, []byte{'\n'})
	lines := make([]string, len(parts))
	for i, p := range parts {
		lines[i] = string(p)
	}
	return lines
}

// readFileAt reads the file at rev:path in full, up to limit bytes.
func (b *Backend) readFileAt(ctx context.Context, rev, path string, limit int64) ([]byte, error) {
	ctx, cancel, err := b.bulk(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	st, err := b.api.ReadFile(ctx, &ascv1.ReadFileRequest{Repo: b.gitRef(), At: &ascv1.ReadFileRequest_RevPath{RevPath: rev + ":" + path}})
	if err != nil {
		return nil, callErr("ReadFile", rev, err)
	}
	var data []byte
	for {
		msg, err := st.Recv()
		if errors.Is(err, io.EOF) {
			return data, nil
		}
		if err != nil {
			return nil, callErr("ReadFile", rev, err)
		}
		if msg.GetTotalSize() > uint64(limit) {
			return nil, fmt.Errorf("%w: file of %d bytes", ErrTooLarge, msg.GetTotalSize())
		}
		if int64(len(data))+int64(len(msg.GetData())) > limit {
			return nil, badAnswer("file is longer than announced")
		}
		data = append(data, msg.GetData()...)
	}
}

// Blame yields the blame of path at commit at, one run of lines per part. .git-blame-ignore-revs is
// not honoured, and PreviousPath is the path in the origin commit.
func (b *Backend) Blame(ctx context.Context, at git.ObjectID, path string, _ git.BlameOpts) iter.Seq2[git.BlamePart, error] {
	return func(yield func(git.BlamePart, error) bool) {
		parts, err := b.blameParts(ctx, at, path)
		if err != nil {
			yield(git.BlamePart{}, err)
			return
		}
		for _, p := range parts {
			if !yield(p, nil) {
				return
			}
		}
	}
}

func (b *Backend) blameParts(ctx context.Context, at git.ObjectID, path string) ([]git.BlamePart, error) {
	if err := requireSha1(at); err != nil {
		return nil, err
	}
	if path == "" || len(path) > maxPathBytes || strings.IndexByte(path, 0) >= 0 {
		return nil, util.NewInvalidArgumentErrorf("blame needs a file path")
	}
	hunks, commits, err := b.blameHunks(ctx, at, path)
	if err != nil {
		return nil, err
	}
	data, err := b.readFileAt(ctx, at.String(), path, maxBlameFileBytes)
	if err != nil {
		return nil, err
	}
	lines := splitLines(data)

	parts := make([]git.BlamePart, 0, len(hunks))
	described := map[string]bool{}
	previousChecks := 0
	next := 0
	for _, h := range hunks {
		if int(h.GetStart()) != next || h.GetLen() == 0 || next+int(h.GetLen()) > len(lines) {
			return nil, badAnswer("blame runs do not cover the file")
		}
		origin, err := idFromWire(h.GetOrigin())
		if err != nil {
			return nil, err
		}
		part := git.BlamePart{Sha: origin.String(), Lines: lines[next : next+int(h.GetLen())]}
		// git blame --porcelain describes a commit (and its previous version) at its first run only.
		entry, known := commits[origin.String()]
		first := !described[origin.String()]
		described[origin.String()] = true
		if known && first && len(entry.GetParents()) > 0 {
			parent, err := idFromWire(entry.GetParents()[0])
			if err != nil {
				return nil, err
			}
			// "previous" names the parent only when the file existed there.
			if previousChecks++; previousChecks <= maxBlamePreviousChecks {
				ok, err := b.fileExists(ctx, parent, string(h.GetOrigPath()))
				if err != nil {
					return nil, err
				}
				if ok {
					part.PreviousSha = parent.String()
					part.PreviousPath = string(h.GetOrigPath())
				}
			}
		}
		parts = append(parts, part)
		next += int(h.GetLen())
	}
	if next != len(lines) {
		return nil, badAnswer("blame covers %d of %d lines", next, len(lines))
	}
	return parts, nil
}

// blameHunks collects the runs and the commits they name.
func (b *Backend) blameHunks(ctx context.Context, at git.ObjectID, path string) ([]*ascv1.BlameHunk, map[string]*ascv1.LogEntry, error) {
	ctx, cancel, err := b.stream(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer cancel()
	st, err := b.api.Blame(ctx, &ascv1.BlameRequest{Repo: b.gitRef(), Rev: at.String(), Path: []byte(path)})
	if err != nil {
		return nil, nil, callErr("Blame", at.String(), err)
	}
	var hunks []*ascv1.BlameHunk
	commits := map[string]*ascv1.LogEntry{}
	for {
		msg, err := st.Recv()
		if errors.Is(err, io.EOF) {
			return hunks, commits, nil
		}
		if err != nil {
			return nil, nil, callErr("Blame", at.String(), err)
		}
		if len(hunks)+len(msg.GetHunks()) > maxBlameParts {
			return nil, nil, fmt.Errorf("%w: more than %d blame runs", ErrTooLarge, maxBlameParts)
		}
		hunks = append(hunks, msg.GetHunks()...)
		for _, e := range msg.GetCommits() {
			commits[hex.EncodeToString(e.GetGit())] = e
		}
	}
}

// fileExists reports whether a file (not a directory) exists at path in the commit rev.
func (b *Backend) fileExists(ctx context.Context, rev git.ObjectID, path string) (bool, error) {
	if path == "" || len(path) > maxPathBytes || strings.IndexByte(path, 0) >= 0 {
		return false, nil
	}
	ctx, cancel, err := b.unary(ctx)
	if err != nil {
		return false, err
	}
	defer cancel()
	resp, err := b.api.Stat(ctx, &ascv1.StatRequest{Repo: b.gitRef(), Rev: rev.String(), Paths: [][]byte{[]byte(path)}})
	if err != nil {
		if isMissing(err) {
			return false, nil
		}
		return false, callErr("Stat", rev.String(), err)
	}
	if len(resp.GetExists()) != 1 || len(resp.GetEntries()) != 1 {
		return false, badAnswer("Stat answered %d results for one path", len(resp.GetExists()))
	}
	return resp.GetExists()[0] && git.EntryMode(resp.GetEntries()[0].GetMode()) != modeTree, nil
}
