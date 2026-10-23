// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package gitcli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"forgejo.org/modules/git"
	"forgejo.org/modules/repostore"
)

var invalidPathPattern = regexp.MustCompile(`Invalid path '`)

// CreateCommit builds a commit from file operations on the parent and publishes it when asked to.
func (w *Writer) CreateCommit(ctx context.Context, req repostore.CommitRequest) (*repostore.CommitResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if err := requireRepoPath(req.Repo); err != nil {
		return nil, err
	}
	if req.Update != nil {
		if err := requireRepoPath(req.Update.Repo); err != nil {
			return nil, err
		}
	}
	if err := req.Repo.RequireObjectFormat(); err != nil {
		return nil, err
	}

	s, err := newScratch(ctx, "upload", true, req.Repo.ObjectFormat, req.Repo)
	if err != nil {
		return nil, err
	}
	defer s.Close()

	if req.Parent != nil {
		if err := s.readTree(req.Parent); err != nil {
			return nil, err
		}
	}
	if err := s.applyOps(req.Ops); err != nil {
		return nil, err
	}
	tree, err := s.writeTree()
	if err != nil {
		return nil, err
	}

	if req.Preview != nil {
		stat, err := s.preview(req.Parent, req.Preview)
		if err != nil {
			return nil, err
		}
		return &repostore.CommitResult{Tree: tree, Stat: stat}, nil
	}

	commit, err := s.commitTree(req.Parent, tree, &req.Meta)
	if err != nil {
		return nil, err
	}
	if req.Update != nil {
		in := repostore.PublishInput{GitDir: s.dir, Commit: commit, Base: req.Parent}
		if err := s.beforePublish(req.BeforePublish, in); err != nil {
			return nil, err
		}
		if err := s.publish(commit.String(), req.Update, req.HookEnv); err != nil {
			return nil, err
		}
	}
	return &repostore.CommitResult{Commit: commit, Tree: tree}, nil
}

func (s *scratch) beforePublish(fn repostore.BeforePublish, in repostore.PublishInput) error {
	if fn == nil {
		return nil
	}
	return fn(s.ctx, in)
}

// readTree reads the tree of a commit into the index.
func (s *scratch) readTree(id git.ObjectID) error {
	if _, err := s.run(git.NewCommand(s.ctx, "read-tree").AddDynamicArguments(id.String()), nil); err != nil {
		return fmt.Errorf("read the parent tree: %w", err)
	}
	return nil
}

func (s *scratch) applyOps(ops []repostore.FileOp) error {
	for i := range ops {
		op := &ops[i]
		switch op.Kind {
		case repostore.OpPut:
			mode := op.Mode
			if mode == 0 {
				mode = git.EntryModeBlob
			}
			var hash string
			var err error
			if mode == git.EntryModeCommit {
				hash, err = s.readCommitID(op.Content)
			} else {
				hash, err = s.hashObject(op.Content)
			}
			if err != nil {
				return err
			}
			if err := s.addToIndex(mode.String(), hash, op.Path); err != nil {
				return err
			}
		case repostore.OpDelete:
			if err := s.removeFromIndex(op.Path); err != nil {
				return err
			}
		}
	}
	return nil
}

// readCommitID reads the hexadecimal id of a commit, for a submodule entry. The commit need not exist here.
func (s *scratch) readCommitID(content io.Reader) (string, error) {
	// An id is 40 or 64 characters, by the object format; the limit leaves room for white space and detects a longer value.
	raw, err := io.ReadAll(io.LimitReader(content, 129))
	if err != nil {
		return "", fmt.Errorf("read the submodule commit id: %w", err)
	}
	id := strings.TrimSpace(string(raw))
	if len(id) != s.format.FullLength() || !s.format.IsValid(id) {
		return "", fmt.Errorf("%w: a submodule needs a full commit id", repostore.ErrInvalidRequest)
	}
	return id, nil
}

