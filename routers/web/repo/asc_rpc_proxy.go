// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"bytes"
	gocontext "context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"forgejo.org/models/perm"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	"forgejo.org/modules/ascclient"
	_ "forgejo.org/modules/ascproto/ascv1" // registers the asc.v1 descriptors the proxy reads
	"forgejo.org/modules/git/ascbackend"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascidentity"
	"forgejo.org/services/context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/mem"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
)

// The asc RPC proxy (SPEC 24.5.3, PLAN unit P04).
//
// `POST /-/asc/rpc/<service>/<method>` lets the browser islands of asc-ui read from `asc serve` through
// Forgejo. The browser speaks gRPC-Web (binary, application/grpc-web+proto) over the signed-in
// session. The proxy
//
//  1. refuses everything that is not on a fixed allowlist of read RPCs (ascRPCReadMethods);
//  2. decodes the single request message and finds every asc.v1.RepoRef in it, at any depth;
//  3. resolves each one to a Forgejo repository and requires the signed-in user (or an anonymous
//     visitor) to have code read access to it, with storage_backend=asc; a repository that cannot be
//     read answers exactly like one that does not exist;
//  4. rewrites every RepoRef to the canonical lower case "owner/repo" name that the asc backend uses
//     and re-encodes the message, so what asc authorises is what Forgejo checked;
//  5. forwards the message to asc with the internal token and hands the answer back unchanged
//     (responses are not decoded).
//
// Nothing from the browser except the checked request message reaches asc: no header, no cookie and no
// credential is forwarded. The proxy never calls a method that writes, and every write to asc from
// the web goes through Forgejo's own handlers.
//
// A signed-in person's call also carries the hub's signed claim about them (ascidentity.AttachDoer, role read, for the
// one repository of the request), so asc decides it as that person; see the note on draft refs below.
//
// Failures that happen before a gRPC call exists (a disabled proxy, an unknown method, a bad
// content type, an over-long body) are plain HTTP errors. Everything after that is a gRPC-Web
// answer: HTTP 200 and a trailer frame that carries grpc-status.
const (
	// ascRPCMaxRequestBytes bounds one request message. Requests are small (a revspec, a path, a
	// pattern); the largest legitimate ones are lists of change ids.
	ascRPCMaxRequestBytes = 1 << 20
	// ascRPCMaxRepoRefs bounds the RepoRef messages in one request.
	ascRPCMaxRepoRefs = 8
	// ascRPCMaxDepth bounds the nesting of a request message.
	ascRPCMaxDepth = 16
	// ascRPCMaxConcurrent bounds the calls in flight through the proxy.
	ascRPCMaxConcurrent = 64
	// ascRPCStreamTimeout bounds a streaming call from start to end. A unary call has the asc client's own
	// call timeout.
	ascRPCStreamTimeout = ascclient.DefaultStreamTimeout
	// ascRPCMaxRepoNameBytes bounds the owner/repo name of a RepoRef before it is looked up.
	ascRPCMaxRepoNameBytes = 255
	// ascRPCMaxStatusMessage caps the text of grpc-message.
	ascRPCMaxStatusMessage = 256
	// ascRPCMaxStatusDetails caps the encoded size of grpc-status-details-bin.
	ascRPCMaxStatusDetails = 8 << 10

	ascRPCContentType = "application/grpc-web+proto"
	ascRPCRepoRefName = protoreflect.FullName("asc.v1.RepoRef")
)

