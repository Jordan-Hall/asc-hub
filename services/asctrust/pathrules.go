// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/google/uuid"
	"google.golang.org/grpc"
)

// Path rules of a repository (asc SPEC section 15.5.1): per-directory access inside one repository. A rule names a plain
// directory (empty is the root), a subject and a level. For a path, the deepest directory with a rule matching the
// viewer decides, the best matching level there wins, and the result never exceeds the viewer's level on the
// repository. A directory the viewer may not read is shown to them as restricted: its name, nothing inside it.
//
// The rules are kept by asc, not in the repository, and only administrators of the repository read or change them:
// the rules tell who may see what. The checks below repeat asc's own (crates/asc-core/src/pathacl.rs), so a person
// learns which row is wrong before anything is sent; asc checks again.

const (
	// MaxPathRules is the most rules a repository has.
	MaxPathRules = 512
	// MaxPathRuleDir is the longest rule directory in bytes.
	MaxPathRuleDir = 1024
	// MaxPathRuleSubject is the longest subject in bytes.
	MaxPathRuleSubject = 128
	// MaxPathCheckPaths bounds the paths of one preview.
	MaxPathCheckPaths = 200
)

// PathRuleLevels are the levels, from least to most.
var PathRuleLevels = []string{"none", "build", "read", "write", "admin"}

var (
	pathRuleSubject   = regexp.MustCompile(`^(\*|(team|user|agent|ci|deploy):[A-Za-z0-9._@+\-/*]+)$`)
	pathRulePrincipal = regexp.MustCompile(`^(user|agent|ci|deploy):[A-Za-z0-9._@+\-:/]{1,128}$`)
)

// AccessRule is one rule as the page shows and edits it.
type AccessRule struct {
	Dir     string
	Subject string
	Level   string
}

// AccessRules is the rule set of a repository.
type AccessRules struct {
	Rules     []AccessRule
	Revision  uint64
	Updated   time.Time
	UpdatedBy string
}

// PathAccess is the level a previewed viewer has on one path.
type PathAccess struct {
	Path      string
	Level     string
	DecidedBy string // the directory whose rules decided; "" is the root or no rule
}

// PathPreview is the answer of a preview.
type PathPreview struct {
	Access []PathAccess
	Teams  []string
}

func validLevel(l string) bool {
	for _, v := range PathRuleLevels {
		if l == v {
			return true
		}
	}
	return false
}

// validRuleDir is asc's check of a rule directory: relative, `/` separated, no empty, `.` or `..` segment, no glob.
func validRuleDir(d string) bool {
	if len(d) > MaxPathRuleDir {
		return false
	}
	if d == "" {
		return true
	}
	if strings.HasPrefix(d, "/") || strings.HasSuffix(d, "/") || strings.ContainsAny(d, "\\*?[") {
		return false
	}
	for _, r := range d {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return false
		}
	}
	for _, s := range strings.Split(d, "/") {
		if s == "" || s == "." || s == ".." {
			return false
		}
	}
	return true
}

// CleanRuleDir turns what a person typed into a rule directory: surrounding spaces and slashes go, and "/" alone
// means the root.
func CleanRuleDir(s string) string {
	return strings.Trim(strings.TrimSpace(s), "/")
}

// CheckPathRules checks a rule set. The error names the row (counted from 1) and what is wrong with it.
func CheckPathRules(rules []AccessRule) error {
	if len(rules) > MaxPathRules {
		return fmt.Errorf("%w: at most %d path rules", ErrBadInput, MaxPathRules)
	}
	seen := make(map[[2]string]int, len(rules))
	for i, r := range rules {
		row := i + 1
		if !validRuleDir(r.Dir) {
			return fmt.Errorf("%w: row %d: a directory is relative, with / between names and no . or .. or wildcards", ErrBadInput, row)
		}
		if len(r.Subject) > MaxPathRuleSubject || !pathRuleSubject.MatchString(r.Subject) {
			return fmt.Errorf("%w: row %d: the subject is *, team:<name>, user:<name>, agent:<name>, ci:<name> or deploy:<name>", ErrBadInput, row)
		}
		if !validLevel(r.Level) {
			return fmt.Errorf("%w: row %d: the level is none, build, read, write or admin", ErrBadInput, row)
		}
		key := [2]string{r.Dir, r.Subject}
		if first, dup := seen[key]; dup {
			return fmt.Errorf("%w: row %d names the same directory and subject as row %d", ErrBadInput, row, first)
		}
		seen[key] = row
	}
	return nil
}

func pathRuleOf(r *ascv1.PathRule) (AccessRule, bool) {
	if r == nil {
		return AccessRule{}, false
	}
	out := AccessRule{Dir: r.GetDir(), Subject: r.GetSubject(), Level: r.GetLevel()}
	if !validRuleDir(out.Dir) || len(out.Subject) > MaxPathRuleSubject || !pathRuleSubject.MatchString(out.Subject) || !validLevel(out.Level) {
		return AccessRule{}, false
	}
	return out, true
}

