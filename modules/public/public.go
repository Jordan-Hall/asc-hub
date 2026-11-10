// Copyright 2016 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package public

import (
	"bytes"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"forgejo.org/modules/assetfs"
	"forgejo.org/modules/container"
	"forgejo.org/modules/httpcache"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/util"
)

func CustomAssets() *assetfs.Layer {
	return assetfs.Local("custom", setting.CustomPath, "public")
}

func AssetFS() *assetfs.LayeredFS {
	return assetfs.Layered(CustomAssets(), BuiltinAssets())
}

// FileHandlerFunc implements the static handler for serving files in "public" assets
func FileHandlerFunc() http.HandlerFunc {
	assetFS := AssetFS()
	return func(resp http.ResponseWriter, req *http.Request) {
		if req.Method != "GET" && req.Method != "HEAD" {
			resp.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleRequest(resp, req, assetFS, req.URL.Path)
	}
}

// parseAcceptEncoding parses Accept-Encoding (deflate, gzip;q=1.0, *;q=0.5) into the codings it names. A coding
// with q=0 is refused and left out, and so is the wildcard: only a coding the client names is used.
func parseAcceptEncoding(val string) container.Set[string] {
	types := make(container.Set[string])
	for part := range strings.SplitSeq(val, ",") {
		name, params, _ := strings.Cut(part, ";")
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" || name == "*" || refused(params) {
			continue
		}
		types.Add(name)
	}
	return types
}

// refused reports whether the parameters of a coding give it q=0.
func refused(params string) bool {
	for p := range strings.SplitSeq(params, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(p), "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(k), "q") {
			continue
		}
		v = strings.TrimRight(strings.TrimSpace(v), "0")
		return v == "" || v == "." || v == "0" || v == "0."
	}
	return false
}

// setWellKnownContentType will set the Content-Type if the file is a well-known type.
// See the comments of detectWellKnownMimeType
func setWellKnownContentType(w http.ResponseWriter, file string) {
	mimeType := detectWellKnownMimeType(path.Ext(file))
	if mimeType != "" {
		w.Header().Set("Content-Type", mimeType)
	}
}

func handleRequest(w http.ResponseWriter, req *http.Request, fs fs.FS, file string) {
	// actually, fs (http.FileSystem) is designed to be a safe interface, relative paths won't bypass its parent directory, it's also fine to do a clean here
	assetPath := util.PathJoinRelX(file)
	f, err := fs.Open(assetPath)
	if err != nil {
		if os.IsNotExist(err) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		log.Error("[Static] Open %q failed: %v", file, err)
		return
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Error("[Static] %q exists, but fails to open: %v", file, err)
		return
	}

	// need to serve index file? (no at the moment)
	if fi.IsDir() {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	serveContent(w, req, fi.Name(), assetPath, fi.ModTime(), f.(io.ReadSeeker))
}

type ZstdBytesProvider interface {
	ZstdBytes() []byte
}

// serveContent serves an asset: name is its base name (for the content type), assetPath its cleaned path in the
// asset tree (to key the compressed form).
func serveContent(w http.ResponseWriter, req *http.Request, name, assetPath string, modtime time.Time, content io.ReadSeeker) {
	setWellKnownContentType(w, name)
	if compressible(name) {
		w.Header().Add("Vary", "Accept-Encoding")
	}

	encodings := parseAcceptEncoding(req.Header.Get("Accept-Encoding"))
	if encodings.Contains("zstd") {
		// If the file was compressed, use the bytes directly.
		if compressed, ok := content.(ZstdBytesProvider); ok {
			rdZstd := bytes.NewReader(compressed.ZstdBytes())
			if w.Header().Get("Content-Type") == "" {
				w.Header().Set("Content-Type", "application/octet-stream")
			}
			w.Header().Set("Content-Encoding", "zstd")
			httpcache.ServeContentWithCacheControl(w, req, name, modtime, rdZstd)
			return
		}
	}

	if encodings.Contains("gzip") && compressible(name) {
		if z := assetGzip.get(assetPath, content); z != nil {
			if w.Header().Get("Content-Type") == "" {
				w.Header().Set("Content-Type", "application/octet-stream")
			}
			w.Header().Set("Content-Encoding", "gzip")
			httpcache.ServeContentWithCacheControl(w, req, name, modtime, bytes.NewReader(z))
			return
		}
	}

	httpcache.ServeContentWithCacheControl(w, req, name, modtime, content)
}
