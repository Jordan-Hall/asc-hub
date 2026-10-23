// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package ascbackend_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const fakeRepoName = "owner/repo"

// fakeAsc is an asc.v1.Repo server over a bare git repository. It answers the way U47's `asc serve` does in
// id_mode GIT: ids are git ids, ReadTree takes a commit (never a tree id), ListRefs speaks asc ids and
// is refused in GIT mode, revisions that are abbreviated ids are refused. Where the real server's
// behaviour matters to the client (ordering, limits, error codes) the fake copies it.
type fakeAsc struct {
	ascv1.UnimplementedRepoServer
	t   *testing.T
	dir string

	// token, when set, is the bearer token every call must carry.
	token string
	// omitRaw leaves raw_git_commit empty, as for a commit made by asc itself.
	omitRaw bool
	// head is the symbolic HEAD that ListRefs reports.
	head string
	// tamper may change a response before it is sent.
	tamper func(method string, resp any)
	// fail answers a method with an error instead of working.
	fail map[string]error

	mu    sync.Mutex
	calls map[string]int
}

func newFake(t *testing.T, dir string) *fakeAsc {
	return &fakeAsc{t: t, dir: dir, calls: map[string]int{}, fail: map[string]error{}}
}

func (f *fakeAsc) callCount(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[method]
}

func (f *fakeAsc) gate(ctx context.Context, method string) error {
	f.mu.Lock()
	f.calls[method]++
	f.mu.Unlock()
	if f.token != "" {
		md, _ := metadata.FromIncomingContext(ctx)
		if got := md.Get("authorization"); len(got) != 1 || got[0] != "Bearer "+f.token {
			return status.Error(codes.Unauthenticated, "bad credential")
		}
	}
	if err := f.fail[method]; err != nil {
		return err
	}
	return nil
}

func (f *fakeAsc) unaryGate(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
	if err := f.gate(ctx, filepath.Base(info.FullMethod)); err != nil {
		return nil, err
	}
	return h(ctx, req)
}

func (f *fakeAsc) streamGate(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, h grpc.StreamHandler) error {
	if err := f.gate(ss.Context(), filepath.Base(info.FullMethod)); err != nil {
		return err
	}
	return h(srv, ss)
}

// serve starts the fake on a loopback port and returns a client for it.
func (f *fakeAsc) serve() (ascv1.RepoClient, string) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(f.t, err)
	srv := grpc.NewServer(grpc.UnaryInterceptor(f.unaryGate), grpc.StreamInterceptor(f.streamGate))
	ascv1.RegisterRepoServer(srv, f)
	go func() { _ = srv.Serve(lis) }()
	f.t.Cleanup(srv.Stop)
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(f.t, err)
	f.t.Cleanup(func() { _ = conn.Close() })
	return ascv1.NewRepoClient(conn), lis.Addr().String()
}

// ---------------------------------------------------------------------------------------- git helpers

func (f *fakeAsc) git(stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"-C", f.dir, "-c", "core.quotepath=off"}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "LC_ALL=C")
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, fmt.Errorf("git %v: %w: %s", args, err, stderr.String())
	}
	return out, nil
}

func (f *fakeAsc) gitText(args ...string) (string, error) {
	out, err := f.git(nil, args...)
	return strings.TrimSpace(string(out)), err
}

var hexRe = regexp.MustCompile(`^[0-9a-f]+$`)

// rev resolves a revision the way the server's `Call::rev` does in GIT mode.
func (f *fakeAsc) rev(spec string) (string, error) {
	if spec == "" {
		spec = "HEAD"
	}
	if len(spec) > 4096 || strings.ContainsAny(spec, "\x00\n") || strings.Contains(spec, ":") {
		return "", status.Error(codes.InvalidArgument, "bad revision")
	}
	if len(spec) == 40 && hexRe.MatchString(spec) {
		typ, err := f.gitText("cat-file", "-t", spec)
		if err != nil || typ != "commit" {
			return "", status.Errorf(codes.NotFound, "unknown commit %s", spec)
		}
		return spec, nil
	}
	if len(spec) >= 4 && len(spec) < 40 && hexRe.MatchString(spec) {
		if _, err := f.gitText("show-ref", "--verify", "--quiet", "refs/heads/"+spec); err != nil {
			return "", status.Error(codes.InvalidArgument, "abbreviated ids are not supported; use a full id or a ref name")
		}
	}
	out, err := f.gitText("rev-parse", "--verify", "--quiet", spec+"^{commit}")
	if err != nil || out == "" {
		return "", status.Errorf(codes.NotFound, "unknown revision %q", spec)
	}
	return out, nil
}

