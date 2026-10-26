// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascbridge

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Call sites of the asc.v1.Stacks service: restack of a whole stack onto a moved trunk and revert of a landed
// stack (docs/BUILDOUT.md section 6).
//
// Authority. The hub has decided who may do this before it calls (Forgejo permissions, branch protection),
// so the calls carry the internal token (ascclient.WithSystem). A restack rewrites the heads in asc; the
// caller (services/pull) moves refs/pull/<n>/head to the new heads. A revert never writes trunk: asc stores
// the reverts as a stack of new changes and the caller opens pull requests for them.
//
// Answers are checked before they are used: id lengths, the number of items, and the states. Conflict paths
// are made printable and bounded. Nothing in an error carries a token.

const (
	// StackCallTimeout bounds one Stacks call. A restack of a deep stack walks every change.
	StackCallTimeout = 90 * time.Second
	// MaxStackItems bounds the items of one answer.
	MaxStackItems = 256
	// MaxConflictPaths bounds the conflict paths kept from one answer.
	MaxConflictPaths = 200
	// MaxPathBytes bounds one kept conflict path.
	MaxPathBytes = 1024

	idLen     = 32
	changeLen = 16
)

// Errors of the Stacks calls. ErrStacksUnavailable means the server does not carry the feature (UNIMPLEMENTED
// or a missing "stacks.v1"); the caller says so and changes nothing.
var (
	ErrStacksUnavailable = errors.New("the asc server does not support whole-stack operations")
	ErrStackFrozen       = errors.New("a change of the stack is frozen: it is in a merge queue or locked")
	ErrStackConflict     = errors.New("the stack has a conflict")
	ErrStackNotLanded    = errors.New("the stack is not landed")
	ErrRevertConflict    = errors.New("the revert conflicts with later changes")
	ErrStackDenied       = errors.New("asc refused the operation")
	ErrStackNotFound     = errors.New("asc does not know the repository or the change")
	ErrStackBadRequest   = errors.New("asc refused the request")
)

// ConflictError carries the paths that conflicted. It unwraps to ErrStackConflict or ErrRevertConflict.
type ConflictError struct {
	Err   error
	Paths []string
}

func (e *ConflictError) Error() string { return e.Err.Error() }
func (e *ConflictError) Unwrap() error { return e.Err }

// RestackState is the outcome of one change of a restack.
type RestackState string

// The states of RestackItem.
const (
	RestackRewritten     RestackState = "rewritten"
	RestackUnchanged     RestackState = "unchanged"
	RestackConflict      RestackState = "conflict"
	RestackBlocked       RestackState = "blocked"
	RestackSkippedLanded RestackState = "skipped_landed"
	RestackSkippedFrozen RestackState = "skipped_frozen"
)

// RestackStackRequest asks asc to restack the whole stack that Change belongs to.
type RestackStackRequest struct {
	Repo         string // owner/name
	Change       string // text form, 32 letters k-z; any change of the stack
	DryRun       bool
	AllowPartial bool
	// ExpectedHeads pins an apply to a reviewed plan: one entry per change the call may rewrite, with the
	// head the caller saw. asc refuses the apply (ABORTED, REF_CONFLICT) when a listed change has another head
	// now, a change it would rewrite is not listed, or a listed change left the stack. Empty means no pin. A
	// dry run ignores it.
	ExpectedHeads []ExpectedHead
}

// ExpectedHead is a change and the head asc showed for it on a dry run.
type ExpectedHead struct {
	Change    string // text form
	OldHeadID string // RestackItem.OldHeadID
}

// RestackItem is one change of the stack, bottom to top. OldHead and NewHead are git ids ("" when there is
// none; a dry run has no NewHead).
type RestackItem struct {
	Change     string
	State      RestackState
	OldHead    string
	NewHead    string
	NewVersion uint32
	Conflicts  []string // paths
	// OldHeadID is the asc id (hex, 32 bytes) of the old head, "" when there is none. It is what
	// ExpectedHead.OldHeadID takes; OldHead is the git id for the same commit.
	OldHeadID string
}

// RestackStackResult is asc's answer. Onto is the git id of the target tip the stack stands on.
type RestackStackResult struct {
	Onto    string
	Items   []RestackItem
	Applied bool
}

// RevertLandedRequest asks asc to revert the landed stack whose top change is Change. Bottom, when set, is the
// lowest change to revert (text form).
type RevertLandedRequest struct {
	Repo    string
	Change  string
	Bottom  string
	Squash  bool
	Message string
	DryRun  bool
}

// RevertedChange is one revert, in the order asc answered (the bottom of the new stack first). RevertGit is the
// git id of the revert (empty on a dry run).
type RevertedChange struct {
	Original     string
	RevertChange string
	RevertGit    string
	Conflicts    []string
}

