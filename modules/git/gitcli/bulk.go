// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package gitcli

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"forgejo.org/modules/git"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/util"
)

// maxAttributePaths bounds the paths one Attributes call asks about.
const maxAttributePaths = 4096

// Archive writes an archive of the tree at to w. A non-empty prefix is put before every path.
func (b *Backend) Archive(ctx context.Context, at git.ObjectID, format git.ArchiveType, prefix string, w io.Writer) error {
	if format.String() == "unknown" {
		return fmt.Errorf("unknown format: %v", format)
	}

	cmd := git.NewCommand(ctx, "archive")
	if prefix != "" {
		cmd.AddOptionFormat("--prefix=%s", prefix)
	}
	cmd.AddOptionFormat("--format=%s", format.String())
	cmd.AddDynamicArguments(at.String())

	// Avoid LFS hooks getting installed because of /etc/gitconfig, which can break pull requests.
	env := append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")

	var stderr strings.Builder
	err := cmd.Run(&git.RunOpts{
		Dir:    b.repo.Path,
		Stdout: w,
		Stderr: &stderr,
		Env:    env,
	})
	if err != nil {
		return git.ConcatenateError(err, stderr.String())
	}
	return nil
}

func hasPrefixFold(s, t string) bool {
	if len(s) < len(t) {
		return false
	}
	return strings.EqualFold(s[:len(t)], t)
}