// ascRPCReadMethods is the allowlist, the repository-scoped reads of PLAN unit P10's relay table
// (SPEC 24.4.5.1). The value tells whether the request may carry no RepoRef. A method that is not
// listed is refused, so a new RPC is closed until someone adds it here on purpose.
//
// Left out on purpose:
//   - every method that writes, and the object-level reads (HasObjects, GetObjects, PutObjects): the
//     browser has no use for them;
//   - Archive: Forgejo has its own archive download, and a 64 MiB reply cap would cut it;
//   - Cloud.GetWorkspace and the other Cloud calls: they select a workspace by owner and name, not by
//     repository permission, and the internal token would let one user read another user's draft;
//   - Workspace (daemon IPC only) and Bridge (IDE only).
var ascRPCReadMethods = map[string]bool{
	"asc.v1.Repo/Version":       true, // no repository: the server version and features only
	"asc.v1.Repo/Log":           false,
	"asc.v1.Repo/Diff":          false,
	"asc.v1.Repo/Blame":         false,
	"asc.v1.Repo/ReadChangeset": false,
	"asc.v1.Repo/ReadFile":      false,
	"asc.v1.Repo/Grep":          false,
	"asc.v1.Repo/CommitGraph":   false,
	"asc.v1.Repo/LineHistory":   false,
	"asc.v1.Repo/Compare":       false,
	"asc.v1.Repo/RepoConfig":    false,

	"asc.v1.Changes/ResolveChanges": false,
	"asc.v1.Changes/Stack":          false,
	"asc.v1.Changes/Versions":       false,
	"asc.v1.Changes/InterDiff":      false,
	"asc.v1.Changes/Mergeability":   false,
	"asc.v1.Changes/Overlaps":       false,

	"asc.v1.Projects/ProjectGraph": false,
	"asc.v1.Projects/Affected":     false,

	"asc.v1.CodeIntel/Search":      false,
	"asc.v1.CodeIntel/Symbols":     false,
	"asc.v1.CodeIntel/Semantic":    false,
	"asc.v1.CodeIntel/IndexStatus": false,
}

// ascRPCSlots bounds the calls in flight.
var ascRPCSlots = make(chan struct{}, ascRPCMaxConcurrent)

// ascRPCRawCodec carries a protobuf message as the bytes it already is. Its name is "proto", so
// the content type on the wire to asc is application/grpc+proto.
type ascRPCRawCodec struct{}

type ascRPCRaw struct{ b []byte }

func (ascRPCRawCodec) Name() string { return "proto" }

func (ascRPCRawCodec) Marshal(v any) (mem.BufferSlice, error) {
	m, ok := v.(*ascRPCRaw)
	if !ok {
		return nil, errors.New("asc rpc proxy: not a raw message")
	}
	return mem.BufferSlice{mem.SliceBuffer(m.b)}, nil
}

func (ascRPCRawCodec) Unmarshal(data mem.BufferSlice, v any) error {
	m, ok := v.(*ascRPCRaw)
	if !ok {
		return errors.New("asc rpc proxy: not a raw message")
	}
	m.b = data.Materialize()
	return nil
}

// ascRPCConn is the connection to asc serve. Tests replace ascRPCNewConn.
var (
	ascRPCConnMu  sync.Mutex
	ascRPCConnVal grpc.ClientConnInterface
	ascRPCNewConn = func() (grpc.ClientConnInterface, error) {
		if setting.InternalToken == "" {
			return nil, errors.New("the internal token is not set")
		}
		return ascclient.New(ascclient.Config{Target: setting.Asc.ServerURL, InternalToken: setting.InternalToken})
	}
)

func ascRPCConn() (grpc.ClientConnInterface, error) {
	ascRPCConnMu.Lock()
	defer ascRPCConnMu.Unlock()
	if ascRPCConnVal == nil {
		c, err := ascRPCNewConn()
		if err != nil {
			return nil, err
		}
		ascRPCConnVal = c
	}
	return ascRPCConnVal, nil
}

// ascRPCStatusError is a gRPC status chosen by the proxy itself.
type ascRPCStatusError struct {
	code codes.Code
	msg  string
}

func (e *ascRPCStatusError) Error() string { return e.msg }

var errAscRPCNotFound = &ascRPCStatusError{codes.NotFound, "repository not found"}

func ascRPCInvalid(msg string) error { return &ascRPCStatusError{codes.InvalidArgument, msg} }

