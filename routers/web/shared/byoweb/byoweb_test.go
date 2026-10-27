// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package byoweb

import (
	context2 "context"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"

	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/base"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/test"
	"forgejo.org/modules/translation"
	svc "forgejo.org/services/ascbyo"
	"forgejo.org/services/ascbyo/ascbyotest"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	unittest.MainTest(m)
}

var localeOnce sync.Once

// locale is the real English translation, so a missing key or a wrong argument list fails a test.
func locale(t *testing.T) translation.Locale {
	t.Helper()
	localeOnce.Do(func() {
		if len(setting.Langs) == 0 {
			setting.Langs, setting.Names = []string{"en-US"}, []string{"English"}
		}
		translation.InitLocales(context2.Background())
	})
	return translation.NewLocale("en-US")
}

// tr is the translated text as a template prints it.
func tr(t *testing.T, key string, args ...any) string {
	t.Helper()
	return string(locale(t).Tr(key, args...))
}

const (
	pageStorage base.TplName = "shared/asc/byo_storage_page"
	pageAI      base.TplName = "shared/asc/byo_ai_page"
)

func mustScope(t *testing.T, k svc.Kind, name string) svc.Scope {
	t.Helper()
	sc, err := svc.NewScope(k, name)
	require.NoError(t, err)
	return sc
}

func userLevel(t *testing.T) *Level {
	return &Level{
		Scope: mustScope(t, svc.KindUser, "alice"), Base: "/user/settings/asc", ScopeLabel: "alice",
		StorageTpl: pageStorage, AITpl: pageAI,
	}
}

func repoLevel(t *testing.T, withOwner bool) *Level {
	l := &Level{
		Scope: mustScope(t, svc.KindRepo, "alice/widgets"), Base: "/alice/widgets/settings/asc", ScopeLabel: "alice/widgets",
		StorageTpl: pageStorage, AITpl: pageAI,
	}
	l.Allowed = []svc.Scope{l.Scope}
	if withOwner {
		l.Allowed = append(l.Allowed, mustScope(t, svc.KindUser, "alice"))
		l.OwnerStorageURL = "/user/settings/asc/storage"
	}
	return l
}

// setup turns [asc] on and starts the fake.
func setup(t *testing.T) *ascbyotest.Fake {
	t.Helper()
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	return ascbyotest.Start(t)
}

type result struct {
	ctx  *context.Context
	resp *httptest.ResponseRecorder
}

func (r result) body() string { return r.resp.Body.String() }
func (r result) loc() string  { return r.resp.Header().Get("Location") }

// run calls fn like the router would: a signed-in alice, the form values, and the {id} of the route.
func run(t *testing.T, fn func(*context.Context, *Level), lvl *Level, method string, form url.Values, id string) result {
	t.Helper()
	path := "/x"
	if lvl != nil {
		path = lvl.Base + "/x"
	}
	ctx, resp := contexttest.MockContext(t, method+" "+path)
	ctx.Locale = locale(t)
	ctx.TemplateContext = context.NewTemplateContextForWeb(ctx)
	ctx.Render = templates.HTMLRenderer()
	ctx.Doer = &user_model.User{ID: 2, Name: "alice", LowerName: "alice"}
	ctx.IsSigned = true
	ctx.Data["CsrfTokenHtml"] = template.HTML(`<input type="hidden" name="_csrf" value="csrf-token">`)
	if method == http.MethodPost {
		if form == nil {
			form = url.Values{}
		}
		ctx.Req.PostForm = form
		ctx.Req.Form = form
	}
	if id != "" {
		ctx.SetParams("id", id)
	}
	fn(ctx, lvl)
	// a key that is missing from the locale file would be printed as itself
	assert.NotRegexp(t, `ascbyo\.[a-z_]+(\.[a-z_0-9]+)+`, resp.Body.String(), "an untranslated key is on the page")
	return result{ctx, resp}
}

func addForm() url.Values {
	return url.Values{
		"name": {"primary"}, "endpoint": {"https://s3.example.com"}, "region": {"eu"}, "bucket": {"my-bucket"}, "prefix": {"asc"},
		"addressing": {"auto"}, "access_key_id": {"AKIAIOSFODNN7EXAMPLE"}, "secret_access_key": {"wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"},
		"session_token": {"FwoGZXIvYXdzEXAMPLETOKENVALUE"}, "op_id": {ascbyotest.OpIDForTest},
	}
}

