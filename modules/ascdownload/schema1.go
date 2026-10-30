// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascdownload

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// The manifest.json that asc's release process writes (scripts/release/assemble.py, docs/RELEASE.md of asc) is "schema 1": a
// file list with `file`, `kind` (cli, vscode, jetbrains, sbom, homebrew, scoop, winget, nfpm, aur), `format` and a full time of
// the release. The hub's page reads the shorter shape of Manifest, so a schema 1 manifest is turned into it here; nothing
// else reads schema 1.
//
// Only what the page can offer truthfully is taken over: the command line archives and installers of every platform, their
// signatures, and SHA256SUMS with its signature (every release has them). The package manager files are not: asc renders them
// (a Homebrew formula, a Scoop manifest, winget manifests) but publishes them to no tap, bucket or index, so a page that says
// `brew install asc` or `scoop install asc` would send people to a command that fails. The editor packages (.vsix, JetBrains
// zip) are release files, not the marketplace addresses the page links to, and the SBOM, nfpm configurations and PKGBUILD
// have no place on it. The release's minisign public key is not part of schema 1; the hub's settings carry it.

type schema1 struct {
	Schema     int    `json:"schema"`
	Name       string `json:"name"`
	Version    string `json:"version"`
	ReleasedAt string `json:"released_at"`
	Files      []struct {
		Kind      string `json:"kind"`
		OS        string `json:"os"`
		Arch      string `json:"arch"`
		Format    string `json:"format"`
		File      string `json:"file"`
		SHA256    string `json:"sha256"`
		Size      int64  `json:"size"`
		Signature string `json:"signature"`
	} `json:"files"`
}

// schemaOf reads the "schema" number of a manifest, 0 when it has none (the hub's own shape).
func schemaOf(data []byte) int {
	var probe struct {
		Schema int `json:"schema"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return 0
	}
	return probe.Schema
}

// fromSchema1 turns an asc release manifest into the page's manifest. It is not validated here; Parse does that.
func fromSchema1(data []byte) (*Manifest, error) {
	var s schema1
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("manifest.json is not valid: %w", jsonErr(err))
	}
	if s.Schema != 1 {
		return nil, fmt.Errorf("manifest.json has schema %d; this hub reads schema 1", s.Schema)
	}
	m := &Manifest{Version: s.Version}
	if date, _, ok := strings.Cut(s.ReleasedAt, "T"); ok {
		m.ReleasedAt = date
	} else {
		m.ReleasedAt = s.ReleasedAt
	}
	for _, f := range s.Files {
		if f.Kind != "cli" {
			continue
		}
		var kind string
		switch f.Format {
		case "tar.gz", "zip":
			kind = KindArchive
		case "msi", "pkg":
			kind = KindInstaller
		default:
			continue
		}
		arch := f.Arch
		if arch == "aarch64" {
			arch = ArchArm64
		}
		m.Files = append(m.Files, File{Name: f.File, OS: f.OS, Arch: arch, Kind: kind, SHA256: f.SHA256, Size: f.Size, Signature: f.Signature})
	}
	if len(m.Files) == 0 {
		return nil, errors.New("manifest.json lists no command line files")
	}
	m.Checksums, m.ChecksumsSignature = "SHA256SUMS", "SHA256SUMS.minisig"
	return m, nil
}
