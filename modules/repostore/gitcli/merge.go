// Copyright 2019 The Gitea Authors.
// All rights reserved.
// SPDX-License-Identifier: MIT

package gitcli

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"forgejo.org/modules/container"
	"forgejo.org/modules/git"
	"forgejo.org/modules/log"
	"forgejo.org/modules/repostore"
)

// The scratch repository of a merge uses fixed branch names, as the code it replaced did.
const (
	baseRef     = "refs/heads/base"          // the base branch, which moves with the merge
	originalRef = "refs/heads/original_base" // the base branch as the merge found it
	trackingRef = "refs/heads/tracking"      // the head
	stagingRef  = "refs/heads/staging"       // a working branch for rebases
)

var escapedSymbols = regexp.MustCompile(`([*[?! \\])`)

// Merge merges the head into the base, or checks whether it would.
func (w *Writer) Merge(ctx context.Context, req repostore.MergeRequest) (*repostore.MergeResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if err := requireRepoPath(req.Base, req.Head); err != nil {
		return nil, err
	}
	if err := req.Base.RequireObjectFormat(); err != nil {
		return nil, err
	}
	if req.Publish != nil {
		if err := requireRepoPath(req.Publish.Repo); err != nil {
			return nil, err
		}
	}
	if req.DryRun {
		return w.dryRun(ctx, &req)
	}
	return w.merge(ctx, &req)
}

// mergeScratch is a scratch repository for a merge: a work tree whose "base" and "tracking"
// branches hold the base and the head.
type mergeScratch struct {
	*scratch
	req    *repostore.MergeRequest
	outbuf *strings.Builder // kept to reduce buffer re-creation; reset before each use
	errbuf *strings.Builder
	env    []string

	author, committer *git.Signature
	signKeyID         string // empty for no-sign, non-empty to sign
}

// runOpts returns options that collect the output of one command.
func (m *mergeScratch) runOpts() *git.RunOpts {
	m.outbuf.Reset()
	m.errbuf.Reset()
	return &git.RunOpts{
		Env:    m.env,
		Dir:    m.dir,
		Stdout: m.outbuf,
		Stderr: m.errbuf,
	}
}

// newMergeScratch creates the scratch repository with "base" and "original_base" from the base,
// and "tracking" from the head.
func newMergeScratch(ctx context.Context, req *repostore.MergeRequest) (*mergeScratch, error) {
	s, err := newScratch(ctx, "pull", false, req.Base.ObjectFormat, req.Base, req.Head)
	if err != nil {
		return nil, err
	}
	m := &mergeScratch{scratch: s, req: req, outbuf: &strings.Builder{}, errbuf: &strings.Builder{}}
	fail := func(format string, args ...any) (*mergeScratch, error) {
		s.Close()
		return nil, fmt.Errorf(format, args...)
	}

	fetchArgs := git.TrustedCmdArgs{"--no-tags", "--no-write-commit-graph"}

	if err := git.NewCommand(ctx, "fetch").AddArguments(fetchArgs...).
		AddDashesAndList(req.Base.Path, req.BaseRef+":"+baseRef, req.BaseRef+":"+originalRef).
		Run(m.runOpts()); err != nil {
		log.Error("Unable to fetch the base ref %s of %s: %v:\n%s\n%s", req.BaseRef, req.Base.Name, err, m.outbuf.String(), m.errbuf.String())
		return fail("unable to fetch the base branch: %w\n%s\n%s", err, limitString(m.outbuf.String()), limitString(m.errbuf.String()))
	}

	if err := git.NewCommand(ctx, "symbolic-ref").AddDynamicArguments("HEAD", baseRef).
		Run(m.runOpts()); err != nil {
		log.Error("Unable to set HEAD to the base branch in %s: %v\n%s\n%s", s.dir, err, m.outbuf.String(), m.errbuf.String())
		return fail("unable to set HEAD to the base branch: %w\n%s\n%s", err, limitString(m.outbuf.String()), limitString(m.errbuf.String()))
	}

	if err := git.NewCommand(ctx, "fetch").AddArguments(fetchArgs...).
		AddDashesAndList(req.Head.Path, req.HeadRev+":"+trackingRef).
		Run(m.runOpts()); err != nil {
		if !refExists(ctx, req.Head.Path, req.HeadRev) {
			s.Close()
			return nil, &repostore.RefNotFoundError{Ref: req.HeadRev}
		}
		log.Error("Unable to fetch the head %s of %s: %v:\n%s\n%s", req.HeadRev, req.Head.Name, err, m.outbuf.String(), m.errbuf.String())
		return fail("unable to fetch the head: %w\n%s\n%s", err, limitString(m.outbuf.String()), limitString(m.errbuf.String()))
	}
	m.outbuf.Reset()
	m.errbuf.Reset()
	return m, nil
}