// hashObject writes content as a blob and returns its id.
func (s *scratch) hashObject(content io.Reader) (string, error) {
	var stdout, stderr bytes.Buffer
	if err := git.NewCommand(s.ctx, "hash-object", "-w", "--stdin").
		Run(&git.RunOpts{Dir: s.dir, Stdin: content, Stdout: &stdout, Stderr: &stderr}); err != nil {
		return "", fmt.Errorf("hash-object: %w\nstderr: %s", err, limitString(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// addToIndex puts an object in the index at a path, replacing what was there.
func (s *scratch) addToIndex(mode, hash, filePath string) error {
	if _, _, err := git.NewCommand(s.ctx, "update-index", "--add", "--replace", "--cacheinfo").
		AddDynamicArguments(mode + "," + hash + "," + filePath).
		RunStdString(&git.RunOpts{Dir: s.dir}); err != nil {
		if invalidPathPattern.MatchString(err.Error()) {
			return &repostore.InvalidPathError{Path: filePath}
		}
		return fmt.Errorf("add %q to the index: %w", filePath, err)
	}
	return nil
}

// removeFromIndex removes one file from the index. It fails when the path is not a file in the index.
func (s *scratch) removeFromIndex(filePath string) error {
	listed, _, err := git.NewCommand(s.ctx, "ls-files", "-z").AddDashesAndList(filePath).RunStdBytes(&git.RunOpts{Dir: s.dir})
	if err != nil {
		return fmt.Errorf("list %q in the index: %w", filePath, err)
	}
	found := false
	for line := range bytes.SplitSeq(listed, []byte{0}) {
		if string(line) == filePath {
			found = true
			break
		}
	}
	if !found {
		return &repostore.FileNotFoundError{Path: filePath}
	}

	var in bytes.Buffer
	in.WriteString("0 ")
	in.WriteString(s.format.EmptyObjectID().String())
	in.WriteByte('\t')
	in.WriteString(filePath)
	in.WriteByte(0)
	var stderr bytes.Buffer
	if err := git.NewCommand(s.ctx, "update-index", "--remove", "-z", "--index-info").
		Run(&git.RunOpts{Dir: s.dir, Stdin: &in, Stdout: io.Discard, Stderr: &stderr}); err != nil {
		return fmt.Errorf("remove %q from the index: %w\nstderr: %s", filePath, err, limitString(stderr.String()))
	}
	return nil
}

// writeTree writes the index as a tree.
func (s *scratch) writeTree() (git.ObjectID, error) {
	out, err := s.run(git.NewCommand(s.ctx, "write-tree"), nil)
	if err != nil {
		return nil, fmt.Errorf("write-tree: %w", err)
	}
	return git.NewIDFromString(out)
}

// commitTree writes a commit for a tree. The parent may be nil.
func (s *scratch) commitTree(parent git.ObjectID, tree git.ObjectID, meta *repostore.CommitMeta) (git.ObjectID, error) {
	committer := meta.Committer
	author := meta.Author
	if author == nil {
		author = committer
	}
	authorWhen, committerWhen := whenOrNow(author.When), whenOrNow(committer.When)

	message := new(bytes.Buffer)
	message.WriteString(meta.Message)
	message.WriteString("\n")

	cmd := git.NewCommand(s.ctx, "commit-tree").AddDynamicArguments(tree.String())
	if parent != nil {
		cmd.AddOptionValues("-p", parent.String())
	}

	var decision *repostore.SignDecision
	if meta.Sign != nil {
		var err error
		decision, err = meta.Sign(s.ctx, repostore.SignInput{GitDir: s.dir, Base: parent})
		if err != nil {
			return nil, fmt.Errorf("signing decision: %w", err)
		}
	}
	if decision != nil {
		cmd.AddOptionFormat("-S%s", decision.KeyID)
		if decision.Committer != nil {
			if committer.Name != author.Name || committer.Email != author.Email {
				message.WriteString("\n")
				message.WriteString("Co-authored-by: ")
				message.WriteString(committer.String())
				message.WriteString("\n")
				message.WriteString("Co-committed-by: ")
				message.WriteString(committer.String())
				message.WriteString("\n")
			}
			committer = decision.Committer
		}
	} else {
		cmd.AddArguments("--no-gpg-sign")
	}

	if meta.Signoff {
		message.WriteString("\n")
		message.WriteString("Signed-off-by: ")
		message.WriteString(committer.String())
	}

	// The environment carries the identities, so a hook or a signing program sees them.
	env := append(os.Environ(),
		"GIT_AUTHOR_NAME="+author.Name,
		"GIT_AUTHOR_EMAIL="+author.Email,
		"GIT_AUTHOR_DATE="+authorWhen.Format(time.RFC3339),
		"GIT_COMMITTER_NAME="+committer.Name,
		"GIT_COMMITTER_EMAIL="+committer.Email,
		"GIT_COMMITTER_DATE="+committerWhen.Format(time.RFC3339),
	)

	var stdout, stderr bytes.Buffer
	if err := cmd.Run(&git.RunOpts{Env: env, Dir: s.dir, Stdin: message, Stdout: &stdout, Stderr: &stderr}); err != nil {
		return nil, fmt.Errorf("commit-tree: %w\nstderr: %s", err, limitString(stderr.String()))
	}
	return git.NewIDFromString(strings.TrimSpace(stdout.String()))
}

func whenOrNow(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now()
	}
	return t
}

// preview writes the diff of the index against a commit to w and returns its size. A failing
// consumer ends the diff quietly: it has what it wanted.
func (s *scratch) preview(parent git.ObjectID, w io.Writer) (git.DiffStat, error) {
	rec := &errRecorder{w: w}
	var stderr bytes.Buffer
	err := git.NewCommand(s.ctx, "diff-index", "--src-prefix=\\a/", "--dst-prefix=\\b/", "--cached", "-p").
		AddDynamicArguments(parent.String()).
		Run(&git.RunOpts{Timeout: previewTimeout, Dir: s.dir, Stdout: rec, Stderr: &stderr})
	if err != nil && rec.err == nil && !git.IsErrCanceledOrKilled(err) {
		return git.DiffStat{}, fmt.Errorf("diff-index: %w\nstderr: %s", err, limitString(stderr.String()))
	}
	files, added, deleted, err := git.GetIndexShortStat(s.ctx, s.dir, parent.String())
	if err != nil {
		return git.DiffStat{}, err
	}
	return git.DiffStat{NumFiles: files, Additions: added, Deletions: deleted}, nil
}