// Grep runs git grep for q.Search in the tree at.
func (b *Backend) Grep(ctx context.Context, at git.ObjectID, q git.GrepQuery) ([]*git.GrepResult, error) {
	search, opts := q.Search, q.Options
	opts.MaxResultLimit = cmp.Or(opts.MaxResultLimit, 50)
	opts.MatchesPerFile = cmp.Or(opts.MatchesPerFile, 20)

	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("unable to create os pipe to grep: %w", err)
	}
	defer func() {
		_ = stdoutReader.Close()
		_ = stdoutWriter.Close()
	}()

	/*
	 The output is like this ("^@" means \x00; the first number denotes the line,
	 the second number denotes the column of the first match in line):

	 HEAD:.air.toml
	 6^@8^@bin = "gitea"

	 HEAD:.changelog.yml
	 2^@10^@repo: go-gitea/gitea
	*/
	var results []*git.GrepResult
	// -I skips binary files
	cmd := git.NewCommand(ctx, "grep",
		"-I", "--null", "--break", "--heading",
		"--line-number", "--ignore-case", "--full-name")
	if opts.Mode == git.RegExpGrepMode {
		// No `--column` -- regexp mode does not support highlighting in the
		// current implementation as the length of the match is unknown from
		// `grep` but required for highlighting.
		cmd.AddArguments("--perl-regexp")
	} else {
		cmd.AddArguments("--fixed-strings", "--column")
	}

	cmd.AddOptionValues("--context", fmt.Sprint(opts.ContextLineNumber))

	if git.SupportGrepMaxCount {
		cmd.AddOptionValues("--max-count", fmt.Sprint(opts.MatchesPerFile))
	} else {
		log.Warn("git-grep: --max-count requires at least git 2.38")
	}

	words := []string{search}
	if opts.Mode == git.FixedAnyGrepMode {
		words = strings.Fields(search)
	}
	for _, word := range words {
		cmd.AddGitGrepExpression(word)
	}

	// pathspec
	includeLen := len(setting.Indexer.IncludePatterns)
	if len(opts.Filename) > 0 {
		includeLen = 1
	}
	files := make([]string, 0, len(setting.Indexer.ExcludePatterns)+includeLen)
	if len(opts.Filename) > 0 && len(setting.Indexer.IncludePatterns) > 0 {
		// if the both a global include pattern and the per search path is defined
		// we only include results where the path matches the globally set pattern
		// (eg, global pattern = "src/**" and path = "node_modules/")

		// FIXME: this is a bit too restrictive, and fails to consider cases where the
		// globally set include pattern refers to a file than a directory
		// (eg, global pattern = "**.go" and path = "modules/git")
		exprMatched := false
		for _, expr := range setting.Indexer.IncludePatterns {
			if expr.Match(opts.Filename) {
				files = append(files, ":(literal)"+opts.Filename)
				exprMatched = true
				break
			}
		}
		if !exprMatched {
			log.Warn("git-grep: filepath %s does not match any include pattern", opts.Filename)
		}
	} else if len(opts.Filename) > 0 {
		// if the path is only set we just include results that matches it
		files = append(files, ":(literal)"+opts.Filename)
	} else {
		// otherwise if global include patterns are set include results that strictly match them
		for _, expr := range setting.Indexer.IncludePatterns {
			files = append(files, ":"+expr.Pattern())
		}
	}
	for _, expr := range setting.Indexer.ExcludePatterns {
		files = append(files, ":^"+expr.Pattern())
	}
	cmd.AddDynamicArguments(at.String()).AddDashesAndList(files...)

	stderr := bytes.Buffer{}
	err = cmd.Run(&git.RunOpts{
		Timeout: time.Duration(setting.Git.Timeout.Grep) * time.Second,

		Dir:    b.repo.Path,
		Stdout: stdoutWriter,
		Stderr: &stderr,
		PipelineFunc: func(ctx context.Context, cancel context.CancelFunc) error {
			_ = stdoutWriter.Close()
			defer stdoutReader.Close()

			isInBlock := false
			scanner := bufio.NewReader(stdoutReader)
			var res *git.GrepResult
			for {
				line, err := scanner.ReadString('\n')
				if err != nil {
					if err == io.EOF {
						return nil
					}
					return err
				}
				// Remove delimiter.
				if len(line) > 0 {
					line = line[:len(line)-1]
				}

				if !isInBlock {
					if _ /* ref */, filename, ok := strings.Cut(line, ":"); ok {
						isInBlock = true
						res = &git.GrepResult{Filename: filename}
						results = append(results, res)
					}
					continue
				}
				if line == "" {
					if len(results) >= opts.MaxResultLimit {
						cancel()
						break
					}
					isInBlock = false
					continue
				}
				if line == "--" {
					continue
				}
				if lineNum, lineCode, ok := strings.Cut(line, "\x00"); ok {
					lineNumInt, _ := strconv.Atoi(lineNum)
					res.LineNumbers = append(res.LineNumbers, lineNumInt)
					// We support highlighting only when `--column` parameter is used.
					if lineCol, lineCode2, ok := strings.Cut(lineCode, "\x00"); ok {
						lineColInt, _ := strconv.Atoi(lineCol)
						start := lineColInt - 1
						matchLen := len(lineCode2)
						for _, word := range words {
							if hasPrefixFold(lineCode2[start:], word) {
								matchLen = len(word)
								break
							}
						}
						res.HighlightedRanges = append(res.HighlightedRanges, [3]int{
							len(res.LineCodes),
							start,
							start + matchLen,
						})
						res.LineCodes = append(res.LineCodes, lineCode2)
						continue
					}
					res.LineCodes = append(res.LineCodes, lineCode)
				}
			}
			return nil
		},
	})
	// git grep exits by cancel (killed), usually it is caused by the limit of results
	if git.IsErrorExitCode(err, -1) && stderr.Len() == 0 {
		return results, nil
	}
	// git grep exits with 1 if no results are found
	if git.IsErrorExitCode(err, 1) && stderr.Len() == 0 {
		return nil, nil
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		return nil, fmt.Errorf("unable to run git grep: %w, stderr: %s", err, stderr.String())
	}
	return results, nil
}

// Attributes reads the named git attributes of each path in the tree at.
func (b *Backend) Attributes(ctx context.Context, at git.ObjectID, paths, names []string) (map[string]map[string]string, error) {
	if len(paths) > maxAttributePaths {
		return nil, util.NewInvalidArgumentErrorf("attributes asked for %d paths, the limit is %d", len(paths), maxAttributePaths)
	}
	res := make(map[string]map[string]string, len(paths))
	for _, path := range paths {
		attrs, err := b.repo.GitAttributes(at.String(), path, names...)
		if err != nil {
			return nil, err
		}
		values := make(map[string]string, len(attrs))
		for name, attr := range attrs {
			values[name] = string(attr)
		}
		res[path] = values
	}
	return res, nil
}

// LanguageStats measures the languages of the tree at.
func (b *Backend) LanguageStats(_ context.Context, at git.ObjectID) (map[string]int64, error) {
	return b.repo.GetLanguageStats(at.String())
}
