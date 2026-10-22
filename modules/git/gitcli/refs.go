// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package gitcli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"

	"forgejo.org/modules/git"
	"forgejo.org/modules/util"
)

// sortKeyPattern is the shape of a for-each-ref sort key such as "-creatordate" or "refname:lstrip=2".
var sortKeyPattern = regexp.MustCompile(`^-?[A-Za-z][A-Za-z0-9_:=.-]{0,127}$`)

// ListRefs lists references with for-each-ref. Remote-tracking references and refs/stash are skipped.
func (b *Backend) ListRefs(ctx context.Context, q git.RefQuery) ([]git.Reference, error) {
	cmd := git.NewCommand(ctx, "for-each-ref")
	if q.Sort != "" {
		if !sortKeyPattern.MatchString(q.Sort) {
			return nil, util.NewInvalidArgumentErrorf("invalid ref sort key %q", q.Sort)
		}
		cmd.AddOptionFormat("--sort=%s", q.Sort)
	}
	if q.Contains != nil {
		cmd.AddOptionValues("--contains", q.Contains.String())
	}

	stdoutReader, stdoutWriter := io.Pipe()
	defer func() {
		_ = stdoutReader.Close()
		_ = stdoutWriter.Close()
	}()

	go func() {
		stderrBuilder := &strings.Builder{}
		err := cmd.Run(&git.RunOpts{
			Dir:    b.repo.Path,
			Stdout: stdoutWriter,
			Stderr: stderrBuilder,
		})
		if err != nil {
			_ = stdoutWriter.CloseWithError(git.ConcatenateError(err, stderrBuilder.String()))
		} else {
			_ = stdoutWriter.Close()
		}
	}()

	refs := make([]git.Reference, 0)
	bufReader := bufio.NewReader(stdoutReader)
	for q.Limit <= 0 || len(refs) < q.Limit {
		// The output of for-each-ref is simply a list:
		// <sha> SP <type> TAB <ref> LF
		sha, err := bufReader.ReadString(' ')
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		sha = sha[:len(sha)-1]

		typ, err := bufReader.ReadString('\t')
		if err == io.EOF {
			// This should not happen, but we'll tolerate it
			break
		}
		if err != nil {
			return nil, err
		}
		typ = typ[:len(typ)-1]

		refName, err := bufReader.ReadString('\n')
		if err == io.EOF {
			// This should not happen, but we'll tolerate it
			break
		}
		if err != nil {
			return nil, err
		}
		refName = refName[:len(refName)-1]

		// refName cannot be HEAD but can be remotes or stash
		if strings.HasPrefix(refName, git.RemotePrefix) || refName == "/refs/stash" {
			continue
		}

		if q.Prefix == "" || strings.HasPrefix(refName, q.Prefix) {
			id, err := git.NewIDFromString(sha)
			if err != nil {
				return nil, fmt.Errorf("gitcli: for-each-ref returned %q: %w", sha, err)
			}
			refs = append(refs, git.Reference{Name: refName, Object: id, Type: typ})
		}
	}
	return refs, nil
}

// Head returns the reference HEAD points at.
func (b *Backend) Head(ctx context.Context) (git.RefName, error) {
	stdout, _, err := git.NewCommand(ctx, "symbolic-ref", "HEAD").RunStdString(&git.RunOpts{Dir: b.repo.Path})
	if err != nil {
		return "", err
	}
	return git.RefName(strings.TrimSpace(stdout)), nil
}

// SetHead points HEAD at a branch. The branch need not exist yet.
func (b *Backend) SetHead(ctx context.Context, branch git.RefName) error {
	if !branch.IsBranch() || !git.IsValidRefPattern(branch.String()) {
		return util.NewInvalidArgumentErrorf("invalid HEAD branch %q", branch)
	}
	_, _, err := git.NewCommand(ctx, "symbolic-ref", "HEAD").AddDynamicArguments(branch.String()).RunStdString(&git.RunOpts{Dir: b.repo.Path})
	return err
}
