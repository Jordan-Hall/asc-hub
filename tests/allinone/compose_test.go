// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package allinone checks the deployment files that consume the all-in-one image against what the
// image reads: the Compose file in the README and, when ASC_ALLINONE_COMPOSE_FILES lists them, the
// Compose files of a deployment (for example the Coolify files of the ASC repository).
package allinone

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

const entrypointPath = "../../docker/allinone/usr/local/bin/asc-hub-entrypoint"

// plainHubVariables are the variables that the hub's container setup (docker-setup.sh) reads, plus
// the ones the entrypoint sets a default for.
var plainHubVariables = map[string]bool{
	"APP_NAME": true, "RUN_MODE": true, "SSH_DOMAIN": true, "HTTP_PORT": true, "ROOT_URL": true,
	"DISABLE_SSH": true, "START_SSH_SERVER": true, "SSH_PORT": true, "SSH_LISTEN_PORT": true, "DB_TYPE": true, "DB_HOST": true,
	"DB_NAME": true, "DB_USER": true, "DB_PASSWD": true, "INSTALL_LOCK": true,
	"DISABLE_REGISTRATION": true, "REQUIRE_SIGNIN_VIEW": true, "SECRET_KEY": true,
}

// entrypointVariables are the variables that asc-hub-entrypoint and asc-hub-healthcheck read.
var entrypointVariables = map[string]bool{
	"ASC_HUB_ADMIN_EMAIL": true, "ASC_HUB_ADMIN_USER": true, "ASC_HUB_BRIDGE": true,
	"ASC_HUB_START_TIMEOUT": true, "ASC_HUB_ASC_PORT": true, "ASC_HUB_SSH_ENABLE": true, "ASC_DATABASE_URL": true,
	"ASC_S3_ENDPOINT": true, "ASC_S3_BUCKET": true, "ASC_S3_REGION": true,
	"ASC_S3_ACCESS_KEY_ID": true, "ASC_S3_SECRET_ACCESS_KEY": true, "ASC_CACHE_MEMORY": true,
	"ASC_CACHE_DISK": true, "ASC_TEXT_MEMORY": true, "ASC_GIT_ENABLED": true, "ASC_LOG": true,
	"ASC_LOG_FORMAT": true,
}

var (
	secretName   = regexp.MustCompile(`(?i)(PASSWORD|PASSWD|SECRET|TOKEN|_KEY|DATABASE_URL)`)
	pureRef      = regexp.MustCompile(`^\$\{[A-Za-z_][A-Za-z0-9_]*(:?[-?+][^}]*)?\}$`)
	envNameInSrc = regexp.MustCompile(`\bASC_[A-Z0-9_]+\b`)
)

