// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package actions

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	actions_model "forgejo.org/models/actions"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/asctrust"

	"code.forgejo.org/forgejo/runner/v12/act/jobparser"
	"go.yaml.in/yaml/v3"
)

// A job may name the projects it builds with `affected:` (SPEC §12.8): a project name, a project
// root, or a path glob, as one string or a list. On an asc repository the job is skipped when the
// push or pull request changes none of them, so CI does only the work the change needs. The key is
// an asc extension: it is taken out of the workflow before the job parser sees it.
const (
	ascAffectedKey = "affected"
	// ascNotAffected marks the `if: false` that skipped a job, so the run page can say why.
	ascNotAffected = "asc-not-affected:"
	// maxAffectedSelectors bounds one job's list.
	maxAffectedSelectors = 256
	ascAffectedTimeout   = 20 * time.Second
)

// splitAffected takes the `affected:` keys out of a workflow. It returns the content unchanged
// when no job has one.
func splitAffected(content []byte) ([]byte, map[string][]string, error) {
	if !bytes.Contains(content, []byte(ascAffectedKey)) {
		return content, nil, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(content, &doc); err != nil || len(doc.Content) == 0 {
		return content, nil, nil // the job parser reports the syntax error
	}
	jobs := mapValue(doc.Content[0], "jobs")
	if jobs == nil || jobs.Kind != yaml.MappingNode {
		return content, nil, nil
	}
	selectors := map[string][]string{}
	for i := 0; i+1 < len(jobs.Content); i += 2 {
		id, job := jobs.Content[i].Value, jobs.Content[i+1]
		if job.Kind != yaml.MappingNode {
			continue
		}
		for k := 0; k+1 < len(job.Content); k += 2 {
			if job.Content[k].Value != ascAffectedKey {
				continue
			}
			list, err := selectorList(job.Content[k+1])
			if err != nil {
				return nil, nil, fmt.Errorf("job %q: %w", id, err)
			}
			selectors[id] = list
			job.Content = slices.Delete(job.Content, k, k+2)
			break
		}
	}
	if len(selectors) == 0 {
		return content, nil, nil
	}
	out, err := yaml.Marshal(&doc)
	if err != nil {
		return nil, nil, err
	}
	return out, selectors, nil
}

func mapValue(m *yaml.Node, key string) *yaml.Node {
	if m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func selectorList(n *yaml.Node) ([]string, error) {
	var items []*yaml.Node
	switch n.Kind {
	case yaml.ScalarNode:
		items = []*yaml.Node{n}
	case yaml.SequenceNode:
		items = n.Content
	default:
		return nil, errors.New("affected must be a string or a list of strings")
	}
	if len(items) == 0 || len(items) > maxAffectedSelectors {
		return nil, fmt.Errorf("affected needs 1 to %d entries", maxAffectedSelectors)
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		s := strings.Trim(strings.TrimSpace(it.Value), "/")
		if it.Kind != yaml.ScalarNode || s == "" || strings.ContainsAny(s, "\x00\n,") {
			return nil, errors.New("affected entries must be non-empty strings")
		}
		if _, err := path.Match(s, ""); err != nil {
			return nil, fmt.Errorf("affected entry %q: %w", s, err)
		}
		out = append(out, s)
	}
	return out, nil
}

// affectedSet is what changed: the affected projects' names and roots, or everything.
type affectedSet struct {
	all   bool
	names map[string]struct{}
	roots []string
}

func (a *affectedSet) matches(sel string) bool {
	if a.all {
		return true
	}
	if _, ok := a.names[sel]; ok {
		return true
	}
	for _, root := range a.roots {
		if root == sel || strings.HasPrefix(root, sel+"/") || strings.HasPrefix(sel, root+"/") {
			return true
		}
		if ok, _ := path.Match(sel, root); ok {
			return true
		}
	}
	return false
}

// unaffectedJobs returns the jobs, among those with selectors, that none of their selectors match.
func unaffectedJobs(selectors map[string][]string, set *affectedSet) map[string][]string {
	out := map[string][]string{}
	for id, sels := range selectors {
		if !slices.ContainsFunc(sels, set.matches) {
			out[id] = sels
		}
	}
	return out
}

// affectedFn asks asc what changed for a run; a seam for tests.
var affectedFn = ascAffectedForRun

// ascAffectedForRun asks the asc server which projects the run's change touches: the pull request
// against its merge base, a push against the commit before it. It returns nil (run everything) when
// there is no base to compare with or the repository is not on asc.
func ascAffectedForRun(ctx context.Context, run *actions_model.ActionRun) (*affectedSet, error) {
	sb, err := gitrepo.StorageBackendOf(run.Repo)
	if err != nil || sb != gitrepo.StorageBackendAsc || !setting.Asc.Enabled {
		return nil, err
	}
	base, mergeBase := "", false
	if pr, err := run.GetPullRequestEventPayload(); err == nil && pr.PullRequest != nil && pr.PullRequest.Base != nil {
		base, mergeBase = pr.PullRequest.Base.Sha, true
	} else if push, err := run.GetPushEventPayload(); err == nil && push.Before != "" && strings.Trim(push.Before, "0") != "" {
		base = push.Before
	}
	if base == "" {
		return nil, nil
	}
	name, err := asctrust.RepoName(run.Repo)
	if err != nil {
		return nil, err
	}
	conn, err := asctrust.Conn()
	if err != nil {
		return nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, ascAffectedTimeout)
	defer cancel()
	resp, err := ascv1.NewProjectsClient(conn).Affected(cctx, &ascv1.AffectedRequest{
		Repo: asctrust.RepoRef(name), Base: base, Head: run.CommitSHA, NoMergeBase: !mergeBase,
	})
	if err != nil {
		return nil, err
	}
	set := &affectedSet{all: resp.GetAll(), names: map[string]struct{}{}}
	for _, p := range resp.GetProjects() {
		info := p.GetProject()
		set.names[string(info.GetName())] = struct{}{}
		set.names[info.GetId()] = struct{}{}
		set.roots = append(set.roots, strings.Trim(string(info.GetRoot()), "/"))
	}
	return set, nil
}

// skipUnaffected turns each unaffected job's `if` into a marked `false`, so InsertRunJobs skips it
// and calculateWarnings names it on the run page. Any failure to learn what changed runs every job.
func skipUnaffected(ctx context.Context, run *actions_model.ActionRun, selectors map[string][]string, jobs []*jobparser.SingleWorkflow) {
	if len(selectors) == 0 {
		return
	}
	set, err := affectedFn(ctx, run)
	if err != nil {
		log.Warn("asc affected for run %d: %v; running every job", run.ID, err)
		return
	}
	if set == nil {
		return
	}
	skip := unaffectedJobs(selectors, set)
	for _, v := range jobs {
		id, job := v.Job()
		sels, ok := skip[id]
		if job == nil || !ok {
			continue
		}
		job.If = yaml.Node{Kind: yaml.ScalarNode, Value: "false", LineComment: "# " + ascNotAffected + " " + strings.Join(sels, ", ")}
		if err := v.SetJob(id, job); err != nil {
			log.Warn("asc affected: skip job %q: %v", id, err)
		}
	}
}

// notAffectedSelectors reports whether skipUnaffected skipped the job, and with which selectors.
func notAffectedSelectors(j *jobparser.Job) (string, bool) {
	c := strings.TrimSpace(strings.TrimPrefix(j.If.LineComment, "#"))
	if j.If.Value != "false" || !strings.HasPrefix(c, ascNotAffected) {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(c, ascNotAffected)), true
}
