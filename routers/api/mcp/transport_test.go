// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package mcp

import (
	stdjson "encoding/json" //nolint:depguard // RawMessage only
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/json"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/modules/web"
	"forgejo.org/routers/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// The running task 47 of repo4 (models/fixtures/action_task.yml).
	testTaskToken = "8061e833a55f6fc0157c98b883e91fcfeeb1a71a"

	objectSchema = `{"type":"object","additionalProperties":false}`
)

type testEnv struct {
	t       *testing.T
	handler http.Handler
	srv     *Server
}

// newTestEnv builds the endpoint over a private registry with a read tool, a write tool and tools that misbehave.
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	require.NoError(t, unittest.PrepareTestDatabase())
	t.Cleanup(test.MockVariableValue(&setting.AppURL, "https://forge.example.com/"))
	t.Cleanup(test.MockVariableValue(&setting.MCP.Enabled, true))
	t.Cleanup(test.MockVariableValue(&setting.MCP.Write, false))
	t.Cleanup(test.MockVariableValue(&setting.MCP.AllowedOrigins, []string{"https://agent.example.com"}))
	RegisterLockdown(nil)
	t.Cleanup(func() { RegisterLockdown(nil) })

	reg := newRegistry()
	issue := []auth_model.AccessTokenScopeCategory{auth_model.AccessTokenScopeCategoryIssue}
	add := func(tool Tool) {
		tool.InputSchema = stdjson.RawMessage(objectSchema)
		if tool.Description == "" {
			tool.Description = "test tool"
		}
		if tool.Level == 0 {
			tool.Level = auth_model.Read
		}
		tool.Categories = issue
		if !tool.Write {
			tool.Annotations.ReadOnlyHint = true
		}
		require.NoError(t, reg.add(tool))
	}
	add(Tool{Name: "echo_read", Handler: func(c *Call) (*ToolResult, error) {
		return &ToolResult{Text: "hello " + c.API.Doer().Name, Structured: map[string]any{"user": c.API.Doer().Name}}, nil
	}})
	add(Tool{Name: "echo_write", Write: true, Level: auth_model.Write, Handler: func(c *Call) (*ToolResult, error) {
		return &ToolResult{Text: "written"}, nil
	}})
	add(Tool{Name: "tool_err", Handler: func(c *Call) (*ToolResult, error) {
		return nil, &ToolError{Code: ErrCodeNotFound, Message: "no such pull request"}
	}})
	add(Tool{Name: "bad_args", Handler: func(c *Call) (*ToolResult, error) {
		var args struct {
			N int `json:"n"`
		}
		if err := c.DecodeArgs(&args); err != nil {
			return nil, err
		}
		return &ToolResult{Text: "ok"}, nil
	}})
	add(Tool{Name: "boom", Handler: func(c *Call) (*ToolResult, error) { panic("secret-panic-detail") }})
	add(Tool{Name: "leaky", Handler: func(c *Call) (*ToolResult, error) {
		return nil, errors.New("dial tcp 10.0.0.5: password=hunter2")
	}})
	add(Tool{Name: "big", Handler: func(c *Call) (*ToolResult, error) {
		return &ToolResult{Text: strings.Repeat("x", MaxResultBytes+1)}, nil
	}})

	srv := &Server{reg: reg}
	r := web.NewRoute()
	r.Use(common.ProtocolMiddlewares()...)
	r.Mount("/api/mcp", srv.Routes())
	r.Get(ProtectedResourcePath, ProtectedResource)
	return &testEnv{t: t, handler: r, srv: srv}
}

// newToken creates a personal access token of user 2 with the scope.
func newToken(t *testing.T, scope auth_model.AccessTokenScope) string {
	t.Helper()
	token := &auth_model.AccessToken{UID: 2, Name: "mcp-" + string(scope), Scope: scope, ResourceAllRepos: true}
	require.NoError(t, auth_model.NewAccessToken(db.DefaultContext, token))
	return token.Token
}

type reqOpt func(*http.Request)

func withHeader(k, v string) reqOpt { return func(r *http.Request) { r.Header.Set(k, v) } }
func withoutHeader(k string) reqOpt { return func(r *http.Request) { r.Header.Del(k) } }

