// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"encoding/csv"
	"strings"

	"forgejo.org/modules/git"
	"forgejo.org/services/context"
)

// ExportCommits serves GET /{owner}/{repo}/commits-export/branch/{ref}: the history the person is looking at as a CSV
// file (id, author, author email, date, subject), the newest first, at most exportCommitsMax rows. The filter `q` of
// the history search applies. It is the same data the history page shows to a reader of the code, nothing more.
func ExportCommits(ctx *context.Context) {
	if ctx.Repo.Commit == nil {
		ctx.NotFound("Commit not found", nil)
		return
	}
	var commits []*git.Commit
	var err error
	if q := ctx.FormTrim("q"); q != "" {
		commits, err = ctx.Repo.Commit.SearchCommits(git.NewSearchCommitsOptions(q, ctx.FormBool("all")))
	} else {
		commits, err = ctx.Repo.Commit.CommitsByRange(1, exportCommitsMax, "")
	}
	if err != nil {
		ctx.ServerError("ExportCommits", err)
		return
	}
	if len(commits) > exportCommitsMax {
		commits = commits[:exportCommitsMax]
	}
	ctx.SetServeHeaders(&context.ServeHeaderOptions{ContentType: "text/csv; charset=utf-8", Filename: ctx.Repo.Repository.Name + "-history.csv"})
	w := csv.NewWriter(ctx.Resp)
	_ = w.Write([]string{"id", "author", "email", "date", "subject"})
	for _, c := range commits {
		_ = w.Write([]string{
			c.ID.String(), csvCell(c.Author.Name), csvCell(c.Author.Email),
			c.Author.When.UTC().Format("2006-01-02T15:04:05Z"), csvCell(c.Summary()),
		})
	}
	w.Flush()
}

const exportCommitsMax = 1000

// csvCell keeps a spreadsheet from running a cell as a formula: a cell that starts with = + - @ or a tab or return is
// prefixed with a quote.
func csvCell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}