func (f *fakeAsc) checkRepo(ref *ascv1.RepoRef, mode ascv1.IdMode) error {
	if ref.GetName() != fakeRepoName {
		return status.Error(codes.NotFound, "no such repository")
	}
	if ref.GetIdMode() != mode {
		return status.Error(codes.Unimplemented, "wrong id mode for this call")
	}
	return nil
}

func oidBytes(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

func ascID(gitOID string) []byte {
	sum := sha256.Sum256([]byte("asc:" + gitOID))
	return sum[:]
}

// ---------------------------------------------------------------------------------------- commits

type rawCommit struct {
	tree      string
	parents   []string
	author    string
	committer string
	message   []byte
}

func parseRawCommit(raw []byte) rawCommit {
	head, body, _ := bytes.Cut(raw, []byte("\n\n"))
	var c rawCommit
	for line := range strings.SplitSeq(string(head), "\n") {
		switch {
		case strings.HasPrefix(line, "tree "):
			c.tree = line[5:]
		case strings.HasPrefix(line, "parent "):
			c.parents = append(c.parents, line[7:])
		case strings.HasPrefix(line, "author "):
			c.author = line[7:]
		case strings.HasPrefix(line, "committer "):
			c.committer = line[10:]
		}
	}
	c.message = body
	return c
}

func sigWire(line string) *ascv1.Signature {
	open := strings.LastIndex(line, " <")
	closeIdx := strings.LastIndex(line, "> ")
	name, email := line[:open], line[open+2:closeIdx]
	fields := strings.Fields(line[closeIdx+2:])
	secs, _ := strconv.ParseInt(fields[0], 10, 64)
	tz := fields[1]
	minutes := int32(0)
	if len(tz) == 5 {
		h, _ := strconv.Atoi(tz[1:3])
		m, _ := strconv.Atoi(tz[3:5])
		minutes = int32(h*60 + m)
		if tz[0] == '-' {
			minutes = -minutes
		}
	}
	return &ascv1.Signature{Name: []byte(name), Email: []byte(email), Time: secs, TzMin: minutes}
}

func (f *fakeAsc) logEntry(oid string) (*ascv1.LogEntry, rawCommit, []byte, error) {
	raw, err := f.git(nil, "cat-file", "commit", oid)
	if err != nil {
		return nil, rawCommit{}, nil, status.Error(codes.NotFound, "no such commit")
	}
	c := parseRawCommit(raw)
	e := &ascv1.LogEntry{
		Id: oidBytes(oid), Git: oidBytes(oid),
		Author: sigWire(c.author), Committer: sigWire(c.committer), Message: c.message,
	}
	for _, p := range c.parents {
		e.Parents = append(e.Parents, oidBytes(p))
	}
	return e, c, raw, nil
}

func (f *fakeAsc) ReadChangeset(_ context.Context, req *ascv1.ReadChangesetRequest) (*ascv1.ReadChangesetResponse, error) {
	if err := f.checkRepo(req.GetRepo(), ascv1.IdMode_ID_MODE_GIT); err != nil {
		return nil, err
	}
	oid, err := f.rev(req.GetRev())
	if err != nil {
		return nil, err
	}
	entry, c, raw, err := f.logEntry(oid)
	if err != nil {
		return nil, err
	}
	resp := &ascv1.ReadChangesetResponse{Header: entry, RootTree: oidBytes(c.tree), RootGit: oidBytes(c.tree)}
	if !f.omitRaw {
		resp.RawGitCommit = raw
	}
	f.applyTamper("ReadChangeset", resp)
	return resp, nil
}

func (f *fakeAsc) applyTamper(method string, resp any) {
	if f.tamper != nil {
		f.tamper(method, resp)
	}
}

// ---------------------------------------------------------------------------------------- trees and files

type lsEntry struct {
	mode, typ, oid string
	size           int64
	path           string
}

// lsTree lists treeish (a commit:path form) the way the server walks it. Entries are sorted by plain
// byte order of each path component, which is not git's tree order.
func (f *fakeAsc) lsTree(treeish string, recursive bool) ([]lsEntry, error) {
	args := []string{"ls-tree", "-z", "--long"}
	if recursive {
		args = append(args, "-r")
	}
	out, err := f.git(nil, append(args, treeish)...)
	if err != nil {
		return nil, status.Error(codes.NotFound, "no such directory")
	}
	var entries []lsEntry
	for rec := range bytes.SplitSeq(out, []byte{0}) {
		if len(rec) == 0 {
			continue
		}
		meta, path, _ := bytes.Cut(rec, []byte{'\t'})
		fields := strings.Fields(string(meta))
		size := int64(0)
		if fields[3] != "-" {
			size, _ = strconv.ParseInt(fields[3], 10, 64)
		}
		entries = append(entries, lsEntry{mode: fields[0], typ: fields[1], oid: fields[2], size: size, path: string(path)})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return slices.Compare(strings.Split(entries[i].path, "/"), strings.Split(entries[j].path, "/")) < 0
	})
	return entries, nil
}

