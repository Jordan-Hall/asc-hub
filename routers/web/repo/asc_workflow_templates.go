// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import "slices"

// Workflow templates: starting points for a new `.asc/workflows/<name>.toml`. The editor offers one as the text of a new file
// (`?asc_template=<name>`); the file is then proposed as a change like any other, so nothing here runs until it is reviewed and
// landed. The definitions follow the strict format asc parses (unknown keys are errors).

// AscWorkflowTemplate is one starting point.
type AscWorkflowTemplate struct {
	Name        string // the file stem and the value of asc_template
	Title       string
	Description string
	Text        string
}

var ascWorkflowTemplates = []AscWorkflowTemplate{
	{
		Name: "review-changes", Title: "Review every new change", Description: "An agent reads each new change and leaves notes; a person decides.",
		Text: `description = "Review every new change and leave notes"
max_parallel_runs = 2

[[trigger]]
kind = "change_opened"

[[step]]
id = "review"
kind = "agent"
agent = "reviewer"
prompt = "Review {{change}}. Leave notes on correctness and tests. Do not change files."
paths = []
timeout_seconds = 1200

[[step]]
id = "decide"
kind = "approval"
needs = ["review"]
approvers = ["role:maintainer"]
`,
	},
	{
		Name: "nightly-checks", Title: "Run the checks every night", Description: "Waits for the named checks on a schedule and records the result.",
		Text: `description = "Check the build every night"

[[trigger]]
kind = "schedule"
cron = "0 2 * * *"

[[step]]
id = "checks"
kind = "checks"
checks = ["build"]
timeout_seconds = 3600
`,
	},
	{
		Name: "release", Title: "Build, check and publish a release", Description: "A command step builds, a person approves, a command step publishes with a secret.",
		Text: `description = "Build, check and publish a release"
max_parallel_runs = 1

[[trigger]]
kind = "manual"

[[input]]
name = "version"
description = "the version to release"
required = true

[[step]]
id = "build"
kind = "command"
command = ["make", "release"]

[[step]]
id = "approve"
kind = "approval"
needs = ["build"]
approvers = ["role:maintainer"]

[[step]]
id = "publish"
kind = "command"
needs = ["approve"]
command = ["make", "publish"]
secrets = { REGISTRY_TOKEN = "registry" }
`,
	},
}

// AscWorkflowTemplates lists the starting points.
func AscWorkflowTemplates() []AscWorkflowTemplate { return slices.Clone(ascWorkflowTemplates) }

// ascWorkflowTemplate returns the template called name.
func ascWorkflowTemplate(name string) (AscWorkflowTemplate, bool) {
	for _, t := range ascWorkflowTemplates {
		if t.Name == name {
			return t, true
		}
	}
	return AscWorkflowTemplate{}, false
}
