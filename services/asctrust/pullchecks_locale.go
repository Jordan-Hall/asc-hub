// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import "strings"

// pullMsg names a sentence of the pull request checks page by its locale key (under "asc_trust.pull.") and its
// arguments.
type pullMsg struct {
	Key  string
	Args []any
}

// pluralKey is the plural form a locale key ends in: "one" for 1, else "other".
func pluralKey(n int) string {
	if n == 1 {
		return "one"
	}
	return "other"
}

// Localize replaces the English text of the page (the headline, the pill, the comparison with main, the label of each
// check and the title of a cause without one) with the text of the locale, and fills the title of the issue that
// each flaky card links to. tr is the translation function of the request.
func (pc *PullChecks) Localize(tr func(key string, args ...any) string) {
	if pc.headline.Key != "" {
		pc.Headline = tr("asc_trust.pull."+pc.headline.Key, pc.headline.Args...)
	}
	if pc.pillKey != "" {
		pc.Pill = tr("asc_trust.pull.pill_" + pc.pillKey)
	}
	if pc.main.Key != "" {
		pc.Main = tr("asc_trust.pull."+pc.main.Key, pc.main.Args...)
	}
	for i := range pc.Statuses {
		if k := pc.Statuses[i].labelKey; k != "" {
			pc.Statuses[i].Label = tr("asc_trust.pull.status_" + k)
		}
	}
	for i := range pc.Causes {
		if k := pc.Causes[i].titleKey; k != "" {
			pc.Causes[i].Title = tr(k)
		}
	}
	for i := range pc.Flaky {
		pc.Flaky[i].IssueQuery = "title=" + queryEscape(tr("asc_trust.pull.flaky_issue_title", pc.Flaky[i].Test))
	}
}

// pullLocaleKeys lists every key Localize can ask for, for the test that checks the locale file.
func pullLocaleKeys() []string {
	keys := []string{
		"pill_failing", "pill_running", "pill_waiting", "pill_none", "pill_passing",
		"headline_running", "headline_waiting_one", "headline_waiting_other", "headline_none", "headline_passed",
		"main_unknown", "main_none", "main_pending", "main_green", "main_failing_one", "main_failing_other",
		"status_pass", "status_failed", "status_error", "status_running", "status_warning", "status_skipped", "status_unknown", "status_no_result",
		"cause_fallback_title", "flaky_issue_title",
	}
	for _, f := range []string{"one", "other"} {
		for _, r := range []string{"none", "one", "other"} {
			keys = append(keys, "headline_failed_"+f+"_"+r)
		}
	}
	for i, k := range keys {
		keys[i] = "asc_trust.pull." + strings.TrimPrefix(k, "asc_trust.pull.")
	}
	return keys
}
