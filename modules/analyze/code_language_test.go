// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package analyze

import (
	"testing"

	"github.com/go-enry/go-enry/v2"
	"github.com/stretchr/testify/assert"
)

func TestTSRXIsDetectedByExtension(t *testing.T) {
	assert.Equal(t, TSRX, GetCodeLanguage("src/card.tsrx", []byte("function A() @{ <p/> }")))
	assert.Equal(t, TSRX, GetCodeLanguage("CARD.TSRX", nil))
	assert.NotEqual(t, TSRX, GetCodeLanguage("src/card.tsx", []byte("export const a = 1;")))
}

func TestTSRXCountsAsProgrammingWithAColour(t *testing.T) {
	assert.Equal(t, enry.Programming, GetLanguageType(TSRX))
	assert.Equal(t, enry.GetLanguageType("Rust"), GetLanguageType("Rust"))
	assert.NotEmpty(t, GetColor(TSRX))
	assert.Equal(t, enry.GetColor("Rust"), GetColor("Rust"))
}