// RevertLandedResult is asc's answer. Ref is where asc stored the reverts.
type RevertLandedResult struct {
	Reverts  []RevertedChange
	StackTop string
	Ref      string
	Applied  bool
}

// StackOps is the Stacks client the hub uses.
type StackOps interface {
	RestackStack(ctx context.Context, req RestackStackRequest) (*RestackStackResult, error)
	RevertLanded(ctx context.Context, req RevertLandedRequest) (*RevertLandedResult, error)
}

type stackOps struct {
	stacks ascv1.StacksClient
	repo   ascv1.RepoClient
}

var (
	stackOpsMu  sync.Mutex
	stackOpsVal StackOps

	// newStackOps builds the client. Tests replace it through UseStackOps.
	newStackOps = func() (StackOps, error) {
		if setting.InternalToken == "" {
			return nil, errors.New("ascbridge: the internal token is not set")
		}
		c, err := ascclient.New(ascclient.Config{Target: setting.Asc.ServerURL, InternalToken: setting.InternalToken, CallTimeout: StackCallTimeout})
		if err != nil {
			return nil, err
		}
		return &stackOps{stacks: c.Stacks(), repo: c.Repo()}, nil
	}
)

// UseStackOps replaces the Stacks client until the returned function is called. It is for tests.
func UseStackOps(o StackOps) (restore func()) {
	stackOpsMu.Lock()
	defer stackOpsMu.Unlock()
	prev := stackOpsVal
	stackOpsVal = o
	return func() {
		stackOpsMu.Lock()
		defer stackOpsMu.Unlock()
		stackOpsVal = prev
	}
}

// StackClient returns the Stacks client, creating it on first use.
func StackClient() (StackOps, error) {
	stackOpsMu.Lock()
	defer stackOpsMu.Unlock()
	if stackOpsVal == nil {
		o, err := newStackOps()
		if err != nil {
			return nil, fmt.Errorf("ascbridge: asc client: %w", err)
		}
		stackOpsVal = o
	}
	return stackOpsVal, nil
}

// ChangeBytes decodes the text form of a change id: letter c is the hex digit 'z' - c, high digit first.
func ChangeBytes(change string) ([]byte, error) {
	if len(change) != 2*changeLen {
		return nil, errors.New("not an asc change id")
	}
	out := make([]byte, changeLen)
	for i := range out {
		hi, lo := 'z'-change[2*i], 'z'-change[2*i+1]
		if change[2*i] < 'k' || change[2*i] > 'z' || change[2*i+1] < 'k' || change[2*i+1] > 'z' {
			return nil, errors.New("not an asc change id")
		}
		out[i] = hi<<4 | lo
	}
	return out, nil
}

// ChangeText encodes a 16-byte change id to its text form.
func ChangeText(b []byte) (string, error) {
	if len(b) != changeLen {
		return "", errors.New("bad change id length")
	}
	out := make([]byte, 2*changeLen)
	for i, v := range b {
		out[2*i] = 'z' - (v >> 4)
		out[2*i+1] = 'z' - (v & 0xf)
	}
	return string(out), nil
}

func (o *stackOps) ctx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ascclient.WithSystem(ctx), StackCallTimeout)
}

// mapStackError turns a failed call into one of the errors above. Only the code and the conflict paths of
// the asc error are used; its message is not passed on.
func mapStackError(op string, err error) error {
	if err == nil {
		return nil
	}
	code := ascclient.ErrorCode(err)
	var paths []string
	if ed := ascclient.ErrorDetail(err); ed != nil {
		for _, c := range ed.GetConflicts() {
			paths = appendPath(paths, c.GetPath())
		}
		for _, p := range ed.GetPaths() {
			paths = appendPath(paths, []byte(p))
		}
	}
	switch code {
	case "CHANGE_FROZEN":
		return fmt.Errorf("%s: %w", op, ErrStackFrozen)
	case "LAND_CONFLICT":
		return &ConflictError{Err: fmt.Errorf("%s: %w", op, ErrStackConflict), Paths: paths}
	case "STACK_NOT_LANDED":
		return fmt.Errorf("%s: %w", op, ErrStackNotLanded)
	case "REVERT_CONFLICT":
		return &ConflictError{Err: fmt.Errorf("%s: %w", op, ErrRevertConflict), Paths: paths}
	}
	switch status.Code(err) {
	case codes.Unimplemented:
		return fmt.Errorf("%s: %w", op, ErrStacksUnavailable)
	case codes.PermissionDenied, codes.Unauthenticated:
		return fmt.Errorf("%s: %w", op, ErrStackDenied)
	case codes.NotFound:
		return fmt.Errorf("%s: %w", op, ErrStackNotFound)
	case codes.InvalidArgument:
		return fmt.Errorf("%s: %w", op, ErrStackBadRequest)
	}
	// Transport or server failure. The status code is kept, the message is not.
	return fmt.Errorf("%s: asc answered %s", op, status.Code(err))
}

