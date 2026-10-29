// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package ascbackend

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"iter"
	"slices"
	"strings"
	"time"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git"
	"forgejo.org/modules/log"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

// sigFromWire converts a signature of the wire.
func sigFromWire(s *ascv1.Signature) (*git.Signature, error) {
	if s == nil {
		return nil, badAnswer("commit without a signature")
	}
	if s.GetTzMin() < -24*60 || s.GetTzMin() > 24*60 {
		return nil, badAnswer("time zone offset of %d minutes", s.GetTzMin())
	}
	return &git.Signature{
		Name:  string(s.GetName()),
		Email: string(s.GetEmail()),
		When:  time.Unix(s.GetTime(), 0).In(time.FixedZone("", int(s.GetTzMin())*60)),
	}, nil
}

// parseCommit parses a raw commit. The parser panics on a malformed id header, which a pushed object can contain.
func parseCommit(repo *git.Repository, id git.ObjectID, raw []byte) (data *git.CommitData, err error) {
	defer func() {
		if r := recover(); r != nil {
			data, err = nil, badAnswer("malformed commit %s: %v", id, r)
		}
	}()
	c, err := git.CommitFromReader(repo, id, bytes.NewReader(raw))
	if err != nil {
		return nil, badAnswer("commit %s does not parse: %v", id, err)
	}
	if c.Tree.ID == nil {
		return nil, badAnswer("commit %s has no tree", id)
	}
	return &git.CommitData{
		ID:        c.ID,
		Tree:      c.Tree.ID,
		Parents:   c.Parents,
		Author:    c.Author,
		Committer: c.Committer,
		Message:   c.CommitMessage,
		Signature: c.Signature,
	}, nil
}

// objectHash is the git object id of content of the given type.
func objectHash(typ string, content []byte) []byte {
	h := sha1.New()
	_, _ = fmt.Fprintf(h, "%s %d\x00", typ, len(content))
	_, _ = h.Write(content)
	return h.Sum(nil)
}

// ReadCommit reads a commit through ReadChangeset. The raw commit (when the server has it) is checked
// against the id it was asked for, and the header is checked against the parsed commit.
func (b *Backend) ReadCommit(ctx context.Context, id git.ObjectID) (*git.CommitData, error) {
	if err := requireSha1(id); err != nil {
		return nil, err
	}
	if data, ok := b.commits.get(id); ok {
		return data, nil
	}
	ctx, cancel, err := b.unary(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	resp, err := b.api.ReadChangeset(ctx, &ascv1.ReadChangesetRequest{Repo: b.gitRef(), Rev: id.String()})
	if err != nil {
		return nil, callErr("ReadChangeset", id.String(), err)
	}
	data, err := b.commitFromChangeset(id, resp)
	if err != nil {
		return nil, err
	}
	b.commits.put(id, data)
	b.facts.putCommit(id)
	b.facts.putTree(data.Tree, treeLoc{rev: id.String()})
	return data, nil
}

func (b *Backend) commitFromChangeset(id git.ObjectID, resp *ascv1.ReadChangesetResponse) (*git.CommitData, error) {
	h := resp.GetHeader()
	if h == nil {
		return nil, badAnswer("ReadChangeset without a header")
	}
	headerID, err := idFromWire(h.GetGit())
	if err != nil {
		return nil, err
	}
	if headerID.String() != id.String() {
		return nil, badAnswer("ReadChangeset answered for a different commit")
	}
	rootGit, err := idFromWire(resp.GetRootGit())
	if err != nil {
		return nil, err
	}
	parents, err := idsFromWire(h.GetParents())
	if err != nil {
		return nil, err
	}

	raw := resp.GetRawGitCommit()
	if len(raw) > 0 {
		if len(raw) > maxRawCommitBytes {
			return nil, fmt.Errorf("%w: raw commit of %d bytes", ErrTooLarge, len(raw))
		}
		if !bytes.Equal(objectHash("commit", raw), id.RawValue()) {
			return nil, badAnswer("raw commit does not hash to %s", id)
		}
		data, err := parseCommit(b.repo, id, raw)
		if err != nil {
			return nil, err
		}
		if data.Tree.String() != rootGit.String() || !sameIDs(data.Parents, parents) {
			return nil, badAnswer("raw commit %s disagrees with its header", id)
		}
		return data, nil
	}

	author, err := sigFromWire(h.GetAuthor())
	if err != nil {
		return nil, err
	}
	committer, err := sigFromWire(h.GetCommitter())
	if err != nil {
		return nil, err
	}
	return &git.CommitData{
		ID:        id,
		Tree:      rootGit,
		Parents:   parents,
		Author:    author,
		Committer: committer,
		Message:   string(h.GetMessage()),
	}, nil
}

func sameIDs(a, b []git.ObjectID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].String() != b[i].String() {
			return false
		}
	}
	return true
}