func (f *fakeAsc) ReadTree(req *ascv1.ReadTreeRequest, st grpc.ServerStreamingServer[ascv1.ReadTreeResponse]) error {
	if err := f.checkRepo(req.GetRepo(), ascv1.IdMode_ID_MODE_GIT); err != nil {
		return err
	}
	oid, err := f.rev(req.GetRev())
	if err != nil {
		return err
	}
	dir := strings.TrimSuffix(string(req.GetPath()), "/")
	spec := oid + ":" + dir
	if dir == "" {
		spec = oid + "^{tree}"
	}
	typ, err := f.gitText("cat-file", "-t", spec)
	if err != nil {
		return status.Error(codes.NotFound, "no such directory")
	}
	if typ != "tree" {
		return status.Error(codes.FailedPrecondition, "not a directory")
	}
	treeOID, _ := f.gitText("rev-parse", spec)
	entries, err := f.lsTree(spec, req.GetRecursive())
	if err != nil {
		return err
	}
	var infos []*ascv1.TreeEntryInfo
	for _, e := range entries {
		mode, _ := strconv.ParseUint(e.mode, 8, 32)
		full := e.path
		if dir != "" {
			full = dir + "/" + e.path
		}
		infos = append(infos, &ascv1.TreeEntryInfo{
			Name: []byte(filepath.Base(e.path)), Path: []byte(full), Mode: uint32(mode),
			Id: oidBytes(e.oid), Git: oidBytes(e.oid), Size: uint64(e.size),
		})
	}
	send := func(chunk []*ascv1.TreeEntryInfo) error {
		resp := &ascv1.ReadTreeResponse{Entries: chunk, TreeId: oidBytes(treeOID), TreeGit: oidBytes(treeOID)}
		f.applyTamper("ReadTree", resp)
		return st.Send(resp)
	}
	const perMessage = 3
	for len(infos) > perMessage {
		if err := send(infos[:perMessage]); err != nil {
			return err
		}
		infos = infos[perMessage:]
	}
	return send(infos)
}

func (f *fakeAsc) ReadFile(req *ascv1.ReadFileRequest, st grpc.ServerStreamingServer[ascv1.ReadFileResponse]) error {
	if err := f.checkRepo(req.GetRepo(), ascv1.IdMode_ID_MODE_GIT); err != nil {
		return err
	}
	var blobOID string
	switch at := req.GetAt().(type) {
	case *ascv1.ReadFileRequest_RevPath:
		rev, path, ok := strings.Cut(at.RevPath, ":")
		if !ok {
			return status.Error(codes.InvalidArgument, "rev_path must be `rev:path`")
		}
		oid, err := f.rev(rev)
		if err != nil {
			return err
		}
		typ, err := f.gitText("cat-file", "-t", oid+":"+path)
		if err != nil {
			return status.Error(codes.NotFound, "does not exist")
		}
		if typ != "blob" {
			return status.Error(codes.FailedPrecondition, "path is not a file")
		}
		blobOID, _ = f.gitText("rev-parse", oid+":"+path)
	case *ascv1.ReadFileRequest_FileId:
		blobOID = hex.EncodeToString(at.FileId)
		if typ, err := f.gitText("cat-file", "-t", blobOID); err != nil || typ != "blob" {
			return status.Errorf(codes.NotFound, "unknown blob %s", blobOID)
		}
	default:
		return status.Error(codes.InvalidArgument, "missing rev_path or file_id")
	}
	data, err := f.git(nil, "cat-file", "blob", blobOID)
	if err != nil {
		return status.Error(codes.Internal, "internal error")
	}
	total := uint64(len(data))
	start := min(req.GetOffset(), total)
	end := total
	if req.GetLength() > 0 {
		end = min(start+req.GetLength(), total)
	}
	data = data[start:end]
	sent := false
	const chunk = 4096
	for len(data) > 0 {
		n := min(chunk, len(data))
		resp := &ascv1.ReadFileResponse{Data: data[:n], TotalSize: total}
		f.applyTamper("ReadFile", resp)
		if err := st.Send(resp); err != nil {
			return err
		}
		data, sent = data[n:], true
	}
	if !sent {
		resp := &ascv1.ReadFileResponse{TotalSize: total}
		f.applyTamper("ReadFile", resp)
		return st.Send(resp)
	}
	return nil
}

