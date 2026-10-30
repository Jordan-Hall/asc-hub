// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascagentui

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/optional"
	"forgejo.org/modules/repostore"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/structs"
	"forgejo.org/services/ascsync"
	"forgejo.org/services/asctrust"
	pull_service "forgejo.org/services/pull"

	"google.golang.org/grpc"
)

// An agent run's changes become pull requests (docs/AGENTS-UI.md).
//
// An agent writes its work as drafts: a stack of changes under refs/users/<person>/heads/agent/..., which only asc
// shows and which never reach the git view of the repository. Nothing in the hub opens a review for them, so a run
// that finished left changes that no one could review. Opening the run for review does what a person's `asc submit`
// does for each change, bottom to top:
//
//   - the head of the change is published under a short-lived shared branch (asc/agent/<agent>/run-<n>-<change>), the
//     only way a draft reaches the git view; the pull request keeps the commit once it has its own head ref and the
//     branch is removed again;
//   - the pull request is an AGit pull request of the change (found by repository and change, so a change has one open
//     pull request however often the run is opened) in the stack of the run: each change sits on the one below;
//   - the poster is the agent's own bot account of the hub (a user of type bot, which cannot sign in), so the agent
//     mark shows wherever a poster is shown, and the pull request remembers the run (issues_model.PullRequestAscRun),
//     which the pull request page links back to.
//
// The person who opens the run needs write access to the repository; the hub's service credential does the writing,
// and what an agent may write is decided by asc when the run is made, never here. A change that is not in the git
// view yet, or was landed, is left out and reported; opening the run again picks it up.

// MaxRunPulls bounds the changes one opening handles.
const MaxRunPulls = 64

// The outcome of one change of a run.
const (
	RunPullOpened  = "opened"  // a pull request was opened now
	RunPullLinked  = "linked"  // the change already had an open pull request
	RunPullWaiting = "waiting" // asc has no git id for the change yet: try again
	RunPullLanded  = "landed"  // the change is part of trunk
	RunPullFailed  = "failed"  // the pull request could not be opened; the run page says nothing more
)

// RunPullRun is what opening a run needs to know of it. Every field comes from asc's answer for the run.
type RunPullRun struct {
	ID        uint64
	Agent     string // [a-z0-9-], 1 to 40 characters
	Title     string
	Task      string
	CreatedBy string // login of the person who started it
	// Changes of the run, bottom to top (AgentRun.changes).
	Changes [][]byte
	// RunURL is the address of the run page, which the pull request links to.
	RunURL string
}

// RunPull is the outcome for one change.
type RunPull struct {
	Change string
	Short  string
	State  string
	// Index is the number of the pull request, 0 without one.
	Index int64
}

// RunPullSeams are the parts of OpenRunPulls that need asc or a git view. Tests replace them.
type RunPullSeams struct {
	// Resolve returns what asc knows of the changes (ChangeInfo), in the order asked, nil for none.
	Resolve func(ctx context.Context, conn grpc.ClientConnInterface, repoName string, changes [][]byte) ([]*ascv1.ChangeInfo, error)
	// Stage points the shared branch ref at the git commit and returns once the git view has it.
	Stage func(ctx context.Context, repo *repo_model.Repository, ref, commit string) error
	// Unstage removes the shared branch again. It is best effort.
	Unstage func(ctx context.Context, repo *repo_model.Repository, ref string) error
	// Commit reads the message of the commit, "" when it is not there.
	Commit func(ctx context.Context, repo *repo_model.Repository, commit string) (string, error)
	// Create opens the pull request of the change as bot, on top of the pull request parentID (0 for the bottom of a
	// stack), and links it to the change.
	Create func(ctx context.Context, repo *repo_model.Repository, bot *user_model.User, change, commit, title, body string, parentID int64) (*issues_model.PullRequest, error)
}

// DefaultRunPullSeams are the seams of production.
var DefaultRunPullSeams = RunPullSeams{Resolve: resolveChanges, Stage: stageRef, Unstage: unstageRef, Commit: commitMessage, Create: createRunPull}

// ErrAgentNameTaken is returned when the account the agent would post as belongs to a person.
var ErrAgentNameTaken = errors.New("ascagentui: the name of the agent's account belongs to a person")

