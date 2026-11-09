// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package packages

import (
	"context"
	"encoding/json"
	"regexp"
)

// The provenance of a package version (design board "Packages": "Published from acme/payments by a landed stack"):
// when an Actions job publishes a version, the hub keeps the repository, the run, the commit and the ref the job ran
// for, as the version property asc.provenance. It is written once, when the version is created, and never taken from
// the client: a version uploaded by a person has none.

// AscProvenanceProperty is the name of the version property that keeps the provenance.
const AscProvenanceProperty = "asc.provenance"

var ascProvenanceCommit = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

// AscProvenance is where a package version was published from.
type AscProvenance struct {
	RepoID int64  `json:"repo"`
	RunID  int64  `json:"run"`
	Commit string `json:"commit"`
	Ref    string `json:"ref,omitempty"`
}

// SetAscProvenance records the provenance of version versionID.
func SetAscProvenance(ctx context.Context, versionID int64, p *AscProvenance) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = InsertProperty(ctx, PropertyTypeVersion, versionID, AscProvenanceProperty, string(b))
	return err
}

// GetAscProvenance returns the provenance of version versionID, or nil when it has none or the record is not well formed.
func GetAscProvenance(ctx context.Context, versionID int64) (*AscProvenance, error) {
	pps, err := GetPropertiesByName(ctx, PropertyTypeVersion, versionID, AscProvenanceProperty)
	if err != nil || len(pps) == 0 {
		return nil, err
	}
	var p AscProvenance
	if json.Unmarshal([]byte(pps[0].Value), &p) != nil || p.RepoID <= 0 || !ascProvenanceCommit.MatchString(p.Commit) {
		return nil, nil
	}
	return &p, nil
}
