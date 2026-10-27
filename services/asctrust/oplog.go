// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/google/uuid"
	"google.golang.org/grpc"
)

// The operation log of a repository and the undo of one operation (Stacks.ListRefLog and Stacks.UndoRef).
//
// Every ref change that asc makes is a row of the log. An undo puts a ref back to the value it had before one row. It
// is allowed only for the person who made the row, only while that row is the newest of its ref, and for a ref
// outside the person's own refs only for a maintainer inside a short window. asc decides all of that and says so in
// every row (undoable, undo_blocked_reason); the hub asks again before it acts and adds its own bounds:
//
//   - the row must be the person's own, by the name asc recorded for the actor;
//   - an undo is previewed first (a dry run), and the confirmation carries the heads the person saw: the value of the
//     ref now (the "current" head) and the value it would be put back to (the "restore" head). The undo is made only
//     when the newest row of the ref still has the same sequence number and the same two heads, so the person
//     confirms what they were shown and nothing else. This is the same bound as RestackStackRequest.expected_heads.
//
// The call is made as the person (UserCall), so asc applies its own rules to the same request.
const (
	// FeatureStacks is the Version.features string of the stack operations, the ref log and the undo.
	FeatureStacks = "stacks.v1"

	// MaxOpLogPage bounds the rows of one page, and so the rows asc may send for one request.
	MaxOpLogPage = 100
	// DefaultOpLogPage is the page size the log asks for.
	DefaultOpLogPage = 50
	// MaxRefBytes bounds a ref name taken from a request or an answer.
	MaxRefBytes = 1024
	// MaxUndoReasonBytes bounds the reason that goes to the audit log (asc allows 256).
	MaxUndoReasonBytes = 256

	targetBytes = 32
)

var (
	// ErrUndoMoved is returned when the ref log has moved since the preview: nothing was undone.
	ErrUndoMoved = errors.New("asctrust: the ref changed since it was shown")
	// ErrUndoNotYours is returned for an operation that another person made.
	ErrUndoNotYours = errors.New("asctrust: the operation is not the signed-in person's own")
	// ErrUndoNotFound is returned when the ref has no such operation.
	ErrUndoNotFound = errors.New("asctrust: no such operation")
)

// UndoBlockedError is returned when asc says the operation cannot be undone. Key is a fixed slug of the reason (see
// blockKeyOf); the text asc sent is not kept.
type UndoBlockedError struct{ Key string }

func (e *UndoBlockedError) Error() string {
	return "asctrust: the operation cannot be undone: " + e.Key
}

