// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

//go:build asc

package integration

import (
	"bytes"
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git/ascbackend"
	ascwriter "forgejo.org/modules/repostore/asc"
	"forgejo.org/modules/setting"
	api "forgejo.org/modules/structs"
	"forgejo.org/modules/test"
	"forgejo.org/routers"
	"forgejo.org/services/ascbridge"
	"forgejo.org/services/ascforward"
	pull_service "forgejo.org/services/pull"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The end-to-end test of the hub with the real `asc serve` and the real `asc` command line (a running hub
// is what the people use, so the pieces that the unit tests fake are real here): the hub's web routes listen
// on a loopback port, `asc serve` runs with the forgejo provider against that port, and every step is made
// by the `asc` and `git` programs through the hub's URL, as a person would.
//
// It is skipped unless both programs are named:
//
//	ASC_E2E_BIN    the `asc` client (cargo build -p asc)
//	ASC_SERVE_BIN  the `asc` that has `serve` (cargo build -p asc --features serve); may be the same file
//
// Run it with: go test -tags 'sqlite sqlite_unlock_notify asc' -run TestAscEndToEnd ./tests/integration/

// The keys are put together here from parts, so that no complete key is written in the source (a scanner of secrets
// would find it, and it would be right to: the test needs text that looks exactly like one).
const e2eAWSKeys = "aws_access_key_id = " + "AKIA" + "J2KDLP7X" + "Q4MNR5TW" + "\n" +
	"aws_secret_access_key = \"" + "9xT2mQ8vLw" + "3RzK7bN4cY" + "d6HfJ1pGsE" + "a5UoV0iXtM" + "\"\n"

type e2eEnv struct {
	t       *testing.T
	ascBin  string
	hubURL  string
	ascAddr string
	root    string // scratch directory
}

// run starts a program as a person would: its own home, no prompts, plain http allowed to the loopback.
func (e *e2eEnv) run(home, dir, stdin string, name string, args ...string) (string, error) {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(e.t.Context(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"ASC_HOME=" + filepath.Join(home, ".asc"),
		"ASC_ALLOW_LOOPBACK_HTTP=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_GLOBAL=" + filepath.Join(home, ".gitconfig"),
		"GIT_CONFIG_SYSTEM=" + os.DevNull,
	}
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return out.String(), err
}

func (e *e2eEnv) asc(home, dir string, args ...string) (string, error) {
	e.t.Helper()
	return e.run(home, dir, "", e.ascBin, args...)
}

func (e *e2eEnv) mustAsc(home, dir string, args ...string) string {
	e.t.Helper()
	out, err := e.asc(home, dir, args...)
	require.NoError(e.t, err, "asc %s\n%s", strings.Join(args, " "), out)
	return out
}

func (e *e2eEnv) git(home, dir string, args ...string) (string, error) {
	e.t.Helper()
	// Plain git is given its credentials in the URL; the person's store (which asc reads) must not keep them: a stored
	// entry for the host with the token as the name and no password is found before the real one.
	return e.run(home, dir, "", "git", append([]string{"-c", "credential.helper="}, args...)...)
}

// person is a signed-in person: a home with the credentials of one hub user.
type e2ePerson struct {
	user  *user_model.User
	token string
	home  string
}

func (e *e2eEnv) newPerson(name string) *e2ePerson {
	e.t.Helper()
	u := unittest.AssertExistsAndLoadBean(e.t, &user_model.User{Name: name})
	tok := createAPIAccessTokenWithoutCleanUp(e.t, "e2e-"+name, u, []auth_model.AccessTokenScope{auth_model.AccessTokenScopeAll})
	home := filepath.Join(e.root, "home-"+name)
	require.NoError(e.t, os.MkdirAll(home, 0o755))
	require.NoError(e.t, os.WriteFile(filepath.Join(home, ".gitconfig"),
		[]byte("[user]\n\tname = "+name+"\n\temail = "+name+"@example.com\n[safe]\n\tdirectory = *\n[credential]\n\thelper = store\n"), 0o600))
	p := &e2ePerson{user: u, token: tok.Token, home: home}
	for _, host := range []string{e.hubURL, "http://" + e.ascAddr} {
		out, err := e.run(home, e.root, tok.Token, e.ascBin, "auth", "login", "--host", host, "--token-stdin")
		require.NoError(e.t, err, "asc auth login %s\n%s", host, out)
	}
	return p
}

func e2eFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// startHubAndAsc listens with the hub's routes and starts `asc serve` against them.
func startHubAndAsc(t *testing.T, serveBin string) (hubURL, ascAddr string) {
	t.Helper()
	root := t.TempDir()
	ascPort, adminPort := e2eFreePort(t), e2eFreePort(t)
	ascAddr = fmt.Sprintf("127.0.0.1:%d", ascPort)

	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	keyFile := filepath.Join(root, "forwarded-key")
	require.NoError(t, os.WriteFile(keyFile, []byte(hex.EncodeToString(key)), 0o600))
	tokenFile := filepath.Join(root, "internal-token")
	require.NoError(t, os.WriteFile(tokenFile, []byte(setting.InternalToken), 0o600))
	secretFile := filepath.Join(root, "webhook-secret")
	require.NoError(t, os.WriteFile(secretFile, []byte("e2e-webhook-secret-0123456789abcdef"), 0o600))

	// The hub: settings first, then the routes (the asc routes exist only when asc is enabled).
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	t.Cleanup(test.MockVariableValue(&setting.Asc.ServerURL, "http://"+ascAddr))
	t.Cleanup(test.MockVariableValue(&setting.Asc.Writer, setting.AscWriterAsc))
	t.Cleanup(test.MockVariableValue(&setting.Asc.NewRepositories, "asc"))
	t.Cleanup(test.MockVariableValue(&setting.Asc.Bridge, setting.AscBridgeEager))
	t.Cleanup(test.MockVariableValue(&setting.Asc.ShadowSamplePercent, 0))
	t.Cleanup(test.MockVariableValue(&setting.Asc.ForwardedKey, key))
	// What the hub's start does with [asc] enabled: the clients for git reads and for writes, and the bridge directory.
	initCtx, stopInit := context.WithCancel(context.Background())
	t.Cleanup(stopInit)
	require.NoError(t, ascbackend.Init(initCtx))
	require.NoError(t, ascwriter.Init(initCtx))
	require.NoError(t, ascbridge.Init(initCtx))
	// The merge of a stack and the merge queue call asc with the internal token (routers.InitWebInstalled does this at start).
	prevStack := pull_service.UseAscStackBackend(nil)
	t.Cleanup(prevStack)
	require.NoError(t, pull_service.InitAscStack())
	// Git's hooks are the hub's own program: it reads the configuration file and calls the hub back at the address of
	// the application URL, so the hub listens there and the file names the repository root of this run (as the other
	// tests that push over http do).
	appURL, err := url.Parse(setting.AppURL)
	require.NoError(t, err)
	listener, err := net.Listen("tcp", appURL.Host)
	for i := 0; err != nil && i <= 10; i++ {
		time.Sleep(100 * time.Millisecond)
		listener, err = net.Listen("tcp", appURL.Host)
	}
	require.NoError(t, err)
	conf0, err := os.ReadFile(setting.CustomConf)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(setting.CustomConf, rootPathRe.ReplaceAll(conf0, []byte("[repository]\nROOT = "+setting.RepoRootPath)), 0o600))
	t.Cleanup(func() { _ = os.WriteFile(setting.CustomConf, conf0, 0o600) })
	hub := httptest.NewUnstartedServer(ascforward.PriorKnowledgeH2C(routers.NormalRoutes())) // as cmd/web.go serves it
	_ = hub.Listener.Close()
	hub.Listener = listener
	hub.Start()
	t.Cleanup(hub.Close)
	t.Cleanup(test.MockVariableValue(&setting.Asc.PublicURL, hub.URL))
	hubURL = hub.URL

	conf := filepath.Join(root, "asc-serve.toml")
	require.NoError(t, os.WriteFile(conf, fmt.Appendf(nil, `listen = "%s"
store = "file://%s"
db = "sqlite://%s?mode=rwc"
cache = { memory = "64MiB", disk_dir = "%s", disk = "512MiB" }

[auth]
provider = "forgejo"
forwarded_key_file = "%s"

[forgejo]
url = "%s"
internal_token_file = "%s"
webhook_secret_file = "%s"
repo_root = "%s"
bridge_promote = true

[limits]
text_memory = "64MiB"
trusted_proxies = ["127.0.0.1/32"]

[git]
enabled = true
view_dir = "%s"

[ssh]
enabled = false

[admin]
listen = "127.0.0.1:%d"
`, ascAddr, filepath.ToSlash(filepath.Join(root, "objects")), filepath.ToSlash(filepath.Join(root, "asc.db")),
		filepath.ToSlash(filepath.Join(root, "cache")), filepath.ToSlash(keyFile), hub.URL, filepath.ToSlash(tokenFile),
		filepath.ToSlash(secretFile), filepath.ToSlash(setting.RepoRootPath), filepath.ToSlash(filepath.Join(root, "gitview")), adminPort), 0o600))

	home := filepath.Join(root, "serve-home")
	require.NoError(t, os.MkdirAll(home, 0o755))
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "ASC_HOME=" + filepath.Join(home, ".asc"),
		"ASC_SERVE_CONFIG=" + conf, "ASC_ALLOW_LOOPBACK_HTTP=1", "RUST_LOG=" + cmp.Or(os.Getenv("ASC_E2E_RUST_LOG"), "info")}
	migrate := exec.CommandContext(t.Context(), serveBin, "serve", "migrate")
	migrate.Env = env
	if out, err := migrate.CombinedOutput(); err != nil {
		t.Fatalf("asc serve migrate: %v\n%s", err, out)
	}
	ctx, cancel := context.WithCancel(t.Context())
	srv := exec.CommandContext(ctx, serveBin, "serve")
	srv.Env = env
	var logs bytes.Buffer
	srv.Stdout, srv.Stderr = &logs, &logs
	require.NoError(t, srv.Start())
	t.Cleanup(func() {
		cancel()
		_ = srv.Wait()
		if path := os.Getenv("ASC_E2E_SERVE_LOG"); path != "" {
			_ = os.WriteFile(path, logs.Bytes(), 0o644)
		}
		if t.Failed() {
			t.Logf("asc serve log:\n%s", tail(logs.String(), 6000))
			var refused []string
			for _, line := range strings.Split(logs.String(), "\n") {
				// The access log has a line for every call, many of them warnings the tests cause on purpose. What is kept
				// are the events (bridge promoted, outbox retry) and the calls that write, to read the order of things.
				access := strings.Contains(line, "asc_serve::access")
				if (!access && strings.TrimSpace(line) != "") || strings.Contains(line, " ERROR ") ||
					(access && (strings.Contains(line, "/UpdateRefs") || strings.Contains(line, "/ImportGitPush") || strings.Contains(line, "/RequestMerge"))) {
					refused = append(refused, line)
				}
			}
			t.Logf("asc serve events and writes:\n%s", tail(strings.Join(refused, "\n"), 40000))
		}
	})
	deadline := time.Now().Add(30 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", ascAddr, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return hubURL, ascAddr
		}
		if time.Now().After(deadline) {
			t.Fatalf("asc serve did not listen on %s: %v\n%s", ascAddr, err, tail(logs.String(), 4000))
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func tail(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

// eventually polls cond until it holds; the bridge and the outbox work after the answer of the call.
// The last answer of the thing that is waited for, when the caller keeps one, is part of the failure.
func eventually(t *testing.T, what string, cond func() bool, last ...*string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			if len(last) > 0 && last[0] != nil {
				t.Fatalf("timed out waiting for %s; the last answer was: %s", what, *last[0])
			}
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// stack makes a stack of n commits in ws, one file each, and returns the output of `asc submit -s`.
func (e *e2eEnv) stack(home, ws string, n int, prefix string) (string, error) {
	e.t.Helper()
	for i := 1; i <= n; i++ {
		f := fmt.Sprintf("src/%s%d.rs", prefix, i)
		require.NoError(e.t, os.MkdirAll(filepath.Join(ws, "src"), 0o755))
		require.NoError(e.t, os.WriteFile(filepath.Join(ws, f), fmt.Appendf(nil, "pub fn %s%d() -> u32 { %d }\n", prefix, i, i), 0o644))
		e.mustAsc(home, ws, "add", f)
		e.mustAsc(home, ws, "commit", "-m", fmt.Sprintf("Add %s step %d", prefix, i))
	}
	return e.asc(home, ws, "submit", "-s")
}

var e2eChangeRe = regexp.MustCompile(`[k-z]{32}`)

func TestAscEndToEnd(t *testing.T) {
	ascBin, serveBin := os.Getenv("ASC_E2E_BIN"), os.Getenv("ASC_SERVE_BIN")
	if ascBin == "" || serveBin == "" {
		t.Skip("ASC_E2E_BIN and ASC_SERVE_BIN are not set")
	}
	defer tests.PrepareTestEnv(t)()

	hubURL, ascAddr := startHubAndAsc(t, serveBin)
	e := &e2eEnv{t: t, ascBin: ascBin, hubURL: hubURL, ascAddr: ascAddr, root: t.TempDir()}
	alice, bob := e.newPerson("user2"), e.newPerson("user4")

	create := func(p *e2ePerson, name string, private bool) *repo_model.Repository {
		e.apiJSON(p, "POST", "/api/v1/user/repos", &api.CreateRepoOption{Name: name, AutoInit: true, Private: private, DefaultBranch: "main"}, http.StatusCreated)
		r := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{OwnerID: p.user.ID, Name: name})
		require.Equal(t, "asc", r.StorageBackend, "a new repository is made on asc")
		return r
	}
	clone := func(p *e2ePerson, repo *repo_model.Repository, as string) string {
		ws := filepath.Join(e.root, as)
		owner := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: repo.OwnerID})
		e.mustAsc(p.home, e.root, "clone", fmt.Sprintf("%s/%s/%s", hubURL, owner.Name, repo.Name), ws)
		// The stack's trunk is the branch the client is told: the default branch of the hub's repositories is main.
		e.mustAsc(p.home, ws, "config", "asc.stack.trunk", "main")
		return ws
	}

	repo := create(alice, "ledger", false)
	ws := clone(alice, repo, "alice-ledger")
	// The policy of the trunk is the file asc enforces (.asc/policy.toml, read from the trunk before an update):
	// landing needs one approval. Alice is the maintainer. The hub's own branch protection is not translated to it.
	require.NoError(t, os.MkdirAll(filepath.Join(ws, ".asc"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ws, ".asc", "policy.toml"), []byte(
		"version = 1\n\n[roles]\nmaintainer = [\"user:"+alice.user.Name+"\"]\n\n[trunk]\nrequire_approvals = 1\nallow_force = false\n"), 0o644))
	e.mustAsc(alice.home, ws, "add", ".asc/policy.toml")
	e.mustAsc(alice.home, ws, "commit", "-m", "policy: one approval to land")
	e.mustAsc(alice.home, ws, "push")

	t.Run("a stack of three is submitted through the hub URL", func(t *testing.T) {
		out, err := e.stack(alice.home, ws, 3, "step")
		require.NoError(t, err, out)
		for i := int64(1); i <= 3; i++ {
			i := i
			eventually(t, fmt.Sprintf("pull request #%d", i), func() bool {
				pr, err := issues_model.GetPullRequestByIndex(t.Context(), repo.ID, i)
				return err == nil && pr != nil
			})
			pr, err := issues_model.GetPullRequestByIndex(t.Context(), repo.ID, i)
			require.NoError(t, err)
			row, err := issues_model.GetPullRequestAscStack(t.Context(), pr.ID)
			require.NoError(t, err)
			require.NotNil(t, row, "pull request #%d has a pull_request_asc_stack row", i)
			assert.Regexp(t, e2eChangeRe, row.AscChange)
			eventually(t, fmt.Sprintf("refs/pull/%d/head", i), func() bool {
				_, err := e.git(alice.home, repo.RepoPath(), "rev-parse", "--verify", fmt.Sprintf("refs/pull/%d/head", i))
				return err == nil
			})
		}
		n, err := db.GetEngine(t.Context()).Where("repo_id = ?", repo.ID).Count(new(issues_model.PullRequestAscStack))
		require.NoError(t, err)
		assert.EqualValues(t, 3, n)
		// The stack parents: #2 sits on #1 and #3 on #2.
		for i := int64(2); i <= 3; i++ {
			pr, _ := issues_model.GetPullRequestByIndex(t.Context(), repo.ID, i)
			below, _ := issues_model.GetPullRequestByIndex(t.Context(), repo.ID, i-1)
			row, _ := issues_model.GetPullRequestAscStack(t.Context(), pr.ID)
			assert.Equal(t, below.ID, row.StackParentID)
		}
	})

	t.Run("a second submit changes nothing", func(t *testing.T) {
		out, err := e.asc(alice.home, ws, "submit", "-s")
		require.NoError(t, err, out)
		n, err := db.GetEngine(t.Context()).Where("repo_id = ?", repo.ID).Count(new(issues_model.PullRequestAscStack))
		require.NoError(t, err)
		assert.EqualValues(t, 3, n, "no pull request was added")
		_, err = issues_model.GetPullRequestByIndex(t.Context(), repo.ID, 4)
		assert.Error(t, err, "there is no fourth pull request")
	})

	t.Run("another person with write access cannot publish a version of the change", func(t *testing.T) {
		// Bob may write to the repository, so only the rule of the owner of the change can refuse him.
		e.apiJSON(alice, "PUT", fmt.Sprintf("/api/v1/repos/%s/%s/collaborators/%s", alice.user.Name, repo.Name, bob.user.Name),
			map[string]string{"permission": "write"}, http.StatusNoContent)
		top, err := issues_model.GetPullRequestByIndex(t.Context(), repo.ID, 3)
		require.NoError(t, err)
		row, err := issues_model.GetPullRequestAscStack(t.Context(), top.ID)
		require.NoError(t, err)
		require.NotNil(t, row)
		bws := clone(bob, repo, "bob-ledger")
		// Bob takes Alice's change into his workspace, adds a file to it and submits it.
		out, err := e.asc(bob.home, bws, "stack", "get", "--unfrozen", row.AscChange)
		require.NoError(t, err, out)
		require.NoError(t, os.WriteFile(filepath.Join(bws, "bobs-edit.txt"), []byte("bob\n"), 0o644))
		e.mustAsc(bob.home, bws, "add", "bobs-edit.txt")
		e.mustAsc(bob.home, bws, "modify")
		out, err = e.asc(bob.home, bws, "submit")
		require.Error(t, err, "Bob published a version of Alice's change:\n%s", out)
		assert.Regexp(t, `(?i)(not allowed|CHANGE_NOT_ALLOWED|publisher)`, out)
		n, _ := db.GetEngine(t.Context()).Where("repo_id = ?", repo.ID).Count(new(issues_model.PullRequestAscStack))
		assert.EqualValues(t, 3, n, "Bob added no pull request of Alice's")

		// His own change goes through: the rule is about whose change it is, not about Bob.
		bs := filepath.Join(e.root, "bob-own")
		e.mustAsc(bob.home, e.root, "clone", fmt.Sprintf("%s/%s/%s", hubURL, alice.user.Name, repo.Name), bs)
		e.mustAsc(bob.home, bs, "config", "asc.stack.trunk", "main")
		require.NoError(t, os.WriteFile(filepath.Join(bs, "bob.txt"), []byte("bob\n"), 0o644))
		e.mustAsc(bob.home, bs, "add", "bob.txt")
		e.mustAsc(bob.home, bs, "commit", "-m", "Bob adds a file")
		out, err = e.asc(bob.home, bs, "submit")
		require.NoError(t, err, out)
		eventually(t, "Bob's pull request", func() bool {
			n, _ := db.GetEngine(t.Context()).Where("repo_id = ?", repo.ID).Count(new(issues_model.PullRequestAscStack))
			return n == 4
		})
	})

	t.Run("a push of an AWS key is refused with the file and the line", func(t *testing.T) {
		other := create(alice, "vault", false)
		vws := clone(alice, other, "alice-vault")
		require.NoError(t, os.WriteFile(filepath.Join(vws, "creds.txt"), []byte("# credentials\n"+e2eAWSKeys), 0o644))
		e.mustAsc(alice.home, vws, "add", "creds.txt")
		e.mustAsc(alice.home, vws, "commit", "-m", "add creds")
		out, err := e.asc(alice.home, vws, "push")
		require.Error(t, err, "the push of an AWS key was accepted:\n%s", out)
		assert.Contains(t, out, "SECRET_DETECTED")
		assert.Contains(t, out, "creds.txt")
		assert.Regexp(t, `creds\.txt:?\s*(line\s*)?2`, out, "the refusal names the line")
		// The same through git, to the clone URL of the hub.
		gdir := filepath.Join(e.root, "git-vault")
		out, err = e.git(alice.home, e.root, "clone", "-q", fmt.Sprintf("http://%s:%s@%s/%s/%s.git", alice.user.Name, alice.token, strings.TrimPrefix(hubURL, "http://"), alice.user.Name, other.Name), gdir)
		require.NoError(t, err, out)
		require.NoError(t, os.WriteFile(filepath.Join(gdir, "creds2.txt"), []byte(e2eAWSKeys), 0o644))
		_, _ = e.git(alice.home, gdir, "add", "-A")
		_, _ = e.git(alice.home, gdir, "commit", "-q", "-m", "creds")
		out, err = e.git(alice.home, gdir, "push", "origin", "HEAD")
		require.Error(t, err, "the git push of an AWS key was accepted:\n%s", out)
		assert.Contains(t, strings.ToLower(out), "secret")
	})

	t.Run("a push of an AWS key to an imported repository is refused", func(t *testing.T) {
		// A history with secrets is imported (not blocked); a new push of a key into it is.
		admin := e.newPerson("user1")
		src := filepath.Join(e.root, "src-secret")
		require.NoError(t, os.MkdirAll(src, 0o755))
		for _, a := range [][]string{{"init", "-q", "-b", "main"}, {"config", "user.email", "a@example.com"}, {"config", "user.name", "a"}} {
			out, err := e.git(admin.home, src, a...)
			require.NoError(t, err, out)
		}
		require.NoError(t, os.WriteFile(filepath.Join(src, "old.txt"), []byte("legacy\n"+e2eAWSKeys), 0o644))
		_, _ = e.git(admin.home, src, "add", "-A")
		out, err := e.git(admin.home, src, "commit", "-q", "-m", "legacy with a key")
		require.NoError(t, err, out)
		defer test.MockVariableValue(&setting.Migrations.AllowLocalNetworks, true)()
		defer test.MockVariableValue(&setting.ImportLocalPaths, true)()
		e.apiJSON(admin, "POST", "/api/v1/repos/migrate", &api.MigrateRepoOptions{
			CloneAddr: src, RepoName: "imported", RepoOwner: admin.user.Name, Service: "git",
		}, http.StatusCreated)
		gdir := filepath.Join(e.root, "git-imported")
		host := strings.TrimPrefix(hubURL, "http://")
		out, err = e.git(admin.home, e.root, "clone", "-q", fmt.Sprintf("http://%s:@%s/%s/imported.git", admin.token, host, admin.user.Name), gdir)
		require.NoError(t, err, out)
		require.NoError(t, os.WriteFile(filepath.Join(gdir, "new.txt"), []byte(e2eAWSKeys), 0o644))
		_, _ = e.git(admin.home, gdir, "add", "-A")
		_, _ = e.git(admin.home, gdir, "commit", "-q", "-m", "a new key")
		out, err = e.git(admin.home, gdir, "push", "origin", "HEAD")
		require.Error(t, err, "the push of an AWS key to the imported repository was accepted:\n%s", out)
		assert.Contains(t, strings.ToLower(out), "secret")
	})

	t.Run("the policy refuses a direct push to the trunk", func(t *testing.T) {
		// Bob has write access; the policy of the trunk (approvals) is what stops him, through git and the hub.
		host := strings.TrimPrefix(hubURL, "http://")
		gdir := filepath.Join(e.root, "git-direct")
		out, err := e.git(bob.home, e.root, "clone", "-q", fmt.Sprintf("http://%s:@%s/%s/%s.git", bob.token, host, alice.user.Name, repo.Name), gdir)
		require.NoError(t, err, out)
		require.NoError(t, os.WriteFile(filepath.Join(gdir, "direct.txt"), []byte("direct\n"), 0o644))
		_, _ = e.git(bob.home, gdir, "add", "-A")
		_, _ = e.git(bob.home, gdir, "commit", "-q", "-m", "direct")
		out, err = e.git(bob.home, gdir, "push", "origin", "HEAD:main")
		require.Error(t, err, "a direct push to the trunk was accepted:\n%s", out)
		assert.Regexp(t, `(?i)(approval|policy|POLICY_DENIED)`, out)
	})

	t.Run("the repository is cloned by asc and by git with a token in the URL", func(t *testing.T) {
		host := strings.TrimPrefix(hubURL, "http://")
		dir := filepath.Join(e.root, "git-ledger")
		out, err := e.git(bob.home, e.root, "clone", "-q", fmt.Sprintf("http://%s:@%s/%s/%s.git", bob.token, host, alice.user.Name, repo.Name), dir)
		require.NoError(t, err, out)
		assert.FileExists(t, filepath.Join(dir, "README.md"))
		// The asc clone made above (Bob's workspace) has the same trunk file.
		assert.FileExists(t, filepath.Join(e.root, "bob-ledger", "README.md"))
	})

	t.Run("an anonymous clone of a private repository is refused", func(t *testing.T) {
		priv := create(alice, "secret-plans", true)
		anon := filepath.Join(e.root, "home-anon")
		require.NoError(t, os.MkdirAll(anon, 0o755))
		out, err := e.git(anon, e.root, "clone", "-q", fmt.Sprintf("%s/%s/%s.git", hubURL, alice.user.Name, priv.Name), filepath.Join(e.root, "anon-git"))
		require.Error(t, err, "an anonymous git clone of a private repository worked:\n%s", out)
		out, err = e.asc(anon, e.root, "clone", fmt.Sprintf("%s/%s/%s", hubURL, alice.user.Name, priv.Name), filepath.Join(e.root, "anon-asc"))
		require.Error(t, err, "an anonymous asc clone of a private repository worked:\n%s", out)
	})

	t.Run("a stack lands with the approval of a second person", func(t *testing.T) {
		merge := func(i int) (int, string) {
			status, b := e.api(alice, "POST", fmt.Sprintf("/api/v1/repos/%s/%s/pulls/%d/merge", alice.user.Name, repo.Name, i),
				map[string]string{"Do": "merge"})
			return status, string(b)
		}
		// Without an approval the hub shows the refusal and says why.
		status, body := merge(1)
		assert.NotEqual(t, http.StatusOK, status, "landed without the approval: %s", body)
		assert.Regexp(t, `(?i)approval`, body, "the hub passes asc's sentence on")

		// Bob reviews every pull request through the hub's API; the hub reports each to asc (RecordApproval).
		for i := 1; i <= 3; i++ {
			e.apiJSON(bob, "POST", fmt.Sprintf("/api/v1/repos/%s/%s/pulls/%d/reviews", alice.user.Name, repo.Name, i),
				&api.CreatePullReviewOptions{Event: api.ReviewStateApproved, Body: "ok"}, http.StatusOK)
		}
		// The stack lands the way a person asks for it, with the asc client: `asc stack merge --now` asks the hub, which
		// lands the stack in asc (the bottom first, one atomic call) as the person who asked.
		top, err := issues_model.GetPullRequestByIndex(t.Context(), repo.ID, 3)
		require.NoError(t, err)
		row, err := issues_model.GetPullRequestAscStack(t.Context(), top.ID)
		require.NoError(t, err)
		require.NotNil(t, row)
		out, err := e.asc(alice.home, ws, "stack", "merge", "--now", row.AscChange)
		require.NoError(t, err, out)
		assert.Contains(t, strings.ToLower(out), "landed", out)
		for i := 1; i <= 3; i++ {
			eventually(t, fmt.Sprintf("pull request #%d to be merged", i), func() bool {
				p, err := issues_model.GetPullRequestByIndex(t.Context(), repo.ID, int64(i))
				return err == nil && p.HasMerged
			})
		}
		// The trunk of a fresh clone holds the three files.
		fresh := clone(alice, repo, "alice-ledger-after")
		for i := 1; i <= 3; i++ {
			assert.FileExists(t, filepath.Join(fresh, "src", fmt.Sprintf("step%d.rs", i)))
		}
	})

	t.Run("a change whose trunk moved lands, rebased onto the new trunk", func(t *testing.T) {
		// Bob's change (#4) was made on the trunk as it was before Alice's stack landed, so asc has to put its head on the
		// new trunk; the hub's record of the exact commit no longer matches, and the hook still has to run as the person.
		e.apiJSON(alice, "POST", fmt.Sprintf("/api/v1/repos/%s/%s/pulls/4/reviews", alice.user.Name, repo.Name),
			&api.CreatePullReviewOptions{Event: api.ReviewStateApproved, Body: "ok"}, http.StatusOK)
		bs := filepath.Join(e.root, "bob-own")
		out, err := e.asc(bob.home, bs, "stack", "merge", "--now")
		require.NoError(t, err, out)
		assert.Contains(t, strings.ToLower(out), "landed", out)
		eventually(t, "the moved change to be merged", func() bool {
			p, err := issues_model.GetPullRequestByIndex(t.Context(), repo.ID, 4)
			return err == nil && p.HasMerged
		})
		fresh := clone(alice, repo, "alice-ledger-moved")
		for i := 1; i <= 3; i++ {
			assert.FileExists(t, filepath.Join(fresh, "src", fmt.Sprintf("step%d.rs", i)))
		}
		assert.FileExists(t, filepath.Join(fresh, "bob.txt"))
		// The linear history is read through git (an asc workspace has no git directory).
		gdir := filepath.Join(e.root, "git-ledger-moved")
		out, err = e.git(alice.home, e.root, "clone", "-q", fmt.Sprintf("http://%s:%s@%s/%s/%s.git", alice.user.Name, alice.token, strings.TrimPrefix(hubURL, "http://"), alice.user.Name, repo.Name), gdir)
		require.NoError(t, err, out)
		out, err = e.git(alice.home, gdir, "rev-list", "--merges", "--count", "HEAD")
		require.NoError(t, err, out)
		assert.Equal(t, "0", strings.TrimSpace(out), "the trunk stays linear: the change was rebased, not merged")
	})

	// The gates that fail closed, through the real listener.
	t.Run("a read-only token cannot write", func(t *testing.T) {
		ro := createAPIAccessTokenWithoutCleanUp(t, "e2e-ro", alice.user, []auth_model.AccessTokenScope{auth_model.AccessTokenScopeReadRepository})
		reader := &e2ePerson{user: alice.user, token: ro.Token, home: filepath.Join(e.root, "home-ro")}
		require.NoError(t, os.MkdirAll(reader.home, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(reader.home, ".gitconfig"), []byte("[user]\n\tname = ro\n\temail = ro@example.com\n[safe]\n\tdirectory = *\n"), 0o600))
		// The API refuses the write.
		status, body := e.api(reader, "POST", fmt.Sprintf("/api/v1/repos/%s/%s/contents/ro.txt", alice.user.Name, repo.Name),
			map[string]string{"content": "cm8K", "message": "ro"})
		assert.Contains(t, []int{http.StatusForbidden, http.StatusUnauthorized}, status, "a read-only token wrote through the API: %s", body)
		// So does asc: the person with the read claim reads (clone) but cannot publish.
		for _, host := range []string{e.hubURL, "http://" + e.ascAddr} {
			out, err := e.run(reader.home, e.root, ro.Token, e.ascBin, "auth", "login", "--host", host, "--token-stdin")
			require.NoError(t, err, out)
		}
		rws := clone(reader, repo, "ro-ledger")
		require.NoError(t, os.WriteFile(filepath.Join(rws, "ro.txt"), []byte("ro\n"), 0o644))
		e.mustAsc(reader.home, rws, "add", "ro.txt")
		e.mustAsc(reader.home, rws, "commit", "-m", "read only write")
		out, err := e.asc(reader.home, rws, "submit")
		require.Error(t, err, "a read claim published a change:\n%s", out)
		n, _ := db.GetEngine(t.Context()).Where("repo_id = ?", repo.ID).Count(new(issues_model.PullRequestAscStack))
		assert.EqualValues(t, 4, n, "the read-only token added no pull request")
	})

	t.Run("a claim for one repository gives nothing in another", func(t *testing.T) {
		private := create(bob, "bobs-private", true)
		status, body := e.api(alice, "GET", fmt.Sprintf("/api/v1/repos/%s/%s", bob.user.Name, private.Name), nil)
		assert.Equal(t, http.StatusNotFound, status, "Alice sees Bob's private repository: %s", body)
		out, err := e.asc(alice.home, e.root, "clone", fmt.Sprintf("%s/%s/%s", hubURL, bob.user.Name, private.Name), filepath.Join(e.root, "alice-in-bobs"))
		require.Error(t, err, "Alice cloned Bob's private repository through asc:\n%s", out)
		host := strings.TrimPrefix(hubURL, "http://")
		out, err = e.git(alice.home, e.root, "clone", "-q", fmt.Sprintf("http://%s:@%s/%s/%s.git", alice.token, host, bob.user.Name, private.Name), filepath.Join(e.root, "alice-in-bobs-git"))
		require.Error(t, err, "Alice cloned Bob's private repository through git:\n%s", out)
	})

	t.Run("a wrong name and a wrong password are not told apart", func(t *testing.T) {
		basic := func(user, pass string) (int, string) {
			req, err := http.NewRequestWithContext(t.Context(), "GET", hubURL+"/api/v1/user", nil)
			require.NoError(t, err)
			req.SetBasicAuth(user, pass)
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			b, _ := io.ReadAll(resp.Body)
			return resp.StatusCode, string(b)
		}
		s1, b1 := basic("no-such-person-"+hex.EncodeToString(key8()), "wrong-password")
		s2, b2 := basic(alice.user.Name, "wrong-password")
		assert.Equal(t, s1, s2, "the status tells a known name from an unknown one")
		assert.Equal(t, b1, b2, "the body tells a known name from an unknown one")
	})

	t.Run("blocking by name answers the same for hidden, private organization and missing names", func(t *testing.T) {
		block := func(name string) (int, string) {
			s, b := e.api(alice, "PUT", "/api/v1/user/block/"+name, nil)
			return s, strings.ReplaceAll(string(b), name, "NAME")
		}
		missing := "no-such-person-" + hex.EncodeToString(key8())
		wantStatus, wantBody := block(missing)
		for _, name := range []string{"user31" /* a private account */, "privated_org" /* a private organization */} {
			s, b := block(name)
			assert.Equal(t, wantStatus, s, "blocking %s answers differently from a name that does not exist", name)
			assert.Equal(t, wantBody, b, "the body for %s differs from the body for a missing name", name)
		}
	})

	t.Run("the sign-up name check is generic, hides private accounts and is rate limited", func(t *testing.T) {
		t.Skip("pending branch ui11/shell (signup_name.go: no taken hint for private accounts, closed under captcha, sign-in-to-view or non-public default visibility, 20 checks a minute per address); remove this line when it lands in asc/v16")
		get := func(name string) (int, string) {
			resp, err := http.Get(hubURL + "/user/sign_up/name?name=" + name)
			require.NoError(t, err)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return resp.StatusCode, ""
			}
			var v map[string]string
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&v))
			assert.Len(t, v, 1, "the answer carries nothing but the state")
			return resp.StatusCode, v["state"]
		}
		status, _ := get("user2")
		if status == http.StatusNotFound {
			return // closed here: nothing to enumerate
		}
		require.Equal(t, http.StatusOK, status)
		_, free := get("nobody-by-this-name-" + hex.EncodeToString(key8()))
		assert.Equal(t, "ok", free)
		_, public := get("user2")
		assert.Equal(t, "taken", public)
		_, private := get("user31") // a private account
		assert.Equal(t, free, private, "a private account reads differently from a free name")
		_, org := get("org3")
		assert.Contains(t, []string{"ok", "taken"}, org)
		_, bad := get("a%20b")
		assert.Equal(t, "invalid", bad)
		limited := false
		for i := 0; i < 40 && !limited; i++ {
			s, _ := get("probe-" + hex.EncodeToString(key8()))
			limited = s == http.StatusTooManyRequests
		}
		assert.True(t, limited, "forty checks from one address are never limited")
	})

	t.Run("the text of an agent run is shown only to its creator", func(t *testing.T) {
		t.Skip("a run is created through asc by an agent credential; routers/web/repo/asc_agents_visibility_test.go covers the page, and this step needs the agent verbs of the asc client")
	})
}