// refExists reports whether a revision resolves in a repository.
func refExists(ctx context.Context, repoPath, rev string) bool {
	_, _, err := git.NewCommand(ctx, "rev-parse", "--verify", "--quiet").AddDynamicArguments(rev + "^{commit}").
		RunStdString(&git.RunOpts{Dir: repoPath})
	return err == nil
}

// merge performs a real merge and publishes it.
func (w *Writer) merge(ctx context.Context, req *repostore.MergeRequest) (*repostore.MergeResult, error) {
	m, err := newMergeScratch(ctx, req)
	if err != nil {
		return nil, err
	}
	defer m.Close()

	headID, err := m.resolveID(trackingRef)
	if err != nil {
		return nil, fmt.Errorf("unable to resolve the head: %w", err)
	}
	if req.ExpectHead != nil && req.ExpectHead.String() != headID.String() {
		return nil, &repostore.HeadMismatchError{Given: req.ExpectHead.String(), Current: headID.String()}
	}
	originalID, err := m.resolveID(originalRef)
	if err != nil {
		return nil, fmt.Errorf("unable to resolve the base: %w", err)
	}
	// The merge base before the merge, for the LFS push of an update by rebase. A missing one is not an error.
	var mergeBaseID git.ObjectID
	if out, err := m.run(git.NewCommand(ctx, "merge-base").AddDashesAndList(baseRef, trackingRef), nil); err == nil {
		mergeBaseID, _ = git.NewIDFromString(out)
	}

	if err := m.prepareForMerge(); err != nil {
		return nil, err
	}

	// Decide on signing and set the identities the commits are written with.
	m.author, m.committer = req.Meta.Author, req.Meta.Committer
	if m.author == nil {
		m.author = m.committer
	}
	if req.Meta.Sign != nil {
		decision, err := req.Meta.Sign(ctx, repostore.SignInput{GitDir: m.dir, Base: originalID, Head: headID})
		if err != nil {
			return nil, fmt.Errorf("signing decision: %w", err)
		}
		if decision != nil {
			m.signKeyID = decision.KeyID
			if decision.Committer != nil {
				m.committer = decision.Committer
			}
		}
	}
	// Because this may call hooks we should pass in the environment
	m.env = append(os.Environ(),
		"GIT_AUTHOR_NAME="+m.author.Name,
		"GIT_AUTHOR_EMAIL="+m.author.Email,
		"GIT_AUTHOR_DATE="+whenOrNow(m.author.When).Format(time.RFC3339),
		"GIT_COMMITTER_NAME="+m.committer.Name,
		"GIT_COMMITTER_EMAIL="+m.committer.Email,
		"GIT_COMMITTER_DATE="+whenOrNow(m.committer.When).Format(time.RFC3339),
	)

	switch req.Style {
	case repostore.MergeStyleMerge:
		err = m.styleMerge(req.Meta.Message)
	case repostore.MergeStyleRebase, repostore.MergeStyleRebaseMerge:
		err = m.styleRebase(req.Style, req.Meta.Message)
	case repostore.MergeStyleSquash:
		err = m.styleSquash(req.Meta.Message)
	case repostore.MergeStyleFastForwardOnly:
		err = m.styleFastForwardOnly()
	case repostore.MergeStyleRebaseUpdate:
		err = m.rebaseTrackingOnToBase(repostore.MergeStyleRebaseUpdate)
	}
	if err != nil {
		return nil, err
	}

	localRef := baseRef
	if req.Style == repostore.MergeStyleRebaseUpdate {
		localRef = stagingRef
	}
	tip, err := m.resolveID(localRef)
	if err != nil {
		return nil, fmt.Errorf("failed to get the commit id of the new merge: %w", err)
	}

	if err := m.beforePublish(req.BeforePublish, repostore.PublishInput{
		GitDir:    m.dir,
		Commit:    tip,
		Base:      originalID,
		Head:      headID,
		MergeBase: mergeBaseID,
	}); err != nil {
		return nil, err
	}

	// Push back to the upstream. This causes an api call to "/api/internal/hook/post-receive/...".
	// If it's a merge, all db transactions and operations should be there but not here to prevent deadlock.
	pushCmd := git.NewCommand(ctx, "push")
	if req.Publish.Force {
		pushCmd.AddArguments("-f")
	}
	pushCmd.AddDashesAndList(req.Publish.Repo.Path, localRef+":"+req.Publish.Ref)
	opts := m.runOpts()
	opts.Env = req.HookEnv
	if err := pushCmd.Run(opts); err != nil {
		return nil, pushError(m.outbuf.String(), m.errbuf.String(), err)
	}

	return &repostore.MergeResult{Commit: tip, BaseBefore: originalID}, nil
}

