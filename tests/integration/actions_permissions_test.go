// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"testing"
	"time"

	actions_model "forgejo.org/models/actions"
	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/git"
	"forgejo.org/modules/setting"
	api "forgejo.org/modules/structs"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestActionsPermissionsPackagesGeneric: a workflow's `permissions: packages: write` lets the job token upload a
// package to the namespace of the owner of the repository, when the repository's setting allows it, and nowhere else.
func TestActionsPermissionsPackagesGeneric(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1}) // user2/repo1
	upload := func(t *testing.T, owner, name, token string, status int) {
		t.Helper()
		req := NewRequestWithBody(t, "PUT", fmt.Sprintf("/api/packages/%s/generic/%s/1.0.0/file.bin", owner, name), bytes.NewReader([]byte{1, 2, 3})).
			AddTokenAuth(token)
		MakeRequest(t, req, status)
	}

	t.Run("packages write with the setting on", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		task := newPermissionsTask(t, repo, permissionsTask{Permissions: "{packages: write, contents: read}", AllowPackagesWrite: true})
		upload(t, "user2", "own-namespace", task.Token, http.StatusCreated)
	})

	t.Run("write-all with the setting on", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		task := newPermissionsTask(t, repo, permissionsTask{Permissions: "write-all", AllowPackagesWrite: true})
		upload(t, "user2", "write-all", task.Token, http.StatusCreated)
	})

	t.Run("the setting is off", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		task := newPermissionsTask(t, repo, permissionsTask{Permissions: "{packages: write}", AllowPackagesWrite: false})
		upload(t, "user2", "setting-off", task.Token, http.StatusUnauthorized)
		task = newPermissionsTask(t, repo, permissionsTask{Permissions: "write-all", AllowPackagesWrite: false})
		upload(t, "user2", "setting-off-write-all", task.Token, http.StatusUnauthorized)
	})

	t.Run("the setting is turned off while the job runs", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		task := newPermissionsTask(t, repo, permissionsTask{Permissions: "{packages: write}", AllowPackagesWrite: true})
		setActionsPackagesWrite(t, repo.ID, false)
		upload(t, "user2", "turned-off", task.Token, http.StatusUnauthorized)
	})

	t.Run("the workflow has no permissions", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		task := newPermissionsTask(t, repo, permissionsTask{AllowPackagesWrite: true})
		upload(t, "user2", "no-block", task.Token, http.StatusUnauthorized)
	})

	t.Run("the workflow does not name packages", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		task := newPermissionsTask(t, repo, permissionsTask{Permissions: "{contents: write}", AllowPackagesWrite: true})
		upload(t, "user2", "not-named", task.Token, http.StatusUnauthorized)
	})

	t.Run("packages read", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		task := newPermissionsTask(t, repo, permissionsTask{Permissions: "{packages: read}", AllowPackagesWrite: true})
		upload(t, "user2", "read-only", task.Token, http.StatusUnauthorized)
	})

	t.Run("another owner's namespace", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		task := newPermissionsTask(t, repo, permissionsTask{Permissions: "write-all", AllowPackagesWrite: true})
		upload(t, "user5", "other-owner", task.Token, http.StatusUnauthorized)
		upload(t, "org3", "other-org", task.Token, http.StatusUnauthorized)
	})

	t.Run("fork pull request", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		task := newPermissionsTask(t, repo, permissionsTask{Permissions: "write-all", IsFork: true, AllowPackagesWrite: true})
		upload(t, "user2", "fork", task.Token, http.StatusUnauthorized)
	})

	t.Run("the job is over", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		task := newPermissionsTask(t, repo, permissionsTask{Permissions: "{packages: write}", AllowPackagesWrite: true})
		task.Status = actions_model.StatusSuccess
		require.NoError(t, actions_model.UpdateTask(db.DefaultContext, task, "status"))
		upload(t, "user2", "job-over", task.Token, http.StatusUnauthorized)
	})
}

