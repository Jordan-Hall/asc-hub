// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package actions

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	actions_model "forgejo.org/models/actions"
	issues_model "forgejo.org/models/issues"

	"google.golang.org/protobuf/types/known/structpb"
)

// The keys C40 adds to the github context of a run for a stacked pull request (SPEC section 20.8). A run
// that is not for a stacked pull request, and a repository on any other backend, get none of them.
const (
	// ascContextChange is the asc change id (32 letters k-z) of the pull request.
	ascContextChange = "asc_change"
	// ascContextStackPosition is the 1-based position of the change in its stack, counted from the bottom.
	// Changes whose parent pull request has merged count from the first unmerged one.
	ascContextStackPosition = "asc_stack_position"
	// ascContextStackSize is the number of unmerged changes in the stack, the bottom to the top.
	ascContextStackSize = "asc_stack_size"
	// ascContextStackParentSHA is the git SHA the change's own diff starts from (the pull request's
	// merge_base, which the C34 rule sets to the parent change's head). `asc affected --base` takes it.
	ascContextStackParentSHA = "asc_stack_parent_sha"
)

// ascStackMaxDepth bounds the walk along a stack. A longer chain, or a cycle, is data corruption and fails
// the run closed.
const ascStackMaxDepth = 1000

// addAscStackContext adds the C40 keys to a task context built for run, after addAscContext has accepted the
// repository as an asc one. It does nothing for a run that is not for a pull request, or for a pull request
// that is not stacked. Every value comes from the database, never from the event payload, which a pull
// request author can influence. It fails closed: a stacked pull request whose stack cannot be read, or has
// no merge base, gets no task, because `asc affected --base` with a blank or wrong base would test the wrong
// projects.
func addAscStackContext(ctx context.Context, taskContext *structpb.Struct, run *actions_model.ActionRun) error {
	payload, err := run.GetPullRequestEventPayload()
	if err != nil || payload.PullRequest == nil {
		return nil //nolint:nilerr // not a pull request event
	}
	pr, err := issues_model.GetPullRequestByIndex(ctx, run.Repo.ID, payload.PullRequest.Index)
	if err != nil {
		if issues_model.IsErrPullRequestNotExist(err) {
			return nil
		}
		return err
	}
	row, err := issues_model.GetPullRequestAscStack(ctx, pr.ID)
	if err != nil {
		return err
	}
	if row == nil {
		return nil
	}
	if !issues_model.IsAscChange(row.AscChange) {
		return fmt.Errorf("pull request %d has a malformed asc change id", pr.ID)
	}
	if pr.MergeBase == "" {
		return fmt.Errorf("pull request %d has no merge base", pr.ID)
	}
	if _, err := parseSha1ID(pr.MergeBase); err != nil {
		return fmt.Errorf("pull request merge base: %w", err)
	}

	// Below: the unmerged ancestors.
	below := 0
	seen := map[int64]bool{pr.ID: true}
	for cur := row; cur.StackParentID != 0; {
		if seen[cur.StackParentID] || below >= ascStackMaxDepth {
			return errors.New("the stack of the pull request is cyclic or too deep")
		}
		seen[cur.StackParentID] = true
		parent, err := issues_model.GetPullRequestByID(ctx, cur.StackParentID)
		if err != nil {
			if issues_model.IsErrPullRequestNotExist(err) {
				break // a parent that is gone does not hold the stack up
			}
			return err
		}
		if parent.HasMerged {
			break
		}
		below++
		next, err := issues_model.GetPullRequestAscStack(ctx, parent.ID)
		if err != nil {
			return err
		}
		if next == nil {
			break
		}
		cur = next
	}

	// Above: the descendants, along the oldest child at a fork. Merged and closed children do not count.
	above := 0
	for cur := pr.ID; ; {
		children, err := issues_model.GetAscStackChildren(ctx, cur)
		if err != nil {
			return err
		}
		next := int64(0)
		for _, c := range children {
			child, err := issues_model.GetPullRequestByID(ctx, c.PullID)
			if err != nil {
				if issues_model.IsErrPullRequestNotExist(err) {
					continue
				}
				return err
			}
			if child.HasMerged {
				continue
			}
			if err := child.LoadIssue(ctx); err != nil {
				return err
			}
			if child.Issue.IsClosed {
				continue
			}
			next = child.ID
			break
		}
		if next == 0 {
			break
		}
		if seen[next] || above >= ascStackMaxDepth {
			return errors.New("the stack of the pull request is cyclic or too deep")
		}
		seen[next] = true
		above++
		cur = next
	}

	fields := taskContext.GetFields()
	fields[ascContextChange] = structpb.NewStringValue(row.AscChange)
	fields[ascContextStackPosition] = structpb.NewStringValue(strconv.Itoa(below + 1))
	fields[ascContextStackSize] = structpb.NewStringValue(strconv.Itoa(below + 1 + above))
	fields[ascContextStackParentSHA] = structpb.NewStringValue(pr.MergeBase)
	return nil
}
