// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/zeebo/blake3"
	"google.golang.org/grpc"
)

// The audit log pages (docs/BUILDOUT.md section 3): one filter form, one table, a pager and an export.

// Limits of the audit surface.
const (
	// AuditPageSizeDefault is the page size when the person chose none.
	AuditPageSizeDefault = 50
	// AuditPageSizeMax is the largest page size (the server's own limit).
	AuditPageSizeMax = 200
	maxFilterList    = 8
	maxActorLen      = 255
	maxActionLen     = 64
	maxTargetLen     = 256
	maxPageToken     = 1024
	maxDetailValue   = 256
	maxDetailKeys    = 32

	// MaxExportBytes bounds the bytes of one export that the hub relays through a scratch file. The server caps an
	// export at a million events; this is the cap in bytes, so a server that does not stop cannot fill the disk.
	MaxExportBytes = 1 << 30
	// maxExportChunk is the largest chunk the server may send (it cuts at 1 MiB; this allows a margin).
	maxExportChunk = 2 << 20
)

var (
	actorPattern  = regexp.MustCompile(`^[A-Za-z0-9._@+-]{1,255}$`)
	actionPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z0-9_]+)*(\.\*)?$`)
	tokenPattern  = regexp.MustCompile(`^[A-Za-z0-9_=.-]{1,1024}$`)
	// secretKey matches the names of audit detail keys whose value is never shown, as a second line of defence: the
	// server builds details from an allow-list and keeps credentials out of them.
	secretKey = regexp.MustCompile(`(?i)(secret|token|password|passwd|credential|private|api[_-]?key|authorization|cookie|signature)`)
)

// AuditQuery is the checked query string of the audit page. The raw strings are what the form shows again.
type AuditQuery struct {
	Actor       string
	Action      string
	Outcome     string
	Target      string
	Since       string
	Until       string
	OldestFirst bool
	Limit       uint32
	PageToken   string
}

// AuditQueryOf checks the form values. It returns the query to draw again, the filter to send, and the problems
// found (fixed texts). With a problem, nothing is sent.
func AuditQueryOf(get func(string) string) (AuditQuery, *ascv1.AuditFilter, []string) {
	var problems []string
	short := func(key string, max int) string {
		s := strings.TrimSpace(strings.ToValidUTF8(get(key), ""))
		if len(s) > max {
			s = s[:max]
			problems = append(problems, "The "+key+" filter is too long.")
		}
		return s
	}
	q := AuditQuery{
		Actor:       short("actor", maxFilterList*(maxActorLen+1)),
		Action:      short("action", maxFilterList*(maxActionLen+1)),
		Outcome:     short("outcome", 16),
		Target:      short("target", maxTargetLen),
		Since:       short("since", 10),
		Until:       short("until", 10),
		OldestFirst: get("order") == "oldest",
		Limit:       AuditPageSizeDefault,
		PageToken:   short("page", maxPageToken),
	}
	f := &ascv1.AuditFilter{}

	if q.Actor != "" {
		for _, a := range splitList(q.Actor) {
			if !actorPattern.MatchString(a) {
				problems = append(problems, "An actor is a user name.")
				break
			}
			f.Actors = append(f.Actors, a)
		}
		if len(f.Actors) > maxFilterList {
			problems = append(problems, "At most 8 actors.")
		}
	}
	if q.Action != "" {
		for _, a := range splitList(q.Action) {
			if len(a) > maxActionLen || !actionPattern.MatchString(a) {
				problems = append(problems, "An action looks like repo.ref.update or storage.*.")
				break
			}
			f.Actions = append(f.Actions, a)
		}
		if len(f.Actions) > maxFilterList {
			problems = append(problems, "At most 8 actions.")
		}
	}
	switch q.Outcome {
	case "":
	case "success":
		f.Outcome = ascv1.AuditOutcome_AUDIT_OUTCOME_SUCCESS
	case "denied":
		f.Outcome = ascv1.AuditOutcome_AUDIT_OUTCOME_DENIED
	case "failed":
		f.Outcome = ascv1.AuditOutcome_AUDIT_OUTCOME_FAILED
	default:
		problems = append(problems, "The outcome is success, denied or failed.")
		q.Outcome = ""
	}
	if q.Target != "" {
		for _, r := range q.Target {
			if r < 0x20 || r == 0x7f {
				problems = append(problems, "The target holds a control character.")
				break
			}
		}
		f.Target = q.Target
	}
	var since, until time.Time
	if q.Since != "" {
		t, err := time.Parse(time.DateOnly, q.Since)
		if err != nil {
			problems = append(problems, "The start date is year-month-day.")
		} else {
			since = t
			f.SinceMs = t.UnixMilli()
		}
	}
	if q.Until != "" {
		t, err := time.Parse(time.DateOnly, q.Until)
		if err != nil {
			problems = append(problems, "The end date is year-month-day.")
		} else {
			until = t
			// The end date is inclusive: the filter ends at the start of the next day.
			f.UntilMs = t.AddDate(0, 0, 1).UnixMilli() - 1
		}
	}
	if !since.IsZero() && !until.IsZero() && since.After(until) {
		problems = append(problems, "The start date is after the end date.")
	}
	if l := get("limit"); l != "" {
		switch l {
		case "25":
			q.Limit = 25
		case "50":
			q.Limit = 50
		case "100":
			q.Limit = 100
		case "200":
			q.Limit = 200
		default:
			problems = append(problems, "The page size is 25, 50, 100 or 200.")
		}
	}
	if q.PageToken != "" && !tokenPattern.MatchString(q.PageToken) {
		problems = append(problems, "The page marker is not valid.")
		q.PageToken = ""
	}
	if len(problems) > 0 {
		return q, nil, problems
	}
	return q, f, nil
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Values returns the query as URL values for the pager and the export link, without the page marker.
func (q AuditQuery) Values() map[string]string {
	m := map[string]string{}
	set := func(k, v string) {
		if v != "" {
			m[k] = v
		}
	}
	set("actor", q.Actor)
	set("action", q.Action)
	set("outcome", q.Outcome)
	set("target", q.Target)
	set("since", q.Since)
	set("until", q.Until)
	if q.OldestFirst {
		m["order"] = "oldest"
	}
	if q.Limit != AuditPageSizeDefault {
		m["limit"] = itoa(int(q.Limit))
	}
	return m
}

// KV is one detail of an audit event.
type KV struct {
	Key, Value string
}

// AuditRow is one event, cleaned for drawing.
type AuditRow struct {
	Seq       uint64
	ID        string
	Time      time.Time
	Actor     string
	ActorKind string
	OnBehalf  string
	Action    string
	What      string
	Repo      string
	Target    string
	Outcome   string
	Tone      string
	Remote    string
	Details   []KV
}

// AuditPage is one page of events.
type AuditPage struct {
	Rows []AuditRow
	Next string
}

// ActionInfo is one entry of the catalogue of actions, for the filter.
type ActionInfo struct {
	Action      string
	Description string
}

// ActionsOf cleans the catalogue of actions. Entries with a name that is not a valid action are dropped.
func ActionsOf(resp *ascv1.ListAuditActionsResponse) []ActionInfo {
	var out []ActionInfo
	for _, a := range resp.GetActions() {
		if len(out) >= MaxListItems {
			break
		}
		if len(a.GetAction()) > maxActionLen || !actionPattern.MatchString(a.GetAction()) {
			continue
		}
		out = append(out, ActionInfo{Action: a.GetAction(), Description: CleanText(a.GetDescription(), MaxTextBytes)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Action < out[j].Action })
	return out
}

// AuditPageOf checks and cleans an answer of Audit.List. A malformed event (no sequence number, no action, an
// unknown outcome in range, a time before 1970 or after the year 3000) fails the whole page: an audit log that is
// half drawn is worse than none.
func AuditPageOf(resp *ascv1.ListAuditResponse, catalogue []ActionInfo) (*AuditPage, error) {
	if len(resp.GetEvents()) > AuditPageSizeMax {
		return nil, fmt.Errorf("%w: too many events", ErrBadAnswer)
	}
	desc := make(map[string]string, len(catalogue))
	for _, a := range catalogue {
		desc[a.Action] = a.Description
	}
	if tok := resp.GetNextPageToken(); tok != "" && !tokenPattern.MatchString(tok) {
		return nil, fmt.Errorf("%w: page token", ErrBadAnswer)
	}
	page := &AuditPage{Next: resp.GetNextPageToken()}
	for _, e := range resp.GetEvents() {
		if e.GetSeq() == 0 || e.GetAction() == "" || len(e.GetAction()) > maxActionLen*2 {
			return nil, fmt.Errorf("%w: event", ErrBadAnswer)
		}
		if e.GetTimeMs() < 0 || e.GetTimeMs() > 32503680000000 {
			return nil, fmt.Errorf("%w: event time", ErrBadAnswer)
		}
		row := AuditRow{
			Seq:       e.GetSeq(),
			ID:        CleanText(e.GetId(), 64),
			Time:      time.UnixMilli(e.GetTimeMs()).UTC(),
			Actor:     principalText(e.GetActor()),
			ActorKind: CleanText(e.GetActor().GetKind(), 16),
			Action:    CleanText(e.GetAction(), maxActionLen*2),
			Repo:      CleanText(e.GetRepo(), 255),
			Target:    CleanText(e.GetTarget(), maxTargetLen),
			Remote:    CleanText(e.GetRemoteAddr(), 64),
		}
		if e.GetOnBehalfOf().GetName() != "" {
			row.OnBehalf = principalText(e.GetOnBehalfOf())
		}
		row.What = desc[row.Action]
		if row.What == "" {
			row.What = row.Action
		}
		switch e.GetOutcome() {
		case ascv1.AuditOutcome_AUDIT_OUTCOME_SUCCESS:
			row.Outcome, row.Tone = "Success", "ok"
		case ascv1.AuditOutcome_AUDIT_OUTCOME_DENIED:
			row.Outcome, row.Tone = "Denied", "no"
		case ascv1.AuditOutcome_AUDIT_OUTCOME_FAILED:
			row.Outcome, row.Tone = "Failed", "wn"
		default:
			row.Outcome, row.Tone = "Unknown", "ro"
		}
		keys := make([]string, 0, len(e.GetDetails()))
		for k := range e.GetDetails() {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if len(keys) > maxDetailKeys {
			keys = keys[:maxDetailKeys]
		}
		for _, k := range keys {
			ck := CleanText(k, 64)
			if ck == "" {
				continue
			}
			v := CleanText(e.GetDetails()[k], maxDetailValue)
			if secretKey.MatchString(ck) {
				v = "hidden"
			}
			row.Details = append(row.Details, KV{Key: ck, Value: v})
		}
		page.Rows = append(page.Rows, row)
	}
	return page, nil
}

func principalText(p *ascv1.PrincipalRef) string {
	name := CleanText(p.GetName(), 128)
	kind := CleanText(p.GetKind(), 16)
	switch {
	case name == "" && kind == "":
		return "unknown"
	case kind == "" || kind == "user" || name == "":
		if name == "" {
			return kind
		}
		return name
	}
	return name + " (" + kind + ")"
}

// ErrExportDigest is returned when the bytes of an export do not match the digest that closes it.
var ErrExportDigest = errors.New("asctrust: the audit export does not match its digest")

// ExportStream is the stream of an Audit.Export call.
type ExportStream = grpc.ServerStreamingClient[ascv1.ExportAuditResponse]

// CopyExport copies an export to w, which must be a scratch file or buffer and not the response: every chunk is
// checked (at most maxExportChunk bytes, at most MaxExportBytes in all, nothing after the last message), hashed,
// and written; the last message must carry the BLAKE3 digest of everything written, or the copy fails with
// ErrExportDigest. The handler sends the bytes to the browser only after CopyExport returns without an error, so
// a download that arrives is a download whose digest matched. It returns the number of events the export holds.
func CopyExport(stream ExportStream, w io.Writer) (events uint64, err error) {
	h := blake3.New()
	var total int64
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			return 0, fmt.Errorf("%w: the export ended without its last message", ErrBadAnswer)
		}
		if err != nil {
			return 0, err
		}
		data := msg.GetData()
		if len(data) > maxExportChunk {
			return 0, fmt.Errorf("%w: export chunk", ErrBadAnswer)
		}
		total += int64(len(data))
		if total > MaxExportBytes {
			return 0, fmt.Errorf("%w: export size", ErrBadAnswer)
		}
		if len(data) > 0 {
			_, _ = h.Write(data)
			if _, err := w.Write(data); err != nil {
				return 0, err
			}
		}
		if !msg.GetLast() {
			continue
		}
		sum := h.Sum(nil)
		if string(sum) != string(msg.GetDigest()) {
			return 0, ErrExportDigest
		}
		if _, err := stream.Recv(); err != io.EOF {
			return 0, fmt.Errorf("%w: data after the last export message", ErrBadAnswer)
		}
		return msg.GetEvents(), nil
	}
}