// AscRPCProxy serves `POST /-/asc/rpc/{service}/{method}`.
func AscRPCProxy(ctx *context.Context) {
	if !setting.Asc.Enabled {
		ctx.Error(http.StatusNotFound)
		return
	}
	service := ctx.PathParamRaw("service")
	if !strings.Contains(service, ".") {
		service = "asc.v1." + service
	}
	name := service + "/" + ctx.PathParamRaw("method")
	allowEmpty, ok := ascRPCReadMethods[name]
	if !ok {
		ctx.Error(http.StatusNotFound)
		return
	}
	if ctx.Req.Method != http.MethodPost {
		ctx.Resp.Header().Set("Allow", http.MethodPost)
		ctx.Error(http.StatusMethodNotAllowed)
		return
	}
	if !ascRPCBinaryGRPCWeb(ctx.Req) {
		ctx.Error(http.StatusUnsupportedMediaType)
		return
	}
	// A gRPC-Web request message is one frame: a flag byte, a 4 byte length, the message.
	body, err := io.ReadAll(io.LimitReader(ctx.Req.Body, ascRPCMaxRequestBytes+6))
	if err != nil {
		ctx.Error(http.StatusBadRequest)
		return
	}
	if len(body) > ascRPCMaxRequestBytes+5 {
		ctx.Error(http.StatusRequestEntityTooLarge)
		return
	}
	msg, ok := ascRPCParseFrame(body)
	if !ok {
		ctx.Error(http.StatusBadRequest)
		return
	}

	select {
	case ascRPCSlots <- struct{}{}:
		defer func() { <-ascRPCSlots }()
	default:
		ascRPCFinish(ctx, nil, &ascRPCStatusError{codes.ResourceExhausted, "too many requests"})
		return
	}

	req, subject, err := ascRPCPrepare(ctx, name, allowEmpty, msg)
	if err != nil {
		ascRPCFinish(ctx, nil, err)
		return
	}
	conn, err := ascRPCConn()
	if err != nil {
		log.Error("asc rpc proxy: no connection to asc serve: %v", err)
		ascRPCFinish(ctx, nil, &ascRPCStatusError{codes.Unavailable, "asc is unavailable"})
		return
	}

	// The call outlives nothing: it ends when the browser goes away. A signed-in person's call carries their claim, so
	// asc decides it as that person: their own draft refs resolve and no one else's do. A visitor's call is the
	// service credential alone, for which no draft ref resolves. A signed-in person whose claim cannot be made gets
	// no call: the hub does not fall back to the service credential for them.
	callCtx := ascclient.WithSystem(ctx.Req.Context())
	if subject != "" && ctx.Doer != nil && ctx.IsSigned {
		var cerr error
		if callCtx, cerr = ascidentity.AttachDoer(callCtx, ctx.Doer, ascidentity.RoleRead, subject); cerr != nil {
			log.Error("asc rpc proxy: the claim about the person: %v", cerr)
			ascRPCFinish(ctx, nil, &ascRPCStatusError{codes.Unavailable, "asc is unavailable"})
			return
		}
	}
	method := "/" + name
	md, _ := ascRPCMethod(name)
	if md.IsStreamingServer() {
		ascRPCStream(ctx, callCtx, conn, method, req)
		return
	}
	ascRPCUnary(ctx, callCtx, conn, method, req)
}

// ascRPCBinaryGRPCWeb reports whether the request is binary gRPC-Web without compression. The
// base64 "grpc-web-text" format and compressed messages are refused.
func ascRPCBinaryGRPCWeb(r *http.Request) bool {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || (mt != ascRPCContentType && mt != "application/grpc-web") {
		return false
	}
	if enc := r.Header.Get("Grpc-Encoding"); enc != "" && enc != "identity" {
		return false
	}
	return true
}

// ascRPCParseFrame returns the message of a body that holds exactly one uncompressed data frame.
func ascRPCParseFrame(body []byte) ([]byte, bool) {
	if len(body) < 5 || body[0] != 0 {
		return nil, false
	}
	n := binary.BigEndian.Uint32(body[1:5])
	if uint64(n) != uint64(len(body)-5) {
		return nil, false
	}
	return body[5:], true
}

// ascRPCMethod finds the descriptor of an allowlisted method.
func ascRPCMethod(name string) (protoreflect.MethodDescriptor, bool) {
	svc, m, ok := strings.Cut(name, "/")
	if !ok {
		return nil, false
	}
	d, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(svc))
	if err != nil {
		return nil, false
	}
	sd, ok := d.(protoreflect.ServiceDescriptor)
	if !ok {
		return nil, false
	}
	md := sd.Methods().ByName(protoreflect.Name(m))
	return md, md != nil && !md.IsStreamingClient()
}