var secretsInAddForm = []string{"wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "FwoGZXIvYXdzEXAMPLETOKENVALUE", "AKIAIOSFODNN7EXAMPLE"}

func assertNoSecrets(t *testing.T, body string, extra ...string) {
	t.Helper()
	for _, s := range append(secretsInAddForm, extra...) {
		assert.NotContains(t, body, s)
	}
}

func TestStorageIndexRenders(t *testing.T) {
	f := setup(t)
	alice := userLevel(t).Scope.Owner()
	pending := f.AddTarget(alice, "primary", ascv1.StorageTargetState_STORAGE_TARGET_STATE_PENDING)
	enabled := f.AddTarget(alice, "backup", ascv1.StorageTargetState_STORAGE_TARGET_STATE_ENABLED)
	f.AddTarget(mustScope(t, svc.KindUser, "bob").Owner(), "bobs-secret-target", ascv1.StorageTargetState_STORAGE_TARGET_STATE_ENABLED)

	r := run(t, StorageIndex, userLevel(t), http.MethodGet, nil, "")
	require.Equal(t, http.StatusOK, r.resp.Code)
	body := r.body()
	assert.Equal(t, "no-store", r.resp.Header().Get("Cache-Control"))
	assert.Contains(t, body, `data-testid="asc-byo-storage"`)
	assert.Contains(t, body, "primary")
	assert.Contains(t, body, "backup")
	assert.NotContains(t, body, "bobs-secret-target", "another owner's target is never listed")
	assert.Contains(t, body, tr(t, "ascbyo.storage.state.pending"))
	assert.Contains(t, body, tr(t, "ascbyo.storage.state.enabled"))
	assert.Contains(t, body, "/user/settings/asc/storage/targets/"+pending.GetId())
	assert.Contains(t, body, "/user/settings/asc/storage/targets/"+enabled.GetId()+"/disable")
	assert.Contains(t, body, "/user/settings/asc/storage/targets/"+pending.GetId()+"/verify")
	assert.Contains(t, body, `name="_csrf"`, "every form carries the CSRF token")
	assert.Equal(t, strings.Count(body, "<form"), strings.Count(body, `name="_csrf"`), "…one per form")
	assert.Contains(t, body, "AKIA...WXYZ", "the hint is shown")
	assertNoSecrets(t, body, "AKIAPLANTEDKEYWXYZ", "planted-secret-primary", "planted-secret-backup")
	// the credential inputs are password fields and never prefilled
	for _, name := range []string{"access_key_id", "secret_access_key", "session_token"} {
		re := regexp.MustCompile(`<input[^>]*name="` + name + `"[^>]*>`)
		m := re.FindString(body)
		require.NotEmpty(t, m, name)
		assert.Contains(t, m, `type="password"`, name)
		assert.Contains(t, m, `value=""`, name)
		assert.Contains(t, m, `autocomplete="new-password"`, name)
	}
	assert.NotContains(t, body, `name="op_id" value=""`)
}

func TestStorageIndexEscapesText(t *testing.T) {
	f := setup(t)
	alice := userLevel(t).Scope.Owner()
	tg := f.AddTarget(alice, "<script>alert(1)</script>", ascv1.StorageTargetState_STORAGE_TARGET_STATE_FAILED)
	f.SetTargetFields(tg.GetId(), func(t *ascv1.StorageTarget) {
		t.Endpoint = `https://"><img src=x onerror=alert(2)>.example.com`
		t.LastError = "<b>bad</b>\x1b[31m"
	})
	r := run(t, StorageIndex, userLevel(t), http.MethodGet, nil, "")
	require.Equal(t, http.StatusOK, r.resp.Code)
	body := r.body()
	assert.NotContains(t, body, "<script>alert(1)</script>")
	assert.NotContains(t, body, "<img src=x")
	assert.NotContains(t, body, "<b>bad</b>")
	assert.NotContains(t, body, "\x1b")
	assert.Contains(t, body, "&lt;script&gt;alert(1)&lt;/script&gt;")
}

func TestStorageCreate(t *testing.T) {
	defer test.MockVariableValue(&setting.SecretKey, "byoweb-test-secret")()
	f := setup(t)
	opID := ascbyotest.OpIDForTest

	t.Run("success redirects to the new target and sends the scope from the route", func(t *testing.T) {
		form := addForm()
		form.Set("owner", "mallory") // ignored: the scope is never taken from the request
		form.Set("kind", "org")
		r := run(t, StorageCreate, userLevel(t), http.MethodPost, form, "")
		require.Equal(t, http.StatusSeeOther, r.resp.Code, r.body())
		assert.Regexp(t, `^/user/settings/asc/storage/targets/st_[A-Za-z0-9]{20}$`, r.loc())
		assert.NotEmpty(t, r.ctx.Flash.SuccessMsg)
		calls := f.CallsTo("CreateTarget")
		require.Len(t, calls, 1)
		req := calls[0].Req.(*ascv1.CreateStorageTargetRequest)
		assert.Equal(t, "alice", req.GetOwner().GetName())
		assert.Equal(t, ascv1.OwnerKind_OWNER_KIND_USER, req.GetOwner().GetKind())
		// The op id sent to asc is derived by HMAC from the user, the scope, the method and the form's id: a
		// valid UUID that is not the one the form carried.
		sent, err := uuid.Parse(calls[0].OpID)
		require.NoError(t, err, calls[0].OpID)
		assert.NotEqual(t, opID, calls[0].OpID)
		assert.NotEqual(t, uuid.Nil, sent)
		assert.Equal(t, "asc/", req.GetPrefix())
		assert.NotContains(t, r.loc()+r.ctx.Flash.SuccessMsg, "wJalr")
	})

	t.Run("invalid input is shown again without any secret", func(t *testing.T) {
		before := len(f.CallsTo("CreateTarget"))
		form := addForm()
		form.Set("name", "Bad Name")
		form.Set("endpoint", "ftp://nope")
		form.Set("bucket", "A")
		r := run(t, StorageCreate, userLevel(t), http.MethodPost, form, "")
		require.Equal(t, http.StatusUnprocessableEntity, r.resp.Code)
		body := r.body()
		assert.Contains(t, body, tr(t, "ascbyo.storage.err.name"))
		assert.Contains(t, body, tr(t, "ascbyo.storage.err.endpoint"))
		assert.Contains(t, body, tr(t, "ascbyo.storage.err.bucket"))
		assert.Contains(t, body, `aria-invalid="true"`)
		assert.Contains(t, body, `role="alert"`)
		assert.Contains(t, body, `value="bad name"`, "non-secret values are kept")
		assertNoSecrets(t, body)
		assert.Len(t, f.CallsTo("CreateTarget"), before, "nothing reached asc")
		assert.Equal(t, "no-store", r.resp.Header().Get("Cache-Control"))
	})

	t.Run("an endpoint asc blocks shows the reason", func(t *testing.T) {
		form := addForm()
		form.Set("name", "other")
		form.Set("endpoint", "http://s3.example.com")
		r := run(t, StorageCreate, userLevel(t), http.MethodPost, form, "")
		require.Equal(t, http.StatusUnprocessableEntity, r.resp.Code)
		assert.Contains(t, r.body(), tr(t, "ascbyo.error.endpoint_blocked"))
		assert.Contains(t, r.body(), tr(t, "ascbyo.blocked.not_https"))
		assertNoSecrets(t, r.body())
	})

	t.Run("a name that exists is a conflict", func(t *testing.T) {
		r := run(t, StorageCreate, userLevel(t), http.MethodPost, addForm(), "")
		require.Equal(t, http.StatusConflict, r.resp.Code)
		assert.Contains(t, r.body(), tr(t, "ascbyo.error.exists"))
		assertNoSecrets(t, r.body())
	})

	t.Run("an oversized value is refused, not processed", func(t *testing.T) {
		form := addForm()
		form.Set("name", "huge")
		form.Set("secret_access_key", strings.Repeat("s", 1<<20))
		r := run(t, StorageCreate, userLevel(t), http.MethodPost, form, "")
		require.Equal(t, http.StatusUnprocessableEntity, r.resp.Code)
		assert.Contains(t, r.body(), tr(t, "ascbyo.storage.err.secret_access_key"))
		assert.NotContains(t, r.body(), strings.Repeat("s", 100))
	})

	t.Run("an unknown addressing is refused before asc is called", func(t *testing.T) {
		before := len(f.CallsTo("CreateTarget"))
		form := addForm()
		form.Set("name", "weird")
		form.Set("addressing", "teleport")
		r := run(t, StorageCreate, userLevel(t), http.MethodPost, form, "")
		require.Equal(t, http.StatusUnprocessableEntity, r.resp.Code)
		assert.Contains(t, r.body(), tr(t, "ascbyo.storage.err.addressing"))
		assert.Len(t, f.CallsTo("CreateTarget"), before)
	})
}

func TestStorageTargetPagesAndOwnership(t *testing.T) {
	f := setup(t)
	alice := userLevel(t).Scope.Owner()
	bob := mustScope(t, svc.KindUser, "bob").Owner()
	mine := f.AddTarget(alice, "mine", ascv1.StorageTargetState_STORAGE_TARGET_STATE_VERIFIED)
	theirs := f.AddTarget(bob, "theirs", ascv1.StorageTargetState_STORAGE_TARGET_STATE_VERIFIED)

	t.Run("details of my target", func(t *testing.T) {
		r := run(t, StorageTarget, userLevel(t), http.MethodGet, nil, mine.GetId())
		require.Equal(t, http.StatusOK, r.resp.Code)
		body := r.body()
		assert.Contains(t, body, `data-testid="asc-byo-storage-target"`)
		assert.Contains(t, body, tr(t, "ascbyo.storage.state_help.verified"))
		assert.Contains(t, body, `value="https://s3.example.com"`, "the edit form is prefilled")
		assert.Contains(t, body, "/enable")
		assertNoSecrets(t, body, "AKIAPLANTEDKEYWXYZ", "planted-secret-mine")
	})

	mutations := f.Mutations()
	for name, fn := range map[string]func(*context.Context, *Level){
		"get":     StorageTarget,
		"update":  StorageUpdate,
		"verify":  StorageVerify,
		"delete":  StorageDelete,
		"enable":  func(c *context.Context, l *Level) { StorageEnable(c, l, true) },
		"disable": func(c *context.Context, l *Level) { StorageEnable(c, l, false) },
	} {
		t.Run("another owner's target, "+name, func(t *testing.T) {
			method := http.MethodPost
			if name == "get" {
				method = http.MethodGet
			}
			form := addForm()
			form.Set("confirm", "on")
			r := run(t, fn, userLevel(t), method, form, theirs.GetId())
			assert.Equal(t, http.StatusNotFound, r.resp.Code)
			assert.NotContains(t, r.body(), "theirs")
		})
	}
	assert.Equal(t, mutations, f.Mutations(), "no mutating call for another owner's target")

	t.Run("a malformed id is a 404 before any call", func(t *testing.T) {
		calls := len(f.Calls())
		for _, id := range []string{"x", "st_short", "../../etc/passwd", "st_abcdefghijklmnopqrst; drop"} {
			r := run(t, StorageTarget, userLevel(t), http.MethodGet, nil, id)
			assert.Equal(t, http.StatusNotFound, r.resp.Code, id)
		}
		assert.Len(t, f.Calls(), calls)
	})
}

func TestStorageLifecycleActions(t *testing.T) {
	f := setup(t)
	alice := userLevel(t).Scope.Owner()
	tg := f.AddTarget(alice, "life", ascv1.StorageTargetState_STORAGE_TARGET_STATE_PENDING)
	id := tg.GetId()

	t.Run("verify shows every step", func(t *testing.T) {
		r := run(t, StorageVerify, userLevel(t), http.MethodPost, nil, id)
		require.Equal(t, http.StatusOK, r.resp.Code)
		body := r.body()
		assert.Contains(t, body, `data-testid="asc-verify-result"`)
		assert.Contains(t, body, tr(t, "ascbyo.storage.detail.verify_ok"))
		for _, step := range []string{"resolve", "connect", "put", "get", "delete"} {
			assert.Contains(t, body, tr(t, "ascbyo.storage.step."+step))
		}
	})
	t.Run("a failed verification shows the failing step", func(t *testing.T) {
		f.VerifyBad = true
		defer func() { f.VerifyBad = false }()
		r := run(t, StorageVerify, userLevel(t), http.MethodPost, nil, id)
		require.Equal(t, http.StatusOK, r.resp.Code)
		assert.Contains(t, r.body(), tr(t, "ascbyo.storage.detail.verify_failed"))
		assert.Contains(t, r.body(), tr(t, "ascbyo.storage.detail.step_failed"))
		assert.NotContains(t, r.body(), "\x1b")
		// verification passes again afterwards
		f.VerifyBad = false
		r = run(t, StorageVerify, userLevel(t), http.MethodPost, nil, id)
		assert.Contains(t, r.body(), tr(t, "ascbyo.storage.detail.verify_ok"))
	})
	t.Run("enable and disable redirect to the page the form came from", func(t *testing.T) {
		r := run(t, func(c *context.Context, l *Level) { StorageEnable(c, l, true) }, userLevel(t), http.MethodPost, url.Values{"return": {"list"}}, id)
		require.Equal(t, http.StatusSeeOther, r.resp.Code, r.body())
		assert.Equal(t, "/user/settings/asc/storage", r.loc())
		r = run(t, func(c *context.Context, l *Level) { StorageEnable(c, l, false) }, userLevel(t), http.MethodPost, nil, id)
		assert.Equal(t, "/user/settings/asc/storage/targets/"+id, r.loc())
		// the return field cannot name another site
		r = run(t, func(c *context.Context, l *Level) { StorageEnable(c, l, true) }, userLevel(t), http.MethodPost, url.Values{"return": {"https://evil.example/"}}, id)
		assert.Equal(t, "/user/settings/asc/storage/targets/"+id, r.loc())
	})
	t.Run("enable of an unverified target says why", func(t *testing.T) {
		other := f.AddTarget(alice, "unverified", ascv1.StorageTargetState_STORAGE_TARGET_STATE_PENDING)
		r := run(t, func(c *context.Context, l *Level) { StorageEnable(c, l, true) }, userLevel(t), http.MethodPost, nil, other.GetId())
		require.Equal(t, http.StatusUnprocessableEntity, r.resp.Code)
		assert.Contains(t, r.body(), tr(t, "ascbyo.error.verify_failed"))
	})
	t.Run("update keeps the stored credentials when none are typed", func(t *testing.T) {
		form := url.Values{"name": {"life"}, "endpoint": {"https://s3.example.com"}, "region": {"eu"}, "bucket": {"bucket-life"}, "prefix": {""}, "addressing": {"auto"}}
		r := run(t, StorageUpdate, userLevel(t), http.MethodPost, form, id)
		require.Equal(t, http.StatusSeeOther, r.resp.Code, r.body())
		assert.Equal(t, "/user/settings/asc/storage/targets/"+id, r.loc())
		assert.Equal(t, tr(t, "ascbyo.flash.nothing_changed"), r.ctx.Flash.InfoMsg)
		assert.Empty(t, f.CallsTo("UpdateTarget"), "identical values send nothing")

		// A new name is not a new place: the stored credentials stay.
		form.Set("prefix", "other/")
		r = run(t, StorageUpdate, userLevel(t), http.MethodPost, form, id)
		require.Equal(t, http.StatusSeeOther, r.resp.Code, r.body())
		req := f.CallsTo("UpdateTarget")[0].Req.(*ascv1.UpdateStorageTargetRequest)
		assert.Equal(t, "other/", req.GetPrefix())
		assert.Nil(t, req.Credentials)
	})
	t.Run("a new place without new credentials is refused", func(t *testing.T) {
		form := url.Values{"name": {"life"}, "endpoint": {"https://s3.example.com"}, "region": {"eu"}, "bucket": {"new-bucket-name"}, "prefix": {"other/"}, "addressing": {"auto"}}
		before := len(f.CallsTo("UpdateTarget"))
		r := run(t, StorageUpdate, userLevel(t), http.MethodPost, form, id)
		require.Equal(t, http.StatusUnprocessableEntity, r.resp.Code)
		assert.Contains(t, r.body(), tr(t, "ascbyo.storage.err.access_key_id_reenter"))
		assert.Contains(t, r.body(), tr(t, "ascbyo.storage.err.secret_access_key_reenter"))
		assert.Len(t, f.CallsTo("UpdateTarget"), before)
	})
	t.Run("update with a half-typed credential pair is refused", func(t *testing.T) {
		form := url.Values{"name": {"life"}, "endpoint": {"https://s3.example.com"}, "bucket": {"new-bucket-name"}, "access_key_id": {"AKIAONLYIDTYPED"}}
		before := len(f.CallsTo("UpdateTarget"))
		r := run(t, StorageUpdate, userLevel(t), http.MethodPost, form, id)
		require.Equal(t, http.StatusUnprocessableEntity, r.resp.Code)
		assert.Contains(t, r.body(), tr(t, "ascbyo.storage.err.secret_access_key"))
		assert.NotContains(t, r.body(), "AKIAONLYIDTYPED")
		assert.Len(t, f.CallsTo("UpdateTarget"), before)
	})
	t.Run("delete needs the confirmation", func(t *testing.T) {
		r := run(t, StorageDelete, userLevel(t), http.MethodPost, nil, id)
		require.Equal(t, http.StatusUnprocessableEntity, r.resp.Code)
		assert.Contains(t, r.body(), tr(t, "ascbyo.error.confirm"))
		assert.Empty(t, f.CallsTo("DeleteTarget"))
		f.InUse = true
		r = run(t, StorageDelete, userLevel(t), http.MethodPost, url.Values{"confirm": {"on"}}, id)
		require.Equal(t, http.StatusConflict, r.resp.Code)
		assert.Contains(t, r.body(), tr(t, "ascbyo.error.in_use"))
		f.InUse = false
		r = run(t, StorageDelete, userLevel(t), http.MethodPost, url.Values{"confirm": {"on"}}, id)
		require.Equal(t, http.StatusSeeOther, r.resp.Code)
		assert.Equal(t, "/user/settings/asc/storage", r.loc())
	})
}

func TestUnavailableAndDisabled(t *testing.T) {
	t.Run("asc is not configured", func(t *testing.T) {
		t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
		t.Cleanup(test.MockVariableValue(&setting.Asc.ServerURL, ""))
		r := run(t, StorageIndex, userLevel(t), http.MethodGet, nil, "")
		assert.Equal(t, http.StatusServiceUnavailable, r.resp.Code)
		assert.Contains(t, r.body(), `data-testid="asc-byo-unavailable"`)
		assert.NotContains(t, r.body(), "asc-add-target", "no forms when nothing can be saved")
		r = run(t, StorageCreate, userLevel(t), http.MethodPost, addForm(), "")
		assert.Equal(t, http.StatusServiceUnavailable, r.resp.Code)
		assertNoSecrets(t, r.body())
	})
	t.Run("asc says UNIMPLEMENTED", func(t *testing.T) {
		f := setup(t)
		f.SetFail("/asc.v1.Storage/ListTargets", ascbyotest.Detail(12, "no", "UNIMPLEMENTED"))
		r := run(t, StorageIndex, userLevel(t), http.MethodGet, nil, "")
		assert.Equal(t, http.StatusServiceUnavailable, r.resp.Code)
		assert.Contains(t, r.body(), `data-testid="asc-byo-unsupported"`)
	})
	t.Run("[asc] is off: every endpoint is a 404", func(t *testing.T) {
		f := ascbyotest.Start(t)
		t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, false))
		id := "st_abcdefghijklmnopqrst"
		for name, fn := range map[string]func(*context.Context, *Level){
			"index": StorageIndex, "target": StorageTarget, "create": StorageCreate, "update": StorageUpdate, "verify": StorageVerify,
			"delete": StorageDelete, "ai": AIIndex, "aikey": AIKey, "aicreate": AICreate, "aidelete": AIDelete, "aitest": AITest,
			"assign": StorageAssign, "migrate": MigrationStart, "status": MigrationStatus,
		} {
			method := http.MethodPost
			if name == "index" || name == "target" || name == "ai" || name == "aikey" || name == "status" {
				method = http.MethodGet
			}
			r := run(t, fn, repoLevel(t, true), method, addForm(), id)
			assert.Equal(t, http.StatusNotFound, r.resp.Code, name)
		}
		assert.Empty(t, f.Calls())
	})
	t.Run("an invalid level is a 404", func(t *testing.T) {
		setup(t)
		r := run(t, StorageIndex, &Level{}, http.MethodGet, nil, "")
		assert.Equal(t, http.StatusNotFound, r.resp.Code)
		r = run(t, StorageIndex, nil, http.MethodGet, nil, "")
		assert.Equal(t, http.StatusNotFound, r.resp.Code)
	})
}

