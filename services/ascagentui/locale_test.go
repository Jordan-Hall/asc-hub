// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascagentui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/translation"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sectionKeys reads the keys of a section of the English locale file, with the section prefix.
func sectionKeys(t *testing.T, section string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.FromSlash("../../options/locale/locale_en-US.ini"))
	require.NoError(t, err)
	keys := map[string]string{}
	in := false
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, "[") {
			in = strings.TrimSpace(line) == "["+section+"]"
			continue
		}
		if !in || strings.TrimSpace(line) == "" {
			continue
		}
		k, v, ok := strings.Cut(line, " = ")
		require.True(t, ok, "malformed line in [%s]: %q", section, line)
		k = section + "." + strings.TrimSpace(k)
		_, dup := keys[k]
		require.False(t, dup, "duplicate key %q", k)
		require.NotEmpty(t, strings.TrimSpace(v), k)
		keys[k] = v
	}
	return keys
}

// englishLocale formats the keys of [asc_agentui] as the English locale does; other keys are returned as they are.
type englishLocale struct {
	translation.Locale
	keys map[string]string
}

func (l englishLocale) TrString(key string, args ...any) string {
	v, ok := l.keys[key]
	if !ok {
		return key
	}
	if len(args) == 0 {
		return v
	}
	return fmt.Sprintf(v, args...)
}

func english(t *testing.T) translation.Locale {
	return englishLocale{Locale: translation.MockLocale{}, keys: sectionKeys(t, "asc_agentui")}
}

var agentUIKeyRE = regexp.MustCompile(`asc_agentui\.[a-z_0-9.]+`)

// Every key the pages use is in the locale file, every key the package builds is there too, and no key of the
// section is unused.
func TestAgentUILocaleKeys(t *testing.T) {
	keys := sectionKeys(t, "asc_agentui")
	review := sectionKeys(t, "asc_review")
	used := map[string]bool{}

	// The keys the package builds.
	add := func(k string) {
		used[k] = true
		assert.Contains(t, keys, k)
	}
	// The states are worded by the messages of the review pages.
	var states []State
	for _, st := range runStates {
		states = append(states, st)
	}
	for _, st := range tokenStates {
		states = append(states, st)
	}
	for _, st := range workflowRunStates {
		states = append(states, st)
	}
	for _, st := range stepStates {
		states = append(states, st)
	}
	states = append(states, RunState(ascv1.AgentRunState(99)), TokenState(ascv1.AgentTokenState(99)),
		WorkflowRunState(ascv1.WorkflowRunState(99)), StepState(ascv1.WorkflowStepState(99)))
	for _, st := range states {
		assert.Contains(t, review, st.Key)
	}
	for _, st := range RunStateSlugs {
		add(st.Key)
	}
	for _, m := range Meters(nil, nil) {
		add("asc_agentui.meter_full." + m.Key)
	}
	for v := range ascv1.WorkflowStepKind_name {
		add(StepKind(ascv1.WorkflowStepKind(v)))
	}
	for v := range ascv1.WorkflowTriggerKind_name {
		add(TriggerLabel(ascv1.WorkflowTriggerKind(v)))
	}
	for v := range ascv1.AgentRunEventKind_name {
		word, _ := EventKind(ascv1.AgentRunEventKind(v))
		add(word)
	}
	for _, k := range failureByCode {
		add(k.key)
	}
	for _, which := range []string{"changes", "tokens", "cost", "paths", "runtime", ""} {
		_, k := budgetMessage([]string{which})
		add(k)
	}
	for _, k := range []string{"n_changes", "n_tokens", "n_paths"} {
		add("asc_agentui." + k)
		add("asc_agentui." + k + "_1")
	}
	// The answer to a decision (routers/web/repo, ascAgentsDecisionText) is named by the state asc sends back.
	for _, st := range []string{"allowed", "denied", "approved", "rejected"} {
		add("asc_agentui.ok.decided_" + st)
	}

	// The keys written out in code and templates.
	var files []string
	for _, root := range []string{".", "../../routers/web", "../../templates"} {
		require.NoError(t, filepath.Walk(filepath.FromSlash(root), func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}
			if (strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go")) || strings.HasSuffix(p, ".tmpl") {
				files = append(files, p)
			}
			return nil
		}))
	}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		require.NoError(t, err)
		for _, m := range agentUIKeyRE.FindAllString(string(raw), -1) {
			if strings.HasSuffix(m, "_") || strings.HasSuffix(m, ".") {
				continue
			}
			used[m] = true
			assert.Contains(t, keys, m, "%s uses a key that is not in [asc_agentui]", f)
		}
	}
	for k := range keys {
		assert.True(t, used[k], "%s is in [asc_agentui] and not used", k)
	}
}