// prepareForMerge takes a repository that has been created by newMergeScratch and sets up the
// sparse-checkout and other things.
func (m *mergeScratch) prepareForMerge() error {
	infoPath := filepath.Join(m.dir, ".git", "info")
	if err := os.MkdirAll(infoPath, 0o700); err != nil {
		log.Error("Unable to create .git/info in %s: %v", m.dir, err)
		return fmt.Errorf("Unable to create .git/info in the scratch repository: %w", err)
	}

	// Enable sparse-checkout
	// Here we use the .git/info/sparse-checkout file as described in the git documentation
	sparseCheckoutListFile, err := os.OpenFile(filepath.Join(infoPath, "sparse-checkout"), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		log.Error("Unable to write .git/info/sparse-checkout file in %s: %v", m.dir, err)
		return fmt.Errorf("Unable to write .git/info/sparse-checkout file in the scratch repository: %w", err)
	}
	defer sparseCheckoutListFile.Close() // we will close it earlier but we need to ensure it is closed if there is an error

	if err := getDiffTree(m.ctx, m.dir, "base", "tracking", sparseCheckoutListFile); err != nil {
		log.Error("getDiffTree(%s, base, tracking): %v", m.dir, err)
		return fmt.Errorf("getDiffTree: %w", err)
	}

	if err := sparseCheckoutListFile.Close(); err != nil {
		log.Error("Unable to close .git/info/sparse-checkout file in %s: %v", m.dir, err)
		return fmt.Errorf("Unable to close .git/info/sparse-checkout file in the scratch repository: %w", err)
	}

	setConfig := func(key, value string) error {
		if err := git.NewCommand(m.ctx, "config", "--local").AddDynamicArguments(key, value).
			Run(m.runOpts()); err != nil {
			log.Error("git config [%s -> %q]: %v\n%s\n%s", key, value, err, m.outbuf.String(), m.errbuf.String())
			return fmt.Errorf("git config [%s -> %q]: %w\n%s\n%s", key, value, err, limitString(m.outbuf.String()), limitString(m.errbuf.String()))
		}
		m.outbuf.Reset()
		m.errbuf.Reset()
		return nil
	}

	// Switch off LFS process (set required, clean and smudge here also)
	for _, kv := range [][2]string{
		{"filter.lfs.process", ""},
		{"filter.lfs.required", "false"},
		{"filter.lfs.clean", ""},
		{"filter.lfs.smudge", ""},
		{"core.sparseCheckout", "true"},
	} {
		if err := setConfig(kv[0], kv[1]); err != nil {
			return err
		}
	}

	// Read base branch index
	if err := git.NewCommand(m.ctx, "read-tree", "HEAD").
		Run(m.runOpts()); err != nil {
		log.Error("git read-tree HEAD: %v\n%s\n%s", err, m.outbuf.String(), m.errbuf.String())
		return fmt.Errorf("Unable to read base branch in to the index: %w\n%s\n%s", err, limitString(m.outbuf.String()), limitString(m.errbuf.String()))
	}
	m.outbuf.Reset()
	m.errbuf.Reset()
	return nil
}