func (f *fakeAsc) Stat(_ context.Context, req *ascv1.StatRequest) (*ascv1.StatResponse, error) {
	if err := f.checkRepo(req.GetRepo(), ascv1.IdMode_ID_MODE_GIT); err != nil {
		return nil, err
	}
	resp := &ascv1.StatResponse{}
	if len(req.GetPaths()) > 0 {
		oid, err := f.rev(req.GetRev())
		if err != nil {
			return nil, err
		}
		for _, p := range req.GetPaths() {
			typ, err := f.gitText("cat-file", "-t", oid+":"+string(p))
			entry := &ascv1.TreeEntryInfo{}
			switch {
			case err != nil:
			case typ == "tree":
				entry.Mode = 0o040000
			default:
				entry.Mode = 0o100644
			}
			resp.Entries = append(resp.Entries, entry)
			resp.Exists = append(resp.Exists, err == nil)
		}
	}
	for _, k := range req.GetObjects() {
		want := map[ascv1.ObjKind]string{
			ascv1.ObjKind_OBJ_KIND_FILE: "blob", ascv1.ObjKind_OBJ_KIND_TREE: "tree", ascv1.ObjKind_OBJ_KIND_CHANGESET: "commit",
		}[k.GetKind()]
		oid := hex.EncodeToString(k.GetId())
		typ, err := f.gitText("cat-file", "-t", oid)
		if err != nil || typ != want {
			// The real server maps the id through git_map first and fails the call when it is unknown.
			return nil, status.Errorf(codes.NotFound, "unknown object %s", oid)
		}
		resp.Entries = append(resp.Entries, &ascv1.TreeEntryInfo{Id: k.GetId()})
		resp.Exists = append(resp.Exists, true)
	}
	f.applyTamper("Stat", resp)
	return resp, nil
}

// ---------------------------------------------------------------------------------------- refs

// ascOfGit maps every commit of the repository to its asc id, and back.
func (f *fakeAsc) commitMap() (toGit map[string]string) {
	toGit = map[string]string{}
	out, _ := f.gitText("rev-list", "--all")
	for oid := range strings.SplitSeq(out, "\n") {
		if oid != "" {
			toGit[string(ascID(oid))] = oid
		}
	}
	return toGit
}

func (f *fakeAsc) ListRefs(_ context.Context, req *ascv1.ListRefsRequest) (*ascv1.ListRefsResponse, error) {
	if err := f.checkRepo(req.GetRepo(), ascv1.IdMode_ID_MODE_ASC); err != nil {
		return nil, err
	}
	out, err := f.gitText("for-each-ref", "--format=%(refname) %(objecttype) %(objectname)")
	if err != nil {
		return nil, status.Error(codes.Internal, "internal error")
	}
	type ref struct{ name, typ, oid string }
	var all []ref
	for line := range strings.SplitSeq(out, "\n") {
		if line == "" {
			continue
		}
		p := strings.Fields(line)
		if strings.HasPrefix(p[0], req.GetPrefix()) {
			all = append(all, ref{p[0], p[1], p[2]})
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].name < all[j].name })
	start := 0
	if req.GetAfter() != "" {
		start = sort.Search(len(all), func(i int) bool { return all[i].name > req.GetAfter() })
	}
	limit := int(req.GetLimit())
	if limit == 0 || limit > 1000 {
		limit = 1000
	}
	page := all[start:]
	next := ""
	if len(page) > limit {
		page = page[:limit]
		next = page[len(page)-1].name
	}
	resp := &ascv1.ListRefsResponse{Next: next, Head: f.head}
	for _, r := range page {
		target := &ascv1.RefTarget{}
		if r.typ == "tag" {
			target.T = &ascv1.RefTarget_Tag{Tag: ascID(r.oid)}
		} else {
			target.T = &ascv1.RefTarget_Changeset{Changeset: ascID(r.oid)}
		}
		resp.Refs = append(resp.Refs, &ascv1.Ref{Name: r.name, Target: target, Version: 1})
	}
	f.applyTamper("ListRefs", resp)
	return resp, nil
}

