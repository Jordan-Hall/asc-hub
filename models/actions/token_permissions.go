// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package actions

import (
	"fmt"
	"slices"
	"sort"

	"forgejo.org/modules/json"

	"go.yaml.in/yaml/v3"
)

// TokenPermissionLevel is the access one scope of a job token's `permissions:` block gives.
type TokenPermissionLevel int

const (
	TokenPermissionNone TokenPermissionLevel = iota
	TokenPermissionRead
	TokenPermissionWrite
)

func (l TokenPermissionLevel) String() string {
	switch l {
	case TokenPermissionRead:
		return "read"
	case TokenPermissionWrite:
		return "write"
	}
	return "none"
}

// The `permissions:` scopes Forgejo enforces on the automatic job token.
const (
	TokenScopeContents     = "contents"            // git (code), releases and wiki
	TokenScopeIssues       = "issues"              // issues
	TokenScopePullRequests = "pull-requests"       // pull requests
	TokenScopeActions      = "actions"             // actions unit
	TokenScopeProjects     = "repository-projects" // projects unit
	TokenScopePackages     = "packages"            // the package and container registry of the repository owner
)

var enforcedTokenScopes = []string{
	TokenScopeActions,
	TokenScopeContents,
	TokenScopeIssues,
	TokenScopePackages,
	TokenScopeProjects,
	TokenScopePullRequests,
}

// Every other scope GitHub names. They are valid in a workflow, can never grant anything, and are not stored.
var unenforcedTokenScopes = []string{
	"attestations", "checks", "deployments", "discussions", "id-token", "models", "pages", "security-events", "statuses",
}

// TokenCeiling is the most a job token can ever have. A `permissions:` block can only narrow it.
type TokenCeiling struct {
	// A fork pull request token is read-only whatever the workflow asks for.
	IsForkPullRequest bool
	// The repository's Actions setting [repo_model.ActionsConfig.AllowPackagesWrite].
	AllowPackagesWrite bool
}

func (c TokenCeiling) level(scope string) TokenPermissionLevel {
	if c.IsForkPullRequest {
		return TokenPermissionRead
	}
	if scope == TokenScopePackages && !c.AllowPackagesWrite {
		return TokenPermissionRead
	}
	return TokenPermissionWrite
}

// TokenPermissions is the effective `permissions:` of one job token, stored on the task as JSON. A nil value means the
// workflow had no `permissions:` block and the token keeps its default capability.
type TokenPermissions struct {
	Scopes map[string]TokenPermissionLevel
}

// Level returns the effective level of an enforced scope. A scope that is not stored is none.
func (p *TokenPermissions) Level(scope string) TokenPermissionLevel {
	return p.Scopes[scope]
}

// ParseTokenPermissions decodes the value of a task's stored permissions. An empty string is nil.
func ParseTokenPermissions(s string) (*TokenPermissions, error) {
	if s == "" {
		return nil, nil
	}
	raw := map[string]string{}
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return nil, fmt.Errorf("task permissions: %w", err)
	}
	p := &TokenPermissions{Scopes: map[string]TokenPermissionLevel{}}
	for scope, v := range raw {
		level, err := parseTokenPermissionLevel(scope, v)
		if err != nil {
			return nil, err
		}
		p.Scopes[scope] = level
	}
	return p, nil
}

// String encodes the permissions for storage on the task.
func (p *TokenPermissions) String() string {
	if p == nil {
		return ""
	}
	raw := make(map[string]string, len(p.Scopes))
	for scope, level := range p.Scopes {
		raw[scope] = level.String()
	}
	b, _ := json.Marshal(raw)
	return string(b)
}

func parseTokenPermissionLevel(scope, v string) (TokenPermissionLevel, error) {
	switch v {
	case "none":
		return TokenPermissionNone, nil
	case "read":
		return TokenPermissionRead, nil
	case "write":
		return TokenPermissionWrite, nil
	}
	return TokenPermissionNone, fmt.Errorf("permissions: unknown value %q for %q, expected none, read or write", v, scope)
}

