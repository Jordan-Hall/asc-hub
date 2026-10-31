// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	gocontext "context"
	"encoding/hex"

	git_model "forgejo.org/models/git"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
	"forgejo.org/services/gitdiff"
)

// The right rail of the open-change form (board NewChange): the stack the change will sit in once it is open (Changes.Stack
// for the head of the comparison), the files it touches with a Show all, and what the policy will ask for before it can land
// (Policy.Evaluate for the land action). The page reads with the hub's own credential: the viewer has already been checked
// for read access to the repository. Everything asc says is advice, checked and cleaned here, and a failed call leaves its
// part out; none of it blocks the form.

const (
	// ascRailStackMax is the most changes of a stack asc may answer with; more is a bad answer.
	ascRailStackMax = 200
	// ascRailStackShown is how many changes are listed before "and N more".
	ascRailStackShown = 12
	// ascRailFilesShown files are open, the others sit behind Show all; ascRailFilesMax are listed at all.
	ascRailFilesShown = 8
	ascRailFilesMax   = 300
	// ascRailChecksMax and the limits below bound what the policy preview shows.
	ascRailChecksMax     = 20
	ascRailApprovalsMax  = 100
	ascRailCheckBytes    = 64
	ascRailStrategyBytes = 32
	ascRailTitleBytes    = 120
)

// AscRailChange is one change of the stack, shown newest (top) first.
type AscRailChange struct {
	Change  string // the first 12 letters of the change id
	Commit  string // the first 8 letters of the commit
	Title   string // the subject of the commit, "" when the comparison does not hold it
	Current bool   // the change the form opens
}

// AscRailFile is one file of the comparison.
type AscRailFile struct {
	Name     string
	Add, Del int
}

// AscRailPolicy is what the policy asks of a change before it lands.
type AscRailPolicy struct {
	Approvals uint32
	Checks    []string
	Signed    bool
	Strategy  string
}

// Any reports whether the policy asks for anything at all.
func (p *AscRailPolicy) Any() bool {
	return p != nil && (p.Approvals > 0 || len(p.Checks) > 0 || p.Signed || p.Strategy != "")
}

// AscNewChangeRail is the template data under .AscNewChangeRail.
type AscNewChangeRail struct {
	Trunk      string
	Stack      []AscRailChange
	StackMore  int // changes of the stack that are not listed
	Files      []AscRailFile
	FilesShown int
	FilesTotal int
	Add, Del   int
	Policy     *AscRailPolicy
}

// HiddenFiles is how many files wait behind Show all.
func (r *AscNewChangeRail) HiddenFiles() int { return len(r.Files) - r.FilesShown }

// Unlisted is how many files of the comparison are not in Files at all (the list is capped).
func (r *AscNewChangeRail) Unlisted() int { return max(0, r.FilesTotal-len(r.Files)) }

// ascRailStackOf checks the answer of Changes.Stack and lists the changes top first. titles gives the subject of a commit by
// its 40 hex letters; current is the commit the form opens. A bad answer gives nothing.
func ascRailStackOf(resp *ascv1.StackResponse, titles map[string]string, current string) (list []AscRailChange, more int) {
	changes := resp.GetChanges()
	if len(changes) == 0 || len(changes) > ascRailStackMax {
		return nil, 0
	}
	out := make([]AscRailChange, 0, len(changes))
	for _, c := range changes {
		if c == nil || len(c.GetChange()) != 16 || len(c.GetGit()) == 0 || len(c.GetGit()) > 32 {
			return nil, 0
		}
		git := hex.EncodeToString(c.GetGit())
		row := AscRailChange{
			Change:  ascChangeText(c.GetChange())[:12],
			Commit:  git[:min(len(git), 8)],
			Title:   asctrust.CleanText(titles[git], ascRailTitleBytes),
			Current: git == current,
		}
		out = append(out, row)
	}
	// asc answers bottom to top; the rail reads top to bottom
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	if len(out) > ascRailStackShown {
		more = len(out) - ascRailStackShown
		out = out[:ascRailStackShown]
	}
	return out, more
}