func (f *fakeAsc) GitMap(_ context.Context, req *ascv1.GitMapRequest) (*ascv1.GitMapResponse, error) {
	if req.GetRepo().GetName() != fakeRepoName {
		return nil, status.Error(codes.NotFound, "no such repository")
	}
	m := f.commitMap()
	resp := &ascv1.GitMapResponse{}
	for _, c := range req.GetAsc() {
		if oid, ok := m[string(c)]; ok {
			resp.AscToGit = append(resp.AscToGit, oidBytes(oid))
		} else {
			resp.AscToGit = append(resp.AscToGit, nil)
		}
	}
	f.applyTamper("GitMap", resp)
	return resp, nil
}

func (f *fakeAsc) Resolve(_ context.Context, req *ascv1.ResolveRequest) (*ascv1.ResolveResponse, error) {
	if err := f.checkRepo(req.GetRepo(), ascv1.IdMode_ID_MODE_GIT); err != nil {
		return nil, err
	}
	resp := &ascv1.ResolveResponse{}
	for _, spec := range req.GetRevspecs() {
		oid, err := f.rev(spec)
		switch {
		case err == nil:
			resp.Ids = append(resp.Ids, oidBytes(oid))
		case status.Code(err) == codes.NotFound:
			resp.Ids = append(resp.Ids, nil)
		default:
			return nil, err
		}
	}
	f.applyTamper("Resolve", resp)
	return resp, nil
}

// ---------------------------------------------------------------------------------------- history

func (f *fakeAsc) Log(req *ascv1.LogRequest, st grpc.ServerStreamingServer[ascv1.LogResponse]) error {
	if err := f.checkRepo(req.GetRepo(), ascv1.IdMode_ID_MODE_GIT); err != nil {
		return err
	}
	if req.GetCountOnly() || len(req.GetPickaxe()) > 0 {
		return status.Error(codes.Unimplemented, "not available on this server yet")
	}
	args := []string{"log", "--format=%H"}
	if req.GetSkip() > 0 {
		args = append(args, fmt.Sprintf("--skip=%d", req.GetSkip()))
	}
	if req.GetLimit() > 0 {
		args = append(args, fmt.Sprintf("--max-count=%d", req.GetLimit()))
	}
	if req.GetAuthor() != "" || req.GetGrep() != "" {
		args = append(args, "--fixed-strings")
	}
	if req.GetAuthor() != "" {
		args = append(args, "--author="+req.GetAuthor())
	}
	if req.GetGrep() != "" {
		args = append(args, "--grep="+req.GetGrep())
	}
	if req.GetSince() != 0 {
		args = append(args, "--since="+time.Unix(req.GetSince(), 0).UTC().Format(time.RFC3339))
	}
	if req.GetUntil() != 0 {
		args = append(args, "--until="+time.Unix(req.GetUntil(), 0).UTC().Format(time.RFC3339))
	}
	if req.GetFollow() {
		args = append(args, "--follow")
	}
	include := req.GetInclude()
	if len(include) == 0 {
		include = []string{"HEAD"}
	}
	for _, spec := range include {
		oid, err := f.rev(spec)
		if err != nil {
			return err
		}
		args = append(args, oid)
	}
	if len(req.GetExclude()) > 0 {
		args = append(args, "--not")
		for _, spec := range req.GetExclude() {
			oid, err := f.rev(spec)
			if err != nil {
				return err
			}
			args = append(args, oid)
		}
	}
	if len(req.GetPaths()) > 0 {
		args = append(args, "--")
		args = append(args, req.GetPaths()...)
	}
	out, err := f.gitText(args...)
	if err != nil {
		return status.Error(codes.Internal, "internal error")
	}
	var entries []*ascv1.LogEntry
	for oid := range strings.SplitSeq(out, "\n") {
		if oid == "" {
			continue
		}
		e, _, _, err := f.logEntry(oid)
		if err != nil {
			return err
		}
		entries = append(entries, e)
	}
	const perMessage = 2
	for {
		n := min(perMessage, len(entries))
		resp := &ascv1.LogResponse{Entries: entries[:n], Count: uint64(n)}
		f.applyTamper("Log", resp)
		if err := st.Send(resp); err != nil {
			return err
		}
		if entries = entries[n:]; len(entries) == 0 {
			return nil
		}
	}
}