// AgentUserName is the login of the bot account of an agent: "agent-" and the agent's name, shortened with a digest
// when the name would be too long for a login.
func AgentUserName(agent string) string {
	name := "agent-" + agent
	if len(name) <= 40 {
		return name
	}
	sum := sha256.Sum256([]byte(agent))
	return name[:31] + "-" + hex.EncodeToString(sum[:4])
}

// EnsureAgentUser returns the bot account of the agent, made when there is none. An existing account of that name
// that is no bot is a person's and is never used.
func EnsureAgentUser(ctx context.Context, agent string) (*user_model.User, error) {
	if !ValidAgentName(agent) {
		return nil, fmt.Errorf("ascagentui: bad agent name")
	}
	name := AgentUserName(agent)
	get := func() (*user_model.User, error) {
		u, err := user_model.GetUserByName(ctx, name)
		if err != nil {
			return nil, err
		}
		if !u.IsBot() {
			return nil, ErrAgentNameTaken
		}
		return u, nil
	}
	if u, err := get(); err == nil || !user_model.IsErrUserNotExist(err) {
		return u, err
	}
	var pass [32]byte
	if _, err := rand.Read(pass[:]); err != nil {
		return nil, err
	}
	public := structs.VisibleTypePublic
	noReply := setting.Service.NoReplyAddress
	if noReply == "" {
		noReply = "noreply.invalid"
	}
	u := &user_model.User{
		Name:             name,
		LowerName:        strings.ToLower(name),
		FullName:         agent + " (agent)",
		Email:            name + "@" + noReply,
		Passwd:           hex.EncodeToString(pass[:]),
		Type:             user_model.UserTypeBot,
		ProhibitLogin:    true,
		IsActive:         true,
		KeepEmailPrivate: true,
		Visibility:       public,
	}
	err := user_model.AdminCreateUser(ctx, u, &user_model.CreateUserOverwriteOptions{
		IsActive:         optional.Some(true),
		KeepEmailPrivate: optional.Some(true),
		Visibility:       &public,
	})
	if err != nil {
		// A second opening made it first.
		if again, gerr := get(); gerr == nil {
			return again, nil
		}
		return nil, err
	}
	return u, nil
}

// OpenRunPulls opens a pull request for each change of the run that has none, bottom to top, as the bot account of
// the agent. It never fails the others for one change: the outcome of every change is in the answer. conn and ctx
// carry the hub's service credential (the changes are read, nothing is written in asc but the short-lived branch).
func OpenRunPulls(ctx context.Context, conn grpc.ClientConnInterface, repo *repo_model.Repository, run RunPullRun, seams RunPullSeams) ([]RunPull, error) {
	repoName, err := asctrust.RepoName(repo)
	if err != nil {
		return nil, err
	}
	if run.ID == 0 || !ValidAgentName(run.Agent) {
		return nil, fmt.Errorf("%w: run", asctrust.ErrBadInput)
	}
	raw := run.Changes
	if len(raw) > MaxRunPulls {
		raw = raw[:MaxRunPulls]
	}
	texts := make([]string, len(raw))
	for i, c := range raw {
		texts[i] = ChangeText(c)
		if texts[i] == "" {
			return nil, fmt.Errorf("%w: change id", asctrust.ErrBadInput)
		}
	}
	if len(raw) == 0 {
		return nil, nil
	}
	infos, err := seams.Resolve(ctx, conn, repoName, raw)
	if err != nil {
		return nil, err
	}
	if len(infos) != len(raw) {
		return nil, fmt.Errorf("%w: resolved changes", asctrust.ErrBadAnswer)
	}
	bot, err := EnsureAgentUser(ctx, run.Agent)
	if err != nil {
		return nil, err
	}

	out := make([]RunPull, 0, len(raw))
	var below *issues_model.PullRequest // the pull request of the change under this one, when the run's stack is joined
	for i, change := range texts {
		res := RunPull{Change: change, Short: ShortChange(change)}
		info := infos[i]
		pr, state, perr := openRunPull(ctx, repo, run, bot, change, info, raw[i], below, seams)
		if perr != nil {
			log.Error("asc agents: opening change %s of run %d for review: %v", res.Short, run.ID, perr)
			state = RunPullFailed
		}
		res.State = state
		if pr != nil {
			res.Index = pr.Index
		}
		// The next change sits on this one only when this one has a pull request; otherwise it is a new bottom.
		below = pr
		out = append(out, res)
	}
	return out, nil
}