// getDiffTree writes all the files that were changed between headBranch and baseBranch to out.
// The file names are escaped to fit the format required for .git/info/sparse-checkout.
func getDiffTree(ctx context.Context, repoPath, baseBranch, headBranch string, out *os.File) error {
	diffOutReader, diffOutWriter, err := os.Pipe()
	if err != nil {
		log.Error("Unable to create os.Pipe for %s", repoPath)
		return err
	}
	defer func() {
		_ = diffOutReader.Close()
		_ = diffOutWriter.Close()
	}()

	scanNullTerminatedStrings := func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		if atEOF && len(data) == 0 {
			return 0, nil, nil
		}
		if i := bytes.IndexByte(data, '\x00'); i >= 0 {
			return i + 1, data[0:i], nil
		}
		if atEOF {
			return len(data), data, nil
		}
		return 0, nil, nil
	}

	return git.NewCommand(ctx, "diff-tree", "--no-commit-id", "--name-only", "-r", "-r", "-z", "--root").AddDynamicArguments(baseBranch, headBranch).
		Run(&git.RunOpts{
			Dir:    repoPath,
			Stdout: diffOutWriter,
			PipelineFunc: func(ctx context.Context, cancel context.CancelFunc) error {
				// Close the writer end of the pipe to begin processing
				_ = diffOutWriter.Close()
				defer func() {
					// Close the reader on return to terminate the git command if necessary
					_ = diffOutReader.Close()
				}()

				// Now scan the output from the command
				scanner := bufio.NewScanner(diffOutReader)
				scanner.Split(scanNullTerminatedStrings)
				for scanner.Scan() {
					filepath := scanner.Text()
					// escape '*', '?', '[', spaces and '!' prefix
					filepath = escapedSymbols.ReplaceAllString(filepath, `\$1`)
					// no necessary to escape the first '#' symbol because the first symbol is '/'
					fmt.Fprintf(out, "/%s\n", filepath)
				}
				return scanner.Err()
			},
		})
}

// commitAndSignNoAuthor commits the staged merge with the message and the author of the environment.
func (m *mergeScratch) commitAndSignNoAuthor(message string) error {
	cmdCommit := git.NewCommand(m.ctx, "commit").AddOptionFormat("--message=%s", message)
	if m.signKeyID == "" {
		cmdCommit.AddArguments("--no-gpg-sign")
	} else {
		cmdCommit.AddOptionFormat("-S%s", m.signKeyID)
	}
	if err := cmdCommit.Run(m.runOpts()); err != nil {
		log.Error("git commit: %v\n%s\n%s", err, m.outbuf.String(), m.errbuf.String())
		return fmt.Errorf("git commit: %w\n%s\n%s", err, limitString(m.outbuf.String()), limitString(m.errbuf.String()))
	}
	return nil
}

// runMergeCommand runs a git merge and turns its failures into the typed conflict errors.
func (m *mergeScratch) runMergeCommand(style repostore.MergeStyle, cmd *git.Command) error {
	if err := cmd.Run(m.runOpts()); err != nil {
		stdout, stderr := limitString(m.outbuf.String()), limitString(m.errbuf.String())
		// Merge will leave a MERGE_HEAD file in the .git folder if there is a conflict
		if _, statErr := os.Stat(filepath.Join(m.dir, ".git", "MERGE_HEAD")); statErr == nil {
			// We have a merge conflict error
			log.Debug("MergeConflict: %v\n%s\n%s", err, stdout, stderr)
			return &repostore.ConflictError{Style: style, StdOut: stdout, StdErr: stderr, Err: err}
		} else if strings.Contains(m.errbuf.String(), "refusing to merge unrelated histories") {
			log.Debug("MergeUnrelatedHistories: %v\n%s\n%s", err, stdout, stderr)
			return &repostore.ConflictError{Style: style, Unrelated: true, StdOut: stdout, StdErr: stderr, Err: err}
		} else if style == repostore.MergeStyleFastForwardOnly && strings.Contains(m.errbuf.String(), "Not possible to fast-forward, aborting") {
			log.Debug("MergeDivergingFastForwardOnly: %v\n%s\n%s", err, stdout, stderr)
			return &repostore.ConflictError{Style: style, Diverging: true, StdOut: stdout, StdErr: stderr, Err: err}
		}
		log.Error("git merge: %v\n%s\n%s", err, stdout, stderr)
		return fmt.Errorf("git merge: %w\n%s\n%s", err, stdout, stderr)
	}
	m.outbuf.Reset()
	m.errbuf.Reset()
	return nil
}

