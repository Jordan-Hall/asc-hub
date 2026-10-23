// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package ascbackend

import (
	"bytes"
	"cmp"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"regexp"
	"slices"
	"strings"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/util"
)

// capWriter fails the write that would take the total over max.
type capWriter struct {
	w       io.Writer
	max     int64
	written int64
}

func (c *capWriter) Write(p []byte) (int, error) {
	if c.written+int64(len(p)) > c.max {
		return 0, fmt.Errorf("%w: more than %d bytes", ErrTooLarge, c.max)
	}
	n, err := c.w.Write(p)
	c.written += int64(n)
	return n, err
}

// Archive writes an archive of the tree at to w. TARGZ and ZIP are supported; a bundle is not, because
// it carries history, which the server does not export through this surface.
func (b *Backend) Archive(ctx context.Context, at git.ObjectID, format git.ArchiveType, prefix string, w io.Writer) error {
	if err := requireSha1(at); err != nil {
		return err
	}
	var wireFormat string
	gzipped := false
	switch format {
	case git.ZIP:
		wireFormat = "zip"
	case git.TARGZ:
		wireFormat, gzipped = "tar", true
	case git.BUNDLE:
		return fmt.Errorf("%w: bundle archives", ErrUnsupported)
	default:
		return util.NewInvalidArgumentErrorf("unknown archive format %v", format)
	}
	if strings.ContainsAny(prefix, "\x00\n") || len(prefix) > maxPathBytes {
		return util.NewInvalidArgumentErrorf("invalid archive prefix")
	}
	ctx, cancel, err := b.bulk(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	st, err := b.api.Archive(ctx, &ascv1.ArchiveRequest{Repo: b.gitRef(), Rev: at.String(), Format: wireFormat, Prefix: prefix})
	if err != nil {
		return callErr("Archive", at.String(), err)
	}
	var out io.Writer = &capWriter{w: w, max: maxArchiveBytes}
	var gz *gzip.Writer
	if gzipped {
		gz = gzip.NewWriter(out)
		out = gz
	}
	var received int64
	for {
		msg, err := st.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return callErr("Archive", at.String(), err)
		}
		if received += int64(len(msg.GetData())); received > maxArchiveBytes {
			return fmt.Errorf("%w: archive of more than %d bytes", ErrTooLarge, int64(maxArchiveBytes))
		}
		if _, err := out.Write(msg.GetData()); err != nil {
			return err
		}
	}
	if gz != nil {
		return gz.Close()
	}
	return nil
}

// grepPathspecs builds the pathspecs of a search in asc's syntax, like the git backend does for git's.
func grepPathspecs(filename string) []string {
	var files []string
	switch {
	case filename != "" && len(setting.Indexer.IncludePatterns) > 0:
		// A search path and a global include pattern: the path must match the pattern.
		matched := false
		for _, expr := range setting.Indexer.IncludePatterns {
			if expr.Match(filename) {
				files = append(files, ":(literal)"+filename)
				matched = true
				break
			}
		}
		if !matched {
			log.Warn("asc-grep: filepath %s does not match any include pattern", filename)
		}
	case filename != "":
		files = append(files, ":(literal)"+filename)
	default:
		for _, expr := range setting.Indexer.IncludePatterns {
			files = append(files, ":(glob)"+expr.Pattern())
		}
	}
	for _, expr := range setting.Indexer.ExcludePatterns {
		files = append(files, ":(exclude,glob)"+expr.Pattern())
	}
	return files
}

// highlighter finds the first match of the search words in a line, ignoring case.
func highlighter(words []string) *regexp.Regexp {
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = regexp.QuoteMeta(w)
	}
	return regexp.MustCompile("(?i)(?:" + strings.Join(quoted, "|") + ")")
}