// ReadTag is not served: the asc.v1.Repo surface has no read of an annotated tag object by git id.
func (b *Backend) ReadTag(_ context.Context, id git.ObjectID) (*git.TagData, error) {
	return nil, fmt.Errorf("%w: annotated tag object %s", ErrUnsupported, id)
}

// ---------------------------------------------------------------------------------------- trees

const (
	modeBlob    = git.EntryModeBlob
	modeExec    = git.EntryModeExec
	modeSymlink = git.EntryModeSymlink
	modeCommit  = git.EntryModeCommit
	modeTree    = git.EntryModeTree
)

// wireEntry is one checked entry of a listing. rel is the path below the listed directory.
type wireEntry struct {
	name string
	rel  string
	mode git.EntryMode
	id   git.ObjectID
	size int64
}

func (e wireEntry) isDir() bool { return e.mode == modeTree }

// gitOrderKey is the key trees are sorted by: a directory sorts as if its name ended in a slash.
func (e wireEntry) gitOrderKey() string {
	if e.isDir() {
		return e.rel + "/"
	}
	return e.rel
}

func sortGitOrder(entries []wireEntry) {
	slices.SortStableFunc(entries, func(a, b wireEntry) int { return strings.Compare(a.gitOrderKey(), b.gitOrderKey()) })
}

func joinPath(dir, name string) string {
	if dir == "" {
		return name
	}
	return dir + "/" + name
}

// listTree lists the directory at dir below the commit rev. When want is set, the answer must be the
// listing of that tree id. The returned id is the id of the listed tree. Entries are in the server's order.
func (b *Backend) listTree(ctx context.Context, rev, dir string, recursive bool, want git.ObjectID) (git.ObjectID, []wireEntry, error) {
	ctx, cancel, err := b.stream(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer cancel()
	st, err := b.api.ReadTree(ctx, &ascv1.ReadTreeRequest{Repo: b.gitRef(), Rev: rev, Path: []byte(dir), Recursive: recursive})
	if err != nil {
		return nil, nil, callErr("ReadTree", rev, err)
	}
	var (
		treeID  git.ObjectID
		entries []wireEntry
		prefix  = dir + "/"
	)
	for {
		msg, err := st.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, callErr("ReadTree", rev, err)
		}
		id, err := idFromWire(msg.GetTreeGit())
		if err != nil {
			return nil, nil, err
		}
		if treeID == nil {
			treeID = id
		} else if treeID.String() != id.String() {
			return nil, nil, badAnswer("ReadTree changed tree id mid-stream")
		}
		for _, e := range msg.GetEntries() {
			if len(entries) >= maxTreeEntries {
				return nil, nil, fmt.Errorf("%w: more than %d tree entries", ErrTooLarge, maxTreeEntries)
			}
			entry, err := checkEntry(e, dir, prefix, recursive)
			if err != nil {
				return nil, nil, err
			}
			entries = append(entries, entry)
		}
	}
	if treeID == nil {
		return nil, nil, badAnswer("ReadTree sent no tree id")
	}
	if want != nil && want.String() != treeID.String() {
		return nil, nil, badAnswer("listing of %s is the listing of %s", want, treeID)
	}
	return treeID, entries, nil
}

