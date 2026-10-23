// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package dual

import (
	"crypto/sha256"
	"encoding/binary"
	"hash"
	"iter"
	"slices"
	"strconv"

	"forgejo.org/modules/git"
)

// fingerprint is a digest of the part of an answer that both backends can give. Comparing digests
// keeps no reference to the answer, so a caller may change what it was given after the call returns.
type fingerprint struct {
	sum   [sha256.Size]byte
	items int
	// head is a short excerpt of ids, counts and names (never file content) for a log line.
	head string
}

func (f fingerprint) equal(o fingerprint) bool { return f.sum == o.sum && f.items == o.items }

// fpBuilder writes a canonical encoding of an answer into a digest.
type fpBuilder struct {
	h     hash.Hash
	items int
	head  []byte
}

func newFP() *fpBuilder { return &fpBuilder{h: sha256.New()} }

// text adds s to the digest only: use it for names and content.
func (b *fpBuilder) text(s string) *fpBuilder {
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], uint64(len(s)))
	b.h.Write(n[:])
	_, _ = b.h.Write([]byte(s))
	return b
}

// ident adds s to the digest and to the log excerpt: use it for ids and short labels.
func (b *fpBuilder) ident(s string) *fpBuilder {
	b.text(s)
	if len(b.head) < maxHeadBytes {
		if len(b.head) > 0 {
			b.head = append(b.head, ' ')
		}
		for i := 0; i < len(s) && len(b.head) < maxHeadBytes; i++ {
			c := s[i]
			if c < 0x20 || c >= 0x7f {
				c = '?'
			}
			b.head = append(b.head, c)
		}
	}
	return b
}

func (b *fpBuilder) num(n int64) *fpBuilder {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(n))
	b.h.Write(buf[:])
	return b
}

func (b *fpBuilder) flag(v bool) *fpBuilder {
	if v {
		return b.num(1)
	}
	return b.num(0)
}

// item marks the end of one element of a list.
func (b *fpBuilder) item() *fpBuilder {
	b.items++
	return b.num(int64(b.items))
}

func (b *fpBuilder) done() fingerprint {
	f := fingerprint{items: b.items, head: string(b.head)}
	b.h.Sum(f.sum[:0])
	return f
}

