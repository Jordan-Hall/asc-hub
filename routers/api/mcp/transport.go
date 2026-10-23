// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package mcp

import (
	stdjson "encoding/json" //nolint:depguard // RawMessage only; marshalling goes through forgejo.org/modules/json
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"forgejo.org/modules/json"
	"forgejo.org/modules/log"
	"forgejo.org/modules/web"
	"forgejo.org/routers/api/shared"
	"forgejo.org/services/context"
)

// Routes returns the router mounted at /api/mcp.
//
// The chain is: guard (disabled, Origin, query token, method, content type, Bearer), then the shared API
// middlewares (the same token and OAuth2 authentication as /api/v1), then the handler, which checks the principal
// and the scope per call.
func Routes() *web.Route {
	return NewServer().Routes()
}

// Routes returns the router of this server.
func (s *Server) Routes() *web.Route {
	m := web.NewRoute()
	m.Use(guard)
	m.Use(shared.Middlewares()...)
	m.Post("", s.handle)
	return m
}

func isJSONContentType(header string) bool {
	mt, _, err := mime.ParseMediaType(header)
	return err == nil && mt == "application/json"
}

// acceptsJSON reports whether the Accept header, when present, allows an application/json answer.
func acceptsJSON(values []string) bool {
	if len(values) == 0 {
		return true
	}
	for _, v := range values {
		for part := range strings.SplitSeq(v, ",") {
			mt, _, err := mime.ParseMediaType(strings.TrimSpace(part))
			if err == nil && (mt == "application/json" || mt == "application/*" || mt == "*/*") {
				return true
			}
		}
	}
	return false
}

// rpcRequest is the envelope of one JSON-RPC request or notification.
type rpcRequest struct {
	ID     stdjson.RawMessage
	Method string
	Params stdjson.RawMessage
	// isNotification is true when the envelope has no id.
	isNotification bool
}

// parseEnvelope validates a request body. A JSON array (batch), a client response and any malformed envelope are
// refused with the id when it could be read.
func parseEnvelope(body []byte) (*rpcRequest, stdjson.RawMessage, *rpcError) {
	trimmed := strings.TrimSpace(string(body))
	if strings.HasPrefix(trimmed, "[") {
		return nil, nil, withStatus(newRPCError(codeInvalidRequest, "batch requests are not supported"), http.StatusBadRequest)
	}
	var fields map[string]stdjson.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return nil, nil, withStatus(newRPCError(codeParseError, "the body is not a JSON object"), http.StatusBadRequest)
	}
	var id stdjson.RawMessage
	rawID, hasID := fields["id"]
	if hasID {
		if !validID(rawID) {
			return nil, nil, withStatus(newRPCError(codeInvalidRequest, "id must be a string or a number"), http.StatusBadRequest)
		}
		id = rawID
	}
	var version string
	if raw, ok := fields["jsonrpc"]; !ok || json.Unmarshal(raw, &version) != nil || version != "2.0" {
		return nil, id, withStatus(newRPCError(codeInvalidRequest, `jsonrpc must be "2.0"`), http.StatusBadRequest)
	}
	var method string
	if raw, ok := fields["method"]; !ok || json.Unmarshal(raw, &method) != nil || method == "" || len(method) > maxMethodBytes {
		return nil, id, withStatus(newRPCError(codeInvalidRequest, "method must be a string"), http.StatusBadRequest)
	}
	params := fields["params"]
	if len(params) > 0 && !isJSONObject(params) {
		return nil, id, withStatus(newRPCError(codeInvalidParams, "params must be an object"), http.StatusBadRequest)
	}
	return &rpcRequest{ID: id, Method: method, Params: params, isNotification: !hasID}, id, nil
}

func validID(raw stdjson.RawMessage) bool {
	if len(raw) == 0 || len(raw) > maxIDBytes {
		return false
	}
	switch raw[0] {
	case '"':
		var s string
		return json.Unmarshal(raw, &s) == nil
	case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		var n stdjson.Number
		return json.Unmarshal(raw, &n) == nil
	}
	return false
}

func withStatus(e *rpcError, status int) *rpcError {
	e.status = status
	return e
}