// TestEntrypointVariablesAreRead guards the list above: every variable it names is in the entrypoint
// or the health check, so a renamed variable does not leave the Compose checks accepting a dead name.
func TestEntrypointVariablesAreRead(t *testing.T) {
	src, err := os.ReadFile(entrypointPath)
	if err != nil {
		t.Fatal(err)
	}
	health, err := os.ReadFile(filepath.Join(filepath.Dir(entrypointPath), "asc-hub-healthcheck"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(src) + string(health)
	for name := range entrypointVariables {
		if !strings.Contains(text, name) {
			t.Errorf("%s is listed as read by the image but the entrypoint does not mention it", name)
		}
	}
	// And the other way round: a variable the entrypoint reads must be known to the checks.
	// Names that are set by the entrypoint for asc serve, test seams, or shell variables of the script.
	internal := map[string]bool{
		"ASC_BINARY": true, "ASC_SERVE_CONFIG": true, "ASC_ALLOW_LOOPBACK_HTTP": true,
		"ASC_DIR": true, "ASC_BIN": true, "ASC_PORT": true,
	}
	seen := map[string]bool{}
	for _, name := range envNameInSrc.FindAllString(text, -1) {
		if internal[name] || strings.HasPrefix(name, "ASC_HUB_TEST_") || seen[name] {
			continue
		}
		seen[name] = true
		if !entrypointVariables[name] {
			t.Errorf("the entrypoint reads %s, which the Compose checks do not know: add it to entrypointVariables and to docs/asc/OPERATIONS.md", name)
		}
	}
}

func TestReadmeCompose(t *testing.T) {
	data, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(string(data), "## Quick start")
	if start < 0 {
		t.Fatal("README.md has no Quick start section")
	}
	block := regexp.MustCompile("(?s)```yaml\n(.*?)```").FindStringSubmatch(string(data)[start:])
	if block == nil {
		t.Fatal("the Quick start section has no yaml block")
	}
	checkCompose(t, "README.md", []byte(block[1]), false)
}

func TestDeploymentCompose(t *testing.T) {
	list := os.Getenv("ASC_ALLINONE_COMPOSE_FILES")
	if list == "" {
		t.Skip("ASC_ALLINONE_COMPOSE_FILES is not set")
	}
	for _, path := range filepath.SplitList(list) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		checkCompose(t, path, data, true)
	}
}

type composeFile struct {
	Services map[string]struct {
		Image       string      `yaml:"image"`
		Restart     string      `yaml:"restart"`
		StopGrace   string      `yaml:"stop_grace_period"`
		User        string      `yaml:"user"`
		Privileged  bool        `yaml:"privileged"`
		NetworkMode string      `yaml:"network_mode"`
		CapAdd      []string    `yaml:"cap_add"`
		Environment yaml.Node   `yaml:"environment"`
		Volumes     []yaml.Node `yaml:"volumes"`
		Ports       []yaml.Node `yaml:"ports"`
	} `yaml:"services"`
	Volumes map[string]any `yaml:"volumes"`
}

// environmentOf reads the map form and the list form of a Compose environment.
func environmentOf(t *testing.T, n yaml.Node) map[string]string {
	t.Helper()
	out := map[string]string{}
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			out[n.Content[i].Value] = n.Content[i+1].Value
		}
	case yaml.SequenceNode:
		for _, item := range n.Content {
			k, v, _ := strings.Cut(item.Value, "=")
			out[k] = v
		}
	}
	return out
}

