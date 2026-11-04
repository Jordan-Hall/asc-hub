// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"strings"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// A new change is not opened when secret scanning would refuse what it adds (board NewChange, "Blocked"): the compare
// page says so above the button, and the create handler refuses with the same reason. asc answers with its push gate
// under the policy in force (SecretScan.Check, which records nothing). The push gate is the guard of the repository;
// this tells the author before they write the title and the brief, so an asc that cannot be asked blocks nothing.

// ascCompareSecrets asks asc what its push gate would do with what head adds to base in repo, as the signed-in person.
// nil means asc could not say.
var ascCompareSecrets = func(ctx *context.Context, repo *repo_model.Repository, base, head string) *asctrust.SecretCheck {
	if !setting.Asc.Enabled || repo == nil || repo.GetStorageBackend() != "asc" || ctx.Doer == nil || base == "" || head == "" {
		return nil
	}
	if !asctrust.HasFeature(ctx, asctrust.FeatureSecretScan) {
		return nil
	}
	name, err := asctrust.RepoName(repo)
	if err != nil {
		return nil
	}
	conn, err := asctrust.Conn()
	if err != nil {
		return nil
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallRepoWrite, repo)
	if err != nil {
		return nil
	}
	defer release()
	check, err := asctrust.CheckSecretsRange(callCtx, conn, name, base, head)
	if err != nil {
		log.Warn("asc secret check of %s: %v", repo.FullName(), asctrust.Classify(err))
		return nil
	}
	return check
}

// ascSecretBlock is the check of a compare inside repo when the push gate would refuse it, else nil. Only a writer
// opens a change from a branch of the repository itself, so the check is not made for a reader.
func ascSecretBlock(ctx *context.Context, repo *repo_model.Repository, base, head string) *asctrust.SecretCheck {
	if ctx.Repo == nil || !ctx.Repo.CanWrite(unit.TypeCode) {
		return nil
	}
	if c := ascCompareSecrets(ctx, repo, base, head); c != nil && c.Blocks {
		return c
	}
	return nil
}

// prepareCompareSecrets puts AscSecretBlock (with its paths as code, AscSecretBlockPaths) into ctx.Data for the compare page.
func prepareCompareSecrets(ctx *context.Context, base, head string) {
	if c := ascSecretBlock(ctx, ctx.Repo.Repository, base, head); c != nil {
		ctx.Data["AscSecretBlock"] = c
		ctx.Data["AscSecretBlockPaths"] = ascCodeList(c.Paths)
	}
}

// ascSecretRefusal is the reason the create handler gives when the push gate would refuse the compare, or "".
func ascSecretRefusal(ctx *context.Context, base, head string) string {
	c := ascSecretBlock(ctx, ctx.Repo.Repository, base, head)
	switch {
	case c == nil:
		return ""
	case len(c.Paths) == 0:
		return ctx.Locale.TrString("asc_rp.cmp_secret_unread_text")
	default:
		return ctx.Locale.TrString("asc_rp.cmp_secret_text", strings.Join(c.Paths, ", "))
	}
}