// TestActionsPermissionsPackagesContainer: the same for a container image pushed with `docker login` and the job token.
func TestActionsPermissionsPackagesContainer(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1}) // user2/repo1

	registryToken := func(t *testing.T, task *actions_model.ActionTask) string {
		t.Helper()
		req := NewRequest(t, "GET", fmt.Sprintf("%sv2/token", setting.AppURL))
		req.Request.SetBasicAuth("token", task.Token)
		resp := MakeRequest(t, req, http.StatusOK)
		var tokenResponse struct {
			Token string `json:"token"`
		}
		DecodeJSON(t, resp, &tokenResponse)
		require.NotEmpty(t, tokenResponse.Token)
		return "Bearer " + tokenResponse.Token
	}
	blob := []byte("a layer")
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(blob))
	push := func(t *testing.T, owner, image, token string, status int) {
		t.Helper()
		req := NewRequestWithBody(t, "POST", fmt.Sprintf("%sv2/%s/%s/blobs/uploads?digest=%s", setting.AppURL, owner, image, digest), bytes.NewReader(blob)).
			AddTokenAuth(token)
		MakeRequest(t, req, status)
	}

	t.Run("packages write with the setting on", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		task := newPermissionsTask(t, repo, permissionsTask{Permissions: "{packages: write}", AllowPackagesWrite: true})
		token := registryToken(t, task)
		push(t, "user2", "image", token, http.StatusCreated)

		// the layer is the image's: it can be pushed again and read back
		req := NewRequest(t, "HEAD", fmt.Sprintf("%sv2/user2/image/blobs/%s", setting.AppURL, digest)).AddTokenAuth(token)
		MakeRequest(t, req, http.StatusOK)

		t.Run("the job is over", func(t *testing.T) {
			task.Status = actions_model.StatusSuccess
			require.NoError(t, actions_model.UpdateTask(db.DefaultContext, task, "status"))
			push(t, "user2", "image-after-job", token, http.StatusUnauthorized)
		})
	})

	t.Run("the setting is off", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		task := newPermissionsTask(t, repo, permissionsTask{Permissions: "{packages: write}", AllowPackagesWrite: false})
		push(t, "user2", "setting-off", registryToken(t, task), http.StatusUnauthorized)
	})

	t.Run("the workflow does not name packages", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		task := newPermissionsTask(t, repo, permissionsTask{Permissions: "{contents: write}", AllowPackagesWrite: true})
		push(t, "user2", "not-named", registryToken(t, task), http.StatusUnauthorized)
	})

	t.Run("another owner's namespace", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		task := newPermissionsTask(t, repo, permissionsTask{Permissions: "write-all", AllowPackagesWrite: true})
		token := registryToken(t, task)
		push(t, "user5", "other-owner", token, http.StatusUnauthorized)
		push(t, "org3", "other-org", token, http.StatusUnauthorized)
	})

	t.Run("fork pull request", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		task := newPermissionsTask(t, repo, permissionsTask{Permissions: "write-all", IsFork: true, AllowPackagesWrite: true})
		push(t, "user2", "fork", registryToken(t, task), http.StatusUnauthorized)
	})

	t.Run("a registry token of a job cannot be made for another job", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		task := newPermissionsTask(t, repo, permissionsTask{Permissions: "{packages: write}", AllowPackagesWrite: true})
		token := registryToken(t, task)
		// The same token with the setting off again.
		setActionsPackagesWrite(t, repo.ID, false)
		push(t, "user2", "turned-off", token, http.StatusUnauthorized)
	})
}

// TestActionsPermissionsAPI: `permissions:` narrows what the job token can do through the API of its repository.
func TestActionsPermissionsAPI(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1}) // user2/repo1
	contentsURL := "/api/v1/repos/user2/repo1/contents/README.md"
	createIssue := func(t *testing.T, token string, status int) {
		t.Helper()
		req := NewRequestWithJSON(t, "POST", "/api/v1/repos/user2/repo1/issues", &api.CreateIssueOption{Title: "from a job"}).AddTokenAuth(token)
		MakeRequest(t, req, status)
	}
	// Creating a release writes the contents of the repository through the API.
	createRelease := func(t *testing.T, token, tag string, status int) {
		t.Helper()
		req := NewRequestWithJSON(t, "POST", "/api/v1/repos/user2/repo1/releases", &api.CreateReleaseOption{
			TagName: tag, Target: "master", Title: tag,
		}).AddTokenAuth(token)
		MakeRequest(t, req, status)
	}

	t.Run("no permissions keeps the default capability", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		task := newPermissionsTask(t, repo, permissionsTask{})
		MakeRequest(t, NewRequest(t, "GET", contentsURL).AddTokenAuth(task.Token), http.StatusOK)
		createIssue(t, task.Token, http.StatusCreated)
		createRelease(t, task.Token, "job-default", http.StatusCreated)
	})

	t.Run("nothing", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		task := newPermissionsTask(t, repo, permissionsTask{Permissions: "{}"})
		assert.Equal(t, map[string]string{"actions": "none", "contents": "none", "issues": "none", "packages": "none", "pull-requests": "none", "repository-projects": "none"}, storedTaskPermissions(t, task))
		MakeRequest(t, NewRequest(t, "GET", contentsURL).AddTokenAuth(task.Token), http.StatusNotFound)
		createIssue(t, task.Token, http.StatusNotFound)
		createRelease(t, task.Token, "job-nothing", http.StatusNotFound)
	})

	t.Run("contents read", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		task := newPermissionsTask(t, repo, permissionsTask{Permissions: "{contents: read}"})
		MakeRequest(t, NewRequest(t, "GET", contentsURL).AddTokenAuth(task.Token), http.StatusOK)
		createRelease(t, task.Token, "job-read", http.StatusForbidden)
		createIssue(t, task.Token, http.StatusNotFound)
	})

	t.Run("contents write is not issues write", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		task := newPermissionsTask(t, repo, permissionsTask{Permissions: "{contents: write}"})
		createRelease(t, task.Token, "job-write", http.StatusCreated)
		createIssue(t, task.Token, http.StatusNotFound)
	})

	t.Run("issues write", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		task := newPermissionsTask(t, repo, permissionsTask{Permissions: "{issues: write}"})
		createIssue(t, task.Token, http.StatusCreated)
		MakeRequest(t, NewRequest(t, "GET", contentsURL).AddTokenAuth(task.Token), http.StatusForbidden)
		createRelease(t, task.Token, "job-issues", http.StatusForbidden)
	})

	t.Run("a key that Forgejo does not enforce grants nothing", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		task := newPermissionsTask(t, repo, permissionsTask{Permissions: "{checks: write, statuses: write, id-token: write}"})
		MakeRequest(t, NewRequest(t, "GET", contentsURL).AddTokenAuth(task.Token), http.StatusNotFound)
		createIssue(t, task.Token, http.StatusNotFound)
	})

	t.Run("fork pull request with write-all", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		task := newPermissionsTask(t, repo, permissionsTask{Permissions: "write-all", IsFork: true, AllowPackagesWrite: true})
		MakeRequest(t, NewRequest(t, "GET", contentsURL).AddTokenAuth(task.Token), http.StatusOK)
		createRelease(t, task.Token, "job-fork", http.StatusForbidden)
	})
}

