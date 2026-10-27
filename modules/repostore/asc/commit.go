// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package asc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git"
	"forgejo.org/modules/repostore"
)

// previewSignature stands in for the committer of a preview, which has none: the commit is never published.
var previewSignature = &repostore.Signature{Name: "Forgejo", Email: "forgejo@localhost"}

// metaWire is a commit's identities and message as asc takes them.
type metaWire struct {
	author, committer *ascv1.Signature
	message           []byte
}

// buildMeta turns a CommitMeta into wire form. base and head are the commits the new commit builds on, for
// the SignFunc. A SignFunc that asks to sign fails the request: asc serve has no signing key (SPEC 8.5).
func buildMeta(ctx context.Context, repo repostore.RepoRef, m repostore.CommitMeta, base, head git.ObjectID, preview bool) (*metaWire, error) {
	committer, author := m.Committer, m.Author
	if committer == nil && preview {
		committer = previewSignature
	}
	if committer == nil {
		return nil, fmt.Errorf("%w: committer is required", repostore.ErrInvalidRequest)
	}
	if author == nil {
		author = committer
	}
	if m.Sign != nil && !preview {
		decision, err := m.Sign(ctx, repostore.SignInput{GitDir: repo.Path, Base: base, Head: head})
		if err != nil {
			return nil, fmt.Errorf("signing decision: %w", err)
		}
		if decision != nil {
			return nil, fmt.Errorf("%w: the commit must be signed, and asc serve has no signing key", repostore.ErrUnsupported)
		}
	}
	var msg bytes.Buffer
	msg.WriteString(m.Message)
	msg.WriteString("\n")
	if m.Signoff {
		msg.WriteString("\nSigned-off-by: ")
		msg.WriteString(committer.String())
		msg.WriteString("\n")
	}
	now := time.Now()
	return &metaWire{
		author:    wireSignature(author, now),
		committer: wireSignature(committer, now),
		message:   msg.Bytes(),
	}, nil
}

// hookMode says which of asc's hooks run for a change of refs. The pull request refs belong to Forgejo
// itself (PR head sync and AGit, SPEC 13.5): its pre-receive hook refuses them from users, so they are written
// with HOOK_MODE_SKIP_PRE, which asc accepts from the internal token only. Everything else runs the full pipeline.
func hookMode(refs ...string) ascv1.HookMode {
	if len(refs) == 0 {
		return ascv1.HookMode_HOOK_MODE_RUN
	}
	for _, r := range refs {
		if !strings.HasPrefix(r, "refs/pull/") {
			return ascv1.HookMode_HOOK_MODE_RUN
		}
	}
	return ascv1.HookMode_HOOK_MODE_SKIP_PRE
}

// publish moves one ref to a changeset with the repository's hooks: a fast-forward check, or a forced
// update when u.Force is set. Objects come from the repository the change was built in.
func (s *session) publish(from *repoHandle, u *repostore.RefUpdate, asc []byte, env []string, commit string) error {
	to, err := handle(u.Repo)
	if err != nil {
		return err
	}
	if err := s.copyClosure(from, to, asc); err != nil {
		return err
	}
	mode := ascv1.UpdateMode_UPDATE_MODE_FAST_FORWARD
	if u.Force {
		mode = ascv1.UpdateMode_UPDATE_MODE_FORCE
	}
	// asc calls the hub's hooks as the hub, with no person. The hub is told whose change this is first.
	note := s.notePusher(to.name, env, []PushedRef{{Name: u.Ref, New: commit}})
	resp, err := s.api.UpdateRefs(s.ctx, &ascv1.UpdateRefsRequest{
		Repo: to.ascRef(),
		Updates: []*ascv1.RefUpdate{{
			Name:   u.Ref,
			Expect: &ascv1.RefUpdate_Any{Any: true},
			New:    &ascv1.RefTarget{T: &ascv1.RefTarget_Changeset{Changeset: asc}},
		}},
		Mode:  mode,
		Hooks: hookMode(u.Ref),
	})
	if err != nil {
		note.forget()
		return publishErr("UpdateRefs", err)
	}
	if len(resp.GetResults()) != 1 {
		return fmt.Errorf("%w: UpdateRefs answered %d results for 1", ErrBadAnswer, len(resp.GetResults()))
	}
	note.wait(s.ctx)
	return nil
}

