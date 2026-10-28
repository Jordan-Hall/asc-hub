// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"testing"
	"time"

	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git"

	"github.com/stretchr/testify/assert"
)

func TestBlameHeat(t *testing.T) {
	at := func(days int) *user_model.UserCommit {
		return &user_model.UserCommit{Commit: &git.Commit{Author: &git.Signature{When: time.Unix(1_700_000_000+int64(days)*86400, 0)}}}
	}
	commits := map[string]*user_model.UserCommit{"a": at(1), "b": at(2), "c": at(3), "d": at(4), "e": at(5)}
	parts := []*git.BlamePart{{Sha: "c"}, {Sha: "a"}, {Sha: "e"}, {Sha: "b"}, {Sha: "d"}, {Sha: "e"}, {Sha: "missing"}}
	heat := blameHeat(parts, commits)
	assert.Equal(t, 4, heat["e"], "the newest change is the hottest")
	assert.Equal(t, 1, heat["a"], "the oldest change is the coldest")
	assert.Greater(t, heat["d"], heat["b"])
	assert.Zero(t, heat["missing"], "a change without a date has no bar")

	single := blameHeat([]*git.BlamePart{{Sha: "a"}}, commits)
	assert.Equal(t, 1, single["a"])
}
