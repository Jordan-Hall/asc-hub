// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTree writes files (slash-separated path to content) under a new temporary directory.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func check(t *testing.T, files map[string]string, table []exception) (int, string) {
	t.Helper()
	var out bytes.Buffer
	code := run(writeTree(t, files), table, &out)
	return code, out.String()
}

const useGit = `package x

import "forgejo.org/modules/git"

func f() { _ = git.NewCommand(nil, "status") }
`

func TestCleanTreePasses(t *testing.T) {
	code, out := check(t, map[string]string{
		"services/a/a.go": "package a\n\nfunc F() {}\n",
	}, nil)
	if code != 0 {
		t.Fatalf("code %d: %s", code, out)
	}
}

func TestNewCommandOutsideAllowedDirsFails(t *testing.T) {
	code, out := check(t, map[string]string{"services/a/a.go": useGit}, nil)
	if code != 1 || !strings.Contains(out, "services/a/a.go:5: git.NewCommand") {
		t.Fatalf("code %d: %s", code, out)
	}
}

func TestAllowedDirsAreNotChecked(t *testing.T) {
	code, out := check(t, map[string]string{
		"modules/git/x.go":                  useGit,
		"modules/git/gitcli/x.go":           useGit,
		"modules/repostore/gitcli/w.go":     useGit,
		"services/ascbridge/bridge/here.go": useGit,
	}, nil)
	if code != 0 {
		t.Fatalf("code %d: %s", code, out)
	}
}

func TestSiblingOfAllowedDirIsChecked(t *testing.T) {
	// "modules/gitrepo" and "modules/repostore/asc" share a prefix with an allowed directory but are not inside one.
	for _, name := range []string{"modules/gitrepo/x.go", "modules/repostore/asc/x.go", "services/ascbridgeless/x.go", "modules/repostore/x.go"} {
		code, out := check(t, map[string]string{name: useGit}, nil)
		if code != 1 {
			t.Errorf("%s: code %d: %s", name, code, out)
		}
	}
}

func TestTestFilesAndIgnoredFilesAreSkipped(t *testing.T) {
	code, out := check(t, map[string]string{
		"services/a/a_test.go":       useGit,
		"build/tool.go":              "//go:build ignore\n\n" + useGit,
		"build/tool2.go":             "// Copyright\n\n//go:build ignore\n\n" + useGit,
		"node_modules/pkg/vendor.go": useGit,
		"services/a/testdata/x.go":   useGit,
		".hidden/x.go":               useGit,
		"_skipped/x.go":              useGit,
	}, nil)
	if code != 0 {
		t.Fatalf("code %d: %s", code, out)
	}
}

func TestOtherBuildConstraintsAreStillChecked(t *testing.T) {
	code, out := check(t, map[string]string{
		"services/a/a.go": "//go:build linux\n\n" + useGit,
		"services/b/b.go": "//go:build !ignore\n\n" + useGit,
	}, nil)
	if code != 1 || !strings.Contains(out, "services/a/a.go") || !strings.Contains(out, "services/b/b.go") {
		t.Fatalf("code %d: %s", code, out)
	}
}

func TestImportAliasAndMethodValue(t *testing.T) {
	code, out := check(t, map[string]string{
		"services/a/a.go": `package a

import gg "forgejo.org/modules/git"

var newCmd = gg.NewCommand
var other = gg.NewCommandContextNoGlobals
var path = gg.GitExecutable
`,
	}, nil)
	if code != 1 || strings.Count(out, "services/a/a.go:") != 3 {
		t.Fatalf("code %d: %s", code, out)
	}
}

func TestDotImportFails(t *testing.T) {
	code, out := check(t, map[string]string{
		"services/a/a.go": "package a\n\nimport . \"forgejo.org/modules/git\"\n\nvar _ = NewCommand\n",
	}, nil)
	if code != 1 || !strings.Contains(out, "dot import") {
		t.Fatalf("code %d: %s", code, out)
	}
}

func TestOtherPackageNamedGitIsIgnored(t *testing.T) {
	code, out := check(t, map[string]string{
		"services/a/a.go": "package a\n\nimport git \"example.com/other/git\"\n\nvar _ = git.NewCommand\n",
	}, nil)
	if code != 0 {
		t.Fatalf("code %d: %s", code, out)
	}
}