// finish runs BeforePublish and publishes the commit, when the request has an Update.
func (s *session) finish(from *repoHandle, update *repostore.RefUpdate, before repostore.BeforePublish, in repostore.PublishInput, asc []byte, env []string) error {
	if update == nil {
		return nil
	}
	if before != nil {
		if err := before(s.caller, in); err != nil {
			return err
		}
	}
	return s.publish(from, update, asc, env, in.Commit.String())
}

// tree reads the git tree id of a commit.
func (s *session) tree(h *repoHandle, commit git.ObjectID) (git.ObjectID, error) {
	resp, err := s.api.ReadChangeset(s.ctx, &ascv1.ReadChangesetRequest{Repo: h.gitRef(), Rev: commit.String()})
	if err != nil {
		return nil, callErr("ReadChangeset", err)
	}
	return idFromWire(resp.GetRootGit())
}

// result builds the CommitResult of a changeset that asc made.
func (s *session) result(h *repoHandle, asc []byte) (*repostore.CommitResult, git.ObjectID, error) {
	ids, err := s.ascToGit(h, asc)
	if err != nil {
		return nil, nil, err
	}
	tree, err := s.tree(h, ids[0])
	if err != nil {
		return nil, nil, err
	}
	return &repostore.CommitResult{Commit: ids[0], Tree: tree}, ids[0], nil
}

// gitOf returns the git id of a changeset, from the answer when it has one.
func (s *session) gitOf(h *repoHandle, asc, gitID []byte) (git.ObjectID, error) {
	if len(gitID) != 0 {
		return idFromWire(gitID)
	}
	ids, err := s.ascToGit(h, asc)
	if err != nil {
		return nil, err
	}
	return ids[0], nil
}

// CreateCommit builds one commit from file operations on Parent and publishes it when Update is set.
func (w *Writer) CreateCommit(ctx context.Context, req repostore.CommitRequest) (*repostore.CommitResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	h, err := handle(req.Repo)
	if err != nil {
		return nil, err
	}
	ops, err := wireOps(req.Ops)
	if err != nil {
		return nil, err
	}
	preview := req.Preview != nil
	meta, err := buildMeta(ctx, req.Repo, req.Meta, req.Parent, nil, preview)
	if err != nil {
		return nil, err
	}
	s, err := w.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer s.close()

	creq := &ascv1.CommitRequest{
		Repo: h.ascRef(), Ops: ops, Message: meta.message, Author: meta.author, Committer: meta.committer,
	}
	if req.Parent != nil {
		parent, err := s.pair(h, req.Parent)
		if err != nil {
			return nil, err
		}
		creq.Parent = hexString(parent.asc)
	}
	resp, err := s.api.Commit(s.ctx, creq)
	if err != nil {
		return nil, callErr("Commit", err)
	}
	commit, err := s.gitOf(h, resp.GetId(), resp.GetGit())
	if err != nil {
		return nil, err
	}
	tree, err := s.tree(h, commit)
	if err != nil {
		return nil, err
	}

	if preview {
		stat, err := s.preview(h, req.Parent, commit, req.Preview)
		if err != nil {
			return nil, err
		}
		return &repostore.CommitResult{Tree: tree, Stat: stat}, nil
	}
	in := repostore.PublishInput{Commit: commit, Base: req.Parent}
	if err := s.finish(h, req.Update, req.BeforePublish, in, resp.GetId(), req.HookEnv); err != nil {
		return nil, err
	}
	return &repostore.CommitResult{Commit: commit, Tree: tree}, nil
}