// checkHeaders validates the MCP request headers against the body. Mcp-Method is required and must equal the
// method. MCP-Protocol-Version must name the supported version, except on server/discover, where the client does
// not know the version yet. Mcp-Name is checked per method by callTool.
func checkHeaders(h http.Header, req *rpcRequest) *rpcError {
	if h.Get("Mcp-Method") != req.Method {
		return headerMismatch("Mcp-Method is missing or does not match the method")
	}
	if req.Method == "server/discover" {
		return nil
	}
	version := h.Get("MCP-Protocol-Version")
	if version != ProtocolVersion {
		e := newRPCError(codeInvalidParams, "MCP-Protocol-Version is missing or not supported")
		e.Data = map[string]any{"supported": []string{ProtocolVersion}}
		e.status = http.StatusBadRequest
		return e
	}
	return nil
}

// handle serves one POST.
func (s *Server) handle(ctx *context.APIContext) {
	if rpcErr := authenticate(ctx); rpcErr != nil {
		writeError(ctx, nil, rpcErr)
		return
	}
	if !acceptsJSON(ctx.Req.Header.Values("Accept")) {
		writeError(ctx, nil, withStatus(newRPCError(codeInvalidRequest, "the client must accept application/json"), http.StatusNotAcceptable))
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(ctx.Resp, ctx.Req.Body, MaxRequestBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(ctx, nil, withStatus(newRPCError(codeInvalidRequest, "the request body is too large"), http.StatusRequestEntityTooLarge))
			return
		}
		writeError(ctx, nil, withStatus(newRPCError(codeParseError, "the request body could not be read"), http.StatusBadRequest))
		return
	}

	req, id, rpcErr := parseEnvelope(body)
	if rpcErr != nil {
		writeError(ctx, id, rpcErr)
		return
	}
	if rpcErr := checkHeaders(ctx.Req.Header, req); rpcErr != nil {
		writeError(ctx, req.ID, rpcErr)
		return
	}
	if req.isNotification {
		// Stateless: nothing to record. 202 with no body, as the transport requires.
		ctx.Resp.WriteHeader(http.StatusAccepted)
		return
	}

	result, rpcErr := s.dispatch(ctx, req)
	if rpcErr != nil {
		writeError(ctx, req.ID, rpcErr)
		return
	}
	writeResult(ctx, req.ID, result)
}

// dispatch runs one JSON-RPC method.
func (s *Server) dispatch(ctx *context.APIContext, req *rpcRequest) (any, *rpcError) {
	switch req.Method {
	case "server/discover":
		return s.discover(), nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		if cursor, ok := paramString(req.Params, "cursor"); ok && cursor != "" {
			return nil, newRPCError(codeInvalidParams, "the list is not paginated")
		}
		return s.listTools(ctx), nil
	case "tools/call":
		return s.callTool(ctx, req.Params, ctx.Req.Header.Get("Mcp-Name"))
	default:
		return nil, newRPCError(codeMethodNotFound, "method not found")
	}
}

func paramString(params stdjson.RawMessage, key string) (string, bool) {
	if len(params) == 0 {
		return "", false
	}
	var fields map[string]stdjson.RawMessage
	if json.Unmarshal(params, &fields) != nil {
		return "", false
	}
	raw, ok := fields[key]
	if !ok {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		// Present but not a string: report it as a non-empty value so the caller refuses it.
		return "?", true
	}
	return s, true
}

type rpcResponse struct {
	JSONRPC string             `json:"jsonrpc"`
	ID      stdjson.RawMessage `json:"id"`
	Result  any                `json:"result,omitempty"`
	Error   *rpcError          `json:"error,omitempty"`
}

func writeResult(ctx *context.APIContext, id stdjson.RawMessage, result any) {
	write(ctx, http.StatusOK, rpcResponse{JSONRPC: "2.0", ID: idOrNull(id), Result: result})
}

func writeError(ctx *context.APIContext, id stdjson.RawMessage, e *rpcError) {
	status := e.status
	if status == 0 {
		status = http.StatusOK
	}
	if e.challenge != "" {
		ctx.Resp.Header().Set("WWW-Authenticate", e.challenge)
	}
	write(ctx, status, rpcResponse{JSONRPC: "2.0", ID: idOrNull(id), Error: e})
}

func idOrNull(id stdjson.RawMessage) stdjson.RawMessage {
	if len(id) == 0 {
		return stdjson.RawMessage("null")
	}
	return id
}

func write(ctx *context.APIContext, status int, resp rpcResponse) {
	body, err := json.Marshal(resp)
	if err != nil {
		log.Error("MCP response is not JSON: %v", err)
		status = http.StatusInternalServerError
		body = []byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32603,"message":"internal error"}}`)
	}
	h := ctx.Resp.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	ctx.Resp.WriteHeader(status)
	_, _ = ctx.Resp.Write(body)
}