// raw posts body with the headers of a well-formed request, then applies opts.
func (e *testEnv) raw(token, body string, opts ...reqOpt) *httptest.ResponseRecorder {
	e.t.Helper()
	req := httptest.NewRequest(http.MethodPost, "https://forge.example.com/api/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", ProtocolVersion)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for _, opt := range opts {
		opt(req)
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec
}

// call posts one JSON-RPC request with matching Mcp-Method and Mcp-Name headers.
func (e *testEnv) call(token, method string, params any, opts ...reqOpt) *httptest.ResponseRecorder {
	e.t.Helper()
	msg := map[string]any{"jsonrpc": "2.0", "id": 7, "method": method}
	headers := []reqOpt{withHeader("Mcp-Method", method)}
	if params != nil {
		msg["params"] = params
		if p, ok := params.(map[string]any); ok {
			if name, ok := p["name"].(string); ok {
				headers = append(headers, withHeader("Mcp-Name", name))
			}
		}
	}
	body, err := json.Marshal(msg)
	require.NoError(e.t, err)
	return e.raw(token, string(body), append(headers, opts...)...)
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out), rec.Body.String())
	return out
}

func rpcErrorCode(t *testing.T, rec *httptest.ResponseRecorder) float64 {
	t.Helper()
	errObj, ok := decode(t, rec)["error"].(map[string]any)
	require.True(t, ok, rec.Body.String())
	return errObj["code"].(float64)
}

func toolNames(t *testing.T, rec *httptest.ResponseRecorder) []string {
	t.Helper()
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	result := decode(t, rec)["result"].(map[string]any)
	names := []string{}
	for _, raw := range result["tools"].([]any) {
		names = append(names, raw.(map[string]any)["name"].(string))
	}
	return names
}

func toolErrorBodyOf(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	result := decode(t, rec)["result"].(map[string]any)
	require.Equal(t, true, result["isError"], rec.Body.String())
	return result["structuredContent"].(map[string]any)["error"].(map[string]any)
}

