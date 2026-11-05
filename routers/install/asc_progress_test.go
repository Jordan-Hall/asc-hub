// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package install

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/modules/translation"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func installStatusOf(t *testing.T, r http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/-/install/status", nil))
	require.Equal(t, http.StatusOK, w.Code)
	return w
}

func TestAscInstallStatus(t *testing.T) {
	defer ascInstallDone.Store(0)
	r := Routes()

	ascInstallDone.Store(2)
	w := installStatusOf(t, r)
	assert.JSONEq(t, `{"done": 2, "total": 5}`, w.Body.String())
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	assert.Empty(t, w.Header().Values("Set-Cookie"), "the status route keeps out of the session of the install")
}

func TestAscInstallStatusStartsOver(t *testing.T) {
	// a new submit starts the count again: a refused form shows no step done
	translation.InitLocales(context.Background())
	defer test.MockVariableValue(&setting.Database, setting.Database)()
	defer ascInstallDone.Store(0)
	r := Routes()
	ascInstallDone.Store(3)
	form := url.Values{
		"db_type": {"postgres"}, "db_host": {"127.0.0.1:1"}, "db_user": {"asc"}, "db_name": {"hub"}, "ssl_mode": {"disable"},
		"app_name": {"ASC Hub"}, "repo_root_path": {t.TempDir()}, "run_user": {setting.RunUser}, "domain": {"hub.example.org"},
		"http_port": {"3000"}, "app_url": {"https://hub.example.org/"}, "log_root_path": {t.TempDir()}, "password_algorithm": {"pbkdf2"},
	}
	req := httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(httptest.NewRecorder(), req)
	assert.JSONEq(t, `{"done": 0, "total": 5}`, installStatusOf(t, r).Body.String())
}

func TestAscInstallProgressMarkup(t *testing.T) {
	translation.InitLocales(context.Background())
	r := Routes()
	steps := []string{"Database tables created", "Repository root checked", "Creating the administrator account", "Starting the merge queue", "Opening your hub"}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	body := w.Body.String()
	assert.Contains(t, body, `data-ins-running data-url="/-/install/status" aria-live="polite" aria-labelledby="ins-run-h" hidden>`)
	assert.Contains(t, body, `data-ins-run data-done="0" data-label="Step {n} of {total}"`)
	assert.Contains(t, body, `data-t-now="(in progress)" data-t-todo="(waiting)"`)
	for _, s := range steps {
		assert.Contains(t, body, "<span>"+s+"</span>")
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/post-install", nil))
	body = w.Body.String()
	wait := body[strings.Index(body, "data-ins-wait hidden"):strings.Index(body, "data-ins-ready")]
	assert.Contains(t, wait, ">Setting up your hub</h1>")
	assert.Contains(t, wait, `data-ins-run data-done="3"`)
	assert.Equal(t, 5, strings.Count(wait, "data-ins-run-step"))
}