// wireOps converts file operations. The content of all files is read once and bounded by MaxCommitBytes.
func wireOps(ops []repostore.FileOp) ([]*ascv1.FileOp, error) {
	out := make([]*ascv1.FileOp, 0, len(ops))
	total := 0
	for i := range ops {
		op := &ops[i]
		wire := &ascv1.FileOp{Path: []byte(op.Path)}
		switch op.Kind {
		case repostore.OpDelete:
			wire.Op = &ascv1.FileOp_Delete{Delete: true}
		case repostore.OpPut:
			switch op.Mode {
			case 0, git.EntryModeBlob:
				wire.Mode = uint32(git.EntryModeBlob)
			case git.EntryModeExec, git.EntryModeSymlink:
				wire.Mode = uint32(op.Mode)
			case git.EntryModeCommit:
				return nil, fmt.Errorf("%w: a submodule entry (%q)", repostore.ErrUnsupported, op.Path)
			default:
				return nil, fmt.Errorf("%w: put of %q has mode %s", repostore.ErrInvalidRequest, op.Path, op.Mode)
			}
			data, err := io.ReadAll(io.LimitReader(op.Content, int64(MaxCommitBytes-total)+1))
			if err != nil {
				return nil, fmt.Errorf("read %q: %w", op.Path, err)
			}
			if total += len(data); total > MaxCommitBytes {
				return nil, fmt.Errorf("%w: file content of more than %d bytes in one commit", ErrTooLarge, MaxCommitBytes)
			}
			wire.Op = &ascv1.FileOp_PutContent{PutContent: data}
		default:
			return nil, fmt.Errorf("%w: file operation %d has unknown kind %d", repostore.ErrInvalidRequest, i, op.Kind)
		}
		out = append(out, wire)
	}
	return out, nil
}

// preview writes the diff of a commit against its parent to w and returns the size of the change.
func (s *session) preview(h *repoHandle, parent, commit git.ObjectID, w io.Writer) (git.DiffStat, error) {
	var stat git.DiffStat
	numstat, err := s.diffText(h, parent, commit, ascv1.DiffFormat_DIFF_FORMAT_NUMSTAT, MaxStatBytes, nil)
	if err != nil {
		return stat, err
	}
	for line := range strings.SplitSeq(string(numstat), "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 {
			return stat, fmt.Errorf("%w: numstat line %q", ErrBadAnswer, sanitize(line))
		}
		stat.NumFiles++
		// A binary file counts "-" lines.
		if n, err := strconv.Atoi(fields[0]); err == nil {
			stat.Additions += n
		}
		if n, err := strconv.Atoi(fields[1]); err == nil {
			stat.Deletions += n
		}
	}
	if _, err := s.diffText(h, parent, commit, ascv1.DiffFormat_DIFF_FORMAT_PATCH, MaxPreviewBytes, w); err != nil {
		return stat, err
	}
	return stat, nil
}

// diffText streams a Diff of base to head. With sink nil the text is collected and returned.
func (s *session) diffText(h *repoHandle, base, head git.ObjectID, format ascv1.DiffFormat, limit int64, sink io.Writer) ([]byte, error) {
	st, err := s.api.Diff(s.ctx, &ascv1.DiffRequest{
		Repo: h.gitRef(), Base: base.String(), Head: head.String(), Format: format, Renames: true,
	})
	if err != nil {
		return nil, callErr("Diff", err)
	}
	var collected bytes.Buffer
	var written int64
	for {
		msg, err := st.Recv()
		if errors.Is(err, io.EOF) {
			return collected.Bytes(), nil
		}
		if err != nil {
			return nil, callErr("Diff", err)
		}
		if written += int64(len(msg.GetText())); written > limit {
			return nil, fmt.Errorf("%w: a diff of more than %d bytes", ErrTooLarge, limit)
		}
		if sink != nil {
			if _, err := sink.Write(msg.GetText()); err != nil {
				return nil, err
			}
			continue
		}
		collected.Write(msg.GetText())
	}
}