// checkEntry validates one entry of a ReadTree answer.
func checkEntry(e *ascv1.TreeEntryInfo, dir, prefix string, recursive bool) (wireEntry, error) {
	if !validEntryName(e.GetName()) {
		return wireEntry{}, badAnswer("tree entry with an invalid name")
	}
	id, err := idFromWire(e.GetGit())
	if err != nil {
		return wireEntry{}, err
	}
	mode := git.EntryMode(e.GetMode())
	switch mode {
	case modeBlob, modeExec, modeSymlink, modeCommit, modeTree:
	default:
		return wireEntry{}, badAnswer("tree entry with mode %o", e.GetMode())
	}
	if e.GetSize() > 1<<62 {
		return wireEntry{}, badAnswer("tree entry with size %d", e.GetSize())
	}
	path := string(e.GetPath())
	rel := string(e.GetName())
	if recursive || len(path) > 0 {
		rel = path
		if dir != "" {
			var ok bool
			if rel, ok = strings.CutPrefix(path, prefix); !ok {
				return wireEntry{}, badAnswer("tree entry outside the listed directory")
			}
		}
		if rel == "" || len(rel) > maxPathBytes || strings.IndexByte(rel, 0) >= 0 || !strings.HasSuffix(rel, string(e.GetName())) {
			return wireEntry{}, badAnswer("tree entry with an inconsistent path")
		}
		if !recursive && strings.Contains(rel, "/") {
			return wireEntry{}, badAnswer("tree entry below the listed directory")
		}
	}
	return wireEntry{name: string(e.GetName()), rel: rel, mode: mode, id: id, size: int64(e.GetSize())}, nil
}

// remember records what a verified listing says about the objects in it.
func (b *Backend) remember(rev, dir string, entries []wireEntry) {
	for _, e := range entries {
		switch e.mode {
		case modeTree:
			b.facts.putTree(e.id, treeLoc{rev: rev, path: joinPath(dir, e.rel)})
		case modeCommit:
			// A submodule commit is not an object of this repository.
		default:
			b.facts.putBlob(e.id, e.size)
		}
	}
}

// locate says where the tree (or commit) id can be listed.
func (b *Backend) locate(ctx context.Context, id git.ObjectID) (treeLoc, error) {
	if f, ok := b.facts.get(id); ok {
		switch {
		case f.typ == git.ObjectCommit:
			return treeLoc{rev: id.String()}, nil
		case f.typ == git.ObjectTree && f.loc != nil:
			return *f.loc, nil
		case f.typ == git.ObjectBlob:
			return treeLoc{}, git.ErrNotExist{ID: id.String()}
		}
	}
	ok, err := b.exists(ctx, ascv1.ObjKind_OBJ_KIND_CHANGESET, id)
	if err != nil {
		return treeLoc{}, err
	}
	if ok {
		b.facts.putCommit(id)
		return treeLoc{rev: id.String()}, nil
	}
	log.Debug("ascbackend: tree %s of %s has no known location", id, b.name)
	return treeLoc{}, git.ErrNotExist{ID: id.String()}
}

// wantTree is the tree id a listing of id must have, when this Backend already knows it.
func (b *Backend) wantTree(id git.ObjectID) git.ObjectID {
	f, ok := b.facts.get(id)
	if !ok {
		return nil
	}
	switch f.typ {
	case git.ObjectTree:
		return id
	case git.ObjectCommit:
		if data, ok := b.commits.get(id); ok {
			return data.Tree
		}
	}
	return nil
}

// ReadTree lists one tree level. A commit id lists its root tree.
func (b *Backend) ReadTree(ctx context.Context, id git.ObjectID) ([]git.TreeEntryData, error) {
	if err := requireSha1(id); err != nil {
		return nil, err
	}
	if isEmptyTree(id) {
		return []git.TreeEntryData{}, nil
	}
	loc, err := b.locate(ctx, id)
	if err != nil {
		return nil, err
	}
	treeID, entries, err := b.listTree(ctx, loc.rev, loc.path, false, b.wantTree(id))
	if err != nil {
		return nil, err
	}
	b.facts.putTree(treeID, loc)
	b.remember(loc.rev, loc.path, entries)
	sortGitOrder(entries)
	out := make([]git.TreeEntryData, len(entries))
	for i, e := range entries {
		out[i] = git.TreeEntryData{ID: e.id, Name: e.name, Mode: e.mode, Size: entrySize(e)}
	}
	return out, nil
}