// ascRPCUnitOf is the unit of the repository whose read access a method needs. The calls go to asc with the internal
// credential, so what asc would ask of the caller is asked here. The changes of a repository (their stacks, versions and
// overlaps) are pull request data and need pull request read access; everything else is code.
func ascRPCUnitOf(method string) unit.Type {
	if strings.HasPrefix(method, "asc.v1.Changes/") {
		return unit.TypePullRequests
	}
	return unit.TypeCode
}

// Draft refs. A person's drafts are the refs refs/users/<user id>/heads/... and only that person reads them. asc
// enforces this: a call that carries the claim of the signed-in person (ascidentity.AttachDoer) is decided as that
// person, whose own drafts resolve and no one else's, and a call without a claim is the hub's service credential, for
// which no draft ref resolves at all. The proxy always attaches the claim of a signed-in person.
//
// The checks below are defence in depth and never the only barrier. They look at the fields that carry revisions
// and refs (names in ascRPCNeutralFields are paths, patterns, search text, tokens and project selectors; any other
// string field is treated as a revision, so a field added to asc later is screened until someone decides otherwise)
// and accept a name only in these forms:
//
//   - a hex object id, or HEAD;
//   - a fully qualified ref under refs/heads/ or refs/tags/, written exactly like that;
//   - a fully qualified draft ref of the signed-in person, refs/users/<their id>/;
//   - a short branch name that is no alias for anything else: asc resolves the short forms users/<id>/heads/<name>
//     and heads/<name> as refs too, so a short name that starts with users/ or refs/, or that is written with a
//     doubled, leading, trailing or backslash separator, a dot segment or a control character, is refused.
//
// A revision expression (A..B, A...B, ^A, A~2, A^{tree}, A@{u}, A:path) is split at its operators and each name in it
// is screened. Bytes fields are ids or paths: an id must be 20 or 32 bytes, and bytes that could be a name are
// screened as a name.
var ascRPCNeutralFields = map[protoreflect.Name]bool{
	"paths": true, "exclude_paths": true, "path": true, "def_path": true, "langs": true, "kinds": true, "pattern": true,
	"query": true, "name": true, "author": true, "grep": true, "pickaxe": true, "select": true,
	"page_token": true, "resume_token": true, "ahead_page_token": true, "behind_page_token": true,
}

// ascRPCIDFields are the bytes fields that hold a change, file or tree id.
var ascRPCIDFields = map[protoreflect.Name]bool{"change": true, "changes": true, "stack_top": true, "file_id": true}

const (
	ascRPCTrunkIDLen = 20 // a git object id
	ascRPCAscIDLen   = 32 // an asc id
)

var errAscRPCUserRef = ascRPCInvalid("refs of other users are not readable here")

// ascRPCScreen reports whether a revision expression, a field of a request, names nothing but what the person may
// read. doer is the signed-in person's id, 0 for a visitor.
func ascRPCScreen(expr string, doer int64) bool {
	if len(expr) > ascRPCMaxRepoNameBytes*8 {
		return false
	}
	for _, r := range expr {
		if r < 0x20 || r == 0x7f || r == '\\' || r == 0xfffd {
			return false
		}
	}
	// Split at the operators of the revision syntax. ".." and "..." first, so a dot is only ever part of a name.
	e := strings.ReplaceAll(expr, "...", " ")
	e = strings.ReplaceAll(e, "..", " ")
	for name := range strings.FieldsFuncSeq(e, ascRPCIsOperator) {
		if !ascRPCScreenName(name, doer) {
			return false
		}
	}
	return true
}

// ascRPCIsOperator is a character that ends a name in a revision expression: the space and the operators ^ ~ : @ ! { }
// ( ) and the comma. Other white space and control characters were refused before.
func ascRPCIsOperator(r rune) bool {
	return strings.ContainsRune(" ^~:@!{}(),", r)
}

