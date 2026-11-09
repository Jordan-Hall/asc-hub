// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package web

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The notes of board About: the hero calls to action are Download ASC and Read the docs; the four principle cards
// are not links; the page has the hero, the story with its before and after table, the principles, open source and
// data ownership; the hero stays navy in light and dark.
func TestAscAboutBoardNotes(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(renderAscAbout(t)))
	require.NoError(t, err)

	t.Run("calls to action", func(t *testing.T) {
		acts := doc.Find(`.site-hero .site-actions a`)
		require.Equal(t, 2, acts.Length())
		assert.Equal(t, "/download", acts.Eq(0).AttrOr("href", ""))
		assert.Equal(t, "Download ASC", strings.TrimSpace(acts.Eq(0).Text()))
		assert.Equal(t, "/-/docs", acts.Eq(1).AttrOr("href", ""))
		assert.Equal(t, "Read the docs", strings.TrimSpace(acts.Eq(1).Text()))
	})

	t.Run("principle cards are not links", func(t *testing.T) {
		cards := doc.Find(`section[aria-labelledby="about-principles"] .ds-land-card`)
		assert.Equal(t, 4, cards.Length())
		assert.Equal(t, 0, cards.Find("a").Length())
		assert.Equal(t, 0, cards.Filter("a").Length())
		cards.Each(func(i int, c *goquery.Selection) {
			assert.NotEmpty(t, strings.TrimSpace(c.Find("h3").Text()), "card %d", i)
			assert.NotEmpty(t, strings.TrimSpace(c.Find("p").Text()), "card %d", i)
		})
	})

	t.Run("the sections", func(t *testing.T) {
		assert.Equal(t, 1, doc.Find(`section.site-hero h1#about-title`).Length())
		assert.Equal(t, 1, doc.Find(`section[aria-labelledby="about-why"] table`).Length(), "the story has its before and after table")
		assert.Equal(t, 4, doc.Find(`section[aria-labelledby="about-principles"] .ds-land-card`).Length())
		assert.Equal(t, 4, doc.Find(`.site-two .site-chips`).First().Find(`.ds-chip`).Length(), "open source chips")
		assert.Equal(t, 3, doc.Find(`.site-two .site-checks`).First().Find(`li`).Length(), "data ownership")
	})

	t.Run("the hero stays navy", func(t *testing.T) {
		css := filepath.Join("..", "..", "web_src", "css", "asc-ds")
		landing, err := os.ReadFile(filepath.Join(css, "landing.css"))
		require.NoError(t, err)
		assert.Regexp(t, regexp.MustCompile(`\.ds-app \.ds-land-hero \{\s*background: var\(--navy\);`), string(landing))
		tokens, err := os.ReadFile(filepath.Join(css, "tokens.css"))
		require.NoError(t, err)
		assert.Equal(t, 1, strings.Count(string(tokens), "--navy:"), "one navy for both themes")
	})
}
