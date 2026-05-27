// Copyright 2026 The Forgejo Authors. All rights reserved.
// Copyright 2019 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package migrations

import (
	"net"
	"path/filepath"
	"testing"

	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/migration"
	"forgejo.org/modules/setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrateWhiteBlocklist(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	adminUser := unittest.AssertExistsAndLoadBean(t, &user_model.User{Name: "user1"})
	nonAdminUser := unittest.AssertExistsAndLoadBean(t, &user_model.User{Name: "user2"})

	setting.Migrations.AllowedDomains = "github.com"
	setting.Migrations.AllowLocalNetworks = false
	require.NoError(t, Init())

	err := IsMigrateURLAllowed("https://gitlab.com/gitlab/gitlab.git", nonAdminUser)
	require.Error(t, err)

	err = IsMigrateURLAllowed("https://github.com/go-gitea/gitea.git", nonAdminUser)
	require.NoError(t, err)

	err = IsMigrateURLAllowed("https://gITHUb.com/go-gitea/gitea.git", nonAdminUser)
	require.NoError(t, err)

	setting.Migrations.AllowedDomains = ""
	setting.Migrations.BlockedDomains = "github.com"
	require.NoError(t, Init())

	err = IsMigrateURLAllowed("https://gitlab.com/gitlab/gitlab.git", nonAdminUser)
	require.NoError(t, err)

	err = IsMigrateURLAllowed("https://github.com/go-gitea/gitea.git", nonAdminUser)
	require.Error(t, err)

	err = IsMigrateURLAllowed("https://10.0.0.1/go-gitea/gitea.git", nonAdminUser)
	require.Error(t, err)

	setting.Migrations.AllowLocalNetworks = true
	require.NoError(t, Init())
	err = IsMigrateURLAllowed("https://10.0.0.1/go-gitea/gitea.git", nonAdminUser)
	require.NoError(t, err)

	old := setting.ImportLocalPaths
	setting.ImportLocalPaths = false

	err = IsMigrateURLAllowed("/home/foo/bar/goo", adminUser)
	require.Error(t, err)

	setting.ImportLocalPaths = true
	abs, err := filepath.Abs(".")
	require.NoError(t, err)

	err = IsMigrateURLAllowed(abs, adminUser)
	require.NoError(t, err)

	err = IsMigrateURLAllowed(abs, nonAdminUser)
	require.Error(t, err)

	nonAdminUser.AllowImportLocal = true
	err = IsMigrateURLAllowed(abs, nonAdminUser)
	require.NoError(t, err)

	setting.ImportLocalPaths = old
}

func TestAllowBlockList(t *testing.T) {
	init := func(allow, block string, local bool) {
		setting.Migrations.AllowedDomains = allow
		setting.Migrations.BlockedDomains = block
		setting.Migrations.AllowLocalNetworks = local
		require.NoError(t, Init())
	}

	// default, allow all external, block none, no local networks
	init("", "", false)
	require.NoError(t, checkByAllowBlockList("domain.com", []net.IP{net.ParseIP("1.2.3.4")}))
	require.Error(t, checkByAllowBlockList("domain.com", []net.IP{net.ParseIP("127.0.0.1")}))

	// allow all including local networks (it could lead to SSRF in production)
	init("", "", true)
	require.NoError(t, checkByAllowBlockList("domain.com", []net.IP{net.ParseIP("1.2.3.4")}))
	require.NoError(t, checkByAllowBlockList("domain.com", []net.IP{net.ParseIP("127.0.0.1")}))

	// allow wildcard, block some subdomains. if the domain name is allowed, then the local network check is skipped
	init("*.domain.com", "blocked.domain.com", false)
	require.NoError(t, checkByAllowBlockList("sub.domain.com", []net.IP{net.ParseIP("1.2.3.4")}))
	require.NoError(t, checkByAllowBlockList("sub.domain.com", []net.IP{net.ParseIP("127.0.0.1")}))
	require.Error(t, checkByAllowBlockList("blocked.domain.com", []net.IP{net.ParseIP("1.2.3.4")}))
	require.Error(t, checkByAllowBlockList("sub.other.com", []net.IP{net.ParseIP("1.2.3.4")}))

	// allow wildcard (it could lead to SSRF in production)
	init("*", "", false)
	require.NoError(t, checkByAllowBlockList("domain.com", []net.IP{net.ParseIP("1.2.3.4")}))
	require.NoError(t, checkByAllowBlockList("domain.com", []net.IP{net.ParseIP("127.0.0.1")}))

	// local network can still be blocked
	init("*", "127.0.0.*", false)
	require.NoError(t, checkByAllowBlockList("domain.com", []net.IP{net.ParseIP("1.2.3.4")}))
	require.Error(t, checkByAllowBlockList("domain.com", []net.IP{net.ParseIP("127.0.0.1")}))

	// reset
	init("", "", false)
}