func ascRPCScreenName(name string, doer int64) bool {
	if name == "" || name == "HEAD" || name == "-" {
		return true
	}
	if ascRPCIsDigits(name) { // the count of ~N and ^N
		return true
	}
	if ascRPCIsHex(name) {
		return true
	}
	// Separators: no empty, dot or dot-dot segment, no leading or trailing slash.
	if strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") {
		return false
	}
	for seg := range strings.SplitSeq(name, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	lower := strings.ToLower(name)
	if strings.Contains(lower, "refs/users") || strings.Contains(lower, "refs/pull") || strings.Contains(lower, "refs/snapshots") {
		// Only the exact fully qualified spellings below name such a ref.
		return doer > 0 && strings.HasPrefix(name, "refs/users/"+strconv.FormatInt(doer, 10)+"/") && !strings.Contains(name[len("refs/"):], "refs/")
	}
	switch {
	case strings.HasPrefix(name, "refs/heads/"), strings.HasPrefix(name, "refs/tags/"):
		return true
	case strings.HasPrefix(lower, "refs/"), lower == "refs", lower == "users", strings.HasPrefix(lower, "users/"):
		return false // every other namespace, and the short alias of the user refs
	}
	return true
}

func ascRPCPrintable(b []byte) bool {
	for _, c := range b {
		if c < 0x20 || c > 0x7e {
			return false
		}
	}
	return true
}

func ascRPCIsDigits(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}

func ascRPCIsHex(s string) bool {
	return len(s) >= 4 && len(s) <= 64 && strings.Trim(s, "0123456789abcdefABCDEF") == ""
}

// ascRPCPrepare validates the request message, checks read access to every repository it names and
// returns the message to send to asc, with canonical repository names, and the one repository (canonical name) it is
// about; the subject is empty for a request that names none.
func ascRPCPrepare(ctx *context.Context, name string, allowEmpty bool, raw []byte) (out []byte, subject string, err error) {
	md, ok := ascRPCMethod(name)
	if !ok {
		return nil, "", &ascRPCStatusError{codes.Unimplemented, "unknown method"}
	}
	msg := dynamicpb.NewMessage(md.Input())
	if err := (proto.UnmarshalOptions{RecursionLimit: ascRPCMaxDepth}).Unmarshal(raw, msg); err != nil {
		return nil, "", ascRPCInvalid("malformed request")
	}
	var doer int64
	if ctx.Doer != nil && ctx.IsSigned {
		doer = ctx.Doer.ID
	}
	var refs []protoreflect.Message
	if err := ascRPCCollectRefs(msg, 0, &refs, doer); err != nil {
		return nil, "", err
	}
	if len(refs) == 0 {
		if !allowEmpty {
			return nil, "", ascRPCInvalid("a repository is required")
		}
		return raw, "", nil
	}

	canon := make(map[string]string, len(refs))
	for _, ref := range refs {
		nameFD := ref.Descriptor().Fields().ByName("name")
		if nameFD == nil || nameFD.Kind() != protoreflect.StringKind {
			return nil, "", &ascRPCStatusError{codes.Internal, "internal error"}
		}
		given := ref.Get(nameFD).String()
		c, seen := canon[given]
		if !seen {
			var err error
			if c, err = ascRPCCanonicalName(ctx, given, ascRPCUnitOf(name)); err != nil {
				return nil, "", err
			}
			canon[given] = c
		}
		// The claim of a call is made for one repository, and so is every RPC the proxy serves.
		if subject != "" && subject != c {
			return nil, "", ascRPCInvalid("one repository per request")
		}
		subject = c
		ref.Set(nameFD, protoreflect.ValueOfString(c))
	}
	out, err = (proto.MarshalOptions{Deterministic: true}).Marshal(msg)
	if err != nil || len(out) > ascRPCMaxRequestBytes {
		return nil, "", ascRPCInvalid("malformed request")
	}
	return out, subject, nil
}