// styleMerge merges the tracking branch into the current HEAD, which is the base.
func (m *mergeScratch) styleMerge(message string) error {
	cmd := git.NewCommand(m.ctx, "merge", "--no-ff", "--no-commit").AddDynamicArguments("tracking")
	if err := m.runMergeCommand(repostore.MergeStyleMerge, cmd); err != nil {
		log.Error("Unable to merge tracking into base: %v", err)
		return err
	}
	if err := m.commitAndSignNoAuthor(message); err != nil {
		log.Error("Unable to make final commit: %v", err)
		return err
	}
	return nil
}

// styleFastForwardOnly merges the tracking branch into the current HEAD, which is the base, by fast-forward.
func (m *mergeScratch) styleFastForwardOnly() error {
	cmd := git.NewCommand(m.ctx, "merge", "--ff-only").AddDynamicArguments("tracking")
	if err := m.runMergeCommand(repostore.MergeStyleFastForwardOnly, cmd); err != nil {
		log.Error("Unable to merge tracking into base: %v", err)
		return err
	}
	return nil
}

// styleSquash squashes the tracking branch on the current HEAD, which is the base.
func (m *mergeScratch) styleSquash(message string) error {
	sig := m.author
	if m.req.SquashAuthor != nil {
		var err error
		if sig, err = m.squashAuthor(); err != nil {
			return fmt.Errorf("getAuthorSignatureSquash: %w", err)
		}
	}

	cmdMerge := git.NewCommand(m.ctx, "merge", "--squash").AddDynamicArguments("tracking")
	if err := m.runMergeCommand(repostore.MergeStyleSquash, cmdMerge); err != nil {
		log.Error("Unable to merge --squash tracking into base: %v", err)
		return err
	}

	cmdCommit := git.NewCommand(m.ctx, "commit").
		AddOptionFormat("--author='%s <%s>'", sig.Name, sig.Email).
		AddOptionFormat("--message=%s", message)
	if m.signKeyID == "" {
		cmdCommit.AddArguments("--no-gpg-sign")
	} else {
		cmdCommit.AddOptionFormat("-S%s", m.signKeyID)
	}
	if err := cmdCommit.Run(m.runOpts()); err != nil {
		log.Error("git commit: %v\n%s\n%s", err, m.outbuf.String(), m.errbuf.String())
		return fmt.Errorf("git commit: %w\n%s\n%s", err, limitString(m.outbuf.String()), limitString(m.errbuf.String()))
	}
	m.outbuf.Reset()
	m.errbuf.Reset()
	return nil
}

// squashAuthor asks the caller to choose the author of the squash commit from the authors of the squashed commits.
func (m *mergeScratch) squashAuthor() (*git.Signature, error) {
	gitRepo, err := git.OpenRepository(m.ctx, m.dir)
	if err != nil {
		log.Error("Unable to open the scratch repository: %v", err)
		return nil, err
	}
	defer gitRepo.Close()

	commits, err := gitRepo.CommitsBetweenIDs("tracking", "HEAD")
	if err != nil {
		log.Error("Unable to get commits between: HEAD tracking: %v", err)
		return nil, err
	}

	uniqueEmails := make(container.Set[string])
	authors := make([]*git.Signature, 0, len(commits))
	for _, commit := range commits {
		if commit.Author != nil && uniqueEmails.Add(commit.Author.Email) {
			authors = append(authors, commit.Author)
		}
	}
	sig, err := m.req.SquashAuthor(m.ctx, authors)
	if err != nil {
		return nil, err
	}
	if sig == nil {
		return m.author, nil
	}
	return sig, nil
}

// styleRebase rebases the tracking branch on the base branch, which is the current HEAD, with or without a
// merge commit to the original branch.
func (m *mergeScratch) styleRebase(style repostore.MergeStyle, message string) error {
	if err := m.rebaseTrackingOnToBase(style); err != nil {
		return err
	}

	// Checkout base branch again
	if err := git.NewCommand(m.ctx, "checkout").AddDynamicArguments("base").
		Run(m.runOpts()); err != nil {
		log.Error("git checkout base prior to merge post staging rebase: %v\n%s\n%s", err, m.outbuf.String(), m.errbuf.String())
		return fmt.Errorf("git checkout base prior to merge post staging rebase: %w\n%s\n%s", err, limitString(m.outbuf.String()), limitString(m.errbuf.String()))
	}
	m.outbuf.Reset()
	m.errbuf.Reset()

	if style == repostore.MergeStyleRebase {
		return m.rebaseFastForward()
	}
	return m.rebaseMergeCommit(message)
}