func TestMcpTransportGate(t *testing.T) {
	e := newTestEnv(t)
	token := newToken(t, auth_model.AccessTokenScopeAll)

	t.Run("disabled", func(t *testing.T) {
		defer test.MockVariableValue(&setting.MCP.Enabled, false)()
		assert.Equal(t, http.StatusNotFound, e.call(token, "ping", nil).Code)

		req := httptest.NewRequest(http.MethodGet, "https://forge.example.com"+ProtectedResourcePath, nil)
		rec := httptest.NewRecorder()
		e.handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("methods other than POST", func(t *testing.T) {
		for _, method := range []string{http.MethodGet, http.MethodDelete, http.MethodPut, http.MethodPatch, http.MethodOptions} {
			req := httptest.NewRequest(method, "https://forge.example.com/api/mcp", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()
			e.handler.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusMethodNotAllowed, rec.Code, method)
			assert.Equal(t, http.MethodPost, rec.Header().Get("Allow"), method)
		}
	})

	t.Run("origin", func(t *testing.T) {
		for origin, want := range map[string]int{
			"https://forge.example.com":     http.StatusOK,
			"https://forge.example.com:443": http.StatusOK,
			"https://agent.example.com":     http.StatusOK,
			"https://AGENT.example.com":     http.StatusOK,
			"https://evil.example.com":      http.StatusForbidden,
			"http://forge.example.com":      http.StatusForbidden,
			"https://forge.example.com:444": http.StatusForbidden,
			"https://forge.example.com/x":   http.StatusForbidden,
			"null":                          http.StatusForbidden,
			"":                              http.StatusForbidden,
			"*":                             http.StatusForbidden,
		} {
			rec := e.call(token, "ping", nil, withHeader("Origin", origin))
			assert.Equal(t, want, rec.Code, "Origin %q: %s", origin, rec.Body.String())
		}
		// An absent Origin is accepted: non-browser clients send none.
		assert.Equal(t, http.StatusOK, e.call(token, "ping", nil).Code)
		// A repeated Origin header is refused.
		rec := e.call(token, "ping", nil, func(r *http.Request) {
			r.Header.Add("Origin", "https://forge.example.com")
			r.Header.Add("Origin", "https://forge.example.com")
		})
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("token in the query string", func(t *testing.T) {
		for _, q := range []string{"token=" + token, "access_token=" + token} {
			rec := e.call(token, "ping", nil, func(r *http.Request) { r.URL.RawQuery = q })
			assert.Equal(t, http.StatusBadRequest, rec.Code, q)
		}
	})

	t.Run("content type and accept", func(t *testing.T) {
		rec := e.call(token, "ping", nil, withHeader("Content-Type", "application/x-www-form-urlencoded"))
		assert.Equal(t, http.StatusUnsupportedMediaType, rec.Code)
		rec = e.call(token, "ping", nil, withoutHeader("Content-Type"))
		assert.Equal(t, http.StatusUnsupportedMediaType, rec.Code)
		rec = e.call(token, "ping", nil, withHeader("Content-Type", "application/json; charset=utf-8"))
		assert.Equal(t, http.StatusOK, rec.Code)
		rec = e.call(token, "ping", nil, withHeader("Accept", "text/event-stream"))
		assert.Equal(t, http.StatusNotAcceptable, rec.Code)
		rec = e.call(token, "ping", nil, withoutHeader("Accept"))
		assert.Equal(t, http.StatusOK, rec.Code)
		rec = e.call(token, "ping", nil, withHeader("Accept", "*/*"))
		assert.Equal(t, http.StatusOK, rec.Code)
	})
}

func TestMcpTransportAuth(t *testing.T) {
	e := newTestEnv(t)
	good := newToken(t, auth_model.AccessTokenScopeReadIssue)
	challenge := `resource_metadata="https://forge.example.com/.well-known/oauth-protected-resource/api/mcp"`

	t.Run("no credential", func(t *testing.T) {
		rec := e.call("", "ping", nil)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
		assert.Equal(t, "Bearer "+challenge, rec.Header().Get("WWW-Authenticate"))
	})

	t.Run("not a Bearer credential", func(t *testing.T) {
		for _, header := range []string{
			"token " + good,
			"Basic dG9rZW46" + good,
			"Bearer",
			"Bearer ",
		} {
			rec := e.call("", "ping", nil, withHeader("Authorization", header))
			assert.Equal(t, http.StatusUnauthorized, rec.Code, header)
			assert.Contains(t, rec.Header().Get("WWW-Authenticate"), challenge, header)
		}
		// A password as Basic credentials never opens the endpoint.
		rec := e.call("", "ping", nil, func(r *http.Request) { r.SetBasicAuth("user2", "password") })
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
		// Two Authorization headers are refused.
		rec = e.call(good, "ping", nil, func(r *http.Request) { r.Header.Add("Authorization", "Bearer "+good) })
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("unknown token", func(t *testing.T) {
		rec := e.call("0000000000000000000000000000000000000000", "ping", nil)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
		assert.Equal(t, `Bearer error="invalid_token", `+challenge, rec.Header().Get("WWW-Authenticate"))
	})

	t.Run("credential kinds", func(t *testing.T) {
		assert.Equal(t, http.StatusOK, e.call(good, "ping", nil).Code)
		// A token without a permission scope.
		for _, scope := range []auth_model.AccessTokenScope{"", auth_model.AccessTokenScopePublicOnly} {
			rec := e.call(newToken(t, scope), "ping", nil)
			assert.Equal(t, http.StatusForbidden, rec.Code, "scope %q", scope)
		}
		// An Actions task token.
		rec := e.call(testTaskToken, "ping", nil)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})
}

func TestMcpTransportEnvelope(t *testing.T) {
	e := newTestEnv(t)
	token := newToken(t, auth_model.AccessTokenScopeAll)
	ping := withHeader("Mcp-Method", "ping")

	for name, tc := range map[string]struct {
		body   string
		status int
		code   float64
	}{
		"not JSON":        {`nope`, 400, -32700},
		"empty":           {``, 400, -32700},
		"null":            {`null`, 400, -32700},
		"batch":           {`[{"jsonrpc":"2.0","id":1,"method":"ping"}]`, 400, -32600},
		"no version":      {`{"id":1,"method":"ping"}`, 400, -32600},
		"old version":     {`{"jsonrpc":"1.0","id":1,"method":"ping"}`, 400, -32600},
		"no method":       {`{"jsonrpc":"2.0","id":1}`, 400, -32600},
		"method number":   {`{"jsonrpc":"2.0","id":1,"method":5}`, 400, -32600},
		"id null":         {`{"jsonrpc":"2.0","id":null,"method":"ping"}`, 400, -32600},
		"id object":       {`{"jsonrpc":"2.0","id":{},"method":"ping"}`, 400, -32600},
		"id bool":         {`{"jsonrpc":"2.0","id":true,"method":"ping"}`, 400, -32600},
		"client response": {`{"jsonrpc":"2.0","id":1,"result":{}}`, 400, -32600},
		"params array":    {`{"jsonrpc":"2.0","id":1,"method":"ping","params":[]}`, 400, -32602},
		"trailing data":   {`{"jsonrpc":"2.0","id":1,"method":"ping"} {}`, 400, -32700},
	} {
		t.Run(name, func(t *testing.T) {
			rec := e.raw(token, tc.body, ping)
			assert.Equal(t, tc.status, rec.Code, rec.Body.String())
			assert.InDelta(t, tc.code, rpcErrorCode(t, rec), 0)
			assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		})
	}

	t.Run("string and number ids are echoed", func(t *testing.T) {
		rec := e.raw(token, `{"jsonrpc":"2.0","id":"abc-1","method":"ping"}`, ping)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "abc-1", decode(t, rec)["id"])
		rec = e.raw(token, `{"jsonrpc":"2.0","id":42,"method":"ping"}`, ping)
		assert.InDelta(t, 42, decode(t, rec)["id"], 0)
	})

	t.Run("body limit", func(t *testing.T) {
		pad := strings.Repeat("a", MaxRequestBytes)
		rec := e.raw(token, `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"pad":"`+pad+`"}}`, ping)
		assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	})

	t.Run("notification", func(t *testing.T) {
		rec := e.raw(token, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, withHeader("Mcp-Method", "notifications/initialized"))
		assert.Equal(t, http.StatusAccepted, rec.Code)
		assert.Empty(t, rec.Body.String())
	})

	t.Run("unknown method", func(t *testing.T) {
		rec := e.call(token, "resources/list", nil)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.InDelta(t, -32601, rpcErrorCode(t, rec), 0)
	})
}

func TestMcpTransportHeaders(t *testing.T) {
	e := newTestEnv(t)
	token := newToken(t, auth_model.AccessTokenScopeAll)

	t.Run("Mcp-Method", func(t *testing.T) {
		rec := e.call(token, "ping", nil, withoutHeader("Mcp-Method"))
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.InDelta(t, -32020, rpcErrorCode(t, rec), 0)
		rec = e.call(token, "ping", nil, withHeader("Mcp-Method", "tools/list"))
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.InDelta(t, -32020, rpcErrorCode(t, rec), 0)
		// A notification is checked as well.
		rec = e.raw(token, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, withHeader("Mcp-Method", "ping"))
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("MCP-Protocol-Version", func(t *testing.T) {
		for _, v := range []string{"", "2025-11-25", "2026-07-28 ", "latest"} {
			rec := e.call(token, "ping", nil, withHeader("MCP-Protocol-Version", v))
			assert.Equal(t, http.StatusBadRequest, rec.Code, "version %q", v)
			assert.InDelta(t, -32602, rpcErrorCode(t, rec), 0)
			errObj := decode(t, rec)["error"].(map[string]any)
			assert.Equal(t, map[string]any{"supported": []any{ProtocolVersion}}, errObj["data"])
		}
		rec := e.call(token, "ping", nil, withoutHeader("MCP-Protocol-Version"))
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("Mcp-Name", func(t *testing.T) {
		params := map[string]any{"name": "echo_read"}
		rec := e.call(token, "tools/call", params, withHeader("Mcp-Name", "echo_write"))
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.InDelta(t, -32020, rpcErrorCode(t, rec), 0)
		rec = e.call(token, "tools/call", params, withoutHeader("Mcp-Name"))
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.InDelta(t, -32020, rpcErrorCode(t, rec), 0)
	})

	t.Run("discover", func(t *testing.T) {
		// The client does not know the version yet, so the header is not needed here.
		rec := e.call(token, "server/discover", nil, withoutHeader("MCP-Protocol-Version"))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		result := decode(t, rec)["result"].(map[string]any)
		assert.Equal(t, []any{ProtocolVersion}, result["supportedVersions"])
		assert.Contains(t, result["capabilities"], "tools")
		assert.Equal(t, "forgejo", result["serverInfo"].(map[string]any)["name"])
		assert.Contains(t, result["instructions"], "data")
		assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	})
}

func TestMcpTransportMetadata(t *testing.T) {
	e := newTestEnv(t)
	req := httptest.NewRequest(http.MethodGet, "https://forge.example.com"+ProtectedResourcePath, nil)
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	doc := decode(t, rec)
	assert.Equal(t, "https://forge.example.com/api/mcp", doc["resource"])
	assert.Equal(t, []any{"https://forge.example.com"}, doc["authorization_servers"])
	assert.Equal(t, []any{"header"}, doc["bearer_methods_supported"])
	assert.Contains(t, doc["scopes_supported"], "read:issue")

	req = httptest.NewRequest(http.MethodPost, "https://forge.example.com"+ProtectedResourcePath, nil)
	rec = httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestMcpTransportTools(t *testing.T) {
	e := newTestEnv(t)
	readToken := newToken(t, auth_model.AccessTokenScopeReadIssue)
	writeToken := newToken(t, auth_model.AccessTokenScopeWriteIssue)
	repoToken := newToken(t, auth_model.AccessTokenScopeReadRepository)

	t.Run("list follows scope and the write gate", func(t *testing.T) {
		readOnly := []string{"bad_args", "big", "boom", "echo_read", "leaky", "tool_err"}
		assert.Equal(t, readOnly, toolNames(t, e.call(readToken, "tools/list", nil)))
		assert.Equal(t, readOnly, toolNames(t, e.call(writeToken, "tools/list", nil)))
		assert.Empty(t, toolNames(t, e.call(repoToken, "tools/list", nil)))

		// [mcp] WRITE alone does not open the gate: the lockdown mode must be available too.
		defer test.MockVariableValue(&setting.MCP.Write, true)()
		assert.Equal(t, readOnly, toolNames(t, e.call(writeToken, "tools/list", nil)))

		lockdownReady := false
		RegisterLockdown(func() bool { return lockdownReady })
		assert.Equal(t, readOnly, toolNames(t, e.call(writeToken, "tools/list", nil)))

		lockdownReady = true
		withWrite := []string{"bad_args", "big", "boom", "echo_read", "echo_write", "leaky", "tool_err"}
		assert.Equal(t, withWrite, toolNames(t, e.call(writeToken, "tools/list", nil)))
		// A read-only token still does not see the write tool.
		assert.Equal(t, readOnly, toolNames(t, e.call(readToken, "tools/list", nil)))

		// The gate needs both settings.
		setting.MCP.Write = false
		assert.Equal(t, readOnly, toolNames(t, e.call(writeToken, "tools/list", nil)))
	})

	t.Run("list entries", func(t *testing.T) {
		rec := e.call(readToken, "tools/list", nil)
		result := decode(t, rec)["result"].(map[string]any)
		entry := result["tools"].([]any)[3].(map[string]any)
		assert.Equal(t, "echo_read", entry["name"])
		assert.Equal(t, map[string]any{"type": "object", "additionalProperties": false}, entry["inputSchema"])
		assert.Equal(t, map[string]any{
			"readOnlyHint": true, "destructiveHint": false, "idempotentHint": false, "openWorldHint": false,
		}, entry["annotations"])
	})

	t.Run("list takes no cursor", func(t *testing.T) {
		rec := e.call(readToken, "tools/list", map[string]any{"cursor": "abc"})
		assert.InDelta(t, -32602, rpcErrorCode(t, rec), 0)
	})

	t.Run("call", func(t *testing.T) {
		rec := e.call(readToken, "tools/call", map[string]any{"name": "echo_read", "arguments": map[string]any{}})
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		result := decode(t, rec)["result"].(map[string]any)
		assert.Equal(t, false, result["isError"])
		assert.Equal(t, map[string]any{"user": "user2"}, result["structuredContent"])
		assert.Equal(t, []any{map[string]any{"type": "text", "text": "hello user2"}}, result["content"])

		// Arguments may be left out.
		rec = e.call(readToken, "tools/call", map[string]any{"name": "echo_read"})
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, false, decode(t, rec)["result"].(map[string]any)["isError"])
	})

	t.Run("call refusals", func(t *testing.T) {
		rec := e.call(readToken, "tools/call", map[string]any{"name": "nope"})
		assert.InDelta(t, -32602, rpcErrorCode(t, rec), 0)
		rec = e.call(readToken, "tools/call", map[string]any{"name": "echo_read", "arguments": []any{}})
		assert.InDelta(t, -32602, rpcErrorCode(t, rec), 0)
		rec = e.call(readToken, "tools/call", map[string]any{"arguments": map[string]any{}}, withHeader("Mcp-Name", ""))
		assert.InDelta(t, -32602, rpcErrorCode(t, rec), 0)
		rec = e.call(readToken, "tools/call", nil, withHeader("Mcp-Name", "echo_read"))
		assert.InDelta(t, -32602, rpcErrorCode(t, rec), 0)
	})

	t.Run("scope is checked on the call", func(t *testing.T) {
		rec := e.call(repoToken, "tools/call", map[string]any{"name": "echo_read"})
		assert.Equal(t, http.StatusForbidden, rec.Code)
		assert.Contains(t, rec.Header().Get("WWW-Authenticate"), `error="insufficient_scope", scope="read:issue"`)
	})

	t.Run("write tool while the gate is closed", func(t *testing.T) {
		rec := e.call(writeToken, "tools/call", map[string]any{"name": "echo_write"})
		assert.Equal(t, ErrCodeNotAvailable, toolErrorBodyOf(t, rec)["code"])
	})

	t.Run("write tool while the gate is open", func(t *testing.T) {
		defer test.MockVariableValue(&setting.MCP.Write, true)()
		RegisterLockdown(func() bool { return true })

		rec := e.call(writeToken, "tools/call", map[string]any{"name": "echo_write"})
		assert.Equal(t, false, decode(t, rec)["result"].(map[string]any)["isError"])

		// The token still needs the write scope.
		rec = e.call(readToken, "tools/call", map[string]any{"name": "echo_write"})
		assert.Equal(t, http.StatusForbidden, rec.Code)
		assert.Contains(t, rec.Header().Get("WWW-Authenticate"), `scope="write:issue"`)
	})

	t.Run("handler failures", func(t *testing.T) {
		rec := e.call(readToken, "tools/call", map[string]any{"name": "tool_err"})
		assert.Equal(t, map[string]any{"code": ErrCodeNotFound, "message": "no such pull request"}, toolErrorBodyOf(t, rec))

		rec = e.call(readToken, "tools/call", map[string]any{"name": "bad_args", "arguments": map[string]any{"n": 1}})
		assert.Equal(t, false, decode(t, rec)["result"].(map[string]any)["isError"])
		rec = e.call(readToken, "tools/call", map[string]any{"name": "bad_args", "arguments": map[string]any{"m": 1}})
		assert.InDelta(t, -32602, rpcErrorCode(t, rec), 0)

		// A panic and an unexpected error give a generic answer: no detail reaches the client.
		for _, name := range []string{"boom", "leaky"} {
			rec = e.call(readToken, "tools/call", map[string]any{"name": name})
			assert.Equal(t, ErrCodeInternal, toolErrorBodyOf(t, rec)["code"], name)
			assert.NotContains(t, rec.Body.String(), "hunter2")
			assert.NotContains(t, rec.Body.String(), "secret-panic-detail")
			assert.NotContains(t, rec.Body.String(), "10.0.0.5")
		}
	})

	t.Run("result limit", func(t *testing.T) {
		rec := e.call(readToken, "tools/call", map[string]any{"name": "big"})
		assert.Less(t, rec.Body.Len(), 1024)
		assert.Equal(t, ErrCodeBudget, toolErrorBodyOf(t, rec)["code"])
	})

	t.Run("in-flight limit", func(t *testing.T) {
		e.srv.inflight.n = map[int64]int{2: MaxInflightPerPrincipal}
		rec := e.call(readToken, "tools/call", map[string]any{"name": "echo_read"})
		body := toolErrorBodyOf(t, rec)
		assert.Equal(t, ErrCodeRateLimited, body["code"])
		assert.InDelta(t, rateRetryAfter, body["retry_after_ms"], 0)

		e.srv.inflight.n = nil
		rec = e.call(readToken, "tools/call", map[string]any{"name": "echo_read"})
		assert.Equal(t, false, decode(t, rec)["result"].(map[string]any)["isError"])
		// A call gives its slot back.
		assert.Empty(t, e.srv.inflight.n)
	})
}

func TestMcpTransportRegistry(t *testing.T) {
	good := Tool{
		Name: "pr_read", Description: "d", InputSchema: stdjson.RawMessage(objectSchema),
		Level: auth_model.Read, Categories: []auth_model.AccessTokenScopeCategory{auth_model.AccessTokenScopeCategoryIssue},
		Annotations: Annotations{ReadOnlyHint: true},
		Handler:     func(*Call) (*ToolResult, error) { return &ToolResult{}, nil },
	}
	require.NoError(t, newRegistry().add(good))

	mutate := func(f func(*Tool)) Tool {
		tool := good
		f(&tool)
		return tool
	}
	for name, tool := range map[string]Tool{
		"empty name":        mutate(func(t *Tool) { t.Name = "" }),
		"upper case":        mutate(func(t *Tool) { t.Name = "PrRead" }),
		"leading underline": mutate(func(t *Tool) { t.Name = "_pr" }),
		"space":             mutate(func(t *Tool) { t.Name = "pr read" }),
		"long name":         mutate(func(t *Tool) { t.Name = strings.Repeat("a", maxToolNameLen+1) }),
		"no description":    mutate(func(t *Tool) { t.Description = "" }),
		"no handler":        mutate(func(t *Tool) { t.Handler = nil }),
		"no level":          mutate(func(t *Tool) { t.Level = auth_model.NoAccess }),
		"no category":       mutate(func(t *Tool) { t.Categories = nil }),
		"write and R":       mutate(func(t *Tool) { t.Write = true; t.Level = auth_model.Write }),
		"read and not R":    mutate(func(t *Tool) { t.Annotations.ReadOnlyHint = false }),
		"write with read":   mutate(func(t *Tool) { t.Write = true; t.Annotations.ReadOnlyHint = false }),
		"schema not object": mutate(func(t *Tool) { t.InputSchema = stdjson.RawMessage(`{"type":"string"}`) }),
		"schema not JSON":   mutate(func(t *Tool) { t.InputSchema = stdjson.RawMessage(`nope`) }),
		"no schema":         mutate(func(t *Tool) { t.InputSchema = nil }),
		"output not object": mutate(func(t *Tool) { t.OutputSchema = stdjson.RawMessage(`[]`) }),
	} {
		require.Error(t, newRegistry().add(tool), name)
	}

	reg := newRegistry()
	require.NoError(t, reg.add(good))
	require.Error(t, reg.add(good), "duplicate")

	assert.Panics(t, func() { Register(Tool{Name: "Bad"}) })
}

func TestMcpTransportInflight(t *testing.T) {
	var f inflight
	for range MaxInflightPerPrincipal {
		require.True(t, f.acquire(5))
	}
	assert.False(t, f.acquire(5))
	assert.True(t, f.acquire(6), "another user is not affected")
	f.release(5)
	assert.True(t, f.acquire(5))
	for range MaxInflightPerPrincipal {
		f.release(5)
	}
	f.release(6)
	assert.Empty(t, f.n)
}
