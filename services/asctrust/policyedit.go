// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/log"
	files_service "forgejo.org/services/repository/files"

	"google.golang.org/grpc"
)

// An edit of the policy file through the file editor or the contents API.
//
// The editor and the API commit through the hub's writer, which writes with the hub's service credential, and asc
// decides every write of that credential as the hub itself: no maintainer, so the policy file could not be committed
// to trunk by anyone. An edit of .asc/policy.toml for the default branch is therefore not committed. It is proposed,
// the way the policy page proposes (Policy.Propose, a change on top of trunk on a draft ref, never trunk), as the
// person: the call carries the person's claim, asc is asked whether that person may write the policy file
// (Policy.Evaluate, which is the same check the gate makes of a push: a maintainer of the policy in force), and a
// person it refuses gets asc's reason. The proposal starts from the trunk the editor was opened on, so an edit made
// on a stale file is a conflict and never an overwrite. A maintainer then lands the change like any other.
//
// A repository without a usable policy file is not touched here: there is no maintainer list to ask, and the first
// file or the repair of a broken one is written by the gate's own rules.

func init() {
	files_service.AscPolicyEdit = proposePolicyEdit
}

// proposePolicyEdit is files_service.AscPolicyEdit.
func proposePolicyEdit(ctx context.Context, repo *repo_model.Repository, doer *user_model.User, opts *files_service.ChangeRepoFilesOptions) error {
	if repo == nil || opts == nil || opts.OldBranch != repo.DefaultBranch {
		return nil
	}
	name, err := RepoName(repo)
	if err != nil {
		return nil // not an asc repository, or asc is off
	}
	touched := false
	for _, f := range opts.Files {
		if touchesPolicy(files_service.CleanUploadFileName(f.TreePath)) || (f.FromTreePath != "" && touchesPolicy(files_service.CleanUploadFileName(f.FromTreePath))) {
			touched = true
		}
	}
	if !touched {
		return nil
	}
	refuse := func(reason string) error { return &files_service.ErrAscPolicyRefused{Reason: reason} }

	conn, err := Conn()
	if err != nil {
		return refuse("The ASC server is not reachable right now. Nothing was changed.")
	}
	callCtx, release, err := UserCall(ctx, doer, CallRepoWrite, repo)
	if err != nil {
		return refuse("You may not change this repository.")
	}
	defer release()

	pol, err := PolicyFileInForce(callCtx, conn, name)
	if err != nil {
		return refuse(FailureSentence("policy", err))
	}
	if !pol.Usable {
		return nil // nothing to enforce or repair yet: the commit goes on under the gate's own rules
	}

	if len(opts.Files) != 1 {
		return refuse("The policy file is changed on its own: edit it in a commit that touches nothing else, or on a branch.")
	}
	f := opts.Files[0]
	switch {
	case f.Operation != "create" && f.Operation != "update":
		return refuse("The policy file is not deleted. Edit it, or open the policy page.")
	case files_service.CleanUploadFileName(f.TreePath) != policyPath,
		f.FromTreePath != "" && files_service.CleanUploadFileName(f.FromTreePath) != policyPath:
		return refuse("The policy file is edited under its own name, .asc/policy.toml.")
	case f.ContentReader == nil:
		return refuse("The policy file has no content.")
	}
	raw, err := io.ReadAll(io.LimitReader(f.ContentReader, MaxPolicyBytes+1))
	if err != nil {
		return refuse("The policy file could not be read.")
	}
	text := strings.ReplaceAll(string(raw), "\r", "")
	if err := CheckText(text); err != nil {
		return refuse(FailureSentence("policy", err))
	}

	// asc decides whether this person may write the policy file: the claim of the call makes it their decision.
	if ok, why, err := MayWritePolicy(callCtx, conn, name); err != nil {
		return refuse(FailureSentence("policy", err))
	} else if !ok {
		if why == "" {
			why = "the repository policy can be changed only by a maintainer"
		}
		return refuse("You may not change the policy: " + CleanText(why, MaxTextBytes) + ". Ask a maintainer.")
	}

	if strings.TrimSpace(text) == strings.TrimSpace(pol.Text) {
		return refuse("Nothing changed: the policy file already says this.")
	}
	prop, err := ProposePolicy(callCtx, conn, name, pol.Base, text, strings.TrimSpace(opts.Message), NewOpID())
	if err != nil {
		return refuse(FailureSentence("policy", err))
	}
	out := &files_service.ErrAscPolicyProposed{Change: prop.Short}
	pr, err := OpenProposalPull(ctx, conn, repo, name, doer, prop, opts.Message, DefaultPullSeams)
	switch {
	case err == nil:
		if lerr := pr.LoadIssue(ctx); lerr != nil {
			log.Error("asc policy edit: load the issue of pull request %d: %v", pr.ID, lerr)
		} else {
			out.Link = pr.Issue.Link()
		}
	case !errors.Is(err, ErrNotMirrored):
		log.Error("asc policy edit: open the pull request of change %s: %v", prop.Short, err)
	}
	return out
}