// rebaseFastForward performs a rebase merge without a merge commit.
func (m *mergeScratch) rebaseFastForward() error {
	baseHeadSHA, err := git.GetFullCommitID(m.ctx, m.dir, "HEAD")
	if err != nil {
		return fmt.Errorf("Failed to get full commit id for HEAD: %w", err)
	}

	cmd := git.NewCommand(m.ctx, "merge", "--ff-only").AddDynamicArguments("staging")
	if err := m.runMergeCommand(repostore.MergeStyleRebase, cmd); err != nil {
		log.Error("Unable to merge staging into base: %v", err)
		return err
	}

	// Check if anything actually changed before we amend the message, fast forward can skip commits.
	newMergeHeadSHA, err := git.GetFullCommitID(m.ctx, m.dir, "HEAD")
	if err != nil {
		return fmt.Errorf("Failed to get full commit id for HEAD: %w", err)
	}
	if baseHeadSHA == newMergeHeadSHA || m.req.AmendMessage == nil {
		return nil
	}

	// Amend last commit message based on the template, if one exists
	lastMessage, _, err := git.NewCommand(m.ctx, "show", "--format=%B", "-s").RunStdString(&git.RunOpts{Dir: m.dir})
	if err != nil {
		return err
	}
	newMessage, err := m.req.AmendMessage(m.ctx, lastMessage)
	if err != nil {
		log.Error("Unable to get commit message for amend: %v", err)
		return err
	}
	if newMessage != "" {
		if err := git.NewCommand(m.ctx, "commit", "--amend").AddOptionFormat("--message=%s", newMessage).Run(&git.RunOpts{Dir: m.dir}); err != nil {
			log.Error("Unable to amend commit message: %v", err)
			return err
		}
	}
	return nil
}

// rebaseMergeCommit performs a rebase merge with a merge commit.
func (m *mergeScratch) rebaseMergeCommit(message string) error {
	cmd := git.NewCommand(m.ctx, "merge").AddArguments("--no-ff", "--no-commit").AddDynamicArguments("staging")
	if err := m.runMergeCommand(repostore.MergeStyleRebaseMerge, cmd); err != nil {
		log.Error("Unable to merge staging into base: %v", err)
		return err
	}
	if err := m.commitAndSignNoAuthor(message); err != nil {
		log.Error("Unable to make final commit: %v", err)
		return err
	}
	return nil
}