func (f *fakeAsc) MergeBase(_ context.Context, req *ascv1.MergeBaseRequest) (*ascv1.MergeBaseResponse, error) {
	if err := f.checkRepo(req.GetRepo(), ascv1.IdMode_ID_MODE_GIT); err != nil {
		return nil, err
	}
	a := hex.EncodeToString(req.GetA())
	args := []string{"merge-base", a}
	for _, b := range req.GetB() {
		args = append(args, hex.EncodeToString(b))
	}
	for _, oid := range args[1:] {
		if typ, err := f.gitText("cat-file", "-t", oid); err != nil || typ != "commit" {
			return nil, status.Errorf(codes.NotFound, "unknown commit %s", oid)
		}
	}
	out, err := f.gitText(args...)
	resp := &ascv1.MergeBaseResponse{}
	if err == nil && out != "" {
		resp.Bases = [][]byte{oidBytes(out)}
	}
	f.applyTamper("MergeBase", resp)
	return resp, nil
}

func (f *fakeAsc) IsAncestor(_ context.Context, req *ascv1.IsAncestorRequest) (*ascv1.IsAncestorResponse, error) {
	if err := f.checkRepo(req.GetRepo(), ascv1.IdMode_ID_MODE_GIT); err != nil {
		return nil, err
	}
	a, d := hex.EncodeToString(req.GetAncestor()), hex.EncodeToString(req.GetDescendant())
	for _, oid := range []string{a, d} {
		if typ, err := f.gitText("cat-file", "-t", oid); err != nil || typ != "commit" {
			return nil, status.Errorf(codes.NotFound, "unknown commit %s", oid)
		}
	}
	_, err := f.gitText("merge-base", "--is-ancestor", a, d)
	return &ascv1.IsAncestorResponse{Result: err == nil}, nil
}

func (f *fakeAsc) LastCommits(_ context.Context, req *ascv1.LastCommitsRequest) (*ascv1.LastCommitsResponse, error) {
	if err := f.checkRepo(req.GetRepo(), ascv1.IdMode_ID_MODE_GIT); err != nil {
		return nil, err
	}
	oid, err := f.rev(req.GetRev())
	if err != nil {
		return nil, err
	}
	dir := strings.Trim(string(req.GetDir()), "/")
	resp := &ascv1.LastCommitsResponse{ByName: map[string]*ascv1.LogEntry{}}
	for _, n := range req.GetNames() {
		path := string(n)
		if dir != "" {
			path = dir + "/" + path
		}
		if _, err := f.gitText("cat-file", "-e", oid+":"+path); err != nil {
			continue
		}
		last, err := f.gitText("log", "-1", "--format=%H", oid, "--", ":(literal)"+path)
		if err != nil || last == "" {
			continue
		}
		e, _, _, err := f.logEntry(last)
		if err != nil {
			return nil, err
		}
		resp.ByName[string(n)] = e
	}
	f.applyTamper("LastCommits", resp)
	return resp, nil
}

