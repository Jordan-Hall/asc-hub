// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

// Command check-raw-git fails when production Go code runs the git binary outside the places that own it.
//
// Raw git is allowed only in the allowed directories below: modules/git (the git package and its gitcli backend),
// modules/repostore/gitcli (the gitcli writer) and services/ascbridge (the bridge to legacy git clients).
// Every other file that runs git must have an entry in exceptions.go with a count limit, a reason and the unit that
// removes it. The check fails on a new site, on an entry whose count is too high, and on an entry that no longer
// matches anything, so the list only shrinks.
//
// What it finds, by syntax alone: any use of git.NewCommand, git.NewCommandContextNoGlobals or git.GitExecutable
// (through whatever name the file imports forgejo.org/modules/git by), a dot import of that package, and a call to
// os/exec Command, CommandContext or LookPath whose program is the literal "git". It does not follow a program name
// held in a variable. It skips test files and files that are not built into the product (//go:build ignore).
//
// Run it from the repository root: go run ./build/check-raw-git
package main

import (
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const (
	gitImportPath  = "forgejo.org/modules/git"
	execImportPath = "os/exec"
)

// allowedDirs are the directories where raw git belongs. Matching is by whole path elements.
var allowedDirs = []string{
	"modules/git",
	"modules/repostore/gitcli",
	"services/ascbridge",
}

// skipDir reports whether the go tool and the product ignore a directory: testdata, and names that start with "." or "_".
// node_modules holds no Go code of the product.
func skipDir(name string) bool {
	return name == "testdata" || name == "node_modules" || (len(name) > 1 && (name[0] == '.' || name[0] == '_'))
}

// site is one place that runs, or can run, the git binary.
type site struct {
	File string // slash-separated, relative to the root
	Line int
	What string
}

func main() {
	root := "."
	switch {
	case len(os.Args) == 2 && os.Args[1] != "" && !strings.HasPrefix(os.Args[1], "-"):
		root = os.Args[1]
	case len(os.Args) != 1:
		fmt.Fprintln(os.Stderr, "usage: go run ./build/check-raw-git [repository root]")
		os.Exit(2)
	}
	os.Exit(run(root, exceptions, os.Stdout))
}

// run checks the tree under root against the exception table and writes the findings to out.
// It returns 0 when the tree is clean and 1 when it is not, including when a file could not be read or parsed.
func run(root string, table []exception, out io.Writer) int {
	problems := validateTable(table)

	sites, scanProblems := scan(root)
	problems = append(problems, scanProblems...)

	byFile := map[string][]site{}
	for _, s := range sites {
		byFile[s.File] = append(byFile[s.File], s)
	}
	limits := map[string]exception{}
	for _, e := range table {
		limits[e.Path] = e
	}

	files := make([]string, 0, len(byFile))
	for file := range byFile {
		files = append(files, file)
	}
	slices.Sort(files)
	for _, file := range files {
		found := byFile[file]
		e, ok := limits[file]
		switch {
		case !ok:
			for _, s := range found {
				problems = append(problems, fmt.Sprintf("%s:%d: %s outside the allowed packages; use the repository backend or the repostore writer", s.File, s.Line, s.What))
			}
		case len(found) > e.Max:
			problems = append(problems, fmt.Sprintf("%s: %d uses of raw git, the limit is %d (%s); new uses are not allowed", file, len(found), e.Max, e.Retire))
			for _, s := range found {
				problems = append(problems, fmt.Sprintf("  %s:%d: %s", s.File, s.Line, s.What))
			}
		case len(found) < e.Max:
			problems = append(problems, fmt.Sprintf("%s: %d uses of raw git are left, the limit is %d; lower Max to %d in build/check-raw-git/exceptions.go", file, len(found), e.Max, len(found)))
		}
	}
	for _, e := range table {
		if _, ok := byFile[e.Path]; !ok {
			problems = append(problems, fmt.Sprintf("%s: no raw git left; remove its entry from build/check-raw-git/exceptions.go", e.Path))
		}
	}

	if len(problems) == 0 {
		fmt.Fprintf(out, "check-raw-git: ok (%d uses of raw git in %d files, all listed in the exception table)\n", len(sites), len(files))
		return 0
	}
	fmt.Fprintf(out, "check-raw-git: %d problems\n", len(problems))
	for _, p := range problems {
		fmt.Fprintln(out, p)
	}
	return 1
}

// validateTable rejects a table that cannot be trusted: an entry with no reason, a bad path, a duplicate or a limit of zero.
func validateTable(table []exception) []string {
	var problems []string
	seen := map[string]bool{}
	for _, e := range table {
		switch {
		case e.Path == "" || path.Clean(e.Path) != e.Path || strings.HasPrefix(e.Path, "/") || strings.HasPrefix(e.Path, "../") || strings.Contains(e.Path, `\`) || !strings.HasSuffix(e.Path, ".go"):
			problems = append(problems, fmt.Sprintf("exception %q: the path must be a clean, relative, slash-separated .go file", e.Path))
		case strings.HasSuffix(e.Path, "_test.go"):
			problems = append(problems, fmt.Sprintf("exception %q: test files are not checked, so they need no entry", e.Path))
		case isAllowed(e.Path):
			problems = append(problems, fmt.Sprintf("exception %q: the file is in an allowed directory and needs no entry", e.Path))
		case seen[e.Path]:
			problems = append(problems, fmt.Sprintf("exception %q: listed twice", e.Path))
		case e.Max < 1:
			problems = append(problems, fmt.Sprintf("exception %q: Max must be at least 1", e.Path))
		case strings.TrimSpace(e.Reason) == "" || strings.TrimSpace(e.Retire) == "":
			problems = append(problems, fmt.Sprintf("exception %q: Reason and Retire must both be set", e.Path))
		case !validKinds[e.Kind]:
			problems = append(problems, fmt.Sprintf("exception %q: unknown kind %q", e.Path, e.Kind))
		}
		seen[e.Path] = true
	}
	return problems
}

func isAllowed(rel string) bool {
	for _, dir := range allowedDirs {
		if rel == dir || strings.HasPrefix(rel, dir+"/") {
			return true
		}
	}
	return false
}

// scan returns every raw git site in the product code under root, and a problem for each file it could not check.
func scan(root string) ([]site, []string) {
	var (
		sites    []site
		problems []string
	)
	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", p, err))
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", p, relErr))
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel != "." && (skipDir(d.Name()) || isAllowed(rel)) {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		found, err := scanFile(p, rel)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", rel, err))
			return nil
		}
		sites = append(sites, found...)
		return nil
	})
	if walkErr != nil {
		problems = append(problems, walkErr.Error())
	}
	return sites, problems
}

// scanFile finds the raw git sites in one Go file.
func scanFile(filename, rel string) ([]site, error) {
	fset := token.NewFileSet()
	// Imports first: most files import neither package and need no full parse.
	header, err := parser.ParseFile(fset, filename, nil, parser.ImportsOnly|parser.ParseComments)
	if err != nil {
		return nil, err
	}
	if builtOnlyByIgnore(header) {
		return nil, nil
	}
	gitName, execName, dotGit := importNames(header)
	if gitName == "" && execName == "" && !dotGit {
		return nil, nil
	}

	f, err := parser.ParseFile(fset, filename, nil, 0)
	if err != nil {
		return nil, err
	}
	var sites []site
	add := func(pos token.Pos, what string) {
		sites = append(sites, site{File: rel, Line: fset.Position(pos).Line, What: what})
	}
	if dotGit {
		for _, imp := range f.Imports {
			if imp.Name != nil && imp.Name.Name == "." && importPath(imp) == gitImportPath {
				add(imp.Pos(), "dot import of "+gitImportPath)
			}
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			if x, ok := n.X.(*ast.Ident); ok && gitName != "" && x.Name == gitName && gitEntryPoints[n.Sel.Name] {
				add(n.Pos(), gitName+"."+n.Sel.Name)
			}
		case *ast.CallExpr:
			if what := execGitCall(n, execName); what != "" {
				add(n.Pos(), what)
			}
		}
		return true
	})
	return sites, nil
}

// gitEntryPoints are the members of modules/git that run the git binary or name it.
var gitEntryPoints = map[string]bool{
	"NewCommand":                 true,
	"NewCommandContextNoGlobals": true,
	"GitExecutable":              true,
}

// execGitCall reports a call of os/exec that starts the program "git".
func execGitCall(call *ast.CallExpr, execName string) string {
	if execName == "" {
		return ""
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	x, ok := sel.X.(*ast.Ident)
	if !ok || x.Name != execName {
		return ""
	}
	program := -1
	switch sel.Sel.Name {
	case "Command", "LookPath":
		program = 0
	case "CommandContext":
		program = 1
	}
	if program < 0 || program >= len(call.Args) {
		return ""
	}
	lit, ok := call.Args[program].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}
	name, err := strconv.Unquote(lit.Value)
	if err != nil {
		return ""
	}
	if base := strings.ToLower(path.Base(filepath.ToSlash(name))); base == "git" || base == "git.exe" {
		return execName + "." + sel.Sel.Name + `("git")`
	}
	return ""
}

// importNames returns the local names of modules/git and os/exec in f. A name is empty when the file does not import
// the package or imports it as "_". dotGit is true for a dot import of modules/git.
func importNames(f *ast.File) (gitName, execName string, dotGit bool) {
	for _, imp := range f.Imports {
		name := ""
		if imp.Name != nil {
			name = imp.Name.Name
		}
		switch importPath(imp) {
		case gitImportPath:
			switch name {
			case "":
				gitName = "git"
			case ".":
				dotGit = true
			case "_":
			default:
				gitName = name
			}
		case execImportPath:
			switch name {
			case "":
				execName = "exec"
			case ".", "_":
			default:
				execName = name
			}
		}
	}
	return gitName, execName, dotGit
}

func importPath(imp *ast.ImportSpec) string {
	p, err := strconv.Unquote(imp.Path.Value)
	if err != nil {
		return ""
	}
	return p
}

// builtOnlyByIgnore reports whether the file carries the constraint "//go:build ignore", which keeps it out of every build.
func builtOnlyByIgnore(f *ast.File) bool {
	for _, group := range f.Comments {
		if group.Pos() >= f.Package {
			break
		}
		for _, c := range group.List {
			if !constraint.IsGoBuild(c.Text) {
				continue
			}
			expr, err := constraint.Parse(c.Text)
			if err != nil {
				return false
			}
			// Built nowhere when it is false even with every tag but "ignore" set.
			return !expr.Eval(func(tag string) bool { return tag != "ignore" })
		}
	}
	return false
}