// ascRPCCollectRefs gathers every RepoRef message under m and screens every revision, ref and id it holds for the
// person doer (0 for a visitor). It refuses a message that holds a field this build does not know: such a field could
// be one that asc reads as a repository.
func ascRPCCollectRefs(m protoreflect.Message, depth int, refs *[]protoreflect.Message, doer int64) error {
	if depth > ascRPCMaxDepth {
		return ascRPCInvalid("request is nested too deeply")
	}
	if len(m.GetUnknown()) > 0 {
		return ascRPCInvalid("unknown field in request")
	}
	isRepoRef := m.Descriptor().FullName() == ascRPCRepoRefName
	if isRepoRef {
		if len(*refs) >= ascRPCMaxRepoRefs {
			return ascRPCInvalid("too many repositories in request")
		}
		*refs = append(*refs, m)
	}
	var err error
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if isRepoRef && fd.Name() == "name" {
			return true // rewritten to the canonical name by the caller
		}
		switch {
		case fd.IsMap():
			mv := fd.MapValue()
			v.Map().Range(func(k protoreflect.MapKey, val protoreflect.Value) bool {
				// A map is screened by key and value alike, under the name of the map.
				if err = ascRPCScreenValue(fd.Name(), fd.MapKey(), k.Value(), doer); err != nil {
					return false
				}
				err = ascRPCScreenOrWalk(fd.Name(), mv, val, depth, refs, doer)
				return err == nil
			})
		case fd.IsList():
			l := v.List()
			for i := range l.Len() {
				if err = ascRPCScreenOrWalk(fd.Name(), fd, l.Get(i), depth, refs, doer); err != nil {
					break
				}
			}
		default:
			err = ascRPCScreenOrWalk(fd.Name(), fd, v, depth, refs, doer)
		}
		return err == nil
	})
	return err
}

// ascRPCScreenOrWalk descends into a message value and screens a string or bytes value.
func ascRPCScreenOrWalk(name protoreflect.Name, fd protoreflect.FieldDescriptor, v protoreflect.Value, depth int, refs *[]protoreflect.Message, doer int64) error {
	if k := fd.Kind(); k == protoreflect.MessageKind || k == protoreflect.GroupKind {
		return ascRPCCollectRefs(v.Message(), depth+1, refs, doer)
	}
	return ascRPCScreenValue(name, fd, v, doer)
}

// ascRPCScreenValue applies the rules of the draft refs to one scalar of a request.
func ascRPCScreenValue(name protoreflect.Name, fd protoreflect.FieldDescriptor, v protoreflect.Value, doer int64) error {
	switch fd.Kind() {
	case protoreflect.StringKind:
		if !ascRPCNeutralFields[name] && !ascRPCScreen(v.String(), doer) {
			return errAscRPCUserRef
		}
	case protoreflect.BytesKind:
		b := v.Bytes()
		switch {
		case ascRPCIDFields[name]:
			if len(b) != ascRPCTrunkIDLen && len(b) != ascRPCAscIDLen {
				return ascRPCInvalid("malformed request")
			}
			// An id is never a name, but 20 or 32 readable bytes could be one: screen those as a name.
			if ascRPCPrintable(b) && !ascRPCScreen(string(b), doer) {
				return errAscRPCUserRef
			}
		case ascRPCNeutralFields[name]:
		default:
			if !ascRPCScreen(string(b), doer) {
				return errAscRPCUserRef
			}
		}
	}
	return nil
}

