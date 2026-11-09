// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package ascsample keeps the sample project of the hub: a small public repository that a new person can look at
// before they make their own (board GetStarted, "Open the sample project"). It is made the first time someone asks for
// it, owned by a bot account that cannot sign in, and archived, so nobody can change it; an admin can unarchive or
// delete it like any repository, and it is made again on the next request after a delete.
package ascsample

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/optional"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/structs"
	repo_service "forgejo.org/services/repository"
	files_service "forgejo.org/services/repository/files"
)

// The names of the sample project.
const (
	OwnerName = "asc-sample"
	RepoName  = "sample-project"
)

// ErrNameTaken is returned when an account named OwnerName exists that is not a bot account.
var ErrNameTaken = errors.New("ascsample: the name of the sample project is taken")

// ErrOff is returned when [asc] SAMPLE_PROJECT is false.
var ErrOff = errors.New("ascsample: the sample project is turned off")

var mu sync.Mutex

// Ensure returns the sample project, making it first when it does not exist. Two requests at once make it once.
func Ensure(ctx context.Context) (*repo_model.Repository, error) {
	if !setting.Asc.SampleProject {
		return nil, ErrOff
	}
	mu.Lock()
	defer mu.Unlock()

	owner, err := ensureOwner(ctx)
	if err != nil {
		return nil, err
	}
	repo, err := repo_model.GetRepositoryByName(ctx, owner.ID, RepoName)
	if err == nil {
		// an admin may have changed it (unarchived it, say); it is still the sample
		return repo, nil
	}
	if !repo_model.IsErrRepoNotExist(err) {
		return nil, err
	}
	return create(ctx, owner)
}

func ensureOwner(ctx context.Context) (*user_model.User, error) {
	u, err := user_model.GetUserByName(ctx, OwnerName)
	if err == nil {
		if !u.IsBot() {
			return nil, ErrNameTaken
		}
		return u, nil
	}
	if !user_model.IsErrUserNotExist(err) {
		return nil, err
	}
	var pass [32]byte
	if _, err := rand.Read(pass[:]); err != nil {
		return nil, err
	}
	public := structs.VisibleTypePublic
	noReply := setting.Service.NoReplyAddress
	if noReply == "" {
		noReply = "noreply.invalid"
	}
	u = &user_model.User{
		Name:             OwnerName,
		LowerName:        OwnerName,
		FullName:         "Sample project",
		Description:      "Owns the read-only sample project of this hub.",
		Email:            OwnerName + "@" + noReply,
		Passwd:           hex.EncodeToString(pass[:]),
		Type:             user_model.UserTypeBot,
		ProhibitLogin:    true,
		IsActive:         true,
		KeepEmailPrivate: true,
		Visibility:       public,
	}
	if err := user_model.AdminCreateUser(ctx, u, &user_model.CreateUserOverwriteOptions{
		IsActive:         optional.Some(true),
		KeepEmailPrivate: optional.Some(true),
		Visibility:       &public,
	}); err != nil {
		return nil, fmt.Errorf("ascsample: the owner: %w", err)
	}
	return u, nil
}

// commit is one commit of the history of the sample.
type commit struct {
	message string
	files   map[string]string
}