// rebaseTrackingOnToBase checks out the tracking branch as staging and rebases it on to the base branch.
// If there is a conflict it returns a *repostore.ConflictError.
func (m *mergeScratch) rebaseTrackingOnToBase(style repostore.MergeStyle) error {
	// Create staging branch
	if err := git.NewCommand(m.ctx, "branch").AddDynamicArguments("staging", "tracking").
		Run(m.runOpts()); err != nil {
		return fmt.Errorf(
			"unable to git branch tracking as staging in the scratch repository: %w\n%s\n%s",
			err,
			limitString(m.outbuf.String()),
			limitString(m.errbuf.String()),
		)
	}
	m.outbuf.Reset()
	m.errbuf.Reset()

	// If the head is zero commits behind, then no rebasing needs to be done.
	behind, err := m.run(git.NewCommand(m.ctx, "rev-list", "--count").AddDynamicArguments("base", "^tracking"), nil)
	if err != nil {
		return fmt.Errorf("unable to count the commits the head is behind: %w", err)
	}
	if behind == "0" {
		return nil
	}

	// Check git version for availability and suitability of git-replay.
	// If it is available, we use it for performance and to preserve unknown
	// commit headers like the "change-id" header used by Jujutsu and GitButler
	// to track changes across rebase, amend etc.
	// While git-replay is generally available since version 2.44, until a fix
	// released with version 2.54 it will spawn empty commits if a commit was
	// already part of the base branch, instead of skipping it like git-rebase.
	if err := git.CheckGitVersionAtLeast("2.54"); err == nil {
		// Use git-replay for performance and to preserve unknown headers,
		// like the "change-id" header used by Jujutsu and GitButler.
		if err := git.NewCommand(m.ctx, "replay", "--onto").AddDynamicArguments("base").
			AddDynamicArguments("base..staging").
			Run(m.runOpts()); err == nil {
			// git-replay worked, stdout contains the instructions for update-ref
			updateRefInstructions := m.outbuf.String()
			opts := m.runOpts()
			opts.Stdin = strings.NewReader(updateRefInstructions)
			if err := git.NewCommand(m.ctx, "update-ref", "--stdin").Run(opts); err != nil {
				return fmt.Errorf(
					"Failed to update ref: %w\n%s\n%s",
					err,
					limitString(m.outbuf.String()),
					limitString(m.errbuf.String()),
				)
			}
			// Checkout staging branch
			if err := git.NewCommand(m.ctx, "checkout").AddDynamicArguments("staging").
				Run(m.runOpts()); err != nil {
				return fmt.Errorf(
					"unable to git checkout staging in the scratch repository: %w\n%s\n%s",
					err,
					limitString(m.outbuf.String()),
					limitString(m.errbuf.String()),
				)
			}
			m.outbuf.Reset()
			m.errbuf.Reset()
			return nil
		}
		// git-replay doesn't tell us which commit first created a merge conflict.
		// In order to preserve the quality of our error messages, fall back to
		// regular git-rebase.
	}

	// The available git version is too old to support git-replay, or git-replay
	// failed and we want to determine the first commit that produced a
	// merge-conflict. Fall back to regular rebase.

	// Checkout head branch
	if err := git.NewCommand(m.ctx, "checkout").AddDynamicArguments("staging").
		Run(m.runOpts()); err != nil {
		return fmt.Errorf("unable to git checkout staging in the scratch repository: %w\n%s\n%s", err, limitString(m.outbuf.String()), limitString(m.errbuf.String()))
	}
	m.outbuf.Reset()
	m.errbuf.Reset()

	// Rebase before merging
	if err := git.NewCommand(m.ctx, "rebase").AddDynamicArguments("base").
		Run(m.runOpts()); err != nil {
		stdout, stderr := limitString(m.outbuf.String()), limitString(m.errbuf.String())
		// Rebase will leave a REBASE_HEAD file in .git if there is a conflict
		if _, statErr := os.Stat(filepath.Join(m.dir, ".git", "REBASE_HEAD")); statErr == nil {
			var commitSha string
			ok := false
			failingCommitPaths := []string{
				filepath.Join(m.dir, ".git", "rebase-apply", "original-commit"), // Git < 2.26
				filepath.Join(m.dir, ".git", "rebase-merge", "stopped-sha"),     // Git >= 2.26
			}
			for _, failingCommitPath := range failingCommitPaths {
				if _, statErr := os.Stat(failingCommitPath); statErr == nil {
					commitShaBytes, readErr := os.ReadFile(failingCommitPath)
					if readErr != nil {
						// Abandon this attempt to handle the error
						return fmt.Errorf("unable to git rebase staging on to base in the scratch repository: %w\n%s\n%s", err, stdout, stderr)
					}
					commitSha = strings.TrimSpace(string(commitShaBytes))
					ok = true
					break
				}
			}
			if !ok {
				log.Error("Unable to determine failing commit sha for failing rebase in the scratch repository. Cannot cast as a rebase conflict.")
				return fmt.Errorf("unable to git rebase staging on to base in the scratch repository: %w\n%s\n%s", err, stdout, stderr)
			}
			log.Debug("Conflict when rebasing staging on to base at %s: %v\n%s\n%s", commitSha, err, stdout, stderr)
			return &repostore.ConflictError{
				Style:     style,
				Rebase:    true,
				CommitSHA: commitSha,
				StdOut:    stdout,
				StdErr:    stderr,
				Err:       err,
			}
		}
		return fmt.Errorf("unable to git rebase staging on to base in the scratch repository: %w\n%s\n%s", err, stdout, stderr)
	}
	m.outbuf.Reset()
	m.errbuf.Reset()
	return nil
}
