// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascbyo

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ascbyoKeys reads the keys of the [ascbyo] section of the English locale file.
func ascbyoKeys(t *testing.T) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.FromSlash("../../options/locale/locale_en-US.ini"))
	require.NoError(t, err)
	keys := map[string]string{}
	in := false
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, "[") {
			in = strings.TrimSpace(line) == "[ascbyo]"
			continue
		}
		if !in || strings.TrimSpace(line) == "" {
			continue
		}
		k, v, ok := strings.Cut(line, " = ")
		require.True(t, ok, "malformed line in [ascbyo]: %q", line)
		_, dup := keys[k]
		require.False(t, dup, "duplicate key %q", k)
		keys["ascbyo."+strings.TrimSpace(k)] = v
	}
	return keys
}

var literalKeyRE = regexp.MustCompile(`"(ascbyo\.[a-z_0-9.]+)"`)

func filesUnder(t *testing.T, root string, ext ...string) []string {
	t.Helper()
	var out []string
	require.NoError(t, filepath.Walk(filepath.FromSlash(root), func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		for _, e := range ext {
			if strings.HasSuffix(p, e) {
				out = append(out, p)
			}
		}
		return nil
	}))
	return out
}

// TestLocaleKeys proves that every translation key the pages use exists in the locale file, and that the
// texts keep the placeholders their callers pass.
func TestLocaleKeys(t *testing.T) {
	have := ascbyoKeys(t)
	require.NotEmpty(t, have)

	used := map[string]string{} // key -> where
	scan := func(path string) {
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		if strings.HasSuffix(path, "_test.go") {
			return
		}
		for _, m := range literalKeyRE.FindAllStringSubmatch(string(raw), -1) {
			used[m[1]] = path
		}
	}
	for _, p := range filesUnder(t, "../../templates/shared/asc", ".tmpl") {
		scan(p)
	}
	for _, lvl := range []string{"user", "org", "repo"} {
		for _, name := range []string{"asc_storage.tmpl", "asc_ai.tmpl"} {
			scan(filepath.FromSlash("../../templates/" + lvl + "/settings/" + name))
		}
	}
	for _, p := range filesUnder(t, "../../services/ascbyo", ".go") {
		scan(p)
	}
	for _, p := range filesUnder(t, "../../routers/web/shared/byoweb", ".go") {
		scan(p)
	}

	// keys built at run time: kinds, reasons, states, providers, statuses
	for _, k := range []FailureKind{FailInvalid, FailNotFound, FailExists, FailDenied, FailEndpointBlocked, FailVerifyFailed, FailNotReady,
		FailInUse, FailMigrationActive, FailQuota, FailRateLimited, FailUnavailable, FailUnimplemented, FailKeyUntested, FailCapReached, FailInternal} {
		used[(&Failure{Kind: k}).LocaleKey()] = "failure kind"
	}
	for r := range blockedReasons {
		used["ascbyo.blocked."+r] = "blocked reason"
	}
	used["ascbyo.blocked.other"] = "blocked reason"
	for _, s := range []string{"pending", "verified", "enabled", "disabled", "failed", "unknown"} {
		used["ascbyo.storage.state_help."+s] = "state"
	}
	for _, s := range []string{"untested", "active", "failed", "capped", "disabled", "unknown"} {
		used["ascbyo.ai.state_help."+s] = "state"
		used["ascbyo.ai.state."+s] = "state"
	}
	for s := range testStatuses {
		used["ascbyo.ai.test_status."+s] = "test status"
	}
	for _, p := range Providers {
		used["ascbyo.ai.provider."+string(p)] = "provider"
	}
	for _, s := range []string{"resolve", "connect", "put", "get", "delete", "unknown"} {
		used["ascbyo.storage.step."+s] = "verify step"
	}

	for key, where := range used {
		if strings.HasSuffix(key, ".") {
			continue // the start of a key that is completed at run time; the completions are listed above
		}
		_, ok := have[key]
		assert.True(t, ok, "%s uses the key %q that the locale file does not have", where, key)
	}
	// every text is plain: no HTML and no stray percent signs, so it is safe wherever it is printed
	for key, text := range have {
		assert.NotContains(t, text, "<", key)
		assert.NotContains(t, text, ">", key)
		for _, m := range regexp.MustCompile(`%(?:\[\d\])?.?`).FindAllString(text, -1) {
			assert.Regexp(t, `^%(s|d|\[\d\][sd])$`, m, "%s has an odd format verb", key)
		}
	}
}