// ascRailPolicyOf checks and cleans the land requirements.
func ascRailPolicyOf(land *ascv1.PolicyLandRequirements) *AscRailPolicy {
	if land == nil {
		return nil
	}
	p := &AscRailPolicy{
		Approvals: min(land.GetApprovals(), ascRailApprovalsMax),
		Signed:    land.GetSigned(),
		Strategy:  asctrust.CleanText(land.GetMergeStrategy(), ascRailStrategyBytes),
	}
	for _, c := range land.GetChecks() {
		if c = asctrust.CleanText(c, ascRailCheckBytes); c != "" && len(p.Checks) < ascRailChecksMax {
			p.Checks = append(p.Checks, c)
		}
	}
	return p
}

// ascRailFilesOf lists the files of the diff with their size: ascRailFilesShown of them are open.
func ascRailFilesOf(diff *gitdiff.Diff) (files []AscRailFile, shown, total int) {
	if diff == nil {
		return nil, 0, 0
	}
	total = len(diff.Files)
	for _, f := range diff.Files {
		if len(files) >= ascRailFilesMax {
			break
		}
		files = append(files, AscRailFile{Name: asctrust.CleanText(f.Name, 300), Add: f.Addition, Del: f.Deletion})
	}
	return files, min(len(files), ascRailFilesShown), total
}

// prepareAscNewChangeRail puts the rail of the open-change form into ctx.Data["AscNewChangeRail"]. The files come from
// the diff of the page and are shown for any repository kept in asc; the stack and the policy need asc to answer.
func prepareAscNewChangeRail(ctx *context.Context) {
	if !setting.Asc.Enabled || !ctx.IsSigned || ctx.Repo.Repository.GetStorageBackend() != string(gitrepo.StorageBackendAsc) {
		return
	}
	rail := &AscNewChangeRail{}
	if diff, ok := ctx.Data["Diff"].(*gitdiff.Diff); ok && diff != nil {
		rail.Files, rail.FilesShown, rail.FilesTotal = ascRailFilesOf(diff)
		rail.Add, rail.Del = diff.TotalAddition, diff.TotalDeletion
	}
	name, err := ascGraphRepoName(ctx.Repo.Repository)
	if err == nil {
		prepareAscRailFromAsc(ctx, name, rail)
	}
	if len(rail.Files) > 0 || len(rail.Stack) > 0 || rail.Policy.Any() {
		ctx.Data["AscNewChangeRail"] = rail
	}
}

func prepareAscRailFromAsc(ctx *context.Context, name string, rail *AscNewChangeRail) {
	conn, err := ascRPCConn()
	if err != nil {
		return
	}
	callCtx, cancel := gocontext.WithTimeout(ascclient.WithSystem(ctx), ascBriefTimeout)
	defer cancel()
	repo := &ascv1.RepoRef{Name: name}
	if resp, err := ascv1.NewPolicyClient(conn).Evaluate(callCtx, &ascv1.EvaluatePolicyRequest{Repo: repo, Action: ascv1.PolicyAction_POLICY_ACTION_LAND}); err == nil {
		rail.Policy = ascRailPolicyOf(resp.GetLand())
	} else {
		log.Warn("asc new change rail: policy: %v", err)
	}
	base, _ := ctx.Data["BaseBranch"].(string)
	head, _ := ctx.Data["AfterCommitID"].(string)
	if base == "" || head == "" {
		return
	}
	resp, err := ascv1.NewChangesClient(conn).Stack(callCtx, &ascv1.StackRequest{Repo: repo, Target: base, At: &ascv1.StackRequest_Rev{Rev: head}})
	if err != nil {
		log.Warn("asc new change rail: stack: %v", err)
		return
	}
	titles := map[string]string{}
	if commits, ok := ctx.Data["Commits"].([]*git_model.SignCommitWithStatuses); ok {
		for _, c := range commits {
			if c != nil && c.Commit != nil {
				titles[c.ID.String()] = c.Summary()
			}
		}
	}
	rail.Trunk = asctrust.CleanText(resp.GetTrunk(), 255)
	rail.Stack, rail.StackMore = ascRailStackOf(resp, titles, head)
}