func checkCompose(t *testing.T, name string, data []byte, deployment bool) {
	t.Helper()
	var c composeFile
	if err := yaml.Unmarshal(data, &c); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if len(c.Services) == 0 {
		t.Fatalf("%s: no services", name)
	}
	for svc, s := range c.Services {
		where := name + " service " + svc
		if !strings.Contains(s.Image, "asc-hub") {
			t.Errorf("%s: image %q is not the ASC Hub image", where, s.Image)
			continue
		}
		checkImage(t, where, s.Image, deployment)

		env := environmentOf(t, s.Environment)
		for key, value := range env {
			switch {
			case strings.HasPrefix(key, "SERVICE_"): // Coolify magic variables
			case strings.HasPrefix(key, "FORGEJO__"):
			case plainHubVariables[key], entrypointVariables[key]:
			default:
				t.Errorf("%s: %s is not read by the image", where, key)
			}
			if secretName.MatchString(key) && value != "" && !pureRef.MatchString(value) {
				t.Errorf("%s: %s looks like a secret and has a literal value in the file; reference an environment variable instead", where, key)
			}
		}
		if _, ok := env["ASC_HUB_ADMIN_EMAIL"]; ok && strings.HasPrefix(env["ASC_HUB_ADMIN_USER"], "-") {
			t.Errorf("%s: ASC_HUB_ADMIN_USER must not start with '-'", where)
		}
		root := env["ROOT_URL"]
		switch {
		case root == "":
			t.Errorf("%s: ROOT_URL is not set; without it the hub announces http://localhost:3000/", where)
		case strings.HasPrefix(root, "http://"):
			t.Errorf("%s: ROOT_URL %q is plain http", where, root)
		case deployment && !strings.Contains(root, ":?"):
			t.Errorf("%s: ROOT_URL %q has no ${VAR:?message} guard, so a missing value is not caught at deploy time", where, root)
		}
		if v, ok := env["SSH_DOMAIN"]; ok && deployment && !strings.Contains(v, ":?") {
			t.Errorf("%s: SSH_DOMAIN has no ${VAR:?message} guard", where)
		}

		// state: one named volume at /var/lib/gitea
		var named string
		for _, v := range s.Volumes {
			src, dst, _ := strings.Cut(v.Value, ":")
			dst, _, _ = strings.Cut(dst, ":")
			if dst == "/var/lib/gitea" {
				named = src
			}
			if strings.Contains(v.Value, "docker.sock") {
				t.Errorf("%s: mounts the docker socket", where)
			}
		}
		if named == "" {
			t.Errorf("%s: nothing is mounted at /var/lib/gitea, so state is lost at every redeploy", where)
		} else if strings.ContainsAny(named, "/.$") {
			t.Errorf("%s: /var/lib/gitea is mounted from %q; use a named volume (a bind mount must be owned by uid 1000)", where, named)
		} else if _, ok := c.Volumes[named]; !ok {
			t.Errorf("%s: volume %q is not declared under volumes:", where, named)
		}

		// SSH is off unless ASC_HUB_SSH_ENABLE is true, and then it is the file's own choice: only an
		// opted-in file publishes the SSH port, and the port it publishes is the one clone addresses show.
		sshOn := env["ASC_HUB_SSH_ENABLE"] == "true"
		if v, ok := env["ASC_HUB_SSH_ENABLE"]; ok && !sshOn && !pureRef.MatchString(v) && v != "false" {
			t.Errorf("%s: ASC_HUB_SSH_ENABLE %q must be true, false or a variable reference", where, v)
		}
		if v := env["DISABLE_SSH"]; v == "false" && !sshOn {
			t.Errorf("%s: DISABLE_SSH is false but ASC_HUB_SSH_ENABLE is not true; the image ignores it", where)
		}
		var sshHost string
		for _, p := range s.Ports {
			i := strings.LastIndex(p.Value, ":")
			if i > 0 && strings.HasSuffix(strings.TrimSuffix(p.Value, "/tcp"), ":2222") {
				host := p.Value[:i]
				if j := strings.LastIndex(host, ":"); j >= 0 && !strings.HasPrefix(host, "${") {
					host = host[j+1:]
				}
				sshHost = host
			}
		}
		switch {
		case sshHost != "" && !sshOn:
			t.Errorf("%s: publishes the SSH port without ASC_HUB_SSH_ENABLE: true; an HTTPS-only deployment publishes no SSH port", where)
		case sshHost != "" && env["SSH_PORT"] != sshHost:
			t.Errorf("%s: SSH_PORT %q does not equal the published host port %q, so clone addresses would show a port that is closed", where, env["SSH_PORT"], sshHost)
		case sshHost == "" && sshOn:
			t.Errorf("%s: ASC_HUB_SSH_ENABLE is true but the SSH port is not published", where)
		}

		grace, err := time.ParseDuration(s.StopGrace)
		if err != nil || grace < 30*time.Second {
			t.Errorf("%s: stop_grace_period %q must be at least 30s (the entrypoint waits up to 25s for the hub and asc serve)", where, s.StopGrace)
		}
		if s.Restart != "unless-stopped" && s.Restart != "always" {
			t.Errorf("%s: restart is %q; the entrypoint ends the container when one process dies, so something must restart it", where, s.Restart)
		}
		if s.Privileged || s.NetworkMode == "host" || len(s.CapAdd) > 0 {
			t.Errorf("%s: privileged, host networking and added capabilities are not needed by the image", where)
		}
		if s.User != "" && s.User != "1000:1000" && s.User != "1000" {
			t.Errorf("%s: user %q; the volume and the hub expect uid 1000", where, s.User)
		}
	}
}

func checkImage(t *testing.T, where, image string, deployment bool) {
	t.Helper()
	slash := strings.LastIndex(image, "/")
	colon := strings.Index(image[slash+1:], ":")
	// ${REPO:-default}:${TAG:?message} has colons inside the variables: look for the one between them.
	tag := ""
	if i := strings.Index(image, "}:"); i >= 0 {
		tag = image[i+2:]
	} else if colon >= 0 {
		tag = image[slash+1+colon+1:]
	}
	if tag == "" {
		t.Errorf("%s: image %q has no tag", where, image)
		return
	}
	switch strings.Trim(tag, "${}") {
	case "latest", "asc-v16":
		t.Errorf("%s: image tag %q moves; pin the 12-character commit tag", where, tag)
	}
	if deployment && !strings.Contains(tag, ":?") {
		t.Errorf("%s: image tag %q has no ${VAR:?message} guard, so a deploy without a pinned tag is not caught", where, tag)
	}
}