var (
	pageTokenPattern = regexp.MustCompile(`^[0-9]{1,19}$`)
	hexPattern       = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// UndoWho is the signed-in person an undo is for.
type UndoWho struct {
	ID   int64
	Name string
}

// ownsActor reports whether the actor asc recorded for a row is this person. asc records the handle of the person
// who made the row, so the comparison is by name, without regard to case.
func (w UndoWho) ownsActor(actor string) bool {
	return w.ID > 0 && w.Name != "" && strings.EqualFold(w.Name, actor)
}

// blockKeyOf turns the fixed phrase asc gives for a row that cannot be undone into a slug the page translates. A
// phrase it does not know becomes "other"; the phrase itself is never shown.
func blockKeyOf(reason string) string {
	switch reason {
	case "":
		return ""
	case "nothing to undo":
		return "nothing"
	case "not your operation", "not your ref":
		return "not_yours"
	case "the ref changed since":
		return "moved"
	case "a protected ref needs a maintainer":
		return "needs_maintainer"
	case "undo of protected refs is off":
		return "protected_off"
	case "outside the undo window of a protected ref":
		return "expired"
	}
	return "other"
}

// viaKeys are the ways a ref moves that the page names; any other becomes "other".
var viaKeys = map[string]bool{"push": true, "land": true, "undo": true, "revert": true, "restack": true, "merge-queue": true, "agent": true}

// OpRow is one operation of the log.
type OpRow struct {
	Seq uint64
	// Ref is the ref name; Label is the ref as a person reads it ("refs/heads/" is dropped).
	Ref   string
	Label string
	// ViaKey is the way the ref moved (push, land, undo, revert, restack, merge-queue, agent, other).
	ViaKey string
	Actor  string
	Mine   bool
	At     time.Time
	// Old and New are the targets as 64 hex letters, "" for a ref that did not exist (Old) or was deleted (New).
	Old, New string
	// OldShort and NewShort are their first 12 letters.
	OldShort, NewShort string
	// Undoable is true only when asc says so and the row is the person's own.
	Undoable bool
	// BlockKey says why not (see blockKeyOf), "" when Undoable.
	BlockKey string
}

// OpLogQuery is what the log page asks for.
type OpLogQuery struct {
	// Ref is a ref name, or a prefix that ends in "/"; empty means every ref the person may see.
	Ref string
	// Mine limits the log to the person's own operations.
	Mine      bool
	PageToken string
	Limit     uint32
}

// OpLogPage is a page of the log.
type OpLogPage struct {
	Rows []OpRow
	Next string
}

func targetHex(t *ascv1.RefTarget) (string, error) {
	var b []byte
	switch v := t.GetT().(type) {
	case nil:
		return "", nil
	case *ascv1.RefTarget_Changeset:
		b = v.Changeset
	case *ascv1.RefTarget_Tag:
		b = v.Tag
	}
	if len(b) != targetBytes {
		return "", fmt.Errorf("%w: ref target", ErrBadAnswer)
	}
	return hex.EncodeToString(b), nil
}

func short(h string) string {
	if len(h) < 12 {
		return h
	}
	return h[:12]
}

func checkRef(ref string, prefixOK bool) error {
	if ref == "" || len(ref) > MaxRefBytes || CleanText(ref, MaxRefBytes) != ref || strings.ContainsAny(ref, "\\ ") {
		return fmt.Errorf("%w: ref", ErrBadInput)
	}
	if !prefixOK && strings.HasSuffix(ref, "/") {
		return fmt.Errorf("%w: ref", ErrBadInput)
	}
	return nil
}

// opRowOf checks and cleans one entry of an answer.
func opRowOf(e *ascv1.RefLogEntry, who UndoWho) (OpRow, error) {
	if e == nil || e.GetSeq() == 0 || e.GetRef() == "" || len(e.GetRef()) > MaxRefBytes {
		return OpRow{}, fmt.Errorf("%w: ref log entry", ErrBadAnswer)
	}
	oldHex, err := targetHex(e.GetOld())
	if err != nil {
		return OpRow{}, err
	}
	newHex, err := targetHex(e.GetNew())
	if err != nil {
		return OpRow{}, err
	}
	ref := CleanText(e.GetRef(), MaxRefBytes)
	if ref == "" {
		return OpRow{}, fmt.Errorf("%w: ref name", ErrBadAnswer)
	}
	via := e.GetVia()
	if !viaKeys[via] {
		via = "other"
	}
	actor := CleanText(e.GetActor(), MaxTextBytes)
	row := OpRow{
		Seq: e.GetSeq(), Ref: ref, Label: strings.TrimPrefix(ref, "refs/heads/"), ViaKey: via,
		Actor: actor, Mine: who.ownsActor(actor), At: time.Unix(e.GetTime(), 0).UTC(),
		Old: oldHex, New: newHex, OldShort: short(oldHex), NewShort: short(newHex),
	}
	switch {
	case e.GetUndoable() && row.Mine:
		row.Undoable = true
	case e.GetUndoable():
		// asc would let this person undo it, but the hub does not unless the row is theirs by name.
		row.BlockKey = "not_yours"
	default:
		row.BlockKey = blockKeyOf(e.GetUndoBlockedReason())
		if row.BlockKey == "" {
			row.BlockKey = "other"
		}
	}
	return row, nil
}

// LoadOpLog reads a page of the log as the person (ctx carries their credential).
func LoadOpLog(ctx context.Context, conn grpc.ClientConnInterface, repoName string, who UndoWho, q OpLogQuery) (*OpLogPage, error) {
	if q.Ref != "" {
		if err := checkRef(q.Ref, true); err != nil {
			return nil, err
		}
	}
	if q.PageToken != "" && !pageTokenPattern.MatchString(q.PageToken) {
		return nil, fmt.Errorf("%w: page", ErrBadInput)
	}
	limit := q.Limit
	if limit == 0 {
		limit = DefaultOpLogPage
	}
	if limit > MaxOpLogPage {
		return nil, fmt.Errorf("%w: page size", ErrBadInput)
	}
	req := &ascv1.ListRefLogRequest{Repo: RepoRef(repoName), Ref: q.Ref, Limit: limit, PageToken: q.PageToken}
	if q.Mine {
		if who.Name == "" {
			return nil, ErrNoDoer
		}
		req.Actor = who.Name
	}
	cctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	resp, err := ascv1.NewStacksClient(conn).ListRefLog(cctx, req)
	if err != nil {
		return nil, err
	}
	if len(resp.GetEntries()) > int(limit) || (resp.GetNextPageToken() != "" && !pageTokenPattern.MatchString(resp.GetNextPageToken())) {
		return nil, fmt.Errorf("%w: ref log page", ErrBadAnswer)
	}
	page := &OpLogPage{Next: resp.GetNextPageToken()}
	for _, e := range resp.GetEntries() {
		row, err := opRowOf(e, who)
		if err != nil {
			return nil, err
		}
		page.Rows = append(page.Rows, row)
	}
	return page, nil
}

// newestRow reads the newest operation of ref, which is the only one that can be undone.
func newestRow(ctx context.Context, conn grpc.ClientConnInterface, repoName string, who UndoWho, ref string) (*OpRow, error) {
	page, err := LoadOpLog(ctx, conn, repoName, who, OpLogQuery{Ref: ref, Limit: 1})
	if err != nil {
		return nil, err
	}
	if len(page.Rows) == 0 || page.Rows[0].Ref != ref {
		return nil, ErrUndoNotFound
	}
	return &page.Rows[0], nil
}

// UndoPreview is what the confirmation page shows: the operation, and the two heads the confirmation is bound to.
type UndoPreview struct {
	Row OpRow
	// Current is the value of the ref now, Restore the value it would be put back to; 64 hex letters or "".
	Current, Restore string
	// Op is the operation id of the form, so a double submit is one undo.
	Op string
}

// requireUndoable returns the error for a row the person may not undo.
func requireUndoable(row *OpRow) error {
	switch {
	case row.Undoable:
		return nil
	case row.BlockKey == "not_yours":
		return ErrUndoNotYours
	case row.BlockKey == "moved":
		return ErrUndoMoved
	}
	return &UndoBlockedError{Key: row.BlockKey}
}

// PreviewUndo checks that operation seq is the newest of ref and the person's to undo, and asks asc for a dry run. It
// changes nothing.
func PreviewUndo(ctx context.Context, conn grpc.ClientConnInterface, repoName string, who UndoWho, ref string, seq uint64) (*UndoPreview, error) {
	if err := checkRef(ref, false); err != nil {
		return nil, err
	}
	if seq == 0 {
		return nil, fmt.Errorf("%w: operation", ErrBadInput)
	}
	row, err := newestRow(ctx, conn, repoName, who, ref)
	if err != nil {
		return nil, err
	}
	if row.Seq != seq {
		return nil, ErrUndoMoved
	}
	if err := requireUndoable(row); err != nil {
		return nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	resp, err := ascv1.NewStacksClient(conn).UndoRef(cctx, &ascv1.UndoRefRequest{Repo: RepoRef(repoName), Ref: ref, Seq: seq, DryRun: true})
	if err != nil {
		return nil, err
	}
	cur, err := targetHex(resp.GetResult().GetOld())
	if err != nil {
		return nil, err
	}
	back, err := targetHex(resp.GetResult().GetNew())
	if err != nil {
		return nil, err
	}
	// A dry run changes nothing, and it answers for the ref and the row that were asked about.
	if resp.GetApplied() || resp.GetResult().GetName() != ref || cur != row.New || back != row.Old {
		return nil, fmt.Errorf("%w: undo preview", ErrBadAnswer)
	}
	return &UndoPreview{Row: *row, Current: cur, Restore: back, Op: NewOpID()}, nil
}

// UndoConfirmation is a confirmed undo: the operation, the heads the person was shown, and the form's operation id.
type UndoConfirmation struct {
	Ref              string
	Seq              uint64
	Current, Restore string
	Reason           string
	Op               string
}

// UndoDone is the result of an undo.
type UndoDone struct {
	Ref      string
	NewSeq   uint64
	Applied  bool
	Restored string
}

// ConfirmUndo makes the undo, after it has read the newest row of the ref again. It refuses unless that row is the
// operation and carries the two heads the person was shown (the expected heads), is undoable and is the person's own.
func ConfirmUndo(ctx context.Context, conn grpc.ClientConnInterface, repoName string, who UndoWho, c UndoConfirmation) (*UndoDone, error) {
	if err := checkRef(c.Ref, false); err != nil {
		return nil, err
	}
	if c.Seq == 0 || !hexPattern.MatchString(c.Current) || (c.Restore != "" && !hexPattern.MatchString(c.Restore)) {
		return nil, fmt.Errorf("%w: heads", ErrBadInput)
	}
	id, err := uuid.Parse(c.Op)
	if err != nil {
		return nil, fmt.Errorf("%w: operation id", ErrBadInput)
	}
	reason := CleanText(c.Reason, MaxUndoReasonBytes)
	if len(c.Reason) > MaxUndoReasonBytes || reason != strings.TrimSpace(c.Reason) {
		return nil, fmt.Errorf("%w: the reason is plain text of at most %d bytes", ErrBadInput, MaxUndoReasonBytes)
	}
	row, err := newestRow(ctx, conn, repoName, who, c.Ref)
	if err != nil {
		return nil, err
	}
	if row.Seq != c.Seq || row.New != c.Current || row.Old != c.Restore {
		return nil, ErrUndoMoved
	}
	if err := requireUndoable(row); err != nil {
		return nil, err
	}
	cctx, cancel := context.WithTimeout(ascclient.WithOpID(ctx, id), CallTimeout)
	defer cancel()
	resp, err := ascv1.NewStacksClient(conn).UndoRef(cctx, &ascv1.UndoRefRequest{Repo: RepoRef(repoName), Ref: c.Ref, Seq: c.Seq, Reason: reason})
	if err != nil {
		// The compare-and-swap inside asc found the ref moved between our read and its write.
		if Classify(err).Code == "VERSION_CONFLICT" {
			return nil, ErrUndoMoved
		}
		return nil, err
	}
	back, err := targetHex(resp.GetResult().GetNew())
	if err != nil {
		return nil, err
	}
	if !resp.GetApplied() || resp.GetResult().GetName() != c.Ref || back != c.Restore {
		return nil, fmt.Errorf("%w: undo", ErrBadAnswer)
	}
	return &UndoDone{Ref: c.Ref, NewSeq: resp.GetNewSeq(), Applied: true, Restored: back}, nil
}
