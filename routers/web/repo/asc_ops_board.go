// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"forgejo.org/modules/log"
	"forgejo.org/routers/web/shared/trustweb"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// The operation log board: filter chips by kind with counts, "who changed branches" bars, and the export of the log.
// The counts and the bars describe the rows of the page the person is looking at (asc pages the log; it has no totals), and
// say so. The export walks the same call as the page, as the same person, page by page, so it can never show a row the page
// would not.

// ascOpsKinds are the chips, in the order of the board. The key is the way a ref moved (asctrust.OpRow.ViaKey).
var ascOpsKinds = []struct{ Key, Slug string }{
	{"push", "push"}, {"land", "land"}, {"restack", "restack"}, {"undo", "undo"}, {"agent", "agent"},
}

// ascOpsKindChip is one chip with how many of the loaded rows are of that kind.
type ascOpsKindChip struct {
	Key   string
	Count int
	On    bool
	URL   string
}

// ascOpsActorBar is one actor of the bar chart.
type ascOpsActorBar struct {
	Actor string
	Count int
	Pct   int
}

func ascOpsKindKnown(k string) bool {
	for _, c := range ascOpsKinds {
		if c.Key == k {
			return true
		}
	}
	return k == ""
}

// ascOpsBoard filters rows by kind and builds the chips and the bars.
func ascOpsBoard(p *ascOpsPage, kind string, rows []asctrust.OpRow) []asctrust.OpRow {
	counts := map[string]int{}
	actors := map[string]int{}
	for _, r := range rows {
		counts[r.ViaKey]++
		actors[r.Actor]++
	}
	base := url.Values{}
	if p.Ref != "" {
		base.Set("ref", p.Ref)
	}
	if p.Mine {
		base.Set("mine", "1")
	}
	p.Total = len(rows)
	p.Kind = kind
	for _, c := range ascOpsKinds {
		q := url.Values{}
		for k, v := range base {
			q[k] = v
		}
		q.Set("kind", c.Key)
		chip := ascOpsKindChip{Key: c.Key, Count: counts[c.Key], On: kind == c.Key, URL: p.OpsURL + "?" + q.Encode()}
		p.Chips = append(p.Chips, chip)
	}
	p.AllURL = p.OpsURL
	if len(base) > 0 {
		p.AllURL += "?" + base.Encode()
	}
	var peak int
	for _, n := range actors {
		peak = max(peak, n)
	}
	for a, n := range actors {
		if a != "" {
			p.Actors = append(p.Actors, ascOpsActorBar{Actor: ascagentui.SafeName(a), Count: n, Pct: ascagentui.Percent(uint64(n), uint64(peak))})
		}
	}
	sort.Slice(p.Actors, func(i, j int) bool {
		if p.Actors[i].Count != p.Actors[j].Count {
			return p.Actors[i].Count > p.Actors[j].Count
		}
		return p.Actors[i].Actor < p.Actors[j].Actor
	})
	if len(p.Actors) > 8 {
		p.Actors = p.Actors[:8]
	}
	if kind == "" {
		return rows
	}
	var out []asctrust.OpRow
	for _, r := range rows {
		if r.ViaKey == kind {
			out = append(out, r)
		}
	}
	return out
}

type ascOpsExportRow struct {
	Seq   uint64 `json:"seq"`
	Ref   string `json:"ref"`
	Via   string `json:"via"`
	Actor string `json:"actor"`
	At    string `json:"at"`
	Old   string `json:"old,omitempty"`
	New   string `json:"new,omitempty"`
}

const (
	ascOpsExportPages = 20 // of 100 rows: at most 2000 rows in one export
)

// AscOpsExport serves GET /{owner}/{repo}/asc/ops/export?ref=&mine=1 as JSON lines. It is read access of the code, as the
// page; the rows are the ones the page shows to this person.
func AscOpsExport(ctx *context.Context) {
	repo, name, ok := trustweb.CodeRepo(ctx, false)
	if !ok {
		return
	}
	trustweb.NoStore(ctx)
	if !asctrust.HasFeature(ctx, asctrust.FeatureStacks) {
		ctx.NotFound("asc ops export", nil)
		return
	}
	conn, err := asctrust.Conn()
	if err != nil {
		ctx.Error(http.StatusBadGateway)
		return
	}
	callCtx, release, err := trustweb.ReadCall(ctx, repo)
	if err != nil {
		ctx.Error(trustweb.FailureOf("ops export credential", err).Status)
		return
	}
	defer release()
	q := asctrust.OpLogQuery{Ref: strings.TrimSpace(ctx.FormString("ref")), Mine: ctx.FormBool("mine"), Limit: asctrust.MaxOpLogPage}
	h := ctx.Resp.Header()
	h.Set("Content-Type", "application/x-ndjson; charset=utf-8")
	h.Set("Content-Disposition", `attachment; filename="operations-`+repo.Name+`.jsonl"`)
	h.Set("X-Content-Type-Options", "nosniff")
	started := false
	enc := json.NewEncoder(ctx.Resp)
	for range ascOpsExportPages {
		page, err := asctrust.LoadOpLog(callCtx, conn, name, ascUndoWho(ctx), q)
		if err != nil {
			f := trustweb.FailureOf("ops export", err)
			if !started {
				ctx.Error(f.Status)
			} else {
				log.Warn("asc ops export stopped: status %d", f.Status)
			}
			return
		}
		started = true
		for _, r := range page.Rows {
			_ = enc.Encode(ascOpsExportRow{Seq: r.Seq, Ref: r.Label, Via: r.ViaKey, Actor: ascagentui.SafeName(r.Actor), At: r.At.UTC().Format(time.RFC3339), Old: r.Old, New: r.New})
		}
		if page.Next == "" {
			return
		}
		q.PageToken = page.Next
	}
}
