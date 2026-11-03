// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package actions

import (
	"testing"

	actions_module "forgejo.org/modules/actions"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func envOf(t *testing.T, content []byte) map[string]string {
	t.Helper()
	var doc struct {
		Env map[string]string `yaml:"env"`
	}
	require.NoError(t, yaml.Unmarshal(content, &doc))
	return doc.Env
}

func TestAscJobEnv(t *testing.T) {
	defer test.MockVariableValue(&setting.Asc.PublicURL, "https://asc.example.org/")()

	push := map[string]string{}
	for _, kv := range ascJobEnv("acme/app", "push") {
		push[kv[0]] = kv[1]
	}
	assert.Equal(t, map[string]string{
		"TURBO_API":   "https://asc.example.org",
		"TURBO_TOKEN": "${{ github.token }}",
		"TURBO_TEAM":  "acme.app",
		"TURBO_CACHE": "local:rw,remote:rw",
		"ASC_REMOTE":  "asc+https://asc.example.org/acme/app",
		"ASC_TOKEN":   "${{ github.token }}",
	}, push)

	for _, event := range []string{"pull_request", "pull_request_target", "pull_request_sync"} {
		for _, kv := range ascJobEnv("acme/app", event) {
			if kv[0] == "TURBO_CACHE" {
				assert.Equal(t, "local:rw,remote:r", kv[1], event)
			}
		}
	}
}

func TestAddWorkflowEnv(t *testing.T) {
	env := [][2]string{{"TURBO_TEAM", "acme.app"}, {"ASC_TOKEN", "${{ github.token }}"}}

	t.Run("no env", func(t *testing.T) {
		out, err := addWorkflowEnv([]byte("on: push\njobs:\n  build:\n    runs-on: docker\n    steps:\n      - run: make\n"), env)
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"TURBO_TEAM": "acme.app", "ASC_TOKEN": "${{ github.token }}"}, envOf(t, out))
		assert.Contains(t, string(out), "runs-on: docker")
	})

	t.Run("the workflow's own key wins", func(t *testing.T) {
		out, err := addWorkflowEnv([]byte("on: push\nenv:\n  TURBO_TEAM: mine\n  OTHER: x\njobs:\n  a:\n    runs-on: docker\n    steps:\n      - run: make\n"), env)
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"TURBO_TEAM": "mine", "OTHER": "x", "ASC_TOKEN": "${{ github.token }}"}, envOf(t, out))
	})

	t.Run("every key set already", func(t *testing.T) {
		in := []byte("on: push\nenv: {TURBO_TEAM: a, ASC_TOKEN: b}\njobs: {}\n")
		out, err := addWorkflowEnv(in, env)
		require.NoError(t, err)
		assert.Equal(t, in, out)
	})

	t.Run("env is an expression", func(t *testing.T) {
		in := []byte("on: push\nenv: ${{ fromJSON(vars.ENV) }}\njobs: {}\n")
		out, err := addWorkflowEnv(in, env)
		require.NoError(t, err)
		assert.Equal(t, in, out)
	})

	t.Run("not a workflow", func(t *testing.T) {
		for _, in := range []string{"", "- a\n- b\n", "on: [push\n"} {
			out, err := addWorkflowEnv([]byte(in), env)
			require.NoError(t, err)
			assert.Equal(t, in, string(out))
		}
	})

	t.Run("every job sees the values", func(t *testing.T) {
		out, err := addWorkflowEnv([]byte("on: push\njobs:\n  a:\n    runs-on: docker\n    steps:\n      - run: make\n  b:\n    runs-on: docker\n    steps:\n      - run: make\n"), env)
		require.NoError(t, err)
		jobs, err := actions_module.JobParser(out)
		require.NoError(t, err)
		require.Len(t, jobs, 2)
		for _, j := range jobs {
			single, err := j.Marshal()
			require.NoError(t, err)
			assert.Equal(t, "acme.app", envOf(t, single)["TURBO_TEAM"])
			assert.Equal(t, "${{ github.token }}", envOf(t, single)["ASC_TOKEN"])
		}
	})
}