// TestActionsPermissionsGit: `contents` decides whether the job token can push to its repository. Fetching it is what
// `actions/checkout` does with the token, so that stays possible whatever the workflow says.
func TestActionsPermissionsGit(t *testing.T) {
	onApplicationRun(t, func(t *testing.T, u *url.URL) {
		repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1}) // user2/repo1

		for _, tc := range []struct {
			name  string
			task  permissionsTask
			clone bool
			push  bool
		}{
			{"no permissions", permissionsTask{}, true, true},
			{"contents write", permissionsTask{Permissions: "{contents: write}"}, true, true},
			{"contents read", permissionsTask{Permissions: "{contents: read}"}, true, false},
			{"nothing", permissionsTask{Permissions: "{}"}, true, false},
			{"contents none", permissionsTask{Permissions: "{contents: none, issues: write}"}, true, false},
			{"fork pull request with write-all", permissionsTask{Permissions: "write-all", IsFork: true}, true, false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				defer tests.PrintCurrentTest(t)()
				task := newPermissionsTask(t, repo, tc.task)

				remote := *u
				remote.User = url.UserPassword("token", task.Token)
				remote.Path = "/user2/repo1.git"

				dst := t.TempDir()
				if tc.clone {
					doGitClone(dst, &remote)(t)
				}

				require.NoError(t, os.WriteFile(path.Join(dst, "from-a-job"), []byte(tc.name), 0o666))
				require.NoError(t, git.AddChanges(dst, true))
				signature := &git.Signature{Email: "user2@example.com", Name: "user2", When: time.Now()}
				require.NoError(t, git.CommitChanges(dst, git.CommitChangesOptions{Committer: signature, Author: signature, Message: "from a job"}))

				branch := "refs/heads/job-" + fmt.Sprint(time.Now().UnixNano())
				err := git.NewCommand(git.DefaultContext, "push", "origin").AddDynamicArguments("HEAD:" + branch).Run(&git.RunOpts{Dir: dst})
				if tc.push {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
			})
		}
	})
}

// A task stores the effective permissions of its job when it is created.
func TestActionsPermissionsTaskCreation(t *testing.T) {
	defer unittest.OverrideFixtures("tests/integration/fixtures/TestActionsPermissions")()
	defer tests.PrepareTestEnv(t)()

	createTask := func(t *testing.T, runnerID int64) *actions_model.ActionTask {
		t.Helper()
		runner := unittest.AssertExistsAndLoadBean(t, &actions_model.ActionRunner{ID: runnerID})
		task, err := actions_model.CreateTaskForRunner(t.Context(), runner, nil, nil)
		require.NoError(t, err)
		return task
	}

	t.Run("job permissions, setting on", func(t *testing.T) {
		setActionsPackagesWrite(t, 1, true)
		task := createTask(t, 1011)
		assert.Equal(t, map[string]string{"actions": "none", "contents": "read", "issues": "none", "packages": "write", "pull-requests": "none", "repository-projects": "none"}, storedTaskPermissions(t, task))
	})

	t.Run("workflow permissions are nothing", func(t *testing.T) {
		setActionsPackagesWrite(t, 1, true)
		task := createTask(t, 1012)
		assert.Equal(t, map[string]string{"actions": "none", "contents": "none", "issues": "none", "packages": "none", "pull-requests": "none", "repository-projects": "none"}, storedTaskPermissions(t, task))
	})

	t.Run("no permissions", func(t *testing.T) {
		setActionsPackagesWrite(t, 1, true)
		task := createTask(t, 1013)
		assert.Empty(t, task.Permissions)
		assert.Nil(t, storedTaskPermissions(t, task))
	})

	t.Run("fork pull request with write-all", func(t *testing.T) {
		setActionsPackagesWrite(t, 1, true)
		task := createTask(t, 1014)
		assert.True(t, task.IsForkPullRequest)
		assert.Equal(t, map[string]string{"actions": "read", "contents": "read", "issues": "read", "packages": "read", "pull-requests": "read", "repository-projects": "read"}, storedTaskPermissions(t, task))
	})
}