// CherryPick applies the change made by one commit onto Parent.
func (w *Writer) CherryPick(ctx context.Context, req repostore.PickRequest) (*repostore.CommitResult, error) {
	return w.pick(ctx, req, ascv1.ApplyMode_APPLY_MODE_CHERRY_PICK)
}

// Revert applies the inverse of the change made by one commit onto Parent.
func (w *Writer) Revert(ctx context.Context, req repostore.PickRequest) (*repostore.CommitResult, error) {
	return w.pick(ctx, req, ascv1.ApplyMode_APPLY_MODE_REVERT)
}

func (w *Writer) pick(ctx context.Context, req repostore.PickRequest, mode ascv1.ApplyMode) (*repostore.CommitResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	h, err := handle(req.Repo)
	if err != nil {
		return nil, err
	}
	meta, err := buildMeta(ctx, req.Repo, req.Meta, req.Parent, nil, false)
	if err != nil {
		return nil, err
	}
	s, err := w.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer s.close()

	ids, err := s.gitToAsc(h, req.Parent, req.Commit)
	if err != nil {
		return nil, err
	}
	return s.apply(h, &ascv1.ApplyRequest{
		Repo: h.ascRef(), Onto: hexString(ids[0]), Changeset: hexString(ids[1]), Mode: mode,
	}, meta, req.Parent, req.Update, req.BeforePublish, req.HookEnv)
}

// ApplyPatch applies a unified diff to Parent.
func (w *Writer) ApplyPatch(ctx context.Context, req repostore.PatchRequest) (*repostore.CommitResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if len(req.Patch) > MaxCommitBytes {
		return nil, fmt.Errorf("%w: a patch of more than %d bytes", ErrTooLarge, MaxCommitBytes)
	}
	h, err := handle(req.Repo)
	if err != nil {
		return nil, err
	}
	meta, err := buildMeta(ctx, req.Repo, req.Meta, req.Parent, nil, false)
	if err != nil {
		return nil, err
	}
	s, err := w.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer s.close()

	parent, err := s.pair(h, req.Parent)
	if err != nil {
		return nil, err
	}
	return s.apply(h, &ascv1.ApplyRequest{
		Repo: h.ascRef(), Onto: hexString(parent.asc), Patch: []byte(req.Patch), Mode: ascv1.ApplyMode_APPLY_MODE_PATCH,
	}, meta, req.Parent, req.Update, req.BeforePublish, req.HookEnv)
}

// apply runs one Apply call without a ref, then publishes its result.
func (s *session) apply(h *repoHandle, areq *ascv1.ApplyRequest, meta *metaWire, base git.ObjectID, update *repostore.RefUpdate, before repostore.BeforePublish, env []string) (*repostore.CommitResult, error) {
	areq.Message, areq.Author, areq.Committer = meta.message, meta.author, meta.committer
	resp, err := s.api.Apply(s.ctx, areq)
	if err != nil {
		return nil, callErr("Apply", err)
	}
	if len(resp.GetConflicts()) > 0 {
		return nil, &repostore.ConflictError{Files: conflictPaths(resp.GetConflicts())}
	}
	if len(resp.GetResult()) == 0 {
		return nil, fmt.Errorf("%w: Apply answered neither a result nor conflicts", ErrBadAnswer)
	}
	result, commit, err := s.result(h, resp.GetResult())
	if err != nil {
		return nil, err
	}
	in := repostore.PublishInput{Commit: commit, Base: base}
	if err := s.finish(h, update, before, in, resp.GetResult(), env); err != nil {
		return nil, err
	}
	return result, nil
}

// conflictPaths converts the conflicting paths of an answer, bounded.
func conflictPaths(paths [][]byte) []string {
	out := make([]string, 0, min(len(paths), repostore.MaxConflictsReported))
	for _, p := range paths {
		if len(out) >= repostore.MaxConflictsReported {
			break
		}
		out = append(out, string(p))
	}
	return out
}
