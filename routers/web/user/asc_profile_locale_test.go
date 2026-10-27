// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every text key the profile uses must exist in the English locale: a missing key is shown to the visitor as
// the key itself ("user.asc_profile.working_now") followed by Go's "%!(EXTRA ...)" for its arguments.
func TestAscProfileTextsAreInTheLocale(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	ini, err := os.ReadFile(filepath.Join(root, "options", "locale", "locale_en-US.ini"))
	require.NoError(t, err)

	used := map[string]bool{}
	key := regexp.MustCompile(`user\.asc_profile\.[a-z_0-9]+`)
	for _, dir := range []string{filepath.Join(root, "templates"), filepath.Join("."), filepath.Join(root, "routers", "web")} {
		require.NoError(t, filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || strings.HasSuffix(path, "_test.go") {
				return err
			}
			if ext := filepath.Ext(path); ext != ".tmpl" && ext != ".go" {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, k := range key.FindAllString(string(b), -1) {
				used[k] = true
			}
			return nil
		}))
	}
	require.NotEmpty(t, used)

	for k := range used {
		name := strings.TrimPrefix(k, "user.")
		assert.Regexp(t, `(?m)^`+regexp.QuoteMeta(name)+` = `, string(ini), "locale key %s", k)
	}
}