// ListPathRules reads the rules of a repository. An answer with a rule asc itself would refuse is a bad answer.
func ListPathRules(ctx context.Context, conn grpc.ClientConnInterface, repoName string) (*AccessRules, error) {
	cctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	resp, err := ascv1.NewPathRulesClient(conn).ListPathRules(cctx, &ascv1.ListPathRulesRequest{Repo: repoName})
	if err != nil {
		return nil, err
	}
	if len(resp.GetRules()) > MaxPathRules {
		return nil, fmt.Errorf("%w: too many path rules", ErrBadAnswer)
	}
	out := &AccessRules{Revision: resp.GetRevision(), UpdatedBy: CleanText(resp.GetUpdatedBy(), 80)}
	if resp.GetUpdated() > 0 {
		out.Updated = time.Unix(resp.GetUpdated(), 0)
	}
	for _, r := range resp.GetRules() {
		rule, ok := pathRuleOf(r)
		if !ok {
			return nil, fmt.Errorf("%w: path rule", ErrBadAnswer)
		}
		out.Rules = append(out.Rules, rule)
	}
	return out, nil
}

// SetPathRules replaces the rules of a repository. expected is the revision the person edited; asc refuses the
// change when the rules moved since (a conflict), so two administrators never overwrite each other.
func SetPathRules(ctx context.Context, conn grpc.ClientConnInterface, repoName string, rules []AccessRule, expected uint64, op string) (uint64, error) {
	if err := CheckPathRules(rules); err != nil {
		return 0, err
	}
	opID, err := uuid.Parse(op)
	if err != nil {
		return 0, fmt.Errorf("%w: operation id", ErrBadInput)
	}
	req := &ascv1.SetPathRulesRequest{Repo: repoName, ExpectedRevision: expected}
	for _, r := range rules {
		req.Rules = append(req.Rules, &ascv1.PathRule{Dir: r.Dir, Subject: r.Subject, Level: r.Level})
	}
	cctx, cancel := context.WithTimeout(ascclient.WithOpID(ctx, opID), CallTimeout)
	defer cancel()
	resp, err := ascv1.NewPathRulesClient(conn).SetPathRules(cctx, req)
	if err != nil {
		return 0, err
	}
	if resp.GetRevision() <= expected {
		return 0, fmt.Errorf("%w: revision", ErrBadAnswer)
	}
	return resp.GetRevision(), nil
}

// PreviewPathAccess shows the level a principal with a repository level would have on each path, under the stored
// rules.
func PreviewPathAccess(ctx context.Context, conn grpc.ClientConnInterface, repoName, principal, repoLevel string, paths []string) (*PathPreview, error) {
	if !pathRulePrincipal.MatchString(principal) {
		return nil, fmt.Errorf("%w: the viewer is user:<login>, agent:<name>, ci:<job> or deploy:<key>", ErrBadInput)
	}
	switch repoLevel {
	case "none", "read", "write", "admin":
	default:
		return nil, fmt.Errorf("%w: the repository level is none, read, write or admin", ErrBadInput)
	}
	if len(paths) == 0 || len(paths) > MaxPathCheckPaths {
		return nil, fmt.Errorf("%w: give 1 to %d paths", ErrBadInput, MaxPathCheckPaths)
	}
	for _, p := range paths {
		if !validRuleDir(p) {
			return nil, fmt.Errorf("%w: a path is relative, with / between names and no . or ..", ErrBadInput)
		}
	}
	cctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	resp, err := ascv1.NewPathRulesClient(conn).CheckPathAccess(cctx, &ascv1.CheckPathAccessRequest{Repo: repoName, Principal: principal, RepoLevel: repoLevel, Paths: paths})
	if err != nil {
		return nil, err
	}
	if len(resp.GetAccess()) != len(paths) {
		return nil, fmt.Errorf("%w: preview", ErrBadAnswer)
	}
	out := &PathPreview{}
	for i, a := range resp.GetAccess() {
		if a.GetPath() != paths[i] || !validLevel(a.GetLevel()) || !validRuleDir(a.GetDecidedBy()) {
			return nil, fmt.Errorf("%w: preview", ErrBadAnswer)
		}
		out.Access = append(out.Access, PathAccess{Path: a.GetPath(), Level: a.GetLevel(), DecidedBy: a.GetDecidedBy()})
	}
	for i, t := range resp.GetTeams() {
		if i >= 64 {
			break
		}
		if c := CleanText(t, 128); c != "" {
			out.Teams = append(out.Teams, c)
		}
	}
	return out, nil
}