// isEmptyTree reports whether id is the empty tree, which is empty by definition.
func isEmptyTree(id git.ObjectID) bool {
	return id.String() == git.Sha1ObjectFormat.EmptyTree().String()
}

func entrySize(e wireEntry) int64 {
	if e.mode == modeTree || e.mode == modeCommit {
		return 0
	}
	return e.size
}

// ReadTreeRecursive walks a tree and all its subtrees. Without opt.Trees one RPC lists the leaves; with it
// the walk opens each directory in turn, because only a level-by-level listing carries directory ids.
func (b *Backend) ReadTreeRecursive(ctx context.Context, id git.ObjectID, opt git.TreeWalkOpts) iter.Seq2[git.TreeEntryData, error] {
	return func(yield func(git.TreeEntryData, error) bool) {
		if err := requireSha1(id); err != nil {
			yield(git.TreeEntryData{}, err)
			return
		}
		if isEmptyTree(id) {
			return
		}
		loc, err := b.locate(ctx, id)
		if err != nil {
			yield(git.TreeEntryData{}, err)
			return
		}
		want := b.wantTree(id)
		if !opt.Trees {
			treeID, entries, err := b.listTree(ctx, loc.rev, loc.path, true, want)
			if err != nil {
				yield(git.TreeEntryData{}, err)
				return
			}
			b.facts.putTree(treeID, loc)
			b.remember(loc.rev, loc.path, entries)
			sortGitOrder(entries)
			for _, e := range entries {
				if e.isDir() {
					yield(git.TreeEntryData{}, badAnswer("recursive listing contains a directory"))
					return
				}
				if !yield(git.TreeEntryData{ID: e.id, Name: e.rel, Mode: e.mode, Size: entrySize(e)}, nil) {
					return
				}
			}
			return
		}
		b.walkWithTrees(ctx, loc, want, yield)
	}
}

type walkFrame struct {
	dir     string // path below the commit
	rel     string // path below the walked tree
	entries []wireEntry
	next    int
}

func (b *Backend) walkWithTrees(ctx context.Context, loc treeLoc, want git.ObjectID, yield func(git.TreeEntryData, error) bool) {
	open := func(dir, rel string, want git.ObjectID) (*walkFrame, error) {
		treeID, entries, err := b.listTree(ctx, loc.rev, dir, false, want)
		if err != nil {
			return nil, err
		}
		b.facts.putTree(treeID, treeLoc{rev: loc.rev, path: dir})
		b.remember(loc.rev, dir, entries)
		sortGitOrder(entries)
		return &walkFrame{dir: dir, rel: rel, entries: entries}, nil
	}
	root, err := open(loc.path, "", want)
	if err != nil {
		yield(git.TreeEntryData{}, err)
		return
	}
	stack := []*walkFrame{root}
	total, dirs := 0, 1
	for len(stack) > 0 {
		top := stack[len(stack)-1]
		if top.next >= len(top.entries) {
			stack = stack[:len(stack)-1]
			continue
		}
		e := top.entries[top.next]
		top.next++
		if total++; total > maxTreeEntries {
			yield(git.TreeEntryData{}, fmt.Errorf("%w: more than %d tree entries", ErrTooLarge, maxTreeEntries))
			return
		}
		rel := joinPath(top.rel, e.name)
		if !yield(git.TreeEntryData{ID: e.id, Name: rel, Mode: e.mode, Size: entrySize(e)}, nil) {
			return
		}
		if !e.isDir() {
			continue
		}
		if len(stack) >= maxTreeDepth || dirs >= maxTreeDirs {
			yield(git.TreeEntryData{}, fmt.Errorf("%w: tree is nested too deeply or has too many directories", ErrTooLarge))
			return
		}
		dirs++
		child, err := open(joinPath(top.dir, e.name), rel, e.id)
		if err != nil {
			yield(git.TreeEntryData{}, err)
			return
		}
		stack = append(stack, child)
	}
}

// ---------------------------------------------------------------------------------------- blobs