func TestRepoStorage(t *testing.T) {
	f := setup(t)
	repo := repoLevel(t, true)
	owner := mustScope(t, svc.KindUser, "alice")
	own := f.AddTarget(repo.Scope.Owner(), "repo-own", ascv1.StorageTargetState_STORAGE_TARGET_STATE_ENABLED)
	ownersT := f.AddTarget(owner.Owner(), "owners", ascv1.StorageTargetState_STORAGE_TARGET_STATE_ENABLED)
	pending := f.AddTarget(owner.Owner(), "pending", ascv1.StorageTargetState_STORAGE_TARGET_STATE_PENDING)
	foreign := f.AddTarget(mustScope(t, svc.KindUser, "mallory").Owner(), "foreign", ascv1.StorageTargetState_STORAGE_TARGET_STATE_ENABLED)

	t.Run("index shows groups, assignment and migration", func(t *testing.T) {
		r := run(t, StorageIndex, repo, http.MethodGet, nil, "")
		require.Equal(t, http.StatusOK, r.resp.Code)
		body := r.body()
		assert.Contains(t, body, "repo-own")
		assert.Contains(t, body, "owners")
		assert.NotContains(t, body, "foreign")
		assert.Contains(t, body, `data-testid="asc-assign"`)
		assert.Contains(t, body, `data-testid="asc-migrate"`)
		assert.Contains(t, body, tr(t, "ascbyo.storage.repo.history_none"))
		assert.Contains(t, body, "/user/settings/asc/storage", "link to the owner's targets")
		// the owner's targets cannot be edited from the repository page
		assert.NotContains(t, body, "/alice/widgets/settings/asc/storage/targets/"+ownersT.GetId())
		assert.Contains(t, body, "/alice/widgets/settings/asc/storage/targets/"+own.GetId())
		// only enabled targets can be chosen
		assert.Contains(t, body, `value="`+own.GetId()+`"`)
		assert.Contains(t, body, `value="`+ownersT.GetId()+`"`)
		assert.NotContains(t, body, `value="`+pending.GetId()+`"`)
		// every form of the page has its own op id
		ops := regexp.MustCompile(`name="op_id" value="([0-9a-f-]{36})"`).FindAllStringSubmatch(body, -1)
		seen := map[string]bool{}
		for _, m := range ops {
			assert.False(t, seen[m[1]], "op id %s is shared by two forms", m[1])
			seen[m[1]] = true
		}
		assert.GreaterOrEqual(t, len(ops), 3)
	})

	t.Run("assign", func(t *testing.T) {
		r := run(t, StorageAssign, repo, http.MethodPost, url.Values{"target_id": {ownersT.GetId()}}, "")
		require.Equal(t, http.StatusSeeOther, r.resp.Code, r.body())
		assert.Equal(t, "/alice/widgets/settings/asc/storage", r.loc())
		req := f.CallsTo("AssignRepo")[0].Req.(*ascv1.AssignRepoStorageRequest)
		assert.Equal(t, "alice/widgets", req.GetRepo().GetName())
		assert.Equal(t, ownersT.GetId(), req.GetTargetId())

		calls := len(f.CallsTo("AssignRepo"))
		r = run(t, StorageAssign, repo, http.MethodPost, url.Values{"target_id": {foreign.GetId()}}, "")
		assert.Equal(t, http.StatusNotFound, r.resp.Code, "a target of another owner")
		r = run(t, StorageAssign, repo, http.MethodPost, url.Values{"target_id": {pending.GetId()}}, "")
		assert.Equal(t, http.StatusUnprocessableEntity, r.resp.Code, "a target that is not enabled")
		assert.Len(t, f.CallsTo("AssignRepo"), calls)

		// without the owner's rights the owner's targets are not usable
		noOwner := repoLevel(t, false)
		r = run(t, StorageAssign, noOwner, http.MethodPost, url.Values{"target_id": {ownersT.GetId()}}, "")
		assert.Equal(t, http.StatusNotFound, r.resp.Code)
		r = run(t, StorageAssign, noOwner, http.MethodPost, url.Values{"target_id": {own.GetId()}}, "")
		assert.Equal(t, http.StatusSeeOther, r.resp.Code)
	})

	t.Run("migration needs the confirmation to delete the source", func(t *testing.T) {
		r := run(t, MigrationStart, repo, http.MethodPost, url.Values{"target_id": {own.GetId()}, "delete_source": {"on"}}, "")
		require.Equal(t, http.StatusUnprocessableEntity, r.resp.Code)
		assert.Contains(t, r.body(), tr(t, "ascbyo.storage.err.confirm_delete_source"))
		assert.Empty(t, f.CallsTo("StartMigration"))
	})

	var migID string
	t.Run("start, progress, cancel", func(t *testing.T) {
		r := run(t, MigrationStart, repo, http.MethodPost, url.Values{"target_id": {own.GetId()}, "delete_source": {"on"}, "confirm_delete_source": {"on"}}, "")
		require.Equal(t, http.StatusSeeOther, r.resp.Code, r.body())
		req := f.CallsTo("StartMigration")[0].Req.(*ascv1.StartStorageMigrationRequest)
		assert.True(t, req.GetDeleteSource())
		assert.Equal(t, own.GetId(), req.GetToTargetId())
		migs := f.MigrationsOf("alice/widgets")
		require.Len(t, migs, 1)
		migID = migs[0].GetId()

		f.UpdateMigration(migID, func(m *ascv1.StorageMigration) {
			m.State, m.ObjectsTotal, m.ObjectsCopied = ascv1.StorageMigrationState_STORAGE_MIGRATION_STATE_COPYING, 200, 50
		})
		r = run(t, StorageIndex, repo, http.MethodGet, nil, "")
		require.Equal(t, http.StatusOK, r.resp.Code)
		assert.Contains(t, r.body(), `data-testid="asc-migration-active"`)
		assert.Contains(t, r.body(), `value="25"`)
		assert.Contains(t, r.body(), "/alice/widgets/settings/asc/storage/migrations/"+migID+"/status")
		assert.NotContains(t, r.body(), `data-testid="asc-migrate"`, "no second migration while one runs")

		r = run(t, MigrationStatus, repo, http.MethodGet, nil, migID)
		require.Equal(t, http.StatusOK, r.resp.Code)
		var st migrationStatus
		contexttest.DecodeJSON(t, r.resp, &st)
		assert.Equal(t, 25, st.Percent)
		assert.True(t, st.Active)
		assert.Equal(t, uint64(50), st.Copied)
		assert.Equal(t, "no-store", r.resp.Header().Get("Cache-Control"))

		r = run(t, MigrationCancel, repo, http.MethodPost, nil, migID)
		require.Equal(t, http.StatusSeeOther, r.resp.Code, r.body())
		r = run(t, MigrationStatus, repo, http.MethodGet, nil, migID)
		contexttest.DecodeJSON(t, r.resp, &st)
		assert.False(t, st.Active)
	})

	t.Run("a migration of another repository is not reachable", func(t *testing.T) {
		other := f.AddMigration(&ascv1.StorageMigration{Repo: &ascv1.RepoRef{Name: "mallory/secret"}, State: ascv1.StorageMigrationState_STORAGE_MIGRATION_STATE_COPYING})
		r := run(t, MigrationStatus, repo, http.MethodGet, nil, other.GetId())
		assert.Equal(t, http.StatusNotFound, r.resp.Code)
		r = run(t, MigrationCancel, repo, http.MethodPost, nil, other.GetId())
		assert.Equal(t, http.StatusNotFound, r.resp.Code)
		assert.Equal(t, ascv1.StorageMigrationState_STORAGE_MIGRATION_STATE_COPYING, other.GetState())
	})

	t.Run("repository-only endpoints are 404 at the user level", func(t *testing.T) {
		calls := len(f.Calls())
		for name, fn := range map[string]func(*context.Context, *Level){"assign": StorageAssign, "migrate": MigrationStart, "cancel": MigrationCancel, "status": MigrationStatus} {
			r := run(t, fn, userLevel(t), http.MethodPost, url.Values{"target_id": {own.GetId()}}, migID)
			assert.Equal(t, http.StatusNotFound, r.resp.Code, name)
		}
		assert.Len(t, f.Calls(), calls)
	})
}

