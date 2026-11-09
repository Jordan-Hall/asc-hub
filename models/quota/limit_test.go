// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package quota

import (
	"testing"

	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"

	"github.com/stretchr/testify/assert"
)

func TestAllowanceFor(t *testing.T) {
	used := Used{}
	used.Size.Repos.Private = 30
	used.Size.Repos.Public = 10
	used.Size.Git.LFS = 20
	used.Size.Assets.Artifacts = 5

	rule := func(limit int64, s ...LimitSubject) Rule { return Rule{Limit: limit, Subjects: s} }
	group := func(rs ...Rule) *Group { return &Group{Rules: rs} }

	t.Run("no groups: the configured default over everything", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Quota.Default.Total, int64(1000))()
		a, ok := (&GroupList{}).AllowanceFor(used, LimitSubjectSizeGitAll)
		assert.True(t, ok)
		assert.Equal(t, Allowance{Limit: 1000, Used: 65}, a)
		a, ok = (*GroupList)(nil).AllowanceFor(used, LimitSubjectSizeGitAll)
		assert.True(t, ok)
		assert.Equal(t, int64(1000), a.Limit)
	})

	t.Run("a rule on the subject counts its own subjects", func(t *testing.T) {
		gl := GroupList{group(rule(100, LimitSubjectSizeGitAll))}
		a, ok := gl.AllowanceFor(used, LimitSubjectSizeGitAll)
		assert.True(t, ok)
		assert.Equal(t, Allowance{Limit: 100, Used: 60}, a)
	})

	t.Run("a rule on a parent covers the subject", func(t *testing.T) {
		gl := GroupList{group(rule(200, LimitSubjectSizeAll))}
		a, ok := gl.AllowanceFor(used, LimitSubjectSizeGitAll)
		assert.True(t, ok)
		assert.Equal(t, Allowance{Limit: 200, Used: 65}, a)
	})

	t.Run("a rule on a child does not cover the subject", func(t *testing.T) {
		gl := GroupList{group(rule(50, LimitSubjectSizeReposPrivate))}
		_, ok := gl.AllowanceFor(used, LimitSubjectSizeGitAll)
		assert.False(t, ok)
	})

	t.Run("in a group the tightest rule applies", func(t *testing.T) {
		gl := GroupList{group(rule(1000, LimitSubjectSizeAll), rule(70, LimitSubjectSizeGitAll))}
		a, _ := gl.AllowanceFor(used, LimitSubjectSizeGitAll)
		assert.Equal(t, Allowance{Limit: 70, Used: 60}, a)
	})

	t.Run("across groups the roomiest group applies", func(t *testing.T) {
		gl := GroupList{group(rule(70, LimitSubjectSizeGitAll)), group(rule(500, LimitSubjectSizeGitAll))}
		a, _ := gl.AllowanceFor(used, LimitSubjectSizeGitAll)
		assert.Equal(t, int64(500), a.Limit)
	})

	t.Run("no limit wins", func(t *testing.T) {
		gl := GroupList{group(rule(70, LimitSubjectSizeGitAll)), group(rule(-1, LimitSubjectSizeAll))}
		a, ok := gl.AllowanceFor(used, LimitSubjectSizeGitAll)
		assert.True(t, ok)
		assert.Equal(t, int64(-1), a.Limit)
	})

	t.Run("a group without a covering rule is skipped", func(t *testing.T) {
		gl := GroupList{group(rule(10, LimitSubjectSizeAssetsAll)), group(rule(90, LimitSubjectSizeGitAll))}
		a, ok := gl.AllowanceFor(used, LimitSubjectSizeGitAll)
		assert.True(t, ok)
		assert.Equal(t, Allowance{Limit: 90, Used: 60}, a)
	})
}
