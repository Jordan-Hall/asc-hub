// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package swagger

import (
	api "forgejo.org/modules/structs"
)

// AscRepoSearchResult
// swagger:response AscRepoSearchResult
type swaggerResponseAscRepoSearchResult struct {
	// in:body
	Body api.AscRepoSearchResult `json:"body"`
}

// AscRepoSymbolsResult
// swagger:response AscRepoSymbolsResult
type swaggerResponseAscRepoSymbolsResult struct {
	// in:body
	Body api.AscRepoSymbolsResult `json:"body"`
}

// AscRepoSemanticResult
// swagger:response AscRepoSemanticResult
type swaggerResponseAscRepoSemanticResult struct {
	// in:body
	Body api.AscRepoSemanticResult `json:"body"`
}

// AscCrossSearchResult
// swagger:response AscCrossSearchResult
type swaggerResponseAscCrossSearchResult struct {
	// in:body
	Body api.AscCrossSearchResult `json:"body"`
}

// AscCrossSymbolsResult
// swagger:response AscCrossSymbolsResult
type swaggerResponseAscCrossSymbolsResult struct {
	// in:body
	Body api.AscCrossSymbolsResult `json:"body"`
}

// AscCrossSemanticResult
// swagger:response AscCrossSemanticResult
type swaggerResponseAscCrossSemanticResult struct {
	// in:body
	Body api.AscCrossSemanticResult `json:"body"`
}

// AscSymbolPanel
// swagger:response AscSymbolPanel
type swaggerResponseAscSymbolPanel struct {
	// in:body
	Body api.AscSymbolPanel `json:"body"`
}

// AscIndexStatusResult
// swagger:response AscIndexStatusResult
type swaggerResponseAscIndexStatusResult struct {
	// in:body
	Body api.AscIndexStatusResult `json:"body"`
}