func TestAIKeysPages(t *testing.T) {
	f := setup(t)
	lvl := userLevel(t)
	secret := "sk-ant-api03-SUPERSECRETKEYVALUE9876"
	form := url.Values{"label": {"Team"}, "provider": {"anthropic"}, "api_key": {secret}, "default_model": {"claude-sonnet-4-5"}, "monthly_cap": {"25"}, "op_id": {ascbyotest.OpIDForTest}}

	r := run(t, AICreate, lvl, http.MethodPost, form, "")
	require.Equal(t, http.StatusSeeOther, r.resp.Code, r.body())
	assert.Regexp(t, `^/user/settings/asc/ai/keys/ak_[A-Za-z0-9]{20}$`, r.loc())
	id := strings.TrimPrefix(r.loc(), "/user/settings/asc/ai/keys/")
	req := f.CallsTo("CreateKey")[0].Req.(*ascv1.CreateAiKeyRequest)
	assert.Equal(t, uint64(25_000_000), req.GetMonthlyCapMicroUsd())
	assert.Equal(t, "alice", req.GetOwner().GetName())
	assert.NotContains(t, r.ctx.Flash.SuccessMsg+r.loc(), "SUPERSECRET")

	t.Run("invalid input is shown again without the key", func(t *testing.T) {
		bad := url.Values{"label": {""}, "provider": {"anthropic"}, "api_key": {secret}, "monthly_cap": {"-1"}, "base_url": {"file:///x"}}
		r := run(t, AICreate, lvl, http.MethodPost, bad, "")
		require.Equal(t, http.StatusUnprocessableEntity, r.resp.Code)
		for _, k := range []string{"ascbyo.ai.err.label", "ascbyo.ai.err.cap", "ascbyo.ai.err.base_url"} {
			assert.Contains(t, r.body(), tr(t, k))
		}
		assert.NotContains(t, r.body(), "SUPERSECRET")
		assert.Len(t, f.CallsTo("CreateKey"), 1)
	})

	t.Run("a blocked address shows the reason", func(t *testing.T) {
		bad := url.Values{"label": {"Local"}, "provider": {"openai_compatible"}, "api_key": {"local-key-0123456789"}, "base_url": {"https://127.0.0.1/v1"}}
		r := run(t, AICreate, lvl, http.MethodPost, bad, "")
		require.Equal(t, http.StatusUnprocessableEntity, r.resp.Code)
		assert.Contains(t, r.body(), tr(t, "ascbyo.blocked.loopback"))
	})

	t.Run("list and details show a hint only", func(t *testing.T) {
		r := run(t, AIIndex, lvl, http.MethodGet, nil, "")
		require.Equal(t, http.StatusOK, r.resp.Code)
		assert.Contains(t, r.body(), "Team")
		assert.Contains(t, r.body(), tr(t, "ascbyo.ai.state.untested"))
		assert.NotContains(t, r.body(), "SUPERSECRET")
		assert.NotContains(t, r.body(), "planted-key")

		r = run(t, AIKey, lvl, http.MethodGet, nil, id)
		require.Equal(t, http.StatusOK, r.resp.Code)
		body := r.body()
		assert.Contains(t, body, `data-testid="asc-byo-ai-key"`)
		assert.NotContains(t, body, "SUPERSECRET")
		apiKeyInput := regexp.MustCompile(`<input[^>]*name="api_key"[^>]*>`).FindString(body)
		assert.Contains(t, apiKeyInput, `type="password"`)
		assert.Contains(t, apiKeyInput, `value=""`)
		assert.NotContains(t, body, `name="provider"`, "the provider of a key cannot be changed")
	})

	t.Run("test, models and usage", func(t *testing.T) {
		f.Models = []*ascv1.AiModel{{Id: "claude-sonnet-4-5", DisplayName: "Sonnet"}}
		f.UsageResp = &ascv1.AiUsageResponse{
			Rows:  []*ascv1.AiUsageRow{{Group: "claude-sonnet-4-5", Requests: 1200, InputTokens: 3_400_000, CostMicroUsd: 4_200_000}},
			Total: &ascv1.AiUsageRow{Requests: 1200, CostMicroUsd: 4_200_000},
		}
		r := run(t, AITest, lvl, http.MethodPost, nil, id)
		require.Equal(t, http.StatusOK, r.resp.Code, r.body())
		assert.Contains(t, r.body(), `data-testid="asc-test-result"`)
		assert.Contains(t, r.body(), tr(t, "ascbyo.ai.detail.test_ok", 80))
		assert.Contains(t, r.body(), "claude-sonnet-4-5", "models of a tested key")
		assert.Contains(t, r.body(), "1,200")
		assert.Contains(t, r.body(), "$4.20")

		f.TestStatus = "unauthorized"
		r = run(t, AITest, lvl, http.MethodPost, nil, id)
		require.Equal(t, http.StatusOK, r.resp.Code)
		assert.Contains(t, r.body(), tr(t, "ascbyo.ai.detail.test_failed", tr(t, "ascbyo.ai.test_status.unauthorized")))
		f.TestStatus = ""
	})

	t.Run("update without a new key keeps the old one", func(t *testing.T) {
		upd := url.Values{"label": {"Team renamed"}, "default_model": {"claude-sonnet-4-5"}, "monthly_cap": {"25"}}
		r := run(t, AIUpdate, lvl, http.MethodPost, upd, id)
		require.Equal(t, http.StatusSeeOther, r.resp.Code, r.body())
		ureq := f.CallsTo("UpdateKey")[0].Req.(*ascv1.UpdateAiKeyRequest)
		assert.Equal(t, "Team renamed", ureq.GetLabel())
		assert.Nil(t, ureq.ApiKey)
	})

	t.Run("disable and enable", func(t *testing.T) {
		r := run(t, func(c *context.Context, l *Level) { AIDisable(c, l, true) }, lvl, http.MethodPost, url.Values{"return": {"list"}}, id)
		require.Equal(t, http.StatusSeeOther, r.resp.Code)
		assert.Equal(t, "/user/settings/asc/ai", r.loc())
		r = run(t, AIIndex, lvl, http.MethodGet, nil, "")
		assert.Contains(t, r.body(), tr(t, "ascbyo.ai.state.disabled"))
		assert.Contains(t, r.body(), "/enable")
		r = run(t, func(c *context.Context, l *Level) { AIDisable(c, l, false) }, lvl, http.MethodPost, url.Values{"return": {"https://evil.example"}}, id)
		assert.Equal(t, "/user/settings/asc/ai/keys/"+id, r.loc())
	})

	t.Run("another owner's key is a 404 everywhere", func(t *testing.T) {
		theirs := f.AddKey(mustScope(t, svc.KindUser, "bob").Owner(), "bobs", ascv1.AiProvider_AI_PROVIDER_OPENAI, ascv1.AiKeyState_AI_KEY_STATE_ACTIVE)
		mutations := f.Mutations()
		for name, fn := range map[string]func(*context.Context, *Level){
			"get": AIKey, "update": AIUpdate, "test": AITest, "delete": AIDelete,
			"enable":  func(c *context.Context, l *Level) { AIDisable(c, l, false) },
			"disable": func(c *context.Context, l *Level) { AIDisable(c, l, true) },
		} {
			method := http.MethodPost
			if name == "get" {
				method = http.MethodGet
			}
			r := run(t, fn, lvl, method, url.Values{"confirm": {"on"}, "label": {"x"}}, theirs.GetId())
			assert.Equal(t, http.StatusNotFound, r.resp.Code, name)
			assert.NotContains(t, r.body(), "bobs")
		}
		assert.Equal(t, mutations, f.Mutations())
		r := run(t, AIIndex, lvl, http.MethodGet, nil, "")
		assert.NotContains(t, r.body(), "bobs")
	})

	t.Run("delete needs the confirmation", func(t *testing.T) {
		r := run(t, AIDelete, lvl, http.MethodPost, nil, id)
		require.Equal(t, http.StatusUnprocessableEntity, r.resp.Code)
		assert.Empty(t, f.CallsTo("DeleteKey"))
		r = run(t, AIDelete, lvl, http.MethodPost, url.Values{"confirm": {"on"}}, id)
		require.Equal(t, http.StatusSeeOther, r.resp.Code)
		assert.Equal(t, "/user/settings/asc/ai", r.loc())
	})
}