func (f *fakeAsc) Diff(req *ascv1.DiffRequest, st grpc.ServerStreamingServer[ascv1.DiffResponse]) error {
	if err := f.checkRepo(req.GetRepo(), ascv1.IdMode_ID_MODE_GIT); err != nil {
		return err
	}
	base, err := f.rev(req.GetBase())
	if err != nil {
		return err
	}
	head, err := f.rev(req.GetHead())
	if err != nil {
		return err
	}
	if req.GetMergeBase() {
		mb, err := f.gitText("merge-base", base, head)
		if err != nil || mb == "" {
			return status.Error(codes.FailedPrecondition, "no merge base")
		}
		base = mb
	}
	rename := "--no-renames"
	if req.GetRenames() {
		rename = "--find-renames"
	}
	names, err := f.git(nil, "diff", "--name-only", "-z", rename, base, head)
	if err != nil {
		return status.Error(codes.Internal, "internal error")
	}
	var changes []*ascv1.PathChange
	for p := range bytes.SplitSeq(names, []byte{0}) {
		if len(p) > 0 {
			changes = append(changes, &ascv1.PathChange{Path: p, Status: "M"})
		}
	}
	truncated := false
	if limit := int(req.GetMaxFiles()); limit > 0 && len(changes) > limit {
		changes, truncated = changes[:limit], true
	}
	if err := st.Send(&ascv1.DiffResponse{Changes: changes}); err != nil {
		return err
	}
	var text []byte
	switch req.GetFormat() {
	case ascv1.DiffFormat_DIFF_FORMAT_PATCH:
		args := []string{"diff", "-p", rename}
		if req.GetBinary() {
			args = append(args, "--binary")
		}
		text, err = f.git(nil, append(args, base, head)...)
	case ascv1.DiffFormat_DIFF_FORMAT_NUMSTAT:
		text, err = f.git(nil, "diff", "--numstat", rename, base, head)
	default:
		text = bytes.Join(func() [][]byte {
			var lines [][]byte
			for _, c := range changes {
				lines = append(lines, c.GetPath())
			}
			return lines
		}(), []byte{'\n'})
	}
	if err != nil {
		return status.Error(codes.Internal, "internal error")
	}
	const chunk = 8192
	for len(text) > chunk {
		resp := &ascv1.DiffResponse{Text: text[:chunk]}
		f.applyTamper("Diff", resp)
		if err := st.Send(resp); err != nil {
			return err
		}
		text = text[chunk:]
	}
	resp := &ascv1.DiffResponse{Text: text, Truncated: truncated}
	f.applyTamper("Diff", resp)
	return st.Send(resp)
}

func (f *fakeAsc) Blame(req *ascv1.BlameRequest, st grpc.ServerStreamingServer[ascv1.BlameResponse]) error {
	if err := f.checkRepo(req.GetRepo(), ascv1.IdMode_ID_MODE_GIT); err != nil {
		return err
	}
	oid, err := f.rev(req.GetRev())
	if err != nil {
		return err
	}
	out, err := f.git(nil, "blame", "--porcelain", oid, "--", string(req.GetPath()))
	if err != nil {
		return status.Error(codes.NotFound, "not a file at that revision")
	}
	type run struct {
		sha      string
		path     string
		start    uint32
		length   uint32
		origLine uint32
	}
	var (
		runs  []run
		files = map[string]string{}
		lines = strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	)
	shaRe := regexp.MustCompile(`^([0-9a-f]{40}) (\d+) (\d+)(?: (\d+))?$`)
	for i := 0; i < len(lines); i++ {
		m := shaRe.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		orig, _ := strconv.Atoi(m[2])
		final, _ := strconv.Atoi(m[3])
		// header lines up to the content line (starts with a tab)
		for i++; i < len(lines) && !strings.HasPrefix(lines[i], "\t"); i++ {
			if p, ok := strings.CutPrefix(lines[i], "filename "); ok {
				files[m[1]] = p
			}
		}
		r := run{sha: m[1], path: files[m[1]], start: uint32(final - 1), length: 1, origLine: uint32(orig - 1)}
		if n := len(runs); n > 0 && runs[n-1].sha == r.sha && runs[n-1].start+runs[n-1].length == r.start &&
			runs[n-1].origLine+runs[n-1].length == r.origLine {
			runs[n-1].length++
		} else {
			runs = append(runs, r)
		}
	}
	resp := &ascv1.BlameResponse{}
	seen := map[string]bool{}
	for _, r := range runs {
		resp.Hunks = append(resp.Hunks, &ascv1.BlameHunk{
			Origin: oidBytes(r.sha), OrigPath: []byte(r.path), OrigStart: r.origLine, Start: r.start, Len: r.length,
		})
		if !seen[r.sha] {
			seen[r.sha] = true
			e, _, _, err := f.logEntry(r.sha)
			if err != nil {
				return err
			}
			resp.Commits = append(resp.Commits, e)
		}
	}
	f.applyTamper("Blame", resp)
	return st.Send(resp)
}

