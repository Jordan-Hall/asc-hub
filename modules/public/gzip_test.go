// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package public

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"forgejo.org/modules/container"

	"github.com/klauspost/compress/gzip"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func serve(t *testing.T, name, body, acceptEncoding string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/assets/"+name, nil)
	if acceptEncoding != "" {
		req.Header.Set("Accept-Encoding", acceptEncoding)
	}
	rec := httptest.NewRecorder()
	serveContent(rec, req, name, time.Unix(1_800_000_000, 0), strings.NewReader(body))
	return rec
}

func gunzip(t *testing.T, b []byte) string {
	t.Helper()
	r, err := gzip.NewReader(bytes.NewReader(b))
	require.NoError(t, err)
	out, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(out)
}

// A script asked for by a browser that takes gzip is sent compressed, much smaller, and decompresses to the file;
// a browser that does not take gzip, or refuses it with q=0, gets the file as it is. Every answer varies on
// Accept-Encoding so a cache in between keeps the two apart.
func TestStaticAssetsAreGzippedForABrowserThatTakesIt(t *testing.T) {
	body := strings.Repeat("function hello() { return 'world'; }\n", 2000)

	rec := serve(t, "js/index.js", body, "gzip, deflate, br")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))
	assert.Contains(t, rec.Header().Get("Vary"), "Accept-Encoding")
	assert.Contains(t, rec.Header().Get("Content-Type"), "javascript")
	assert.Less(t, rec.Body.Len(), len(body)/10)
	assert.Equal(t, body, gunzip(t, rec.Body.Bytes()))

	for _, ae := range []string{"", "identity", "gzip;q=0", "br, gzip;q=0.000"} {
		rec = serve(t, "js/index.js", body, ae)
		assert.Empty(t, rec.Header().Get("Content-Encoding"), ae)
		assert.Equal(t, body, rec.Body.String(), ae)
		assert.Contains(t, rec.Header().Get("Vary"), "Accept-Encoding", ae)
	}
}

// Images and fonts that are compressed already are sent as they are, and say nothing about the encoding.
func TestCompressedFormatsAreNotGzippedAgain(t *testing.T) {
	body := strings.Repeat("x", 4096)
	for _, name := range []string{"img/logo.png", "fonts/a.woff2", "img/a.webp"} {
		rec := serve(t, name, body, "gzip")
		assert.Empty(t, rec.Header().Get("Content-Encoding"), name)
		assert.Empty(t, rec.Header().Get("Vary"), name)
		assert.Equal(t, body, rec.Body.String(), name)
	}
	assert.True(t, compressible("css/INDEX.CSS"))
	assert.True(t, compressible("img/svg/octicon.svg"))
	assert.False(t, compressible("img/favicon.png"))
}

// The compressed form is made once per version of a file: a second request is served from the cache, and a file
// that changed (another time or size) is compressed anew. A file that would not shrink is sent as it is.
func TestTheGzipCacheIsKeyedByTheVersionOfTheFile(t *testing.T) {
	c := &gzipCache{items: map[gzipKey][]byte{}}
	at := time.Unix(1_800_000_000, 0)
	body := strings.Repeat("a{color:red}\n", 500)

	first := c.get("css/x.css", at, strings.NewReader(body))
	require.NotNil(t, first)
	assert.Len(t, c.items, 1)
	again := c.get("css/x.css", at, strings.NewReader(body))
	assert.Equal(t, &first[0], &again[0], "the same bytes, from the cache")

	changed := c.get("css/x.css", at.Add(time.Second), strings.NewReader(body+"b{}"))
	require.NotNil(t, changed)
	assert.Len(t, c.items, 2)
	assert.Equal(t, body+"b{}", gunzip(t, changed))

	assert.Nil(t, c.get("css/tiny.css", at, strings.NewReader("a")), "gzip of one byte is larger than the byte")
	assert.Nil(t, c.get("css/empty.css", at, strings.NewReader("")))

	// A full cache compresses nothing more and keeps what it has.
	c.bytes = gzipMaxCache
	assert.Nil(t, c.get("css/new.css", at, strings.NewReader(body)))
	assert.NotNil(t, c.get("css/x.css", at, strings.NewReader(body)))
}

// The content is left at its start whatever happens, so the raw file can still be sent.
func TestTheGzipCacheLeavesTheContentAtItsStart(t *testing.T) {
	c := &gzipCache{items: map[gzipKey][]byte{}}
	r := strings.NewReader("a")
	assert.Nil(t, c.get("css/tiny.css", time.Unix(1, 0), r))
	b, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, "a", string(b))
}

func TestParseAcceptEncodingDropsRefusedCodings(t *testing.T) {
	assert.Equal(t, container.SetOf("br", "zstd"), parseAcceptEncoding("gzip;q=0, br;q=0.8, ZSTD, *"))
	assert.Equal(t, container.SetOf("gzip"), parseAcceptEncoding("gzip ; q=1.0"))
	assert.Empty(t, parseAcceptEncoding(""))
}