func TestAIIndexEscapesAndShowsSpend(t *testing.T) {
	f := setup(t)
	alice := userLevel(t).Scope.Owner()
	k := f.AddKey(alice, `<img src=x onerror=alert(1)>`, ascv1.AiProvider_AI_PROVIDER_OPENAI, ascv1.AiKeyState_AI_KEY_STATE_CAPPED)
	f.SetKeyFields(k.GetId(), func(k *ascv1.AiKey) {
		k.MonthlyCapMicroUsd, k.MonthSpentMicroUsd = 20_000_000, 25_000_000
		k.LastError = "<script>x</script>"
	})
	r := run(t, AIIndex, userLevel(t), http.MethodGet, nil, "")
	require.Equal(t, http.StatusOK, r.resp.Code)
	body := r.body()
	assert.NotContains(t, body, "<img src=x")
	assert.NotContains(t, body, "<script>x</script>")
	assert.Contains(t, body, "asc-meter-bad", "over the cap")
	assert.Contains(t, body, `value="100"`)
	assert.Contains(t, body, tr(t, "ascbyo.ai.state.capped"))
}

func TestFormLimit(t *testing.T) {
	ctx, _ := contexttest.MockContext(t, "POST /x")
	ctx.Req.PostForm = url.Values{"a": {strings.Repeat("x", 1000)}, "b": {"short"}}
	f := form{ctx}
	assert.Len(t, f.str("a", 10), 11, "cut one byte past the limit so validators refuse it")
	assert.Equal(t, "short", f.str("b", 10))
	assert.Equal(t, "", f.str("missing", 10))
	assert.False(t, f.on("b"))
}