// ---------------------------------------------------------------------------------------- bulk

func (f *fakeAsc) CheckAttr(_ context.Context, req *ascv1.CheckAttrRequest) (*ascv1.CheckAttrResponse, error) {
	if err := f.checkRepo(req.GetRepo(), ascv1.IdMode_ID_MODE_GIT); err != nil {
		return nil, err
	}
	oid, err := f.rev(req.GetRev())
	if err != nil {
		return nil, err
	}
	resp := &ascv1.CheckAttrResponse{}
	for _, p := range req.GetPaths() {
		args := append([]string{"check-attr", "-z", "--source", oid}, req.GetNames()...)
		out, err := f.git(nil, append(args, "--", string(p))...)
		if err != nil {
			return nil, status.Error(codes.Internal, "internal error")
		}
		values := map[string]string{}
		fields := bytes.Split(out, []byte{0})
		for i := 0; i+2 < len(fields); i += 3 {
			values[string(fields[i+1])] = string(fields[i+2])
		}
		resp.PerPath = append(resp.PerPath, &ascv1.AttrValues{Values: values})
	}
	f.applyTamper("CheckAttr", resp)
	return resp, nil
}

func (f *fakeAsc) Grep(req *ascv1.GrepRequest, st grpc.ServerStreamingServer[ascv1.GrepResponse]) error {
	if err := f.checkRepo(req.GetRepo(), ascv1.IdMode_ID_MODE_GIT); err != nil {
		return err
	}
	oid, err := f.rev(req.GetRev())
	if err != nil {
		return err
	}
	pattern := req.GetPattern()
	if req.GetFixed() {
		pattern = regexp.QuoteMeta(pattern)
	}
	if req.GetIgnoreCase() {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return status.Error(codes.InvalidArgument, "bad pattern")
	}
	var only string
	for _, p := range req.GetPaths() {
		if lit, ok := strings.CutPrefix(p, ":(literal)"); ok {
			only = lit
		}
	}
	entries, err := f.lsTree(oid+"^{tree}", true)
	if err != nil {
		return err
	}
	limit := int(req.GetMaxResults())
	if limit == 0 {
		limit = 1000
	}
	var matches []*ascv1.GrepMatch
	for _, e := range entries {
		if e.typ != "blob" || e.size == 0 || e.size > 1<<20 || (only != "" && e.path != only) {
			continue
		}
		data, err := f.git(nil, "cat-file", "blob", e.oid)
		if err != nil || bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0 {
			continue
		}
		for i, line := range bytes.Split(data, []byte{'\n'}) {
			if re.Match(line) {
				matches = append(matches, &ascv1.GrepMatch{Path: []byte(e.path), Line: uint32(i + 1), Text: line})
				if len(matches) >= limit {
					return st.Send(&ascv1.GrepResponse{Matches: matches})
				}
			}
		}
	}
	return st.Send(&ascv1.GrepResponse{Matches: matches})
}

func (f *fakeAsc) Archive(req *ascv1.ArchiveRequest, st grpc.ServerStreamingServer[ascv1.ArchiveResponse]) error {
	if err := f.checkRepo(req.GetRepo(), ascv1.IdMode_ID_MODE_GIT); err != nil {
		return err
	}
	oid, err := f.rev(req.GetRev())
	if err != nil {
		return err
	}
	format := req.GetFormat()
	if format != "tar" && format != "zip" {
		return status.Error(codes.InvalidArgument, "format must be `tar` or `zip`")
	}
	args := []string{"archive", "--format=" + format}
	if req.GetPrefix() != "" {
		args = append(args, "--prefix="+req.GetPrefix())
	}
	out, err := f.git(nil, append(args, oid)...)
	if err != nil {
		return status.Error(codes.Internal, "internal error")
	}
	const chunk = 8192
	for len(out) > chunk {
		if err := st.Send(&ascv1.ArchiveResponse{Data: out[:chunk]}); err != nil {
			return err
		}
		out = out[chunk:]
	}
	return st.Send(&ascv1.ArchiveResponse{Data: out})
}