func isKnownTokenScope(scope string) bool {
	return slices.Contains(enforcedTokenScopes, scope) || slices.Contains(unenforcedTokenScopes, scope)
}

// parseTokenPermissionsNode reads one `permissions:` value. present is false when the node is absent. Scopes the block
// does not name are none; read-all and write-all name every scope.
func parseTokenPermissionsNode(n *yaml.Node) (levels map[string]TokenPermissionLevel, present bool, err error) {
	if n == nil || n.Kind == 0 {
		return nil, false, nil
	}
	if n.Kind == yaml.AliasNode && n.Alias != nil {
		n = n.Alias
	}
	all := append(slices.Clone(enforcedTokenScopes), unenforcedTokenScopes...)
	levels = make(map[string]TokenPermissionLevel, len(all))
	switch n.Kind {
	case yaml.ScalarNode:
		switch n.Value {
		case "~", "null", "Null", "NULL", "": // `permissions:` with no value grants nothing
		case "read-all":
			for _, s := range all {
				levels[s] = TokenPermissionRead
			}
		case "write-all":
			for _, s := range all {
				levels[s] = TokenPermissionWrite
			}
		default:
			return nil, true, fmt.Errorf("permissions: unknown value %q, expected read-all, write-all or a mapping of scopes", n.Value)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			scope, value := n.Content[i].Value, n.Content[i+1]
			if !isKnownTokenScope(scope) {
				return nil, true, fmt.Errorf("permissions: unknown scope %q", scope)
			}
			if value.Kind != yaml.ScalarNode {
				return nil, true, fmt.Errorf("permissions: the value of %q must be none, read or write", scope)
			}
			if _, dup := levels[scope]; dup {
				return nil, true, fmt.Errorf("permissions: %q is set twice", scope)
			}
			level, err := parseTokenPermissionLevel(scope, value.Value)
			if err != nil {
				return nil, true, err
			}
			levels[scope] = level
		}
	default:
		return nil, true, fmt.Errorf("permissions: expected read-all, write-all or a mapping of scopes")
	}
	return levels, true, nil
}

// UnenforcedTokenScopes names the scopes of a `permissions:` value that Forgejo ignores, for the run's warning.
func UnenforcedTokenScopes(n *yaml.Node) []string {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	var out []string
	for i := 0; i+1 < len(n.Content); i += 2 {
		if scope := n.Content[i].Value; !slices.Contains(enforcedTokenScopes, scope) {
			out = append(out, scope)
		}
	}
	sort.Strings(out)
	return out
}

// ResolveTokenPermissions returns the effective permissions of a job: the job's `permissions:` block if it has one,
// otherwise the workflow's, intersected with the ceiling. It returns nil when neither has a block. An unknown scope or
// value is an error and the job must fail.
func ResolveTokenPermissions(workflow, job *yaml.Node, ceiling TokenCeiling) (*TokenPermissions, error) {
	node := job
	if node == nil || node.Kind == 0 {
		node = workflow
	}
	levels, present, err := parseTokenPermissionsNode(node)
	if err != nil {
		return nil, err
	}
	if !present {
		return nil, nil
	}
	p := &TokenPermissions{Scopes: make(map[string]TokenPermissionLevel, len(enforcedTokenScopes))}
	for _, scope := range enforcedTokenScopes {
		p.Scopes[scope] = min(levels[scope], ceiling.level(scope))
	}
	return p, nil
}

// ValidateTokenPermissions reports the first error in the `permissions:` of a workflow or its job.
func ValidateTokenPermissions(workflow, job *yaml.Node) error {
	for _, n := range []*yaml.Node{workflow, job} {
		if _, _, err := parseTokenPermissionsNode(n); err != nil {
			return err
		}
	}
	return nil
}