func openRunPull(ctx context.Context, repo *repo_model.Repository, run RunPullRun, bot *user_model.User, change string, info *ascv1.ChangeInfo, rawChange []byte, below *issues_model.PullRequest, seams RunPullSeams) (*issues_model.PullRequest, string, error) {
	if info == nil || string(info.GetChange()) != string(rawChange) {
		return nil, RunPullWaiting, nil
	}
	if len(info.GetLandedAs()) != 0 {
		return nil, RunPullLanded, nil
	}
	existing, err := issues_model.GetOpenPullRequestByAscChange(ctx, repo.ID, change)
	switch {
	case err == nil:
		// A pull request of the change that is already open is the review of it, whoever opened it.
		return existing, RunPullLinked, nil
	case !issues_model.IsErrPullRequestNotExist(err):
		return nil, RunPullFailed, err
	}
	gitID := info.GetGit()
	if len(gitID) != 20 {
		return nil, RunPullWaiting, nil
	}
	commit := hex.EncodeToString(gitID)

	ref := git.BranchPrefix + "asc/agent/" + run.Agent + "/run-" + strconv.FormatUint(run.ID, 10) + "-" + ShortChange(change)
	// The branch is removed whatever happens next, also after a publish that failed half way.
	defer func() {
		if uerr := seams.Unstage(context.WithoutCancel(ctx), repo, ref); uerr != nil {
			log.Warn("asc agents: removing the branch %s: %v", ref, uerr)
		}
	}()
	if err := seams.Stage(ctx, repo, ref, commit); err != nil {
		return nil, RunPullFailed, fmt.Errorf("publish the draft: %w", err)
	}
	message, err := seams.Commit(ctx, repo, commit)
	if err != nil {
		return nil, RunPullFailed, err
	}
	if message == "" {
		return nil, RunPullWaiting, nil
	}
	title, body := splitMessage(message)
	if title == "" {
		title = CleanLine(run.Title, 200)
	}
	body = runProvenance(run, body)

	// Join the stack of the run: the change under this one is its parent when asc says so.
	var parentID int64
	if below != nil && len(info.GetParentChange()) != 0 && ChangeText(info.GetParentChange()) != "" {
		if belowStack, serr := issues_model.GetPullRequestAscStack(ctx, below.ID); serr == nil && belowStack != nil && belowStack.AscChange == ChangeText(info.GetParentChange()) {
			parentID = below.ID
		}
	}
	pr, err := seams.Create(ctx, repo, bot, change, commit, title, body, parentID)
	if err != nil {
		return nil, RunPullFailed, err
	}
	if err := issues_model.SetPullRequestAscRun(ctx, pr, run.ID, run.Agent, run.CreatedBy); err != nil {
		return pr, RunPullOpened, err
	}
	return pr, RunPullOpened, nil
}

func createRunPull(ctx context.Context, repo *repo_model.Repository, bot *user_model.User, change, commit, title, body string, parentID int64) (*issues_model.PullRequest, error) {
	issue := &issues_model.Issue{
		RepoID:   repo.ID,
		Title:    title,
		PosterID: bot.ID,
		Poster:   bot,
		IsPull:   true,
		Content:  body,
	}
	pr := &issues_model.PullRequest{
		HeadRepoID:   repo.ID,
		BaseRepoID:   repo.ID,
		HeadBranch:   strings.ToLower(bot.Name) + "/asc-" + change,
		HeadCommitID: commit,
		BaseBranch:   repo.DefaultBranch,
		HeadRepo:     repo,
		BaseRepo:     repo,
		Type:         issues_model.PullRequestGitea,
		Flow:         issues_model.PullRequestFlowAGit,
	}
	if err := pull_service.NewPullRequest(ctx, repo, issue, nil, nil, pr, nil); err != nil {
		return nil, fmt.Errorf("open the pull request: %w", err)
	}
	if err := issues_model.SetPullRequestAscStack(ctx, pr, change, parentID); err != nil {
		return nil, fmt.Errorf("link the pull request to its change: %w", err)
	}
	if err := pull_service.RecomputeAscStackBase(ctx, pr); err != nil {
		return nil, fmt.Errorf("stack base: %w", err)
	}
	return pr, nil
}