// Grep searches the tree at for q.Search. The server matches with Rust regular expressions, not PCRE,
// and walks files in path order; a regular expression it cannot parse is an error.
func (b *Backend) Grep(ctx context.Context, at git.ObjectID, q git.GrepQuery) ([]*git.GrepResult, error) {
	if err := requireSha1(at); err != nil {
		return nil, err
	}
	search, opts := q.Search, q.Options
	opts.MaxResultLimit = cmp.Or(opts.MaxResultLimit, 50)
	opts.MatchesPerFile = cmp.Or(opts.MatchesPerFile, 20)
	if opts.ContextLineNumber < 0 || opts.ContextLineNumber > 100 {
		return nil, util.NewInvalidArgumentErrorf("grep context of %d lines", opts.ContextLineNumber)
	}

	words := []string{search}
	if opts.Mode == git.FixedAnyGrepMode {
		words = strings.Fields(search)
	}
	if len(words) == 0 || search == "" {
		return nil, util.NewInvalidArgumentErrorf("grep needs something to search for")
	}
	req := &ascv1.GrepRequest{
		Repo: b.gitRef(), Rev: at.String(), IgnoreCase: true,
		Paths: grepPathspecs(opts.Filename), MaxResults: maxGrepMatches,
	}
	var hl *regexp.Regexp
	switch opts.Mode {
	case git.RegExpGrepMode:
		req.Pattern = search
	case git.FixedAnyGrepMode:
		quoted := make([]string, len(words))
		for i, w := range words {
			quoted[i] = regexp.QuoteMeta(w)
		}
		req.Pattern = strings.Join(quoted, "|")
		hl = highlighter(words)
	default:
		req.Pattern, req.Fixed = search, true
		hl = highlighter(words)
	}

	ctx, cancel, err := b.bulk(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	st, err := b.api.Grep(ctx, req)
	if err != nil {
		return nil, callErr("Grep", at.String(), err)
	}

	type hit struct {
		line int
		text string
	}
	type file struct {
		name string
		hits []hit
	}
	var files []*file
recv:
	for {
		msg, err := st.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, callErr("Grep", at.String(), err)
		}
		for _, m := range msg.GetMatches() {
			name := string(m.GetPath())
			if name == "" || len(name) > maxPathBytes || m.GetLine() == 0 {
				return nil, badAnswer("grep match without a path or line")
			}
			if len(files) == 0 || files[len(files)-1].name != name {
				if slices.ContainsFunc(files, func(f *file) bool { return f.name == name }) {
					return nil, badAnswer("grep matches of a file are not together")
				}
				if len(files) >= opts.MaxResultLimit {
					break recv
				}
				files = append(files, &file{name: name})
			}
			f := files[len(files)-1]
			if len(f.hits) < opts.MatchesPerFile {
				f.hits = append(f.hits, hit{line: int(m.GetLine()), text: string(m.GetText())})
			}
		}
	}
	cancel() // the stream is done; context lines below make their own calls
	ctx = context.WithoutCancel(ctx)

	results := make([]*git.GrepResult, 0, len(files))
	for _, f := range files {
		res := &git.GrepResult{Filename: f.name}
		var fileLines [][]byte
		if opts.ContextLineNumber > 0 {
			fileLines = b.contextLines(ctx, at, f.name)
		}
		texts := make(map[int]string, len(f.hits))
		show := map[int]bool{}
		for _, h := range f.hits {
			texts[h.line] = h.text
			show[h.line] = true
			for n := max(h.line-opts.ContextLineNumber, 1); n <= h.line+opts.ContextLineNumber && n <= len(fileLines); n++ {
				show[n] = true
			}
		}
		for _, n := range slices.Sorted(maps.Keys(show)) {
			text, isHit := texts[n]
			if !isHit {
				text = string(fileLines[n-1])
			} else if hl != nil {
				if loc := hl.FindStringIndex(text); loc != nil {
					res.HighlightedRanges = append(res.HighlightedRanges, [3]int{len(res.LineCodes), loc[0], loc[1]})
				}
			}
			res.LineNumbers = append(res.LineNumbers, n)
			res.LineCodes = append(res.LineCodes, text)
		}
		results = append(results, res)
	}
	if len(results) == 0 {
		return nil, nil
	}
	return results, nil
}

// contextLines reads a file for its context lines. A file that cannot be read gives no context.
func (b *Backend) contextLines(ctx context.Context, at git.ObjectID, path string) [][]byte {
	data, err := b.readFileAt(ctx, at.String(), path, maxContextFileBytes)
	if err != nil {
		log.Debug("ascbackend: context lines of %s: %v", path, err)
		return nil
	}
	if len(data) == 0 {
		return nil
	}
	// A final newline ends the last line; it does not start another.
	return bytes.Split(bytes.TrimSuffix(data, []byte{'\n'}), []byte{'\n'})
}

// Attributes reads the named git attributes of each path in the tree at. A name that is not set
// for a path has the value "unspecified", as in git check-attr.
func (b *Backend) Attributes(ctx context.Context, at git.ObjectID, paths, names []string) (map[string]map[string]string, error) {
	if err := requireSha1(at); err != nil {
		return nil, err
	}
	if len(paths) > maxAttributePaths {
		return nil, util.NewInvalidArgumentErrorf("attributes asked for %d paths, the limit is %d", len(paths), maxAttributePaths)
	}
	if len(names) == 0 || len(names) > maxAttributeNames {
		return nil, util.NewInvalidArgumentErrorf("attributes asked for %d names, the limit is %d", len(names), maxAttributeNames)
	}
	res := make(map[string]map[string]string, len(paths))
	if len(paths) == 0 {
		return res, nil
	}
	wire := make([][]byte, len(paths))
	for i, p := range paths {
		if p == "" || len(p) > maxPathBytes || strings.IndexByte(p, 0) >= 0 {
			return nil, util.NewInvalidArgumentErrorf("attributes asked for an invalid path")
		}
		wire[i] = []byte(p)
	}
	ctx, cancel, err := b.unary(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	resp, err := b.api.CheckAttr(ctx, &ascv1.CheckAttrRequest{Repo: b.gitRef(), Rev: at.String(), Paths: wire, Names: names})
	if err != nil {
		return nil, callErr("CheckAttr", at.String(), err)
	}
	if len(resp.GetPerPath()) != len(paths) {
		return nil, badAnswer("CheckAttr answered %d paths for %d", len(resp.GetPerPath()), len(paths))
	}
	for i, p := range paths {
		values := make(map[string]string, len(names))
		got := resp.GetPerPath()[i].GetValues()
		for _, n := range names {
			v, ok := got[n]
			if !ok {
				v = "unspecified"
			}
			values[n] = v
		}
		res[p] = values
	}
	return res, nil
}