// blobReader streams a blob from a ReadFile stream. It checks the sizes the server announces, never
// delivers more than was asked, and, when the whole blob is read, that the bytes hash to the blob id.
type blobReader struct {
	stream grpc.ServerStreamingClient[ascv1.ReadFileResponse]
	cancel context.CancelFunc
	buf    []byte
	size   int64 // full size of the blob
	want   int64 // bytes this reader delivers
	got    int64
	sum    hash.Hash // nil when the read is truncated
	id     git.ObjectID
	err    error
}

func (r *blobReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for len(r.buf) == 0 {
		if r.err != nil {
			return 0, r.err
		}
		if r.got >= r.want {
			r.err = r.finish()
			continue
		}
		msg, err := r.stream.Recv()
		switch {
		case errors.Is(err, io.EOF):
			r.err = badAnswer("blob %s ended after %d of %d bytes", r.id, r.got, r.want)
		case err != nil:
			r.err = callErr("ReadFile", r.id.String(), err)
		default:
			r.err = r.accept(msg)
		}
	}
	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

// accept takes one message; its data becomes the pending buffer.
func (r *blobReader) accept(msg *ascv1.ReadFileResponse) error {
	if int64(msg.GetTotalSize()) != r.size {
		return badAnswer("blob %s changed size mid-stream", r.id)
	}
	data := msg.GetData()
	if int64(len(data)) > r.want-r.got {
		return badAnswer("blob %s: more data than asked for", r.id)
	}
	r.got += int64(len(data))
	if r.sum != nil {
		_, _ = r.sum.Write(data)
	}
	r.buf = data
	return nil
}

// finish runs when all wanted bytes were delivered: the server must have nothing more to say.
func (r *blobReader) finish() error {
	if r.sum != nil && !bytes.Equal(r.sum.Sum(nil), r.id.RawValue()) {
		return badAnswer("blob %s does not hash to its id", r.id)
	}
	return io.EOF
}

func (r *blobReader) Close() error {
	r.cancel()
	return nil
}

// OpenBlob opens a blob by id. A non-negative limit truncates the content to limit bytes; the size
// returned is still the full size.
func (b *Backend) OpenBlob(ctx context.Context, id git.ObjectID, limit int64) (io.ReadCloser, int64, error) {
	if err := requireSha1(id); err != nil {
		return nil, 0, err
	}
	ctx, cancel, err := b.bulk(ctx)
	if err != nil {
		return nil, 0, err
	}
	req := &ascv1.ReadFileRequest{
		Repo: b.gitRef(),
		At:   &ascv1.ReadFileRequest_FileId{FileId: id.RawValue()},
	}
	switch {
	case limit == 0:
		req.Length = 1 // a length of zero means "to the end"; one byte is enough to learn the size
	case limit > 0:
		req.Length = uint64(limit)
	}
	st, err := b.api.ReadFile(ctx, req)
	if err != nil {
		cancel()
		return nil, 0, callErr("ReadFile", id.String(), err)
	}
	first, err := st.Recv()
	if err != nil {
		cancel()
		if errors.Is(err, io.EOF) {
			return nil, 0, badAnswer("ReadFile sent nothing")
		}
		return nil, 0, callErr("ReadFile", id.String(), err)
	}
	total := first.GetTotalSize()
	if total > 1<<62 {
		cancel()
		return nil, 0, badAnswer("blob size %d", total)
	}
	size := int64(total)
	want := size
	if limit >= 0 && limit < size {
		want = limit
	}
	r := &blobReader{stream: st, cancel: cancel, size: size, want: want, id: id}
	if want == size {
		r.sum = sha1.New()
		_, _ = fmt.Fprintf(r.sum, "blob %d\x00", size)
	}
	// With nothing to deliver the first message is only read for the size; an empty blob still
	// has its hash checked when the caller reads to the end.
	if want > 0 {
		if err := r.accept(first); err != nil {
			cancel()
			return nil, 0, err
		}
	}
	b.facts.putBlob(id, size)
	return r, size, nil
}

// ---------------------------------------------------------------------------------------- stat and resolve

// exists reports whether one object of kind and git id exists. An id the server does not know is false.
func (b *Backend) exists(ctx context.Context, kind ascv1.ObjKind, id git.ObjectID) (bool, error) {
	ctx, cancel, err := b.unary(ctx)
	if err != nil {
		return false, err
	}
	defer cancel()
	resp, err := b.api.Stat(ctx, &ascv1.StatRequest{
		Repo:    b.gitRef(),
		Objects: []*ascv1.ObjKey{{Kind: kind, Id: id.RawValue()}},
	})
	if err != nil {
		if isMissing(err) {
			return false, nil
		}
		return false, callErr("Stat", id.String(), err)
	}
	if len(resp.GetExists()) != 1 {
		return false, badAnswer("Stat answered %d results for one object", len(resp.GetExists()))
	}
	return resp.GetExists()[0], nil
}

// blobSize learns the size of a blob from a one-byte read.
func (b *Backend) blobSize(ctx context.Context, id git.ObjectID) (int64, error) {
	ctx, cancel, err := b.stream(ctx)
	if err != nil {
		return 0, err
	}
	defer cancel()
	st, err := b.api.ReadFile(ctx, &ascv1.ReadFileRequest{
		Repo:   b.gitRef(),
		At:     &ascv1.ReadFileRequest_FileId{FileId: id.RawValue()},
		Length: 1,
	})
	if err != nil {
		return 0, callErr("ReadFile", id.String(), err)
	}
	msg, err := st.Recv()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return 0, badAnswer("ReadFile sent nothing")
		}
		return 0, callErr("ReadFile", id.String(), err)
	}
	if msg.GetTotalSize() > 1<<62 {
		return 0, badAnswer("blob size %d", msg.GetTotalSize())
	}
	return int64(msg.GetTotalSize()), nil
}

