// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package main

// kind says why a file may still run git itself.
type kind string

const (
	// kindProtocol is code that is a git hook, or that speaks the git transport protocol, against the bare repository.
	kindProtocol kind = "protocol"
	// kindBridge is a mirror, migration or clone path. These move behind services/ascbridge (K13).
	kindBridge kind = "bridge"
	// kindLifecycle maintains the bare repository directory itself, which an asc-backed repository does not have.
	kindLifecycle kind = "lifecycle"
	// kindGap is a read or write for which Backend or repostore has no method yet.
	kindGap kind = "gap"
	// kindProbe only checks that the git binary is installed. It runs nothing.
	kindProbe kind = "probe"
	// kindHistorical is a released database migration. It is frozen and must keep working on a bare repository.
	kindHistorical kind = "historical"
)

var validKinds = map[kind]bool{
	kindProtocol:   true,
	kindBridge:     true,
	kindLifecycle:  true,
	kindGap:        true,
	kindHistorical: true,
	kindProbe:      true,
}

// exception allows a file to run git itself, up to Max times. Paths are slash-separated and relative to the repository root.
//
// The table is a ratchet. Adding an entry or raising Max needs a reason that a reviewer accepts. Lowering Max, or deleting
// an entry once the last use is gone, is required: the check fails until you do it.
type exception struct {
	Path   string
	Max    int
	Kind   kind
	Reason string
	Retire string // the unit that removes the entry
}

var exceptions = []exception{
	// Hooks and the git transport.
	{"cmd/hook.go", 1, kindProtocol, "the post-update hook runs git update-server-info inside the hook process", "K13"},
	{"cmd/serv.go", 2, kindProtocol, "the SSH command proxies git-upload-pack and git-receive-pack to the bare repository", "K13"},
	{"routers/private/hook_pre_receive.go", 1, kindProtocol, "the force-push check reads objects that exist only in the push quarantine", "K09"},
	{"routers/private/hook_verification.go", 3, kindProtocol, "commit verification reads objects that exist only in the push quarantine", "K09"},
	{"routers/web/repo/githttp.go", 5, kindProtocol, "the smart HTTP handler runs upload-pack and receive-pack on the bare repository", "K13"},

	// Mirrors, migrations and clones.
	{"services/migrations/dump.go", 4, kindBridge, "restoring a dump fetches pull request heads into the bare repository", "K13"},
	{"services/migrations/gitea_uploader.go", 4, kindBridge, "migration fetches pull request heads into the bare repository", "K13"},
	{"services/mirror/mirror_pull.go", 8, kindBridge, "pull mirrors run git remote and git fetch against the bare repository", "K13"},
	{"services/mirror/mirror_push.go", 8, kindBridge, "push mirrors configure remotes in the bare repository", "K13"},
	{"services/repository/migrate.go", 4, kindBridge, "migration sets up and cleans the remotes of the bare repository", "K13"},

	// The bare repository directory.
	{"services/doctor/heads.go", 3, kindLifecycle, "doctor repairs HEAD of the bare repository", "K13"},
	{"services/doctor/misc.go", 2, kindLifecycle, "doctor reads and sets git config in the bare repository", "K13"},
	{"services/repository/adopt.go", 1, kindLifecycle, "update-server-info on the adopted bare repository", "K13"},
	{"services/repository/check.go", 1, kindLifecycle, "git gc on the bare repository", "K13"},
	{"services/repository/create.go", 1, kindLifecycle, "update-server-info on the new bare repository", "K13"},
	{"services/repository/fork.go", 1, kindLifecycle, "update-server-info on the forked bare repository", "K13"},
	{"services/repository/generate.go", 1, kindLifecycle, "update-server-info on the generated bare repository", "K13"},

	// Missing Backend or repostore methods.
	{"modules/gitrepo/branch.go", 1, kindGap, "SetDefaultBranch runs symbolic-ref; Backend.SetHead does the same and is not wired in yet", "K08"},
	{"routers/web/repo/pull.go", 1, kindGap, "reads the parent of a commit with rev-list --skip; Backend.Log can do it", "K10"},
	{"services/asymkey/sign.go", 4, kindGap, "reads the repository git config for signing settings; Backend has no config method", "K10"},
	{"services/gitdiff/gitdiff.go", 1, kindGap, "git diff with whitespace flags, skip-to and pathspecs; DiffOpts has none of them", "K10"},
	{"services/pull/check.go", 1, kindGap, "rev-list --ancestry-path --merges; LogQuery cannot express it", "K10"},
	{"services/repository/contributors_graph.go", 1, kindGap, "git log --shortstat; Backend has no per-commit statistics stream", "K10"},
	{"services/repository/gitgraph/graph.go", 1, kindGap, "git log --graph; Backend has no graph layout", "K10"},

	// Probes.
	{"routers/install/install.go", 1, kindProbe, "the install page checks that the git binary exists", "none, git stays required"},

	// Released migrations.
	{"models/gitea_migrations/v1_12/v128.go", 4, kindHistorical, "a released migration that reads merge bases from the bare repository", "none, frozen"},
	{"models/gitea_migrations/v1_12/v134.go", 2, kindHistorical, "a released migration that reads merge bases from the bare repository", "none, frozen"},
}