// splitMessage returns the title (first line) and the rest of a commit message.
func splitMessage(m string) (title, body string) {
	m = strings.TrimSpace(m)
	first, rest, _ := strings.Cut(m, "\n")
	return CleanLine(first, 200), strings.TrimSpace(rest)
}

// runProvenance puts the line that says where the pull request came from first in its description: the agent, the
// run, the person who started it, and the task as the person gave it.
func runProvenance(run RunPullRun, body string) string {
	var b strings.Builder
	b.WriteString("Opened for ")
	if run.RunURL != "" {
		b.WriteString("[agent run #" + strconv.FormatUint(run.ID, 10) + "](" + run.RunURL + ")")
	} else {
		b.WriteString("agent run #" + strconv.FormatUint(run.ID, 10))
	}
	b.WriteString(" of the agent `" + run.Agent + "`")
	if run.CreatedBy != "" {
		b.WriteString(", started by @" + run.CreatedBy)
	}
	b.WriteString(". It is a change under review like any other: read it, ask for another version, or land it.")
	if t := strings.TrimSpace(CleanText(run.Task, 1000)); t != "" {
		b.WriteString("\n\n> " + strings.ReplaceAll(t, "\n", "\n> "))
	}
	if body != "" {
		b.WriteString("\n\n" + body)
	}
	return b.String()
}

// ------------------------------------------------------------------------------------------------ production seams

func resolveChanges(ctx context.Context, conn grpc.ClientConnInterface, repoName string, changes [][]byte) ([]*ascv1.ChangeInfo, error) {
	cctx, cancel := context.WithTimeout(ctx, asctrust.CallTimeout)
	defer cancel()
	resp, err := ascv1.NewChangesClient(conn).ResolveChanges(cctx, &ascv1.ResolveChangesRequest{Repo: asctrust.RepoRef(repoName), Changes: changes})
	if err != nil {
		return nil, err
	}
	// asc answers the changes it knows, in the order asked; match them by id so a missing one is not shifted.
	byID := make(map[string]*ascv1.ChangeInfo, len(resp.GetChanges()))
	for _, c := range resp.GetChanges() {
		byID[string(c.GetChange())] = c
	}
	out := make([]*ascv1.ChangeInfo, len(changes))
	for i, c := range changes {
		out[i] = byID[string(c)]
	}
	return out, nil
}

func stageRef(ctx context.Context, repo *repo_model.Repository, ref, commit string) error {
	w, err := repostore.Default()
	if err != nil {
		return err
	}
	res, err := w.UpdateRefs(ctx, repostore.UpdateRefsRequest{
		Repo:    repo.StoreRef(),
		Updates: []repostore.RefUpdateSpec{{Name: ref, NewRev: commit, Force: true}},
	})
	if err != nil {
		return err
	}
	for _, r := range res {
		if r.Name == ref && r.New != nil {
			return ascsync.WaitForRefs(ctx, repo, []ascsync.PushRef{{Name: r.Name, New: r.New.String()}})
		}
	}
	return errors.New("the branch was not set")
}

func unstageRef(ctx context.Context, repo *repo_model.Repository, ref string) error {
	w, err := repostore.Default()
	if err != nil {
		return err
	}
	_, err = w.UpdateRefs(ctx, repostore.UpdateRefsRequest{
		Repo:    repo.StoreRef(),
		Updates: []repostore.RefUpdateSpec{{Name: ref, Delete: true, Force: true}},
	})
	return err
}

func commitMessage(ctx context.Context, repo *repo_model.Repository, commit string) (string, error) {
	gitRepo, err := gitrepo.OpenRepository(ctx, repo)
	if err != nil {
		return "", err
	}
	defer gitRepo.Close()
	c, err := gitRepo.GetCommit(commit)
	if err != nil {
		return "", nil
	}
	return c.CommitMessage, nil
}
