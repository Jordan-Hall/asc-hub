// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/base"
	"forgejo.org/modules/git/ascbackend"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The asc commit graph page (SPEC 24.4, PLAN unit C42).
//
// `GET /{owner}/{repo}/asc/graph` shows the history of a repository whose storage_backend is asc, as
// rows that the asc server has already laid out in lanes (Repo.CommitGraph, SPEC 20.6). Forgejo only
// reads the rows and draws them; it computes no layout.
//
// Trust: the page is reached through the repository's own code-read check, and it reads with the
// internal token, so asc never decides who may see what. Everything asc answers is checked before it is
// drawn; an answer that does not check out fails the whole page (ascGraphBadAnswer), never a part of
// it. Nothing from asc's status text reaches the browser or the log, only its code.
//
// The only state that travels through the browser is the page token. Forgejo signs it with an HMAC
// over the repository id, so a token from another repository, or one the browser made up, is refused
// before asc sees it. The "lanes" parameter (which lanes are open at the top of a later page) is
// only used to draw; it is bounded and validated, and a bad value is a bad request.
const (
	tplAscGraph base.TplName = "repo/asc_graph"

	// ascGraphPageRows is the number of rows of one page. asc may return fewer, never more.
	ascGraphPageRows = 100
	// ascGraphMaxMessages bounds the stream messages of one page (asc sends at most 100 rows each).
	ascGraphMaxMessages = 8
	// ascGraphTimeout bounds the whole call.
	ascGraphTimeout = ascclient.DefaultStreamTimeout
	// ascGraphMaxRevs bounds the revisions named in the include and exclude fields.
	ascGraphMaxRevs = 16
	// ascGraphMaxRevBytes bounds one revision.
	ascGraphMaxRevBytes = 255
	// ascGraphMaxFormBytes bounds the include and exclude fields before they are split.
	ascGraphMaxFormBytes = 4096
	// ascGraphMaxTokenBytes bounds the page token as the browser sends it: asc's own limit (8192) plus the
	// signature.
	ascGraphMaxTokenBytes = 8192 + 1 + 43
	// ascGraphMaxLane is asc's overflow column, the largest column it sends.
	ascGraphMaxLane = 256
	// ascGraphMaxEdges bounds the parent lines of one row.
	ascGraphMaxEdges = 256
	// ascGraphMaxRowRefs bounds the ref labels shown on one row.
	ascGraphMaxRowRefs = 16
	// ascGraphMaxRefBytes bounds one ref label.
	ascGraphMaxRefBytes = 255
	// ascGraphMaxSubjectRunes and ascGraphMaxNameRunes bound the text shown from a commit.
	ascGraphMaxSubjectRunes = 200
	ascGraphMaxNameRunes    = 100
	// ascGraphMaxStatFiles bounds the files of one diffstat (asc stops after 10,001).
	ascGraphMaxStatFiles = 10001
	// ascGraphMaxBadgeRunes bounds the review texts shown.
	ascGraphMaxBadgeRunes = 24

	// Drawing: lanes are ascGraphLaneWidth apart, rows ascGraphRowHeight high. At most
	// ascGraphMaxDrawnLanes lanes are drawn apart; further columns share the last one.
	ascGraphLaneWidth     = 14
	ascGraphRowHeight     = 28
	ascGraphMaxDrawnLanes = 32

	ascGraphDefaultPreset = "smart"
)

// ascGraphPresets maps the preset names of the form to asc's presets.
var ascGraphPresets = map[string]ascv1.GraphPreset{
	"all":     ascv1.GraphPreset_GRAPH_PRESET_ALL,
	"current": ascv1.GraphPreset_GRAPH_PRESET_CURRENT,
	"smart":   ascv1.GraphPreset_GRAPH_PRESET_SMART,
	"stack":   ascv1.GraphPreset_GRAPH_PRESET_STACK,
}

// ascGraphPresetOrder is the order of the preset choices on the page.
var ascGraphPresetOrder = []string{"smart", "all", "current", "stack"}

// ascGraphPalette colours the lanes; the last entry is the shared overflow column.
var ascGraphPalette = []string{"#e5534b", "#57ab5a", "#539bf5", "#c69026", "#986ee2", "#39c5cf", "#e5679f", "#ae7c4b"}