// ascRPCCanonicalName resolves "owner/repo" to the repository the asc backend names with
// ascbackend.RepoName, after checking that the requester may read its code. Every refusal returns
// errAscRPCNotFound, so a private repository and a missing one look the same.
func ascRPCCanonicalName(ctx *context.Context, given string, readUnit unit.Type) (string, error) {
	owner, name, ok := strings.Cut(given, "/")
	if !ok || owner == "" || name == "" || len(given) > ascRPCMaxRepoNameBytes || strings.Contains(name, "/") {
		return "", errAscRPCNotFound
	}
	repo, err := repo_model.GetRepositoryByOwnerAndName(ctx, owner, name)
	if err != nil {
		if repo_model.IsErrRepoNotExist(err) {
			return "", errAscRPCNotFound
		}
		log.Error("asc rpc proxy: GetRepositoryByOwnerAndName: %v", err)
		return "", &ascRPCStatusError{codes.Internal, "internal error"}
	}
	if repo.IsBeingCreated() || repo.IsBroken() || repo.GetStorageBackend() != string(gitrepo.StorageBackendAsc) {
		return "", errAscRPCNotFound
	}
	if err := repo.LoadOwner(ctx); err != nil {
		log.Error("asc rpc proxy: LoadOwner: %v", err)
		return "", &ascRPCStatusError{codes.Internal, "internal error"}
	}
	if !repo.Owner.IsOrganization() && !repo.Owner.IsActive {
		return "", errAscRPCNotFound
	}
	if ctx.Doer != nil && (!ctx.Doer.IsActive || ctx.Doer.ProhibitLogin) {
		return "", errAscRPCNotFound
	}

	var permission access_model.Permission
	if reducer := ctx.Authentication.Reducer(); reducer != nil {
		permission, err = access_model.GetUserRepoPermissionWithReducer(ctx, repo, ctx.Doer, reducer)
	} else {
		permission, err = access_model.GetUserRepoPermission(ctx, repo, ctx.Doer)
	}
	if err != nil {
		log.Error("asc rpc proxy: GetUserRepoPermission: %v", err)
		return "", &ascRPCStatusError{codes.Internal, "internal error"}
	}
	if !permission.CanAccess(perm.AccessModeRead, readUnit) {
		return "", errAscRPCNotFound
	}

	canonical, err := ascbackend.RepoName(setting.RepoRootPath, repo_model.RepoPath(repo.Owner.Name, repo.Name))
	if err != nil {
		return "", errAscRPCNotFound
	}
	return canonical, nil
}

func ascRPCUnary(ctx *context.Context, callCtx gocontext.Context, conn grpc.ClientConnInterface, method string, req []byte) {
	var reply ascRPCRaw
	err := conn.Invoke(callCtx, method, &ascRPCRaw{b: req}, &reply, grpc.ForceCodecV2(ascRPCRawCodec{}))
	if err != nil {
		ascRPCFinish(ctx, nil, err)
		return
	}
	if len(reply.b) > ascRPCMaxResponseBytes {
		ascRPCFinish(ctx, nil, &ascRPCStatusError{codes.ResourceExhausted, "response is too large"})
		return
	}
	w := ascRPCBegin(ctx)
	if err := ascRPCWriteFrame(w, 0, reply.b); err != nil {
		return
	}
	ascRPCFinish(ctx, w, nil)
}

func ascRPCStream(ctx *context.Context, callCtx gocontext.Context, conn grpc.ClientConnInterface, method string, req []byte) {
	callCtx, cancel := gocontext.WithTimeout(callCtx, ascRPCStreamTimeout)
	defer cancel()
	cs, err := conn.NewStream(callCtx, &grpc.StreamDesc{ServerStreams: true}, method, grpc.ForceCodecV2(ascRPCRawCodec{}))
	if err == nil {
		if err = cs.SendMsg(&ascRPCRaw{b: req}); err == nil {
			err = cs.CloseSend()
		}
	}
	if err != nil {
		ascRPCFinish(ctx, nil, err)
		return
	}

	var w *ascRPCWriter
	total := 0
	for {
		var m ascRPCRaw
		if err = cs.RecvMsg(&m); err != nil {
			if errors.Is(err, io.EOF) {
				err = nil
			}
			break
		}
		if total += len(m.b); total > ascRPCMaxResponseBytes {
			err = &ascRPCStatusError{codes.ResourceExhausted, "response is too large"}
			break
		}
		if w == nil {
			w = ascRPCBegin(ctx)
		}
		if werr := ascRPCWriteFrame(w, 0, m.b); werr != nil {
			return // the browser is gone
		}
	}
	ascRPCFinish(ctx, w, err)
}

// ascRPCWriter writes gRPC-Web frames to the response and flushes after each one.
type ascRPCWriter struct{ ctx *context.Context }

func ascRPCBegin(ctx *context.Context) *ascRPCWriter {
	h := ctx.Resp.Header()
	h.Set("Content-Type", ascRPCContentType)
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	ctx.Resp.WriteHeader(http.StatusOK)
	return &ascRPCWriter{ctx: ctx}
}

