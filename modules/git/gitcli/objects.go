// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package gitcli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"strconv"
	"strings"

	"forgejo.org/modules/git"
	"forgejo.org/modules/log"
)

// session is a borrowed cat-file --batch process. release must be called once the reply is fully consumed.
type session struct {
	wr      git.WriteCloserError
	rd      *bufio.Reader
	release func()
}

// request asks the process for one object.
func (s *session) request(id string) error {
	_, err := s.wr.Write([]byte(id + "\n"))
	return err
}

// open borrows the batch process and requests id from it.
func (b *Backend) open(ctx context.Context, id string) (*session, error) {
	wr, rd, release, err := b.repo.CatFileBatch(b.batchCtx(ctx))
	if err != nil {
		return nil, err
	}
	s := &session{wr: wr, rd: rd, release: release}
	if err := s.request(id); err != nil {
		release()
		return nil, err
	}
	return s, nil
}

// readBody reads the size-byte body of a batch reply and its trailing newline.
// A body above limit is discarded, so the stream stays in step, and reported as an error.
func readBody(rd *bufio.Reader, size, limit int64) ([]byte, error) {
	if size < 0 || size > limit {
		if size >= 0 {
			if err := git.DiscardFull(rd, size+1); err != nil {
				return nil, err
			}
		}
		return nil, fmt.Errorf("gitcli: object of %d bytes exceeds the %d byte limit", size, limit)
	}
	body := make([]byte, size)
	if _, err := io.ReadFull(rd, body); err != nil {
		return nil, err
	}
	if _, err := rd.Discard(1); err != nil {
		return nil, err
	}
	return body, nil
}

func validRev(rev string) bool {
	return rev != "" && !strings.ContainsAny(rev, "\n\x00")
}

// Resolve resolves rev through cat-file --batch-check.
func (b *Backend) Resolve(ctx context.Context, rev string) (git.ObjectID, git.ObjectType, error) {
	if !validRev(rev) {
		return nil, "", git.ErrNotExist{ID: rev}
	}
	wr, rd, cancel, err := b.repo.CatFileBatchCheck(b.batchCtx(ctx))
	if err != nil {
		return nil, "", err
	}
	defer cancel()
	if _, err = wr.Write([]byte(rev + "\n")); err != nil {
		return nil, "", err
	}
	sha, typ, _, err := git.ReadBatchLine(rd)
	if err != nil {
		if git.IsErrNotExist(err) {
			return nil, "", git.ErrNotExist{ID: rev}
		}
		return nil, "", err
	}
	id, err := git.NewIDFromString(string(sha))
	if err != nil {
		return nil, "", err
	}
	return id, git.ObjectType(typ), nil
}

// ReadCommit reads a commit, peeling annotated tags.
func (b *Backend) ReadCommit(ctx context.Context, id git.ObjectID) (*git.CommitData, error) {
	for range maxTagDepth {
		commit, target, err := b.readCommitOrTag(ctx, id)
		if err != nil {
			return nil, err
		}
		if commit != nil {
			return commit, nil
		}
		id = target
	}
	return nil, git.ErrNotExist{ID: id.String()}
}

// readCommitOrTag returns the commit id names, or the object a tag id names.
func (b *Backend) readCommitOrTag(ctx context.Context, id git.ObjectID) (*git.CommitData, git.ObjectID, error) {
	s, err := b.open(ctx, id.String())
	if err != nil {
		return nil, nil, err
	}
	defer s.release()
	rd := s.rd

	_, typ, size, err := git.ReadBatchLine(rd)
	if err != nil {
		if errors.Is(err, io.EOF) || git.IsErrNotExist(err) {
			return nil, nil, git.ErrNotExist{ID: id.String()}
		}
		return nil, nil, err
	}

	switch typ {
	case "tag":
		body, err := readBody(rd, size, maxMetaObjectSize)
		if err != nil {
			return nil, nil, err
		}
		tag, err := git.ParseTagData(id.Type(), body)
		if err != nil {
			return nil, nil, err
		}
		return nil, tag.Object, nil
	case "commit":
		body, err := readBody(rd, size, maxMetaObjectSize)
		if err != nil {
			return nil, nil, err
		}
		commit, err := parseCommit(b.repo, id, body)
		return commit, nil, err
	default:
		log.Debug("Unknown typ: %s", typ)
		if err := git.DiscardFull(rd, size+1); err != nil {
			return nil, nil, err
		}
		return nil, nil, git.ErrNotExist{ID: id.String()}
	}
}