func key8() []byte {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return b
}

// The People page shows who has no second factor to the owners of the organization only. It needs no asc.
func TestAscGateOrgSecondFactorHidden(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	// org3: user2 owns it, user4 is a plain member.
	fetch := func(who string) string {
		session := loginUser(t, who)
		resp := session.MakeRequest(t, NewRequest(t, "GET", "/org/org3/members"), http.StatusOK)
		return resp.Body.String()
	}
	owner, member := fetch("user2"), fetch("user4")
	assert.Contains(t, owner, "2FA", "the owner sees the second-factor state")
	assert.NotContains(t, member, "2FA", "a plain member must not see who has no second factor")
}

// api calls the hub's API on the real listener with the person's token and returns the status and body.
func (e *e2eEnv) api(p *e2ePerson, method, path string, body any) (int, []byte) {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(e.t, err)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(e.t.Context(), method, e.hubURL+path, rd)
	require.NoError(e.t, err)
	req.Header.Set("Authorization", "token "+p.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(e.t, err)
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	require.NoError(e.t, err)
	return resp.StatusCode, b
}

// apiJSON is api with the status asserted.
func (e *e2eEnv) apiJSON(p *e2ePerson, method, path string, body any, want int) []byte {
	e.t.Helper()
	status, b := e.api(p, method, path, body)
	require.Equal(e.t, want, status, "%s %s: %s", method, path, b)
	return b
}