// printable makes a path from asc fit for a page: valid UTF-8, no control characters, bounded.
func printable(b []byte) string {
	s := strings.ToValidUTF8(string(b), "�")
	if len(s) > MaxPathBytes {
		s = s[:MaxPathBytes]
		for !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
	}
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return '�'
		}
		return r
	}, s)
}

func appendPath(paths []string, p []byte) []string {
	if len(p) == 0 || len(paths) >= MaxConflictPaths {
		return paths
	}
	return append(paths, printable(p))
}

func conflictPaths(cs []*ascv1.Conflict) []string {
	var out []string
	for _, c := range cs {
		out = appendPath(out, c.GetPath())
	}
	return out
}

// toGit maps asc ids to git ids. A zero-length id maps to "". An id asc cannot map is an error.
func (o *stackOps) toGit(ctx context.Context, repo string, ids [][]byte) ([]string, error) {
	out := make([]string, len(ids))
	var want [][]byte
	var at []int
	for i, id := range ids {
		if len(id) == 0 {
			continue
		}
		if len(id) != idLen {
			return nil, fmt.Errorf("%w: an id of %d bytes", ErrBadAnswer, len(id))
		}
		want, at = append(want, id), append(at, i)
	}
	for start := 0; start < len(want); start += MapBatch {
		end := min(start+MapBatch, len(want))
		resp, err := o.repo.GitMap(ctx, &ascv1.GitMapRequest{Repo: &ascv1.RepoRef{Name: repo}, Asc: want[start:end]})
		if err != nil {
			return nil, mapStackError("GitMap", err)
		}
		if len(resp.GetAscToGit()) != end-start {
			return nil, fmt.Errorf("%w: GitMap answered %d ids for %d", ErrBadAnswer, len(resp.GetAscToGit()), end-start)
		}
		for j, g := range resp.GetAscToGit() {
			if len(g) == 0 {
				return nil, fmt.Errorf("%w: an id has no git id", ErrBadAnswer)
			}
			out[at[start+j]] = hex.EncodeToString(g)
		}
	}
	return out, nil
}

func restackState(s ascv1.RestackItemState) (RestackState, bool) {
	switch s {
	case ascv1.RestackItemState_RESTACK_ITEM_STATE_REWRITTEN:
		return RestackRewritten, true
	case ascv1.RestackItemState_RESTACK_ITEM_STATE_UNCHANGED:
		return RestackUnchanged, true
	case ascv1.RestackItemState_RESTACK_ITEM_STATE_CONFLICT:
		return RestackConflict, true
	case ascv1.RestackItemState_RESTACK_ITEM_STATE_BLOCKED:
		return RestackBlocked, true
	case ascv1.RestackItemState_RESTACK_ITEM_STATE_SKIPPED_LANDED:
		return RestackSkippedLanded, true
	case ascv1.RestackItemState_RESTACK_ITEM_STATE_SKIPPED_FROZEN:
		return RestackSkippedFrozen, true
	}
	return "", false
}