// PolicyFile is the policy file in force as an edit needs it.
type PolicyFile struct {
	// Usable is true when the file is present and valid.
	Usable bool
	Text   string
	// Base is the trunk changeset it was read from.
	Base []byte
}

// PolicyFileInForce reads the policy file of the repository: its text, and the trunk changeset a proposal starts from.
func PolicyFileInForce(ctx context.Context, conn grpc.ClientConnInterface, repoName string) (*PolicyFile, error) {
	src, err := getPolicySource(ctx, conn, repoName)
	if err != nil {
		return nil, err
	}
	return &PolicyFile{
		Usable: src.GetPresent() && src.GetParsed().GetError() == "",
		Text:   string(src.GetText()),
		Base:   src.GetSource(),
	}, nil
}

// MayWritePolicy asks asc whether the person of the call may write the policy file to trunk, and when not, why.
func MayWritePolicy(ctx context.Context, conn grpc.ClientConnInterface, repoName string) (bool, string, error) {
	dec, err := evaluateWrite(ctx, conn, repoName, policyPath)
	if err != nil {
		return false, "", err
	}
	return dec.GetAllowed(), dec.GetReason(), nil
}

func getPolicySource(ctx context.Context, conn grpc.ClientConnInterface, repoName string) (*ascv1.GetPolicySourceResponse, error) {
	cctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	src, err := ascv1.NewPolicyClient(conn).GetSource(cctx, &ascv1.GetPolicySourceRequest{Repo: RepoRef(repoName)})
	if err != nil {
		return nil, err
	}
	if len(src.GetText()) > MaxPolicyBytes {
		return nil, fmt.Errorf("%w: policy text", ErrBadAnswer)
	}
	if src.GetPresent() && len(src.GetSource()) != 0 && len(src.GetSource()) != 32 {
		return nil, fmt.Errorf("%w: policy source", ErrBadAnswer)
	}
	return src, nil
}

// evaluateWrite asks asc whether the person of the call may write path to trunk.
func evaluateWrite(ctx context.Context, conn grpc.ClientConnInterface, repoName, path string) (*ascv1.PolicyDecision, error) {
	cctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	resp, err := ascv1.NewPolicyClient(conn).Evaluate(cctx, &ascv1.EvaluatePolicyRequest{
		Repo: RepoRef(repoName), Action: ascv1.PolicyAction_POLICY_ACTION_WRITE, Paths: [][]byte{[]byte(path)},
	})
	if err != nil {
		return nil, err
	}
	// One decision for the one path, and for that path; anything else is not an answer to the question.
	if len(resp.GetDecisions()) != 1 || string(resp.GetDecisions()[0].GetPath()) != path {
		return nil, fmt.Errorf("%w: policy decision", ErrBadAnswer)
	}
	return resp.GetDecisions()[0], nil
}

// FailureSentence is the sentence for a person about a failed policy call. The text asc sent is never part of it.
func FailureSentence(what string, err error) string {
	e := Classify(err)
	switch {
	case e == nil:
		return ""
	case errors.Is(err, ErrBadInput):
		return "The " + what + " file is not valid: " + CleanText(err.Error(), MaxTextBytes) + "."
	case e.Code == "POLICY_LOCKOUT":
		return "The proposed file would not name you as a maintainer, so it would lock you out. Nothing was changed."
	case e.Code == "POLICY_INVALID" || e.Kind == KindInvalid:
		return "asc does not accept this " + what + " file. Open the policy page to see what is wrong."
	case e.Kind == KindConflict:
		return "The " + what + " changed while you were editing. Reload the file, apply your edit again, and save."
	case e.Kind == KindDenied:
		return "You may not change the " + what + " of this repository."
	case e.Kind == KindNotFound:
		return "The repository was not found."
	case e.Kind == KindUnimplemented:
		return "This server does not have the policy API yet."
	}
	return "The ASC server could not be asked right now. Nothing was changed."
}
