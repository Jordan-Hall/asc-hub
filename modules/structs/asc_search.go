// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package structs

// AscScope says what an answer was computed against.
// swagger:model
type AscScope struct {
	// the changeset the answer was computed at, as hex
	Changeset string `json:"changeset,omitempty"`
	// the root tree of that changeset, as hex
	Root string `json:"root,omitempty"`
	// the generation of the index the answer used
	Generation uint64 `json:"generation"`
	// the share (0 to 1) of the scope the index covers
	Coverage float32 `json:"coverage"`
}

// AscLineMatch is one matching line of a file with its context. The text is already made safe to show; it is
// empty for a redacted hit.
// swagger:model
type AscLineMatch struct {
	// the 1-based line number
	Line uint32 `json:"line"`
	// the byte columns of the matches in the line
	Cols []uint32 `json:"cols,omitempty"`
	// the line
	Text string `json:"text"`
	// the lines before the match
	Before []string `json:"before,omitempty"`
	// the lines after the match
	After []string `json:"after,omitempty"`
}

// AscFileHit is a file with matches. A redacted hit carries line numbers only, because the server found a
// secret in the file.
// swagger:model
type AscFileHit struct {
	// the path in the repository
	Path string `json:"path"`
	// the id of the file content, as hex
	FileID string `json:"file_id,omitempty"`
	// the language of the file
	Lang string `json:"lang,omitempty"`
	// up to 5 other paths of the same file
	AlsoAt []string `json:"also_at,omitempty"`
	// the matching lines
	Matches []AscLineMatch `json:"matches"`
	// whether the lines are hidden because they may hold secrets
	Redacted bool `json:"redacted"`
}

// AscRepoHits is the hits of one repository.
// swagger:model
type AscRepoHits struct {
	// the repository, "owner/name"
	Repo string `json:"repo"`
	// the revision that was searched
	Rev   string    `json:"rev,omitempty"`
	Scope *AscScope `json:"scope,omitempty"`
	// the files with hits
	Files []AscFileHit `json:"files"`
}

// AscSymbol is one occurrence of a symbol.
// swagger:model
type AscSymbol struct {
	// the name
	Name string `json:"name"`
	// the kind, such as fn, struct or const
	Kind string `json:"kind"`
	// whether this is the definition
	IsDef bool `json:"is_def"`
	// the path in the repository
	Path string `json:"path"`
	// the first line
	Line uint32 `json:"line"`
	// the last line
	EndLine uint32 `json:"end_line"`
	// the declaration
	Signature string `json:"signature,omitempty"`
	// the name of the enclosing symbol
	Container string `json:"container,omitempty"`
	// the documentation comment on one line
	Doc string `json:"doc,omitempty"`
	// how the match was found: precise (uploaded compiler index), import_match or name_match
	Confidence string `json:"confidence,omitempty"`
}

// AscSymbolHit is a symbol in a repository.
// swagger:model
type AscSymbolHit struct {
	// the repository, "owner/name"
	Repo string `json:"repo"`
	// the revision
	Rev    string    `json:"rev,omitempty"`
	Symbol AscSymbol `json:"symbol"`
	// how well the name matches, 0 to 1
	Score float32 `json:"score"`
}

// AscChunk is one ranked piece of code of the search by meaning.
// swagger:model
type AscChunk struct {
	// the repository, "owner/name"; left out by calls that search one repository
	Repo string `json:"repo,omitempty"`
	// the path in the repository
	Path string `json:"path"`
	// the id of the file content, as hex
	FileID string `json:"file_id,omitempty"`
	// the first line
	StartLine uint32 `json:"start_line"`
	// the last line
	EndLine uint32 `json:"end_line"`
	// the symbol the chunk belongs to
	Symbol string `json:"symbol,omitempty"`
	// the relevance
	Score float64 `json:"score"`
	// where the chunk came from, such as vector or lexical
	Sources []string `json:"sources,omitempty"`
	// the code; empty for a redacted chunk
	Text string `json:"text"`
	// whether the code is hidden because it may hold secrets
	Redacted bool `json:"redacted"`
}

// AscCoverage says how much of the searched scope the server has indexed.
// swagger:model
type AscCoverage struct {
	// the repositories asked about their index
	Asked int `json:"asked"`
	// the repositories that answered
	Known int `json:"known"`
	// whether there were more repositories than were asked
	Sampled bool `json:"sampled"`
	// the mean coverage in percent over the repositories that answered
	Percent int `json:"percent"`
	// whether every repository was asked, answered, and is fully indexed
	Complete bool `json:"complete"`
	// the largest lag of the index behind the repository, in seconds
	LagSeconds int64 `json:"lag_seconds"`
	// the revision the index covers, for one repository
	Revision string `json:"revision,omitempty"`
}

// AscFacet is a value with the number of files that have it.
// swagger:model
type AscFacet struct {
	// the language or the top-level directory
	Name string `json:"name"`
	// the number of files
	Count int `json:"count"`
}

// AscFileRefs groups occurrences of a symbol by file.
// swagger:model
type AscFileRefs struct {
	// the repository, "owner/name"
	Repo string `json:"repo"`
	// the path in the repository
	Path string `json:"path"`
	// the occurrences
	Symbols []AscSymbol `json:"symbols"`
}