func idString(id git.ObjectID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

func (b *fpBuilder) sig(s *git.Signature) {
	if s == nil {
		b.flag(false)
		return
	}
	_, offset := s.When.Zone()
	b.flag(true).text(s.Name).text(s.Email).num(s.When.Unix()).num(int64(offset))
}

// commit adds a commit. The message and signatures go to the digest only.
func (b *fpBuilder) commit(c *git.CommitData) {
	if c == nil {
		b.flag(false)
		return
	}
	b.flag(true).ident(idString(c.ID)).ident(idString(c.Tree)).num(int64(len(c.Parents)))
	for _, p := range c.Parents {
		b.ident(idString(p))
	}
	b.sig(c.Author)
	b.sig(c.Committer)
	b.text(c.Message)
	if c.Signature == nil {
		b.flag(false)
	} else {
		b.flag(true).text(c.Signature.Signature).text(c.Signature.Payload)
	}
}

func fpCommit(c *git.CommitData) fingerprint {
	b := newFP()
	b.commit(c)
	return b.item().done()
}

// fpTree fingerprints one tree level in the order given. Sizes are not compared: git does not load
// them and asc reports 0 for a tree.
func fpTree(entries []git.TreeEntryData) fingerprint {
	b := newFP()
	for _, e := range entries {
		b.text(e.Name).ident(idString(e.ID)).num(int64(e.Mode)).item()
	}
	return b.done()
}

// treeRow is one entry of a recursive walk, as a string that sorts by path.
func treeRow(e git.TreeEntryData) string {
	return e.Name + "\x00" + idString(e.ID) + "\x00" + strconv.Itoa(int(e.Mode))
}

// fpRows fingerprints rows as a set: the order is not compared.
func fpRows(rows []string) fingerprint {
	slices.Sort(rows)
	b := newFP()
	for _, r := range rows {
		b.ident(r).item()
	}
	return b.done()
}

// fpWalk collects a recursive tree walk, up to maxCompareItems entries.
func fpWalk(seq iter.Seq2[git.TreeEntryData, error]) (fingerprint, error) {
	var rows []string
	for e, err := range seq {
		if err != nil {
			return fingerprint{}, err
		}
		if len(rows) >= maxCompareItems {
			return fingerprint{}, errOverLimit
		}
		rows = append(rows, treeRow(e))
	}
	return fpRows(rows), nil
}

func fpString(s string) fingerprint { return newFP().ident(s).item().done() }

func fpInt(n int64) fingerprint { return newFP().ident(strconv.FormatInt(n, 10)).item().done() }

func fpBool(v bool) fingerprint {
	if v {
		return fpString("true")
	}
	return fpString("false")
}

// fpStat fingerprints StatObjects answers. Tag ids (skip) are left out: asc cannot read them. Only a
// blob has a size on both sides.
func fpStat(infos []git.ObjectInfo, skip []bool) fingerprint {
	b := newFP()
	for i, in := range infos {
		if i < len(skip) && skip[i] {
			continue
		}
		b.ident(idString(in.ID)).flag(in.Missing)
		if in.Missing {
			b.item()
			continue
		}
		b.ident(string(in.Type))
		if in.Type == git.ObjectBlob {
			b.num(in.Size)
		}
		b.item()
	}
	return b.done()
}

// fpRefs fingerprints a reference list as a set. An annotated tag is compared by name only: asc lists
// the commit it peels to where git lists the tag object.
func fpRefs(refs []git.Reference, annotated map[string]bool) fingerprint {
	rows := make([]string, 0, len(refs))
	for _, r := range refs {
		if annotated[r.Name] {
			rows = append(rows, r.Name)
			continue
		}
		rows = append(rows, r.Name+"\x00"+idString(r.Object))
	}
	return fpRows(rows)
}

func annotatedTags(refs []git.Reference) map[string]bool {
	var out map[string]bool
	for _, r := range refs {
		if r.Type == string(git.ObjectTag) {
			if out == nil {
				out = map[string]bool{}
			}
			out[r.Name] = true
		}
	}
	return out
}

// fpPaths fingerprints a list of paths as a set.
func fpPaths(changes []git.PathChange) fingerprint {
	rows := make([]string, 0, len(changes))
	for _, c := range changes {
		rows = append(rows, c.Path)
	}
	return fpRows(rows)
}

func fpDiffStat(s git.DiffStat) fingerprint {
	return newFP().num(int64(s.NumFiles)).num(int64(s.Additions)).num(int64(s.Deletions)).item().done()
}

// fpLastCommits fingerprints a map of names to commit ids.
func fpLastCommits(m map[string]git.ObjectID) fingerprint {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	b := newFP()
	for _, k := range keys {
		b.text(k).ident(idString(m[k])).item()
	}
	return b.done()
}

func fpLanguages(m map[string]int64) fingerprint {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	b := newFP()
	for _, k := range keys {
		b.ident(k).num(m[k]).item()
	}
	return b.done()
}

func fpAttributes(m map[string]map[string]string) fingerprint {
	paths := make([]string, 0, len(m))
	for p := range m {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	b := newFP()
	for _, p := range paths {
		b.text(p)
		names := make([]string, 0, len(m[p]))
		for n := range m[p] {
			names = append(names, n)
		}
		slices.Sort(names)
		for _, n := range names {
			b.text(n).text(m[p][n])
		}
		b.item()
	}
	return b.done()
}

// fpGrep fingerprints search results by file, with line numbers and line text. The order of files
// is not compared; highlight ranges are not compared.
func fpGrep(results []*git.GrepResult) fingerprint {
	sorted := make([]*git.GrepResult, 0, len(results))
	for _, r := range results {
		if r != nil {
			sorted = append(sorted, r)
		}
	}
	slices.SortStableFunc(sorted, func(a, b *git.GrepResult) int {
		switch {
		case a.Filename < b.Filename:
			return -1
		case a.Filename > b.Filename:
			return 1
		}
		return 0
	})
	b := newFP()
	for _, r := range sorted {
		b.ident(r.Filename).num(int64(len(r.LineNumbers)))
		for _, n := range r.LineNumbers {
			b.num(int64(n))
		}
		b.num(int64(len(r.LineCodes)))
		for _, c := range r.LineCodes {
			b.text(c)
		}
		b.item()
	}
	return b.done()
}

// blamePart adds one blame run. PreviousSha and PreviousPath are not compared (ascbackend documents
// that it cannot honour .git-blame-ignore-revs and reports the origin path).
func (b *fpBuilder) blamePart(p git.BlamePart) {
	b.ident(p.Sha).num(int64(len(p.Lines)))
	for _, l := range p.Lines {
		b.text(l)
	}
}

// drain collects an ordered stream into a digest, up to maxCompareItems elements.
func drain[T any](seq iter.Seq2[T, error], add func(*fpBuilder, T)) (fingerprint, error) {
	b := newFP()
	for v, err := range seq {
		if err != nil {
			return fingerprint{}, err
		}
		if b.items >= maxCompareItems {
			return fingerprint{}, errOverLimit
		}
		add(b, v)
		b.item()
	}
	return b.done(), nil
}
