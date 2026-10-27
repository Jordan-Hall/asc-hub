// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// trustKeys reads the keys of the [asc_trust] section of the English locale file, with the section prefix.
func trustKeys(t *testing.T) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.FromSlash("../../options/locale/locale_en-US.ini"))
	require.NoError(t, err)
	keys := map[string]string{}
	in := false
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, "[") {
			in = strings.TrimSpace(line) == "[asc_trust]"
			continue
		}
		if !in || strings.TrimSpace(line) == "" {
			continue
		}
		k, v, ok := strings.Cut(line, " = ")
		require.True(t, ok, "malformed line in [asc_trust]: %q", line)
		_, dup := keys["asc_trust."+k]
		require.False(t, dup, "duplicate key %q", k)
		require.NotEmpty(t, strings.TrimSpace(v), k)
		keys["asc_trust."+strings.TrimSpace(k)] = v
	}
	return keys
}

var trustKeyRE = regexp.MustCompile(`asc_trust\.[a-z_0-9.\-]+[a-z_0-9]`)

func filesUnder(t *testing.T, root string, ext ...string) []string {
	t.Helper()
	var out []string
	require.NoError(t, filepath.Walk(filepath.FromSlash(root), func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		base := filepath.Base(p)
		for _, e := range ext {
			if strings.HasSuffix(p, e) && !strings.HasSuffix(p, "_test.go") && (strings.HasPrefix(base, "asc") || strings.Contains(filepath.ToSlash(p), "trustweb") || strings.Contains(filepath.ToSlash(p), "asctrust")) {
				out = append(out, p)
			}
		}
		return nil
	}))
	return out
}

// Every key a page of the trust surfaces uses is in the locale file, and no key of the section is unused.
func TestTrustLocaleKeys(t *testing.T) {
	keys := trustKeys(t)
	used := map[string]bool{}
	var files []string
	files = append(files, filesUnder(t, "../../routers/web/repo", ".go")...)
	files = append(files, filesUnder(t, "../../routers/web/shared/trustweb", ".go")...)
	files = append(files, filesUnder(t, ".", ".go")...)
	files = append(files, filesUnder(t, "../../templates/repo", ".tmpl")...)
	files = append(files, "../../templates/repo/header.tmpl")
	for _, f := range files {
		raw, err := os.ReadFile(f)
		require.NoError(t, err)
		for _, m := range trustKeyRE.FindAllString(string(raw), -1) {
			if strings.HasSuffix(m, "_") || strings.Contains(m, "%s") {
				continue
			}
			used[m] = true
		}
	}
	for k := range used {
		if _, ok := keys[k]; !ok {
			// A prefix of a key that a template builds with printf ("asc_trust.ops.via.") is checked below.
			if hasKeyWithPrefix(keys, k+".") {
				continue
			}
			assert.Failf(t, "missing key", "%s is used and not in [asc_trust]", k)
		}
	}

	// The keys the code builds: every code and kind of failure has a text, and so has every way a ref moves and
	// every reason an operation cannot be undone.
	for code := range messages {
		assert.Contains(t, codeKeys, code, "a text without a locale key")
		assert.Contains(t, keys, (&Error{Code: code}).MessageKey(), code)
	}
	for kind := KindOther; kind <= KindUnimplemented; kind++ {
		assert.Contains(t, keys, (&Error{Kind: kind}).MessageKey(), "kind %d", kind)
	}
	for via := range viaKeys {
		assert.Contains(t, keys, "asc_trust.ops.via."+via)
	}
	assert.Contains(t, keys, "asc_trust.ops.via.other")
	for _, view := range []string{"failures", "flaky", "trends"} {
		assert.Contains(t, keys, "asc_trust.checks.view."+view)
	}
	for _, st := range GroupStates {
		assert.Contains(t, keys, "asc_trust.checks.state."+st.Key)
		assert.Contains(t, keys, "asc_trust.checks.state_done."+st.Key)
	}
	for _, sev := range DepSeverityKeys {
		assert.Contains(t, keys, "asc_trust.deps.severity."+sev.Key)
	}
	assert.Contains(t, keys, "asc_trust.deps.severity.unknown")
	for _, k := range DepKindKeys {
		assert.Contains(t, keys, "asc_trust.deps.kind."+k.Key)
	}
	assert.Contains(t, keys, "asc_trust.deps.kind.unknown")
	for _, r := range DepDismissReasons {
		assert.Contains(t, keys, "asc_trust.deps.reason."+r.Key)
	}
	for _, st := range DepStateKeys {
		assert.Contains(t, keys, "asc_trust.deps.state."+st.Key)
		assert.Contains(t, keys, "asc_trust.deps.dismiss_done."+st.Key)
	}
	assert.Contains(t, keys, "asc_trust.deps.dismiss_done.unknown")
	for _, l := range []string{"partial", "failed", "unknown"} {
		assert.Contains(t, keys, "asc_trust.deps.lookup_"+l)
		assert.Contains(t, keys, "asc_trust.deps.last_lookup_"+l)
	}
	for _, v := range []string{"findings", "updates", "config"} {
		assert.Contains(t, keys, "asc_trust.deps.view."+v)
	}
	for _, v := range []string{"file", "default"} {
		assert.Contains(t, keys, "asc_trust.deps.source."+v)
	}
	assert.Contains(t, keys, "asc_trust.checks.state.unknown")
	assert.Contains(t, keys, "asc_trust.checks.state_done.unknown")
	for _, reason := range []string{"nothing", "not_yours", "moved", "needs_maintainer", "protected_off", "expired", "other"} {
		assert.Contains(t, keys, "asc_trust.ops.blocked."+reason)
		assert.Contains(t, keys, "asc_trust.err.undo_blocked_"+reason)
	}
}

func hasKeyWithPrefix(keys map[string]string, prefix string) bool {
	for k := range keys {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}

func TestBlockKeysCoverTheReasonsAscGives(t *testing.T) {
	for reason, want := range map[string]string{
		"nothing to undo": "nothing", "not your operation": "not_yours", "not your ref": "not_yours",
		"the ref changed since": "moved", "a protected ref needs a maintainer": "needs_maintainer",
		"undo of protected refs is off": "protected_off", "outside the undo window of a protected ref": "expired",
		"a phrase of a newer server": "other", "": "",
	} {
		assert.Equal(t, want, blockKeyOf(reason), reason)
	}
}
