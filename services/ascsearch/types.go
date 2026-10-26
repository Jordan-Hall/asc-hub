// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascsearch

import (
	"math"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	api "forgejo.org/modules/structs"
)

// Scope says what an answer was computed against.
type Scope = api.AscScope

// ScopeOf checks and converts a resolved scope; a coverage outside 0..1 is reported as zero.
func ScopeOf(s *ascv1.ResolvedScope) *Scope {
	if s == nil {
		return nil
	}
	c := s.GetCoverage()
	if math.IsNaN(float64(c)) || c < 0 || c > 1 {
		c = 0
	}
	return &Scope{Changeset: Hex(s.GetChangeset()), Root: Hex(s.GetRoot()), Generation: s.GetGeneration(), Coverage: c}
}

// LineMatch is one matching line of a file with its context.
type LineMatch = api.AscLineMatch

// FileHit is one file with matches. A redacted hit carries line numbers only.
type FileHit = api.AscFileHit

const (
	maxMatchesPerFile = 50
	maxAlsoAt         = 5
	maxFilesPerRepo   = 200
)

// FileHitOf checks and converts a hit. It returns false when the hit has no usable path, so a hit that
// asc built badly is dropped instead of drawn.
func FileHitOf(h *ascv1.FileHit) (FileHit, bool) {
	path, ok := Path(h.GetPath())
	if !ok {
		return FileHit{}, false
	}
	out := FileHit{
		Path:     path,
		FileID:   Hex(h.GetFileId()),
		Lang:     Label(h.GetLang()),
		Matches:  []LineMatch{},
		Redacted: h.GetRedacted(),
	}
	for _, a := range h.GetAlsoAt() {
		if len(out.AlsoAt) >= maxAlsoAt {
			break
		}
		if p, ok := Path(a); ok {
			out.AlsoAt = append(out.AlsoAt, p)
		}
	}
	for _, m := range h.GetMatches() {
		if len(out.Matches) >= maxMatchesPerFile {
			break
		}
		lm := LineMatch{Line: m.GetLine()}
		// A redacted hit shows its line numbers only, whatever text the message carried.
		if !h.GetRedacted() {
			lm.Text, lm.Cols = CleanAt(m.GetText(), maxLineBytes, m.GetCols())
			for _, b := range m.GetBefore() {
				if len(lm.Before) < MaxContext {
					lm.Before = append(lm.Before, Line(b))
				}
			}
			for _, a := range m.GetAfter() {
				if len(lm.After) < MaxContext {
					lm.After = append(lm.After, Line(a))
				}
			}
		}
		out.Matches = append(out.Matches, lm)
	}
	return out, true
}

// RepoResult is the hits of one repository.
type RepoResult = api.AscRepoHits

// Symbol is one symbol occurrence.
type Symbol = api.AscSymbol

const maxLine = 1 << 24

// Confidences are the confidences the server defines (SPEC 23.4.1).
var confidences = map[string]bool{"precise": true, "import_match": true, "name_match": true}

// SymbolOf checks and converts a symbol. It returns false for a symbol with a bad path, absurd lines or an
// unknown confidence, so such a symbol is dropped.
func SymbolOf(s *ascv1.Symbol) (Symbol, bool) {
	path, ok := Path(s.GetPath())
	if !ok {
		return Symbol{}, false
	}
	if s.GetLine() > maxLine || s.GetEndLine() > maxLine || (s.GetEndLine() != 0 && s.GetEndLine() < s.GetLine()) {
		return Symbol{}, false
	}
	if c := s.GetConfidence(); c != "" && !confidences[c] {
		return Symbol{}, false
	}
	return Symbol{
		Name: Clean([]byte(s.GetName()), MaxName), Kind: Label(s.GetKind()), IsDef: s.GetIsDef(),
		Path: path, Line: s.GetLine(), EndLine: s.GetEndLine(),
		Signature: Clean(s.GetSignature(), 300), Container: Label(s.GetContainer()),
		Doc: Doc(s.GetDoc()), Confidence: s.GetConfidence(),
	}, true
}

// SymbolHit is a symbol in a repository.
type SymbolHit = api.AscSymbolHit

// Chunk is one ranked piece of code of the meaning search.
type Chunk = api.AscChunk

const maxSources = 8

// ChunkOf checks and converts a chunk. It returns false for a chunk with a bad path or lines.
func ChunkOf(repo string, c *ascv1.Chunk) (Chunk, bool) {
	path, ok := Path(c.GetPath())
	if !ok {
		return Chunk{}, false
	}
	if c.GetStartLine() > maxLine || c.GetEndLine() > maxLine || (c.GetEndLine() != 0 && c.GetEndLine() < c.GetStartLine()) {
		return Chunk{}, false
	}
	score := c.GetScore()
	if math.IsNaN(score) || math.IsInf(score, 0) {
		score = 0
	}
	out := Chunk{
		Repo: repo, Path: path, FileID: Hex(c.GetFileId()), StartLine: c.GetStartLine(), EndLine: c.GetEndLine(),
		Symbol: Clean([]byte(c.GetSymbol()), MaxName), Score: score, Redacted: c.GetRedacted(),
	}
	for _, s := range c.GetSources() {
		if len(out.Sources) >= maxSources {
			break
		}
		out.Sources = append(out.Sources, Label(s))
	}
	if !c.GetRedacted() { // a redacted chunk shows no text, whatever the message carried
		out.Text = Block(c.GetText())
	}
	return out, true
}