func TestExecOfGit(t *testing.T) {
	code, out := check(t, map[string]string{
		"a/a.go": `package a

import (
	"context"
	"os/exec"
)

func f(ctx context.Context) {
	_ = exec.Command("git", "status")
	_ = exec.CommandContext(ctx, "/usr/bin/git", "status")
	_, _ = exec.LookPath("git.exe")
	_ = exec.Command("go", "git")
	_ = exec.CommandContext(ctx, "git-lfs")
}
`,
	}, nil)
	if code != 1 || strings.Count(out, "a/a.go:") != 3 {
		t.Fatalf("code %d: %s", code, out)
	}
}

func TestExceptionLimit(t *testing.T) {
	files := map[string]string{"services/a/a.go": useGit + "\nfunc g() { _ = git.NewCommand(nil, \"gc\") }\n"}
	entry := func(max int) []exception {
		return []exception{{"services/a/a.go", max, kindLifecycle, "reason", "K13"}}
	}

	if code, out := check(t, files, entry(2)); code != 0 {
		t.Fatalf("exact limit: code %d: %s", code, out)
	}
	if code, out := check(t, files, entry(1)); code != 1 || !strings.Contains(out, "the limit is 1") {
		t.Fatalf("over the limit: code %d: %s", code, out)
	}
	if code, out := check(t, files, entry(3)); code != 1 || !strings.Contains(out, "lower Max to 2") {
		t.Fatalf("under the limit: code %d: %s", code, out)
	}
}

func TestStaleExceptionFails(t *testing.T) {
	code, out := check(t, map[string]string{"services/a/a.go": "package a\n"}, []exception{{"services/a/a.go", 1, kindGap, "reason", "K10"}})
	if code != 1 || !strings.Contains(out, "remove its entry") {
		t.Fatalf("code %d: %s", code, out)
	}
	code, out = check(t, nil, []exception{{"services/gone.go", 1, kindGap, "reason", "K10"}})
	if code != 1 || !strings.Contains(out, "remove its entry") {
		t.Fatalf("missing file: code %d: %s", code, out)
	}
}

func TestBadTableFails(t *testing.T) {
	files := map[string]string{"services/a/a.go": useGit}
	bad := map[string]exception{
		"no reason":      {"services/a/a.go", 1, kindGap, " ", "K10"},
		"no retire":      {"services/a/a.go", 1, kindGap, "reason", ""},
		"zero limit":     {"services/a/a.go", 0, kindGap, "reason", "K10"},
		"unknown kind":   {"services/a/a.go", 1, "whatever", "reason", "K10"},
		"allowed dir":    {"modules/git/x.go", 1, kindGap, "reason", "K10"},
		"test file":      {"services/a/a_test.go", 1, kindGap, "reason", "K10"},
		"parent path":    {"../a.go", 1, kindGap, "reason", "K10"},
		"absolute path":  {"/services/a/a.go", 1, kindGap, "reason", "K10"},
		"backslash path": {`services\a\a.go`, 1, kindGap, "reason", "K10"},
		"not a go file":  {"services/a", 1, kindGap, "reason", "K10"},
		"unclean path":   {"services/./a/a.go", 1, kindGap, "reason", "K10"},
	}
	for name, e := range bad {
		if code, out := check(t, files, []exception{e}); code != 1 || !strings.Contains(out, "exception") {
			t.Errorf("%s: code %d: %s", name, code, out)
		}
	}
	dup := []exception{{"services/a/a.go", 1, kindGap, "reason", "K10"}, {"services/a/a.go", 1, kindGap, "reason", "K10"}}
	if code, out := check(t, files, dup); code != 1 || !strings.Contains(out, "listed twice") {
		t.Errorf("duplicate: code %d: %s", code, out)
	}
}

func TestUnparsableFileFails(t *testing.T) {
	code, out := check(t, map[string]string{"services/a/a.go": "package a\n\nimport \"forgejo.org/modules/git\"\n\nfunc {{{\n"}, nil)
	if code != 1 || !strings.Contains(out, "services/a/a.go") {
		t.Fatalf("code %d: %s", code, out)
	}
}

func TestShippedTableIsValid(t *testing.T) {
	if problems := validateTable(exceptions); len(problems) != 0 {
		t.Fatal(strings.Join(problems, "\n"))
	}
}

// TestRepository runs the check on this repository, so a plain go test of the package catches a new raw git call.
func TestRepository(t *testing.T) {
	root := filepath.Join("..", "..")
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Skip("not inside the repository")
	}
	var out bytes.Buffer
	if code := run(root, exceptions, &out); code != 0 {
		t.Fatalf("code %d:\n%s", code, out.String())
	}
}
