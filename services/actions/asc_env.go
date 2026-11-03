// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package actions

import (
	"bytes"
	"context"
	"strings"

	actions_model "forgejo.org/models/actions"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/asctrust"

	"go.yaml.in/yaml/v3"
)

// Every job of an asc repository gets the environment that makes the shared cache and the asc CLI work with no
// setup step (SPEC §12.10): turbo reads and writes the repository's remote cache on asc serve, and `asc ci
// cache|artifact|status|log` find the server and the task token by themselves. A key the workflow sets in its own
// top-level `env:` is left as the workflow sets it.

// ascJobEnv is the environment for a run of the asc repository name, started by event.
func ascJobEnv(name, event string) [][2]string {
	server := strings.TrimRight(setting.AscPublicURL(), "/")
	// A pull request reads the shared cache and does not write it; asc serve keeps any write it makes out of the
	// trusted scope anyway (SPEC §12.4).
	cache := "local:rw,remote:rw"
	if strings.HasPrefix(event, "pull_request") {
		cache = "local:rw,remote:r"
	}
	return [][2]string{
		{"TURBO_API", server},
		{"TURBO_TOKEN", "${{ github.token }}"},
		{"TURBO_TEAM", strings.ReplaceAll(name, "/", ".")},
		{"TURBO_CACHE", cache},
		// The server is asc itself, so the CLI dials it without discovery.
		{"ASC_REMOTE", "asc+" + server + "/" + name},
		{"ASC_TOKEN", "${{ github.token }}"},
	}
}

// withAscEnv is the workflow content of run with the asc environment added. It returns content unchanged for a
// repository that is not on asc, and when the workflow cannot be read (the job parser reports that) or sets `env:`
// to an expression.
func withAscEnv(ctx context.Context, run *actions_model.ActionRun, content []byte) []byte {
	if !setting.Asc.Enabled || setting.AscPublicURL() == "" {
		return content
	}
	if err := run.LoadRepo(ctx); err != nil {
		log.Warn("asc environment of run %d: %v", run.ID, err)
		return content
	}
	name, err := asctrust.RepoName(run.Repo)
	if err != nil {
		return content
	}
	out, err := addWorkflowEnv(content, ascJobEnv(name, string(run.Event)))
	if err != nil {
		log.Warn("asc environment for %s: %v", run.Repo.FullName(), err)
		return content
	}
	return out
}

// addWorkflowEnv adds each key of env that the top-level `env:` of a workflow does not set.
func addWorkflowEnv(content []byte, env [][2]string) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(content, &doc); err != nil || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return content, nil
	}
	top := doc.Content[0]
	vars := mapValue(top, "env")
	if vars == nil {
		vars = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		top.Content = append(top.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "env"}, vars)
	}
	if vars.Kind != yaml.MappingNode {
		return content, nil
	}
	added := false
	for _, kv := range env {
		if mapValue(vars, kv[0]) != nil {
			continue
		}
		vars.Content = append(vars.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: kv[0]},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: kv[1], Style: yaml.DoubleQuotedStyle})
		added = true
	}
	if !added {
		return content, nil
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