// history is the content of the sample: a small service in three commits, so the history, the blame and the file
// tree have something to show.
var history = []commit{
	{
		message: "Add the payment retry policy",
		files: map[string]string{
			"src/retry/policy.rs": `// The retry policy of a payment call: how often and how long to wait.

pub struct Policy {
    pub attempts: u32,
    pub base_ms: u64,
}

impl Policy {
    pub fn delay_ms(&self, attempt: u32) -> u64 {
        self.base_ms * 2u64.pow(attempt.min(10))
    }
}
`,
			"src/lib.rs":       "pub mod retry;\n",
			"src/retry/mod.rs": "pub mod policy;\n",
		},
	},
	{
		message: "Wire the retry policy into the client",
		files: map[string]string{
			"src/client.rs": `use crate::retry::policy::Policy;

// The payment client tries a call again after a failure, waiting longer each time.
pub fn send<F: FnMut() -> Result<(), String>>(policy: &Policy, mut call: F) -> Result<(), String> {
    let mut last = String::new();
    for attempt in 0..policy.attempts {
        match call() {
            Ok(()) => return Ok(()),
            Err(e) => {
                last = e;
                std::thread::sleep(std::time::Duration::from_millis(policy.delay_ms(attempt)));
            }
        }
    }
    Err(last)
}
`,
			"src/lib.rs": "pub mod client;\npub mod retry;\n",
		},
	},
	{
		message: "Document how to work on the sample",
		files: map[string]string{
			"docs/STACKS.md": `# Working in stacks

Each change is small and reviewed on its own. A stack is a row of changes, each on top of the one before.

    asc init acme/payments
    asc stack submit

Press Ctrl K anywhere on the hub to open the palette.
`,
		},
	},
}

const readme = `# Sample project

This is the read-only sample project of this hub. Look around: the files, the history, the blame of a file and the
palette (Ctrl K). Nothing here can be changed; make your own repository to try changes and reviews.
`

// create makes the sample. A failure on the way deletes what was made, so the next request starts again.
func create(ctx context.Context, owner *user_model.User) (*repo_model.Repository, error) {
	repo, err := fill(ctx, owner)
	if err != nil && repo != nil {
		if derr := repo_service.DeleteRepositoryDirectly(ctx, repo.ID, repo_service.DeleteRepositoryOpts{}); derr != nil {
			return nil, errors.Join(err, fmt.Errorf("ascsample: delete the half-made sample: %w", derr))
		}
		return nil, err
	}
	return repo, err
}

func fill(ctx context.Context, owner *user_model.User) (*repo_model.Repository, error) {
	repo, err := repo_service.CreateRepositoryDirectly(ctx, owner, owner, repo_service.CreateRepoOptions{
		Name:          RepoName,
		Description:   "A small service to look around in. Read-only.",
		DefaultBranch: "main",
		IsPrivate:     false,
	})
	if err != nil {
		return nil, fmt.Errorf("ascsample: the repository: %w", err)
	}
	steps := append([]commit{{message: "Start the sample project", files: map[string]string{"README.md": readme}}}, history...)
	last := ""
	for i, c := range steps {
		// an update names the commit it is made on, so the hub knows the file has not changed since
		opts := &files_service.ChangeRepoFilesOptions{Message: c.message, OldBranch: "main", NewBranch: "main", LastCommitID: last}
		if i == 0 {
			// the repository is empty: the first commit makes the branch
			opts.OldBranch = ""
		}
		for _, path := range slices.Sorted(maps.Keys(c.files)) {
			op := "create"
			if exists(steps[:i], path) {
				op = "update"
			}
			opts.Files = append(opts.Files, &files_service.ChangeRepoFile{Operation: op, TreePath: path, ContentReader: strings.NewReader(c.files[path])})
		}
		resp, err := files_service.ChangeRepoFiles(ctx, repo, owner, opts)
		if err != nil {
			return repo, fmt.Errorf("ascsample: commit %q: %w", c.message, err)
		}
		last = resp.Commit.SHA
		if i == 0 {
			fresh, err := repo_model.GetRepositoryByID(ctx, repo.ID)
			if err != nil {
				return repo, err
			}
			repo = fresh
		}
	}
	if err := repo_model.SetArchiveRepoState(ctx, repo, true); err != nil {
		return repo, fmt.Errorf("ascsample: archive: %w", err)
	}
	return repo_model.GetRepositoryByID(ctx, repo.ID)
}

func exists(before []commit, path string) bool {
	for _, c := range before {
		if _, ok := c.files[path]; ok {
			return true
		}
	}
	return false
}