// StatObjects reports type and size for each id. Only blobs have a size; a commit or tree reports 0.
// An annotated tag id is reported missing, because the server cannot read it.
func (b *Backend) StatObjects(ctx context.Context, ids []git.ObjectID) ([]git.ObjectInfo, error) {
	if len(ids) > maxQueryItems*16 {
		return nil, fmt.Errorf("%w: %d objects to stat", ErrTooLarge, len(ids))
	}
	infos := make([]git.ObjectInfo, 0, len(ids))
	for _, id := range ids {
		info, err := b.statOne(ctx, id)
		if err != nil {
			return nil, err
		}
		infos = append(infos, info)
	}
	return infos, nil
}

func (b *Backend) statOne(ctx context.Context, id git.ObjectID) (git.ObjectInfo, error) {
	if err := requireSha1(id); err != nil {
		return git.ObjectInfo{}, err
	}
	if f, ok := b.facts.get(id); ok {
		switch {
		case f.typ == git.ObjectBlob && f.size >= 0:
			return git.ObjectInfo{ID: id, Type: git.ObjectBlob, Size: f.size}, nil
		case f.typ == git.ObjectTree || f.typ == git.ObjectCommit:
			return git.ObjectInfo{ID: id, Type: f.typ}, nil
		}
	}
	probes := []struct {
		kind ascv1.ObjKind
		typ  git.ObjectType
	}{
		{ascv1.ObjKind_OBJ_KIND_CHANGESET, git.ObjectCommit},
		{ascv1.ObjKind_OBJ_KIND_FILE, git.ObjectBlob},
		{ascv1.ObjKind_OBJ_KIND_TREE, git.ObjectTree},
	}
	for _, p := range probes {
		ok, err := b.exists(ctx, p.kind, id)
		if err != nil {
			return git.ObjectInfo{}, err
		}
		if !ok {
			continue
		}
		info := git.ObjectInfo{ID: id, Type: p.typ}
		switch p.typ {
		case git.ObjectBlob:
			size, err := b.blobSize(ctx, id)
			if err != nil {
				return git.ObjectInfo{}, err
			}
			info.Size = size
			b.facts.putBlob(id, size)
		case git.ObjectCommit:
			b.facts.putCommit(id)
		default:
			b.facts.update(id, git.ObjectTree, -1, nil)
		}
		return info, nil
	}
	return git.ObjectInfo{ID: id, Missing: true}, nil
}

