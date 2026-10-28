// Copyright 2020 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package analyze

import (
	"path/filepath"
	"strings"

	"github.com/go-enry/go-enry/v2"
)

// TSRX is the language of .tsrx files (https://tsrx.dev/). The linguist data that enry carries does not know it yet,
// so the hub names it here: detection by extension, the language type and the colour of its chip.
const TSRX = "TSRX"

const tsrxColor = "#7c3aed"

// GetLanguageType is enry.GetLanguageType with TSRX counted as a programming language, so it takes part in the
// language statistics of a repository.
func GetLanguageType(language string) enry.Type {
	if language == TSRX {
		return enry.Programming
	}
	return enry.GetLanguageType(language)
}

// GetColor is enry.GetColor with the colour of TSRX.
func GetColor(language string) string {
	if language == TSRX {
		return tsrxColor
	}
	return enry.GetColor(language)
}

// GetCodeLanguage detects code language based on file name and content
func GetCodeLanguage(filename string, content []byte) string {
	if strings.EqualFold(filepath.Ext(filename), ".tsrx") {
		return TSRX
	}

	if language, ok := enry.GetLanguageByExtension(filename); ok {
		return language
	}

	if language, ok := enry.GetLanguageByFilename(filename); ok {
		return language
	}

	if len(content) == 0 {
		return enry.OtherLanguage
	}

	return enry.GetLanguage(filepath.Base(filename), content)
}