const ascGraphOverflowColor = "#8b949e"

// errAscGraphBadAnswer marks an answer from asc that failed a check.
var errAscGraphBadAnswer = errors.New("asc commit graph: bad answer")

func ascGraphBadAnswer(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errAscGraphBadAnswer, fmt.Sprintf(format, args...))
}

// ascGraphSegment is one line of a row's lane picture.
type ascGraphSegment struct {
	X1, Y1, X2, Y2 int
	Color          string
}

// ascGraphRef is a branch or tag label.
type ascGraphRef struct {
	Name  string
	IsTag bool
}

// ascGraphReview is the review state asc knows for a row.
type ascGraphReview struct {
	Number    uint64
	State     string
	Draft     bool
	Approvals uint32
	Checks    string
}

// ascGraphRow is one checked, drawable row.
type ascGraphRow struct {
	ID        string // 40 hex digits
	ShortID   string
	Subject   string
	Author    string
	When      time.Time
	Segments  []ascGraphSegment
	NodeX     int
	NodeColor string
	Overflow  bool
	Refs      []ascGraphRef
	MoreRefs  int
	HasStats  bool
	Added     uint64
	Removed   uint64
	Files     int
	Review    *ascGraphReview
}

// ascGraphQuery is the checked query string of the page.
type ascGraphQuery struct {
	preset  string
	include []string
	exclude []string
	stats   bool
	token   string // asc's own token, signature removed; empty on the first page
	active  []int  // lanes open at the top of this page; empty on the first page
}

// AscGraph serves `GET /{owner}/{repo}/asc/graph`.
func AscGraph(ctx *context.Context) {
	repo := ctx.Repo.Repository
	if !setting.Asc.Enabled || repo == nil || repo.GetStorageBackend() != string(gitrepo.StorageBackendAsc) {
		ctx.NotFound("AscGraph", nil)
		return
	}
	ctx.Data["Title"] = ctx.Tr("repo.asc_graph")
	ctx.Data["PageIsAscGraph"] = true
	ctx.Data["AscGraphPresets"] = ascGraphPresetOrder

	q, errKey := ascGraphParse(ctx, repo)
	ctx.Data["AscGraphPreset"] = q.preset
	ctx.Data["AscGraphStats"] = q.stats
	ctx.Data["AscGraphInclude"] = strings.Join(q.include, " ")
	ctx.Data["AscGraphExclude"] = strings.Join(q.exclude, " ")
	if errKey != "" {
		ascGraphFail(ctx, http.StatusBadRequest, errKey)
		return
	}

	name, err := ascGraphRepoName(repo)
	if err != nil {
		ctx.NotFound("AscGraph", nil)
		return
	}
	conn, err := ascRPCConn()
	if err != nil {
		log.Error("asc commit graph: no connection to asc serve: %v", err)
		ascGraphFail(ctx, http.StatusBadGateway, "repo.asc_graph.error.unavailable")
		return
	}

	// The call ends when the browser goes away, and in any case at ascGraphTimeout.
	callCtx, cancel := gocontext.WithTimeout(ascclient.WithSystem(ctx.Req.Context()), ascGraphTimeout)
	defer cancel()
	req := &ascv1.CommitGraphRequest{
		Repo:      &ascv1.RepoRef{Name: name, IdMode: ascv1.IdMode_ID_MODE_GIT},
		Include:   q.include,
		Exclude:   q.exclude,
		Preset:    ascGraphPresets[q.preset],
		Lanes:     true,
		Stats:     q.stats,
		Limit:     ascGraphPageRows,
		PageToken: q.token,
	}
	msgs, err := ascGraphCall(callCtx, ascv1.NewRepoClient(conn), req)
	if err != nil {
		ascGraphMapError(ctx, err)
		return
	}
	rows, active, next, err := ascGraphBuild(msgs, q.active)
	if err != nil {
		ascGraphMapError(ctx, err)
		return
	}

	width := ascGraphDrawnWidth(rows, active)
	ctx.Data["AscGraphRows"] = rows
	ctx.Data["AscGraphLaneWidth"] = width
	ctx.Data["AscGraphRowHeight"] = ascGraphRowHeight
	ctx.Data["AscGraphFirstURL"] = ascGraphURL(ctx, q, "", nil)
	if next != "" {
		if signed := ascGraphSignToken(repo.ID, next); signed != "" {
			ctx.Data["AscGraphNextURL"] = ascGraphURL(ctx, q, signed, active)
		}
	}
	ctx.Data["AscGraphIsFirst"] = q.token == ""
	ctx.HTML(http.StatusOK, tplAscGraph)
}