// parseCommit parses a commit object. The parser panics on a malformed id header, which a pushed object can contain.
func parseCommit(repo *git.Repository, id git.ObjectID, body []byte) (data *git.CommitData, err error) {
	defer func() {
		if r := recover(); r != nil {
			data, err = nil, fmt.Errorf("gitcli: malformed commit %s: %v", id, r)
		}
	}()
	c, err := git.CommitFromReader(repo, id, bytes.NewReader(body))
	if err != nil {
		return nil, err
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

// ReadTree lists one tree level. A commit id lists its root tree.
func (b *Backend) ReadTree(ctx context.Context, id git.ObjectID) ([]git.TreeEntryData, error) {
	s, err := b.open(ctx, id.String())
	if err != nil {
		return nil, err
	}
	defer s.release()
	rd := s.rd

	_, typ, size, err := git.ReadBatchLine(rd)
	if err != nil {
		return nil, err
	}
	if typ == "commit" {
		treeID, err := git.ReadTreeID(rd, size)
		if err != nil && err != io.EOF {
			return nil, err
		}
		if err := s.request(treeID); err != nil {
			return nil, err
		}
		_, typ, size, err = git.ReadBatchLine(rd)
		if err != nil {
			return nil, err
		}
	}
	if typ == "tree" {
		body, err := readBody(rd, size, maxTreeListing)
		if err != nil {
			return nil, err
		}
		return parseBatchTree(id.Type(), body)
	}

	// Not a tree: let ls-tree peel it.
	if err := git.DiscardFull(rd, size+1); err != nil {
		return nil, err
	}
	return b.lsTree(ctx, id, true, false, false)
}

// parseBatchTree parses the binary body of a tree object:
//
//	<mode-in-ascii> SP <fname> NUL <binary hash>
func parseBatchTree(objectFormat git.ObjectFormat, body []byte) ([]git.TreeEntryData, error) {
	rd := bufio.NewReader(bytes.NewReader(body))
	fnameBuf := make([]byte, 4096)
	modeBuf := make([]byte, 40)
	shaBuf := make([]byte, objectFormat.FullLength())
	entries := make([]git.TreeEntryData, 0, 10)

	for remaining := int64(len(body)); remaining > 0; {
		mode, fname, sha, count, err := git.ParseTreeLine(objectFormat, rd, modeBuf, fnameBuf, shaBuf)
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		remaining -= int64(count)
		entryMode, err := git.ParseEntryMode(string(mode))
		if err != nil {
			return nil, err
		}
		entries = append(entries, git.TreeEntryData{
			ID:   objectFormat.MustID(sha),
			Name: string(fname),
			Mode: entryMode,
			Size: -1,
		})
	}
	return entries, nil
}

// ReadTreeRecursive walks a tree and all its subtrees with ls-tree.
func (b *Backend) ReadTreeRecursive(ctx context.Context, id git.ObjectID, opt git.TreeWalkOpts) iter.Seq2[git.TreeEntryData, error] {
	return func(yield func(git.TreeEntryData, error) bool) {
		entries, err := b.lsTree(ctx, id, false, opt.Trees, opt.Sizes)
		if err != nil {
			yield(git.TreeEntryData{}, err)
			return
		}
		for _, entry := range entries {
			if !yield(entry, nil) {
				return
			}
		}
	}
}

// limitedBuffer collects at most max bytes and then fails the write, which stops the command feeding it.
type limitedBuffer struct {
	bytes.Buffer
	max int
}

var errOutputTooLarge = errors.New("gitcli: command output exceeds the limit")

func (w *limitedBuffer) Write(p []byte) (int, error) {
	if w.Len()+len(p) > w.max {
		return 0, errOutputTooLarge
	}
	return w.Buffer.Write(p)
}

// lsTree runs ls-tree for id. A single level (flat) is listed with its sizes when sizes is set;
// otherwise the whole tree is walked, with the tree entries themselves when trees is set.
func (b *Backend) lsTree(ctx context.Context, id git.ObjectID, flat, trees, sizes bool) ([]git.TreeEntryData, error) {
	cmd := git.NewCommand(ctx, "ls-tree", "-z")
	if !flat {
		cmd.AddArguments("-r")
		if trees {
			cmd.AddArguments("-t")
		}
	}
	if sizes || flat {
		cmd.AddArguments("--long")
	}
	cmd.AddDynamicArguments(id.String())

	out := &limitedBuffer{max: maxTreeListing}
	var stderr bytes.Buffer
	if err := cmd.Run(&git.RunOpts{Dir: b.repo.Path, Stdout: out, Stderr: &stderr}); err != nil {
		msg := stderr.String() + err.Error()
		if strings.Contains(msg, "fatal: Not a valid object name") || strings.Contains(msg, "fatal: not a tree object") {
			return nil, git.ErrNotExist{ID: id.String()}
		}
		return nil, git.ConcatenateError(err, stderr.String())
	}
	return parseLsTree(out.Bytes())
}

// parseLsTree parses the output of "git ls-tree -z [--long]":
//
//	<mode> SP <type> SP <sha> [SP* <size>] TAB <path> NUL
func parseLsTree(data []byte) ([]git.TreeEntryData, error) {
	entries := make([]git.TreeEntryData, 0, bytes.Count(data, []byte{0})+1)
	for len(data) > 0 {
		var line []byte
		line, data, _ = bytes.Cut(data, []byte{0})
		attrs, name, ok := bytes.Cut(line, []byte{'\t'})
		if !ok {
			return nil, fmt.Errorf("invalid ls-tree output (no tab): %q", line)
		}
		mode, attrs, _ := bytes.Cut(attrs, []byte{' '})
		_, attrs, _ = bytes.Cut(attrs, []byte{' '}) // the type is not used, the mode is enough to determine the type
		oid, attrs, _ := bytes.Cut(attrs, []byte{' '})

		entry := git.TreeEntryData{Name: string(name), Size: -1}
		if len(attrs) > 0 {
			// The last field is the space-padded size; it is "-" for a tree or a submodule.
			entry.Size, _ = strconv.ParseInt(strings.TrimSpace(string(attrs)), 10, 64)
		}
		var err error
		if entry.Mode, err = git.ParseEntryMode(string(mode)); err != nil {
			return nil, err
		}
		if entry.ID, err = git.NewIDFromString(string(oid)); err != nil {
			return nil, fmt.Errorf("invalid ls-tree output (invalid object id): %q, err: %w", line, err)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// smallBlobSize is the size under which a blob is copied out and the batch process released at once.
const smallBlobSize = 4096

// OpenBlob opens a blob through cat-file --batch. The batch process stays borrowed until the reader is closed.
func (b *Backend) OpenBlob(ctx context.Context, id git.ObjectID, limit int64) (io.ReadCloser, int64, error) {
	s, err := b.open(ctx, id.String())
	if err != nil {
		return nil, 0, err
	}
	_, _, size, err := git.ReadBatchLine(s.rd)
	if err != nil {
		s.release()
		return nil, 0, err
	}

	keep := size
	if limit >= 0 && limit < size {
		keep = limit
	}

	if size < smallBlobSize {
		defer s.release()
		buf := make([]byte, size)
		if _, err := io.ReadFull(s.rd, buf); err != nil {
			return nil, size, err
		}
		if _, err := s.rd.Discard(1); err != nil {
			return nil, size, err
		}
		return io.NopCloser(bytes.NewReader(buf[:keep])), size, nil
	}

	return &blobReader{
		rd:                s.rd,
		n:                 keep,
		additionalDiscard: size - keep,
		cancel:            s.release,
	}, size, nil
}

// blobReader reads n bytes of a blob from a batch stream and discards the rest on Close.
type blobReader struct {
	rd                *bufio.Reader
	n                 int64 // number of bytes to read
	additionalDiscard int64 // additional number of bytes to discard
	cancel            func()
}

func (b *blobReader) Read(p []byte) (n int, err error) {
	if b.n <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > b.n {
		p = p[0:b.n]
	}
	n, err = b.rd.Read(p)
	b.n -= int64(n)
	return n, err
}

// Close implements io.Closer
func (b *blobReader) Close() error {
	if b.rd == nil {
		return nil
	}

	defer b.cancel()

	// discard the unread bytes, the truncated bytes and the trailing newline
	if err := git.DiscardFull(b.rd, b.n+b.additionalDiscard+1); err != nil {
		return err
	}

	b.rd = nil

	return nil
}

// StatObjects reads type and size of each id through cat-file --batch-check.
func (b *Backend) StatObjects(ctx context.Context, ids []git.ObjectID) ([]git.ObjectInfo, error) {
	infos := make([]git.ObjectInfo, 0, len(ids))
	for start := 0; start < len(ids); start += statBatchSize {
		chunk := ids[start:min(start+statBatchSize, len(ids))]
		if err := b.statChunk(ctx, chunk, &infos); err != nil {
			return nil, err
		}
	}
	return infos, nil
}

func (b *Backend) statChunk(ctx context.Context, ids []git.ObjectID, infos *[]git.ObjectInfo) error {
	wr, rd, cancel, err := b.repo.CatFileBatchCheck(b.batchCtx(ctx))
	if err != nil {
		return err
	}
	defer cancel()

	var request strings.Builder
	for _, id := range ids {
		request.WriteString(id.String())
		request.WriteByte('\n')
	}
	if _, err := wr.Write([]byte(request.String())); err != nil {
		return err
	}
	for _, id := range ids {
		_, typ, size, err := git.ReadBatchLine(rd)
		switch {
		case err == nil:
			*infos = append(*infos, git.ObjectInfo{ID: id, Type: git.ObjectType(typ), Size: size})
		case git.IsErrNotExist(err):
			*infos = append(*infos, git.ObjectInfo{ID: id, Missing: true})
		default:
			return err
		}
	}
	return nil
}

// ReadTag reads an annotated tag object.
func (b *Backend) ReadTag(ctx context.Context, id git.ObjectID) (*git.TagData, error) {
	s, err := b.open(ctx, id.String())
	if err != nil {
		return nil, err
	}
	defer s.release()

	_, typ, size, err := git.ReadBatchLine(s.rd)
	if err != nil {
		if errors.Is(err, io.EOF) || git.IsErrNotExist(err) {
			return nil, git.ErrNotExist{ID: id.String()}
		}
		return nil, err
	}
	if typ != "tag" {
		if err := git.DiscardFull(s.rd, size+1); err != nil {
			return nil, err
		}
		return nil, git.ErrNotExist{ID: id.String()}
	}
	body, err := readBody(s.rd, size, maxMetaObjectSize)
	if err != nil {
		return nil, err
	}
	tag, err := git.ParseTagData(id.Type(), body)
	if err != nil {
		return nil, err
	}
	return &git.TagData{
		ID:        id,
		Object:    tag.Object,
		Type:      tag.Type,
		Tagger:    tag.Tagger,
		Message:   tag.Message,
		Signature: tag.Signature,
	}, nil
}