// isFullHexID reports whether s is a 40-digit hex id.
func isFullHexID(s string) bool {
	if len(s) != 40 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// abbrevScanLimit bounds the commits read to expand an abbreviated id.
const abbrevScanLimit = 5000

// isShortHexID reports whether s looks like an abbreviated commit id: 4 to 39 hex digits.
func isShortHexID(s string) bool {
	if len(s) < 4 || len(s) >= 40 {
		return false
	}
	_, err := hex.DecodeString(s + strings.Repeat("0", len(s)%2))
	return err == nil
}

// resolveAbbrev expands an abbreviated commit id. The asc server resolves revisions and full ids, not
// abbreviations, so the commits reachable from every reference are scanned (bounded) for the one id with this
// prefix. An abbreviation that matches no commit or more than one is a missing revision, as with git.
func (b *Backend) resolveAbbrev(ctx context.Context, short string) (git.ObjectID, git.ObjectType, error) {
	prefix := strings.ToLower(short)
	var found git.ObjectID
	for e, err := range b.logEntries(ctx, git.LogQuery{All: true, Limit: abbrevScanLimit}) {
		if err != nil {
			return nil, "", err
		}
		id, err := idFromWire(e.GetGit())
		if err != nil {
			return nil, "", err
		}
		if !strings.HasPrefix(id.String(), prefix) {
			continue
		}
		if found != nil && found.String() != id.String() {
			return nil, "", git.ErrNotExist{ID: short}
		}
		found = id
	}
	if found == nil {
		return nil, "", git.ErrNotExist{ID: short}
	}
	b.facts.putCommit(found)
	return found, git.ObjectCommit, nil
}

// Resolve resolves a ref, a full id, "id^" or "id~n" to a commit id. A tag is peeled. A revision the
// server refuses (an abbreviated id, a message search) is reported as not existing.
func (b *Backend) Resolve(ctx context.Context, rev string) (git.ObjectID, git.ObjectType, error) {
	if !validRev(rev) {
		return nil, "", git.ErrNotExist{ID: rev}
	}
	if isFullHexID(rev) {
		id, err := git.NewIDFromString(rev)
		if err != nil {
			return nil, "", git.ErrNotExist{ID: rev}
		}
		if f, ok := b.facts.get(id); ok && f.typ != "" {
			return id, f.typ, nil
		}
	}
	ctx, cancel, err := b.unary(ctx)
	if err != nil {
		return nil, "", err
	}
	defer cancel()
	resp, err := b.api.Resolve(ctx, &ascv1.ResolveRequest{Repo: b.gitRef(), Revspecs: []string{rev}})
	if err != nil {
		if isCode(err, codes.InvalidArgument) {
			log.Debug("ascbackend: revision refused by asc serve: %v", err)
			if isShortHexID(rev) {
				return b.resolveAbbrev(ctx, rev)
			}
			return nil, "", git.ErrNotExist{ID: rev}
		}
		if isMissing(err) && isFullHexID(rev) {
			return b.resolveObject(ctx, rev)
		}
		return nil, "", callErr("Resolve", rev, err)
	}
	if len(resp.GetIds()) != 1 {
		return nil, "", badAnswer("Resolve answered %d ids for one revision", len(resp.GetIds()))
	}
	if len(resp.GetIds()[0]) == 0 {
		if isFullHexID(rev) {
			return b.resolveObject(ctx, rev)
		}
		if isShortHexID(rev) {
			return b.resolveAbbrev(ctx, rev)
		}
		return nil, "", git.ErrNotExist{ID: rev}
	}
	id, err := idFromWire(resp.GetIds()[0])
	if err != nil {
		return nil, "", err
	}
	b.facts.putCommit(id)
	return id, git.ObjectCommit, nil
}

// resolveObject finds a full id that is not a commit: a tree or a blob.
func (b *Backend) resolveObject(ctx context.Context, rev string) (git.ObjectID, git.ObjectType, error) {
	id, err := git.NewIDFromString(rev)
	if err != nil {
		return nil, "", git.ErrNotExist{ID: rev}
	}
	info, err := b.statOne(ctx, id)
	if err != nil {
		return nil, "", err
	}
	if info.Missing {
		return nil, "", git.ErrNotExist{ID: rev}
	}
	return id, info.Type, nil
}