func TestURLAllowedSSH(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	user := unittest.AssertExistsAndLoadBean(t, &user_model.User{Name: "user2"})
	sshURL := "ssh://git@git.gay/gitgay/forgejo"

	t.Run("Migrate URL", func(t *testing.T) {
		require.Error(t, IsMigrateURLAllowed(sshURL, user))
	})

	t.Run("Pushmirror URL", func(t *testing.T) {
		require.NoError(t, IsPushMirrorURLAllowed(sshURL, user))
	})
}

var _ migration.Uploader = nullUploader{}

type nullUploader struct{}

func (nullUploader) MaxBatchInsertSize(string) int {
	return 1
}

func (nullUploader) CreateRepo(*migration.Repository, migration.MigrateOptions) error {
	return nil
}

func (nullUploader) CreateTopics(...string) error {
	return nil
}

func (nullUploader) CreateMilestones(...*migration.Milestone) error {
	return nil
}

func (nullUploader) CreateReleases(...*migration.Release) error {
	return nil
}

func (nullUploader) SyncTags() error {
	return nil
}

func (nullUploader) CreateLabels(...*migration.Label) error {
	return nil
}

func (nullUploader) CreateIssues(...*migration.Issue) error {
	return nil
}

func (nullUploader) CreateComments(...*migration.Comment) error {
	return nil
}

func (nullUploader) CreatePullRequests(...*migration.PullRequest) error {
	return nil
}

func (nullUploader) CreateReviews(...*migration.Review) error {
	return nil
}

func (nullUploader) Rollback() error {
	return nil
}

func (nullUploader) Finish() error {
	return nil
}

func (nullUploader) Close() {}

type testingDownloader struct {
	migration.NullDownloader
}

func (testingDownloader) GetRepoInfo() (*migration.Repository, error) {
	return &migration.Repository{
		CloneURL: "https://codeberg.org/forgejo-contrib/delightful-forgejo.git",
	}, nil
}

func (testingDownloader) GetIssues(page, _ int) ([]*migration.Issue, bool, error) {
	return make([]*migration.Issue, 1), page == 2, nil
}

func (testingDownloader) GetPullRequests(page, _ int) ([]*migration.PullRequest, bool, error) {
	return make([]*migration.PullRequest, 1), page == 3, nil
}

func TestMigrateRepository(t *testing.T) {
	messages := []struct {
		key  string
		args []any
	}{
		{key: "migrate.in_progress.git"},
		{key: "migrate.in_progress.topics"},
		{key: "migrate.in_progress.issues"},
		{key: "migrate.in_progress.issues.progress", args: []any{1}},
		{key: "migrate.in_progress.issues.progress", args: []any{2}},
		{key: "migrate.in_progress.pulls"},
		{key: "migrate.in_progress.pulls.progress", args: []any{1}},
		{key: "migrate.in_progress.pulls.progress", args: []any{2}},
		{key: "migrate.in_progress.pulls.progress", args: []any{3}},
	}
	messenger := func(key string, args ...any) {
		if assert.NotEmpty(t, messages, key) {
			assert.Equal(t, messages[0].key, key)
			assert.Equal(t, messages[0].args, args)
			messages = messages[1:]
		}
	}

	require.NoError(t, migrateRepository(nil, nil, testingDownloader{}, nullUploader{}, migration.MigrateOptions{
		PullRequests: true,
		Issues:       true,
	}, messenger))

	assert.Empty(t, messages)
}
