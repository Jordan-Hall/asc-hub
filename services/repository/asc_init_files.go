// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repository

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/repostore"
)

// The extra files of the first commit of a new repository, asked for on the new-repository page: AGENTS.md (what an agent
// may do here) and the policy file .asc/policy.toml (who maintains the repository and what landing needs). The policy
// file is meaningful only where asc keeps the repository: asc reads it and refuses a file it cannot use, so what is
// written here is built from checked values only.

const (
	// PolicyMaxApprovals is the most approvals the page offers.
	PolicyMaxApprovals = 5
	// PolicyMaxChecks is the most required checks the page accepts.
	PolicyMaxChecks = 10
	// agentsFilePath and policyFilePath are where the files go.
	agentsFilePath = "AGENTS.md"
	policyFilePath = ".asc/policy.toml"
)

var (
	// ErrBadPolicyChecks is returned for a check name the policy file cannot carry safely.
	ErrBadPolicyChecks = errors.New("a required check is a name of letters, digits, space, '.', '_', '/', ':' and '-' (at most 64 characters), at most 10 of them")
	checkNameRe        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._/:-]{0,63}$`)
	maintainerRe       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)
)

// ParsePolicyChecks splits the checks field (one per line or separated by commas) into checked names. Repeats are dropped.
func ParsePolicyChecks(raw string) ([]string, error) {
	if len(raw) > PolicyMaxChecks*80 {
		return nil, ErrBadPolicyChecks
	}
	var out []string
	seen := map[string]bool{}
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == '\n' || r == '\r' || r == ',' }) {
		name := strings.TrimSpace(part)
		if name == "" || seen[name] {
			continue
		}
		if !checkNameRe.MatchString(name) || len(out) >= PolicyMaxChecks {
			return nil, ErrBadPolicyChecks
		}
		seen[name] = true
		out = append(out, name)
	}
	return out, nil
}

// PolicyFileText is the starting policy of a repository: the person who creates it is its maintainer, and landing needs
// the given approvals and checks.
func PolicyFileText(maintainer string, approvals int, checks []string) (string, error) {
	if !maintainerRe.MatchString(maintainer) {
		return "", fmt.Errorf("policy file: maintainer %q is not a login", maintainer)
	}
	if approvals < 0 || approvals > PolicyMaxApprovals || len(checks) > PolicyMaxChecks {
		return "", errors.New("policy file: approvals or checks out of range")
	}
	var b strings.Builder
	b.WriteString("version = 1\n\n[roles]\nmaintainer = [\"user:" + maintainer + "\"]\n")
	if approvals > 0 || len(checks) > 0 {
		b.WriteString("\n[trunk]\n")
		if approvals > 0 {
			b.WriteString("require_approvals = " + strconv.Itoa(approvals) + "\n")
		}
		if len(checks) > 0 {
			quoted := make([]string, len(checks))
			for i, c := range checks {
				if !checkNameRe.MatchString(c) {
					return "", ErrBadPolicyChecks
				}
				quoted[i] = strconv.Quote(c)
			}
			b.WriteString("require_checks = [" + strings.Join(quoted, ", ") + "]\n")
		}
	}
	return b.String(), nil
}

// AgentsFileText is the AGENTS.md of a new repository.
func AgentsFileText(repoName string) string {
	return "# Agents in " + repoName + "\n\n" +
		"This file tells an agent (and a person who works with one) how to work in this repository.\n\n" +
		"- Work in small changes. Each change is reviewed on its own; a stack of changes lands bottom to top.\n" +
		"- Never push to trunk. Propose changes; a person reviews and lands them.\n" +
		"- Stay inside the write scope you were given. The repository policy (`.asc/policy.toml`) lists the paths an agent may write and the paths it may not.\n" +
		"- Run the checks the policy requires before you ask for review.\n" +
		"- Say what you changed and why in the message of each change.\n"
}

// ascExtraFiles are the files the new-repository page asked for beyond the README, .gitignore and license. The policy
// file is left out of a repository asc does not keep.
func ascExtraFiles(repo *repo_model.Repository, opts CreateRepoOptions) ([]repostore.FileOp, error) {
	var ops []repostore.FileOp
	if opts.AgentsFile {
		ops = append(ops, repostore.FileOp{Kind: repostore.OpPut, Path: agentsFilePath, Content: strings.NewReader(AgentsFileText(repo.Name))})
	}
	if opts.PolicyFile && repo.GetStorageBackend() == string(gitrepo.StorageBackendAsc) {
		text, err := PolicyFileText(opts.PolicyMaintainer, opts.PolicyApprovals, opts.PolicyChecks)
		if err != nil {
			return nil, err
		}
		ops = append(ops, repostore.FileOp{Kind: repostore.OpPut, Path: policyFilePath, Content: strings.NewReader(text)})
	}
	return ops, nil
}

// wantsFirstCommit reports whether the first commit has any file at all.
func (o CreateRepoOptions) wantsFirstCommit() bool {
	return o.AutoInit || o.AgentsFile || o.PolicyFile
}