// ascGraphFail renders the page with an error and no rows.
func ascGraphFail(ctx *context.Context, status int, key string) {
	ctx.Data["AscGraphError"] = key
	ctx.Data["AscGraphFirstURL"] = ctx.Repo.RepoLink + "/asc/graph"
	ctx.HTML(status, tplAscGraph)
}

// ascGraphMapError turns a failed call or a bad answer into a page. Only the status code of an asc
// error is used.
func ascGraphMapError(ctx *context.Context, err error) {
	if errors.Is(err, errAscGraphBadAnswer) {
		log.Error("asc commit graph: %v", err)
		ascGraphFail(ctx, http.StatusBadGateway, "repo.asc_graph.error.bad_answer")
		return
	}
	if ctx.Req.Context().Err() != nil {
		return // the browser went away
	}
	st, ok := status.FromError(err)
	if !ok {
		log.Error("asc commit graph: call failed: %v", err)
		ascGraphFail(ctx, http.StatusBadGateway, "repo.asc_graph.error.unavailable")
		return
	}
	switch st.Code() {
	case codes.NotFound:
		ascGraphFail(ctx, http.StatusNotFound, "repo.asc_graph.error.not_found")
	case codes.InvalidArgument:
		ascGraphFail(ctx, http.StatusBadRequest, "repo.asc_graph.error.bad_request")
	case codes.FailedPrecondition:
		ascGraphFail(ctx, http.StatusConflict, "repo.asc_graph.error.token_gone")
	case codes.ResourceExhausted, codes.OutOfRange:
		ascGraphFail(ctx, http.StatusRequestEntityTooLarge, "repo.asc_graph.error.too_large")
	case codes.DeadlineExceeded, codes.Canceled:
		ascGraphFail(ctx, http.StatusGatewayTimeout, "repo.asc_graph.error.unavailable")
	default:
		log.Error("asc commit graph: asc answered %s", st.Code())
		ascGraphFail(ctx, http.StatusBadGateway, "repo.asc_graph.error.unavailable")
	}
}

// ascGraphRepoName is the name asc knows the repository by, as in the asc RPC proxy.
func ascGraphRepoName(repo *repo_model.Repository) (string, error) {
	return ascbackend.RepoName(setting.RepoRootPath, repo_model.RepoPath(repo.OwnerName, repo.Name))
}

// ascGraphParse checks the query string. A non-empty key names the (translated) error. The returned
// query always carries what the form shows again.
func ascGraphParse(ctx *context.Context, repo *repo_model.Repository) (ascGraphQuery, string) {
	q := ascGraphQuery{preset: ascGraphDefaultPreset}
	bad := ""

	if p := ctx.FormTrim("preset"); p != "" {
		if _, ok := ascGraphPresets[p]; ok {
			q.preset = p
		} else {
			bad = "repo.asc_graph.error.bad_request"
		}
	}
	q.stats = ctx.FormBool("stats")
	for field, dst := range map[string]*[]string{"refs": &q.include, "exclude": &q.exclude} {
		revs, ok := ascGraphRevs(ctx.FormString(field))
		if !ok {
			bad = "repo.asc_graph.error.bad_request"
		}
		*dst = revs
	}

	if raw := ctx.FormString("page_token"); raw != "" {
		tok, ok := ascGraphVerifyToken(repo.ID, raw)
		if !ok {
			// A token that Forgejo did not issue for this repository: start again.
			return q, "repo.asc_graph.error.token_gone"
		}
		q.token = tok
		active, ok := ascGraphParseLanes(ctx.FormString("lanes"))
		if !ok {
			return q, "repo.asc_graph.error.bad_request"
		}
		q.active = active
	}
	return q, bad
}

