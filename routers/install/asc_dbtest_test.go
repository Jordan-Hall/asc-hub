// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package install

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/modules/translation"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscDbFailureNamesTheField(t *testing.T) {
	cases := []struct {
		err, dbType, field, hint string
	}{
		{`pq: password authentication failed for user "asc"`, "postgres", "db_passwd", "asc_install.db_hint_passwd"},
		{`Error 1045 (28000): Access denied for user 'asc'@'10.0.0.2'`, "mysql", "db_passwd", "asc_install.db_hint_passwd"},
		{`pq: role "nobody" does not exist`, "postgres", "db_user", "asc_install.db_hint_user"},
		{`pq: database "hub" does not exist`, "postgres", "db_name", "asc_install.db_hint_name"},
		{`Error 1049 (42000): Unknown database 'hub'`, "mysql", "db_name", "asc_install.db_hint_name"},
		{`dial tcp 127.0.0.1:1: connect: connection refused`, "postgres", "db_host", "asc_install.db_hint_host"},
		{`dial tcp: lookup db.example.org: no such host`, "mysql", "db_host", "asc_install.db_hint_host"},
		{`unable to open database file: permission denied`, "sqlite3", "db_path", "asc_install.db_hint_path"},
		{`pq: SSL is not enabled on the server`, "postgres", "", "asc_install.db_hint_other"},
	}
	for _, c := range cases {
		field, hint := ascDbFailure(errors.New(c.err), c.dbType)
		assert.Equal(t, c.field, field, c.err)
		assert.Equal(t, c.hint, hint, c.err)
	}
	field, hint := ascDbFailure(nil, "postgres")
	assert.Empty(t, field)
	assert.Empty(t, hint)
	assert.Equal(t, "/data/asc.db", ascDbTarget("sqlite3", "db:5432", "/data/asc.db"))
	assert.Equal(t, "db:5432", ascDbTarget("postgres", "db:5432", "/data/asc.db"))
}

func postTestDb(t *testing.T, r http.Handler, form url.Values, header bool) (*httptest.ResponseRecorder, ascDbTestResult) {
	t.Helper()
	req := httptest.NewRequest("POST", "/test-db", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if header {
		req.Header.Set(ascDbTestHeader, "1")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var res ascDbTestResult
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res), w.Body.String())
	return w, res
}

func TestAscTestDatabase(t *testing.T) {
	translation.InitLocales(context.Background())
	r := Routes()
	saved := setting.Database

	t.Run("needs the install header", func(t *testing.T) {
		w, _ := postTestDb(t, r, url.Values{"db_type": {"sqlite3"}}, false)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("refuses an unknown database type", func(t *testing.T) {
		w, res := postTestDb(t, r, url.Values{"db_type": {"oracle"}}, true)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Equal(t, "db_type", res.Field)
	})

	t.Run("an empty SQLite path names the path", func(t *testing.T) {
		w, res := postTestDb(t, r, url.Values{"db_type": {"sqlite3"}, "db_path": {""}}, true)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.False(t, res.OK)
		assert.Equal(t, "db_path", res.Field)
	})

	t.Run("SQLite connects and says the version", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "asc.db")
		w, res := postTestDb(t, r, url.Values{"db_type": {"sqlite3"}, "db_path": {path}}, true)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.True(t, res.OK, res.Message)
		assert.NotEmpty(t, res.Version)
		assert.Equal(t, "Connected to SQLite3 "+res.Version, res.Message)
	})

	t.Run("a refused server names the host", func(t *testing.T) {
		w, res := postTestDb(t, r, url.Values{"db_type": {"postgres"}, "db_host": {"127.0.0.1:1"}, "db_user": {"asc"}, "db_name": {"hub"}, "ssl_mode": {"disable"}}, true)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.False(t, res.OK)
		assert.Equal(t, "Connection failed.", res.Head)
		assert.Contains(t, res.Message, "ASC Hub could not connect to 127.0.0.1:1:")
		assert.Contains(t, res.Message, "Nothing was changed.")
		assert.Equal(t, "db_host", res.Field)
		assert.Equal(t, "Check the host and port, and that the database server is running.", res.Hint)
	})

	t.Run("a finished install refuses tests", func(t *testing.T) {
		defer test.MockVariableValue(&setting.InstallLock, true)()
		w, _ := postTestDb(t, r, url.Values{"db_type": {"sqlite3"}}, true)
		assert.Equal(t, http.StatusConflict, w.Code)
	})

	// a test never leaves its settings behind
	assert.Equal(t, saved, setting.Database)
}

func TestAscInstallPageHooks(t *testing.T) {
	translation.InitLocales(context.Background())
	r := Routes()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	for _, want := range []string{
		`data-ins-db-test data-url="/test-db"`,
		`data-ins-db-error tabindex="-1" role="alert" hidden`,
		`data-ins-sqlite>Use SQLite instead</button>`,
		`data-ins-step-for="4"`,
		`4. Review`,
		`data-ins-sum-db>`,
		`data-ins-sum-reg>`,
		`Install ASC Hub</button>`,
		`Takes about 20 seconds. Do not close this tab.`,
		`data-label="First run · step {n} of {total}"`,
		`data-next-review="Next: review"`,
		`id="ins-db" data-ins-step="1"`,
		`id="ins-admin" data-ins-step="3"`,
	} {
		assert.Contains(t, body, want)
	}
}

func TestAscInstallDatabaseErrorCard(t *testing.T) {
	translation.InitLocales(context.Background())
	defer test.MockVariableValue(&setting.Database, setting.Database)()
	r := Routes()
	form := url.Values{
		"db_type": {"postgres"}, "db_host": {"127.0.0.1:1"}, "db_user": {"asc"}, "db_name": {"hub"}, "ssl_mode": {"disable"},
		"app_name": {"ASC Hub"}, "repo_root_path": {t.TempDir()}, "run_user": {setting.RunUser}, "domain": {"hub.example.org"},
		"http_port": {"3000"}, "app_url": {"https://hub.example.org/"}, "log_root_path": {t.TempDir()}, "password_algorithm": {"pbkdf2"},
	}
	req := httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()

	card := body[strings.Index(body, "data-ins-db-error "):]
	card = card[:strings.Index(card, "</div>")]
	assert.Contains(t, card, `role="alert">`)
	assert.NotContains(t, card, "hidden")
	assert.Contains(t, card, "Connection failed.")
	assert.Contains(t, card, "ASC Hub could not connect to 127.0.0.1:1:")
	assert.NotContains(t, body, `id="flash-message"`, "the error shows once, in the card")
	assert.Contains(t, body, `data-ins-db-error-actions>`)
	assert.Contains(t, body, `class="ds-input is-err" id="db_host" name="db_host" type="text" value="127.0.0.1:1"`)
	assert.Contains(t, body, "Check the host and port, and that the database server is running.")
	assert.Contains(t, body, `class="ds-input ds-input-pr" id="db_passwd"`, "only the field the error names is marked")
}

func TestAscPostInstallPage(t *testing.T) {
	translation.InitLocales(context.Background())
	r := Routes()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/post-install", nil))
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, `data-ins-poll="/user/login"`)
	assert.Contains(t, body, "ASC Hub is ready")
	assert.Contains(t, body, "Create the first account: it owns the hub. Next, connect the command line.")
	assert.Contains(t, body, "asc auth login --host")
	assert.Contains(t, body, ">Open the hub</a>")
	assert.Contains(t, body, ">Download ASC</a>")
	assert.Contains(t, body, `data-ins-wait hidden`)
}