func ascRPCWriteFrame(w *ascRPCWriter, flag byte, payload []byte) error {
	var head [5]byte
	head[0] = flag
	binary.BigEndian.PutUint32(head[1:], uint32(len(payload)))
	if _, err := w.ctx.Resp.Write(head[:]); err != nil {
		return err
	}
	if _, err := w.ctx.Resp.Write(payload); err != nil {
		return err
	}
	w.ctx.Resp.Flush()
	return nil
}

// ascRPCFinish ends a call with its trailer frame. w is nil when nothing was written yet. A nil err
// is grpc-status 0.
func ascRPCFinish(ctx *context.Context, w *ascRPCWriter, err error) {
	if w == nil {
		w = ascRPCBegin(ctx)
	}
	code, msg, details := ascRPCMapError(ctx.Req.Context(), err)
	var t bytes.Buffer
	fmt.Fprintf(&t, "grpc-status: %d\r\n", code)
	if msg != "" {
		fmt.Fprintf(&t, "grpc-message: %s\r\n", ascRPCEncodeMessage(msg))
	}
	if details != "" {
		fmt.Fprintf(&t, "grpc-status-details-bin: %s\r\n", details)
	}
	_ = ascRPCWriteFrame(w, 0x80, t.Bytes())
}

// ascRPCMapError turns an error into what the browser may see. asc's own messages for the statuses
// a client can act on pass through, cut to a fixed size; anything that could describe the server or
// the connection (UNAVAILABLE, INTERNAL, an authentication failure of the internal token) is replaced
// by a fixed text and logged.
func ascRPCMapError(reqCtx gocontext.Context, err error) (code codes.Code, msg, details string) {
	if err == nil {
		return codes.OK, "", ""
	}
	var own *ascRPCStatusError
	if errors.As(err, &own) {
		return own.code, own.msg, ""
	}
	if reqCtx.Err() != nil {
		return codes.Canceled, "canceled", ""
	}
	st, ok := status.FromError(err)
	if !ok {
		log.Error("asc rpc proxy: %v", err)
		return codes.Internal, "internal error", ""
	}
	switch st.Code() {
	case codes.InvalidArgument, codes.NotFound, codes.FailedPrecondition, codes.OutOfRange,
		codes.ResourceExhausted, codes.Aborted, codes.AlreadyExists, codes.Canceled:
		msg = st.Message()
		if len(msg) > ascRPCMaxStatusMessage {
			msg = msg[:ascRPCMaxStatusMessage]
		}
		spb := st.Proto()
		spb.Message = msg
		if raw, merr := proto.Marshal(spb); merr == nil && len(raw) <= ascRPCMaxStatusDetails && len(spb.GetDetails()) > 0 {
			details = base64.RawStdEncoding.EncodeToString(raw)
		}
		return st.Code(), msg, details
	case codes.Unavailable:
		return codes.Unavailable, "asc is unavailable", ""
	case codes.DeadlineExceeded:
		return codes.DeadlineExceeded, "deadline exceeded", ""
	default:
		// UNKNOWN, INTERNAL, DATA_LOSS, UNIMPLEMENTED, and PERMISSION_DENIED or UNAUTHENTICATED, which
		// for the internal token mean a misconfiguration.
		log.Error("asc rpc proxy: asc answered %s", st.Code())
		return codes.Internal, "internal error", ""
	}
}

// ascRPCEncodeMessage percent-encodes grpc-message as the gRPC HTTP/2 spec says: printable ASCII
// except '%' stays, every other byte becomes %XX.
func ascRPCEncodeMessage(s string) string {
	var b strings.Builder
	for i := range len(s) {
		c := s[i]
		if c >= 0x20 && c <= 0x7e && c != '%' {
			b.WriteByte(c)
		} else {
			b.WriteString("%" + strings.ToUpper(strconv.FormatUint(uint64(c)|0x100, 16)[1:]))
		}
	}
	return b.String()
}

// ascRPCMaxResponseBytes bounds everything one call sends back to the browser. It is a variable
// only so that a test can lower it.
var ascRPCMaxResponseBytes = 64 << 20