// ascGraphRevs splits a space separated list of revisions and checks each.
func ascGraphRevs(field string) ([]string, bool) {
	if len(field) > ascGraphMaxFormBytes {
		return nil, false
	}
	revs := strings.Fields(field)
	if len(revs) > ascGraphMaxRevs {
		return revs[:ascGraphMaxRevs], false
	}
	for _, r := range revs {
		if len(r) > ascGraphMaxRevBytes || !utf8.ValidString(r) || strings.HasPrefix(r, "-") ||
			strings.ContainsFunc(r, unicode.IsControl) {
			return nil, false
		}
	}
	return revs, true
}

// ascGraphParseLanes reads the comma separated open lanes.
func ascGraphParseLanes(s string) ([]int, bool) {
	if s == "" {
		return nil, true
	}
	if len(s) > (ascGraphMaxLane+1)*4 {
		return nil, false
	}
	parts := strings.Split(s, ",")
	if len(parts) > ascGraphMaxLane+1 {
		return nil, false
	}
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > ascGraphMaxLane || strconv.Itoa(n) != p {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}

// ascGraphMAC signs a page token for one repository.
func ascGraphMAC(repoID int64, token string) []byte {
	mac := hmac.New(sha256.New, []byte(setting.SecretKey))
	mac.Write([]byte("forgejo asc graph page token\x00"))
	mac.Write([]byte(strconv.FormatInt(repoID, 10)))
	mac.Write([]byte{0})
	mac.Write([]byte(token))
	return mac.Sum(nil)
}

// ascGraphSignToken is the page token as the browser holds it: asc's token, a dot and the signature.
// base64url has no dot, so the last dot splits them. Without a secret key it returns "".
func ascGraphSignToken(repoID int64, token string) string {
	if setting.SecretKey == "" {
		return "" // no key, no link to the next page
	}
	return token + "." + base64.RawURLEncoding.EncodeToString(ascGraphMAC(repoID, token))
}

// ascGraphVerifyToken checks the signature and returns asc's token. It fails closed when there is no
// secret key.
func ascGraphVerifyToken(repoID int64, signed string) (string, bool) {
	if setting.SecretKey == "" || len(signed) > ascGraphMaxTokenBytes {
		return "", false
	}
	i := strings.LastIndexByte(signed, '.')
	if i <= 0 {
		return "", false
	}
	token, sig := signed[:i], signed[i+1:]
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(got, ascGraphMAC(repoID, token)) {
		return "", false
	}
	return token, true
}

// ascGraphURL builds a link to a page of the same query. token is the signed page token, empty for
// the first page.
func ascGraphURL(ctx *context.Context, q ascGraphQuery, token string, active []int) string {
	v := url.Values{}
	if q.preset != ascGraphDefaultPreset {
		v.Set("preset", q.preset)
	}
	if q.stats {
		v.Set("stats", "1")
	}
	if len(q.include) > 0 {
		v.Set("refs", strings.Join(q.include, " "))
	}
	if len(q.exclude) > 0 {
		v.Set("exclude", strings.Join(q.exclude, " "))
	}
	if token != "" {
		v.Set("page_token", token)
		if len(active) > 0 {
			parts := make([]string, len(active))
			for i, a := range active {
				parts[i] = strconv.Itoa(a)
			}
			v.Set("lanes", strings.Join(parts, ","))
		}
	}
	link := ctx.Repo.RepoLink + "/asc/graph"
	if enc := v.Encode(); enc != "" {
		link += "?" + enc
	}
	return link
}

// ascGraphCall reads the stream to its end, at most ascGraphMaxMessages messages.
func ascGraphCall(callCtx gocontext.Context, api ascv1.RepoClient, req *ascv1.CommitGraphRequest) ([]*ascv1.CommitGraphResponse, error) {
	st, err := api.CommitGraph(callCtx, req)
	if err != nil {
		return nil, err
	}
	var msgs []*ascv1.CommitGraphResponse
	for {
		m, err := st.Recv()
		if errors.Is(err, io.EOF) {
			return msgs, nil
		}
		if err != nil {
			return nil, err
		}
		if len(msgs) >= ascGraphMaxMessages {
			return nil, ascGraphBadAnswer("more than %d messages", ascGraphMaxMessages)
		}
		msgs = append(msgs, m)
	}
}

// ascGraphBuild checks the messages and lays the rows out. initial is the set of lanes open at the
// top. It returns the rows, the lanes open after the last row and asc's token for the next page.
func ascGraphBuild(msgs []*ascv1.CommitGraphResponse, initial []int) (rows []ascGraphRow, active []int, next string, err error) {
	open := map[int]bool{}
	for _, c := range initial {
		open[c] = true
	}
	var raw []*ascv1.GraphRow
	for _, m := range msgs {
		if m.GetNextPageToken() != "" {
			if next != "" {
				return nil, nil, "", ascGraphBadAnswer("two page tokens")
			}
			next = m.GetNextPageToken()
		}
		raw = append(raw, m.GetRows()...)
		if len(raw) > ascGraphPageRows {
			return nil, nil, "", ascGraphBadAnswer("more than %d rows", ascGraphPageRows)
		}
	}
	if len(next) > 8192 || strings.ContainsFunc(next, func(r rune) bool { return !ascGraphTokenRune(r) }) {
		return nil, nil, "", ascGraphBadAnswer("malformed page token")
	}
	rows = make([]ascGraphRow, 0, len(raw))
	for _, r := range raw {
		row, err := ascGraphCheckRow(r, open)
		if err != nil {
			return nil, nil, "", err
		}
		rows = append(rows, row)
	}
	for c := range open {
		active = append(active, c)
	}
	slices.Sort(active)
	return rows, active, next, nil
}

// ascGraphTokenRune reports whether r is in the base64url alphabet asc's tokens use.
func ascGraphTokenRune(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_'
}

// ascGraphDrawnLane clamps a column to the lanes that are drawn apart.
func ascGraphDrawnLane(col int) int { return min(col, ascGraphMaxDrawnLanes-1) }

func ascGraphX(col int) int { return ascGraphDrawnLane(col)*ascGraphLaneWidth + ascGraphLaneWidth/2 }

func ascGraphColor(col int) string {
	if col >= ascGraphMaxLane {
		return ascGraphOverflowColor
	}
	return ascGraphPalette[ascGraphDrawnLane(col)%len(ascGraphPalette)]
}

// ascGraphCheckRow checks one row and draws it. open holds the lanes open above the row and is
// updated to the lanes open below it: the lane the row sits in ends here, and every parent line
// opens (or joins) the lane it names.
func ascGraphCheckRow(r *ascv1.GraphRow, open map[int]bool) (ascGraphRow, error) {
	e := r.GetEntry()
	if e == nil {
		return ascGraphRow{}, ascGraphBadAnswer("row without an entry")
	}
	id := e.GetId()
	if len(id) != 20 || (len(e.GetGit()) != 0 && !slices.Equal(e.GetGit(), id)) {
		return ascGraphRow{}, ascGraphBadAnswer("row with an id that is not a git id")
	}
	col := int(r.GetColumn())
	if r.GetColumn() > ascGraphMaxLane || r.GetOverflow() != (col == ascGraphMaxLane) {
		return ascGraphRow{}, ascGraphBadAnswer("row in column %d", r.GetColumn())
	}
	if len(r.GetEdges()) > ascGraphMaxEdges {
		return ascGraphRow{}, ascGraphBadAnswer("row with %d parent lines", len(r.GetEdges()))
	}
	for _, edge := range r.GetEdges() {
		if edge.GetToColumn() > ascGraphMaxLane || len(edge.GetParent()) != 20 {
			return ascGraphRow{}, ascGraphBadAnswer("row with a bad parent line")
		}
	}

	hexID := hex.EncodeToString(id)
	out := ascGraphRow{
		ID:        hexID,
		ShortID:   hexID[:10],
		Subject:   ascGraphSubject(e.GetMessage()),
		Author:    ascGraphText(string(e.GetAuthor().GetName()), ascGraphMaxNameRunes),
		NodeX:     ascGraphX(col),
		NodeColor: ascGraphColor(col),
		Overflow:  r.GetOverflow(),
	}
	if t := e.GetAuthor().GetTime(); t > 0 && t < 1<<40 {
		out.When = time.Unix(t, 0)
	}

	// Lines: the lanes that pass the row, the line that ends in the node, the node's parent lines.
	seen := map[int]bool{}
	add := func(x1, y1, x2, y2 int, color string) {
		out.Segments = append(out.Segments, ascGraphSegment{x1, y1, x2, y2, color})
	}
	mid := ascGraphRowHeight / 2
	for _, c := range sortedKeys(open) {
		if c == col {
			continue
		}
		if d := ascGraphDrawnLane(c); !seen[d] {
			seen[d] = true
			add(ascGraphX(c), 0, ascGraphX(c), ascGraphRowHeight, ascGraphColor(c))
		}
	}
	if open[col] {
		add(ascGraphX(col), 0, ascGraphX(col), mid, ascGraphColor(col))
	}
	delete(open, col)
	toSeen := map[int]bool{}
	for _, edge := range r.GetEdges() {
		to := int(edge.GetToColumn())
		open[to] = true
		if d := ascGraphDrawnLane(to); !toSeen[d] {
			toSeen[d] = true
			add(ascGraphX(col), mid, ascGraphX(to), ascGraphRowHeight, ascGraphColor(to))
		}
	}

	refs := r.GetRefs()
	for _, name := range refs {
		if len(out.Refs) >= ascGraphMaxRowRefs {
			out.MoreRefs++
			continue
		}
		if len(name) > ascGraphMaxRefBytes {
			name = name[:ascGraphMaxRefBytes]
		}
		name = ascGraphText(name, ascGraphMaxRefBytes)
		switch {
		case strings.HasPrefix(name, "refs/tags/"):
			out.Refs = append(out.Refs, ascGraphRef{Name: strings.TrimPrefix(name, "refs/tags/"), IsTag: true})
		default:
			out.Refs = append(out.Refs, ascGraphRef{Name: strings.TrimPrefix(name, "refs/heads/")})
		}
	}

	if s := e.GetStats(); s != nil {
		if len(s.GetFiles()) > ascGraphMaxStatFiles {
			return ascGraphRow{}, ascGraphBadAnswer("diffstat with %d files", len(s.GetFiles()))
		}
		out.HasStats, out.Files = true, len(s.GetFiles())
		for _, f := range s.GetFiles() {
			out.Added += uint64(f.GetAdded())
			out.Removed += uint64(f.GetRemoved())
		}
	}
	if rv := r.GetReview(); rv != nil && rv.GetNumber() > 0 {
		out.Review = &ascGraphReview{
			Number:    rv.GetNumber(),
			State:     ascGraphText(rv.GetState(), ascGraphMaxBadgeRunes),
			Draft:     rv.GetDraft(),
			Approvals: rv.GetApprovals(),
			Checks:    ascGraphText(rv.GetChecks(), ascGraphMaxBadgeRunes),
		}
	}
	return out, nil
}

func sortedKeys(m map[int]bool) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// ascGraphDrawnWidth is the width of the lane picture: the same for every row of the page.
func ascGraphDrawnWidth(rows []ascGraphRow, active []int) int {
	widest := 0
	for _, r := range rows {
		for _, s := range r.Segments {
			widest = max(widest, s.X1, s.X2)
		}
		widest = max(widest, r.NodeX)
	}
	for _, c := range active {
		widest = max(widest, ascGraphX(c))
	}
	return widest + ascGraphLaneWidth/2
}

// ascGraphSubject is the first line of a commit message, cut short.
func ascGraphSubject(msg []byte) string {
	s := string(msg)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return ascGraphText(s, ascGraphMaxSubjectRunes)
}

// ascGraphText makes s printable: invalid UTF-8 and control characters are replaced, and the result is
// cut at max runes.
func ascGraphText(s string, limit int) string {
	s = strings.ToValidUTF8(s, "�")
	var b strings.Builder
	n := 0
	for _, r := range s {
		if n >= limit {
			b.WriteString("...")
			break
		}
		if unicode.IsControl(r) {
			r = ' '
		}
		b.WriteRune(r)
		n++
	}
	return strings.TrimSpace(b.String())
}