// RestackStack calls Stacks.Restack. The answer lists the changes bottom to top.
func (o *stackOps) RestackStack(ctx context.Context, req RestackStackRequest) (*RestackStackResult, error) {
	change, err := ChangeBytes(req.Change)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrStackBadRequest, err)
	}
	ctx, cancel := o.ctx(ctx)
	defer cancel()
	var expected []*ascv1.ExpectedHead
	for _, e := range req.ExpectedHeads {
		c, err := ChangeBytes(e.Change)
		if err != nil {
			return nil, fmt.Errorf("%w: expected head: %w", ErrStackBadRequest, err)
		}
		h, err := hex.DecodeString(e.OldHeadID)
		if err != nil || len(h) != idLen {
			return nil, fmt.Errorf("%w: expected head of a change is not an id", ErrStackBadRequest)
		}
		expected = append(expected, &ascv1.ExpectedHead{Change: c, OldHead: h})
	}
	resp, err := o.stacks.Restack(ctx, &ascv1.RestackStackRequest{
		Repo: &ascv1.RepoRef{Name: req.Repo}, Change: change, DryRun: req.DryRun, AllowPartial: req.AllowPartial,
		ExpectedHeads: expected,
	})
	if err != nil {
		return nil, mapStackError("Restack", err)
	}
	if len(resp.GetItems()) > MaxStackItems {
		return nil, fmt.Errorf("%w: %d items", ErrBadAnswer, len(resp.GetItems()))
	}
	applied := resp.GetApplied() && !req.DryRun
	var olds, news [][]byte
	items := make([]RestackItem, len(resp.GetItems()))
	for i, it := range resp.GetItems() {
		text, err := ChangeText(it.GetChange())
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrBadAnswer, err)
		}
		st, ok := restackState(it.GetState())
		if !ok {
			return nil, fmt.Errorf("%w: unknown restack state", ErrBadAnswer)
		}
		items[i] = RestackItem{Change: text, State: st, NewVersion: it.GetNewVersion(), Conflicts: conflictPaths(it.GetConflicts())}
		if len(it.GetOldHead()) == idLen {
			items[i].OldHeadID = hex.EncodeToString(it.GetOldHead())
		}
		olds = append(olds, it.GetOldHead())
		// Only an applied rewrite has a head in the store to map; a dry run prints none. A patch-identical
		// rewrite is reported as unchanged and may carry its new head too.
		switch {
		case applied && st == RestackRewritten:
			if len(it.GetNewHead()) != idLen {
				return nil, fmt.Errorf("%w: a rewritten change has no new head", ErrBadAnswer)
			}
			news = append(news, it.GetNewHead())
		case applied && st == RestackUnchanged && len(it.GetNewHead()) == idLen:
			news = append(news, it.GetNewHead())
		default:
			news = append(news, nil)
		}
	}
	oldGit, err := o.toGit(ctx, req.Repo, olds)
	if err != nil {
		return nil, err
	}
	newGit, err := o.toGit(ctx, req.Repo, news)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].OldHead, items[i].NewHead = oldGit[i], newGit[i]
	}
	res := &RestackStackResult{Items: items, Applied: applied}
	if len(resp.GetOnto()) > 0 {
		g, err := o.toGit(ctx, req.Repo, [][]byte{resp.GetOnto()})
		if err != nil {
			return nil, err
		}
		res.Onto = g[0]
	}
	return res, nil
}

// RevertLanded calls Stacks.RevertLanded. Trunk is never written; the reverts are stored by asc as new changes.
func (o *stackOps) RevertLanded(ctx context.Context, req RevertLandedRequest) (*RevertLandedResult, error) {
	change, err := ChangeBytes(req.Change)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrStackBadRequest, err)
	}
	var bottom []byte
	if req.Bottom != "" {
		if bottom, err = ChangeBytes(req.Bottom); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrStackBadRequest, err)
		}
	}
	ctx, cancel := o.ctx(ctx)
	defer cancel()
	resp, err := o.stacks.RevertLanded(ctx, &ascv1.RevertLandedRequest{
		Repo: &ascv1.RepoRef{Name: req.Repo}, At: &ascv1.RevertLandedRequest_Change{Change: change},
		Bottom: bottom, Squash: req.Squash, Message: []byte(req.Message), DryRun: req.DryRun,
	})
	if err != nil {
		return nil, mapStackError("RevertLanded", err)
	}
	if len(resp.GetReverts()) > MaxStackItems {
		return nil, fmt.Errorf("%w: %d reverts", ErrBadAnswer, len(resp.GetReverts()))
	}
	applied := resp.GetApplied() && !req.DryRun
	res := &RevertLandedResult{Ref: resp.GetRef(), Applied: applied}
	var sets [][]byte
	var conflicts []string
	for _, r := range resp.GetReverts() {
		orig, err := ChangeText(r.GetOriginal())
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrBadAnswer, err)
		}
		rc := RevertedChange{Original: orig, Conflicts: conflictPaths(r.GetConflicts())}
		conflicts = append(conflicts, rc.Conflicts...)
		if applied {
			if rc.RevertChange, err = ChangeText(r.GetRevertChange()); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrBadAnswer, err)
			}
			if len(r.GetRevertChangeset()) != idLen {
				return nil, fmt.Errorf("%w: a revert has no changeset", ErrBadAnswer)
			}
			sets = append(sets, r.GetRevertChangeset())
		} else {
			sets = append(sets, nil)
		}
		res.Reverts = append(res.Reverts, rc)
	}
	if applied && len(res.Reverts) == 0 {
		return nil, fmt.Errorf("%w: applied with no reverts", ErrBadAnswer)
	}
	if applied && len(conflicts) > 0 {
		return nil, &ConflictError{Err: fmt.Errorf("RevertLanded: %w", ErrRevertConflict), Paths: conflicts}
	}
	gits, err := o.toGit(ctx, req.Repo, sets)
	if err != nil {
		return nil, err
	}
	for i := range res.Reverts {
		res.Reverts[i].RevertGit = gits[i]
	}
	if len(resp.GetStackTop()) > 0 {
		if res.StackTop, err = ChangeText(resp.GetStackTop()); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrBadAnswer, err)
		}
	}
	return res, nil
}