func TestRepoAIKeysPage(t *testing.T) {
	f := setup(t)
	repo := repoLevel(t, true)
	repo.OwnerAIURL = "/user/settings/asc/ai"
	own := f.AddKey(repo.Scope.Owner(), "repo key", ascv1.AiProvider_AI_PROVIDER_GOOGLE, ascv1.AiKeyState_AI_KEY_STATE_ACTIVE)
	f.AddKey(mustScope(t, svc.KindUser, "alice").Owner(), "owner key", ascv1.AiProvider_AI_PROVIDER_OPENAI, ascv1.AiKeyState_AI_KEY_STATE_ACTIVE)

	r := run(t, AIIndex, repo, http.MethodGet, nil, "")
	require.Equal(t, http.StatusOK, r.resp.Code)
	body := r.body()
	assert.Contains(t, body, "repo key")
	assert.NotContains(t, body, "owner key", "the owner's keys are managed on the owner's page")
	assert.Contains(t, body, tr(t, "ascbyo.ai.owner_note"))
	assert.Contains(t, body, `href="/user/settings/asc/ai"`)
	assert.Contains(t, body, "/alice/widgets/settings/asc/ai/keys/"+own.GetId())

	// a key is created for the repository, from the repository route
	r = run(t, AICreate, repo, http.MethodPost, url.Values{"label": {"new"}, "provider": {"openai"}, "api_key": {"sk-openai-0123456789abcdef"}}, "")
	require.Equal(t, http.StatusSeeOther, r.resp.Code, r.body())
	req := f.CallsTo("CreateKey")[0].Req.(*ascv1.CreateAiKeyRequest)
	assert.Equal(t, ascv1.OwnerKind_OWNER_KIND_REPO, req.GetOwner().GetKind())
	assert.Equal(t, "alice/widgets", req.GetOwner().GetName())

	// the repository page cannot reach the owner's key
	ownerKey := f.AddKey(mustScope(t, svc.KindUser, "alice").Owner(), "owner key 2", ascv1.AiProvider_AI_PROVIDER_OPENAI, ascv1.AiKeyState_AI_KEY_STATE_ACTIVE)
	r = run(t, AIKey, repo, http.MethodGet, nil, ownerKey.GetId())
	assert.Equal(t, http.StatusNotFound, r.resp.Code)
}