// AscSymbolPanel is the definition of a symbol with its references and callers.
// swagger:model
type AscSymbolPanel struct {
	Scope *AscScope `json:"scope,omitempty"`
	// the definitions; more than one when the name is ambiguous
	Definitions []AscSymbolHit `json:"definitions"`
	// references in the repository, by file
	References []AscFileRefs `json:"references"`
	// callers in the repository, by file
	Callers []AscFileRefs `json:"callers"`
	// references in other repositories the caller can read, by file
	CrossRepo []AscFileRefs `json:"cross_repo"`
	// whether more exist than are listed
	Truncated bool `json:"truncated"`
	// Fallback says the panel was built from symbol calls (never serialised)
	Fallback bool `json:"-"`
}

// AscCrossSearchResult is the answer to a search over several repositories.
// swagger:model
type AscCrossSearchResult struct {
	// the repositories that have hits, newest first
	Repos []AscRepoHits `json:"repos"`
	// the number of file hits
	Files int `json:"files"`
	// whether more hits exist than are listed
	Truncated bool `json:"truncated"`
	// continues the search; only given when the search was one call of at most 64 repositories
	NextPageToken string `json:"next_page_token,omitempty"`
	// the repositories searched
	SearchedRepos int `json:"searched_repos"`
	// the repositories searched without an index, which is slower
	UnindexedRepos int `json:"unindexed_repos"`
	// the repositories whose search failed
	FailedRepos int `json:"failed_repos"`
	// the repositories the caller can read and that were in the search
	ReposTotal int `json:"repos_total"`
	// whether the caller can read more repositories than were searched
	ReposTruncated bool `json:"repos_truncated"`
	// an estimate of the tokens the answer costs an agent
	EstTokens uint32 `json:"est_tokens"`
	// files per language, most first
	Languages []AscFacet `json:"languages"`
	// files per top-level directory, most first
	Directories []AscFacet `json:"directories"`
}

// AscCrossSymbolsResult is the answer to a symbol lookup over several repositories.
// swagger:model
type AscCrossSymbolsResult struct {
	// the symbols, best match first
	Symbols []AscSymbolHit `json:"symbols"`
	// whether more exist than are listed
	Truncated bool `json:"truncated"`
	// continues the lookup; only given when the lookup was one call of at most 64 repositories
	NextPageToken string `json:"next_page_token,omitempty"`
	// the repositories whose lookup failed
	FailedRepos int `json:"failed_repos"`
	// the repositories the caller can read and that were in the lookup
	ReposTotal int `json:"repos_total"`
	// whether the caller can read more repositories than were looked in
	ReposTruncated bool `json:"repos_truncated"`
}

// AscCrossSemanticResult is the answer to a search by meaning over several repositories.
// swagger:model
type AscCrossSemanticResult struct {
	// the chunks, best first
	Chunks []AscChunk `json:"chunks"`
	// the embedding model
	Model string `json:"model,omitempty"`
	// the repositories that answered
	SearchedRepos int `json:"searched_repos"`
	// the repositories whose search failed
	FailedRepos int `json:"failed_repos"`
	// the repositories the caller can read and that were in the search
	ReposTotal int `json:"repos_total"`
	// whether the caller can read more repositories than were searched (a meaning search covers at most 16)
	ReposTruncated bool `json:"repos_truncated"`
	// an estimate of the tokens the answer costs an agent
	EstTokens uint32 `json:"est_tokens"`
	// whether the nearest-neighbour search ran out of candidates
	AnnExhausted bool `json:"ann_exhausted"`
}

// AscIndexStatusResult is how much of a repository the index covers.
// swagger:model
type AscIndexStatusResult struct {
	// the repository, "owner/name"
	Repo string `json:"repo"`
	// the revision
	Rev string `json:"rev"`
	// the index that text and symbol search read
	Files AscCoverage `json:"files"`
	// the index that the search by meaning reads
	Vectors AscCoverage `json:"vectors"`
}

// AscRepoSearchResult is the answer to a text search in one repository.
// swagger:model
type AscRepoSearchResult struct {
	Scope *AscScope `json:"scope,omitempty"`
	// the files with hits
	Files []AscFileHit `json:"files"`
	// whether more hits exist than are listed
	Truncated bool `json:"truncated"`
	// continues the search
	NextPageToken string `json:"next_page_token,omitempty"`
	// an estimate of the tokens the answer costs an agent
	EstTokens uint32 `json:"est_tokens"`
}

// AscRepoSymbolsResult is the answer to a symbol call in one repository.
// swagger:model
type AscRepoSymbolsResult struct {
	Scope *AscScope `json:"scope,omitempty"`
	// the symbols
	Symbols []AscSymbol `json:"symbols"`
	// whether more exist than are listed
	Truncated bool `json:"truncated"`
	// continues the call
	NextPageToken string `json:"next_page_token,omitempty"`
	// an estimate of the tokens the answer costs an agent
	EstTokens uint32 `json:"est_tokens"`
}

// AscRepoSemanticResult is the answer to a search by meaning in one repository.
// swagger:model
type AscRepoSemanticResult struct {
	Scope *AscScope `json:"scope,omitempty"`
	// the chunks, best first
	Chunks []AscChunk `json:"chunks"`
	// the embedding model
	Model string `json:"model,omitempty"`
	// an estimate of the tokens the answer costs an agent
	EstTokens uint32 `json:"est_tokens"`
	// whether the nearest-neighbour search ran out of candidates
	AnnExhausted bool `json:"ann_exhausted"`
}
