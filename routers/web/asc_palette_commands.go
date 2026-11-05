// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package web

import (
	"net/url"
	"strconv"
	"strings"

	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/context"
)

// The Commands group of the palette (board Shell, 04): what the person can do to the repository or the change they
// are on. Typing a command verb (undo, restack, merge, ask) switches the palette to these. Every command goes to a page
// that shows what it would do and asks before it writes; the checks of that page are the ones that hold.

// ascPaletteVerbs are the verbs that switch the palette to its commands, by the command they name.
var ascPaletteVerbs = []string{"undo", "restack", "merge", "ask"}

// ascPaletteVerb is the verb the first word of q starts (two letters or more), or "".
func ascPaletteVerb(q string) string {
	f := strings.Fields(strings.ToLower(q))
	if len(f) == 0 || len(f[0]) < 2 {
		return ""
	}
	for _, v := range ascPaletteVerbs {
		if strings.HasPrefix(v, f[0]) {
			return v
		}
	}
	return ""
}

// ascPaletteAt is the repository path and the pull request number of at, the path of the page the palette is open
// on: /{owner}/{repo}, and /{owner}/{repo}/pulls/{n} on a change. ok is false for a page outside a repository.
func ascPaletteAt(at string) (owner, name string, pull int64, ok bool) {
	at = strings.TrimPrefix(at, setting.AppSubURL)
	parts := strings.Split(strings.Trim(at, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" || strings.HasPrefix(parts[0], "-") {
		return "", "", 0, false
	}
	if len(parts) >= 4 && parts[2] == "pulls" {
		if n, err := strconv.ParseInt(parts[3], 10, 64); err == nil && n > 0 {
			pull = n
		}
	}
	return parts[0], parts[1], pull, true
}

type ascPaletteCommand struct {
	verb string
	item AscPaletteItem
}

// ascPaletteCommandsAt lists the commands of the page at for the signed-in person: Undo last operation for a writer
// of the code, Restack this stack and Merge when ready on an open change, and Ask an agent to change this… when the
// person turned agent features on.
func ascPaletteCommandsAt(ctx *context.Context, at string) []ascPaletteCommand {
	if !ctx.IsSigned || !setting.Asc.Enabled {
		return nil
	}
	owner, name, n, ok := ascPaletteAt(at)
	if !ok {
		return nil
	}
	repo, err := repo_model.GetRepositoryByOwnerAndName(ctx, owner, name)
	if err != nil {
		return nil
	}
	perm, err := access.GetUserRepoPermission(ctx, repo, ctx.Doer)
	if err != nil || !perm.CanWrite(unit.TypeCode) || repo.IsArchived {
		return nil
	}
	l := ctx.Locale
	link := repo.Link()
	out := []ascPaletteCommand{{verb: "undo", item: AscPaletteItem{
		Title: l.TrString("asc_shell.palette_cmd_undo"), Sub: l.TrString("asc_shell.palette_cmd_undo_sub", repo.FullName()),
		Href: link + "/asc/ops/undo/last",
	}}}
	target := ""
	if n > 0 && perm.CanRead(unit.TypePullRequests) {
		if pr, err := issues_model.GetPullRequestByIndex(ctx, repo.ID, n); err == nil && !pr.HasMerged && pr.LoadIssue(ctx) == nil && !pr.Issue.IsClosed {
			pull := link + "/pulls/" + strconv.FormatInt(n, 10)
			num := "#" + strconv.FormatInt(n, 10)
			out = append(out,
				ascPaletteCommand{verb: "restack", item: AscPaletteItem{
					Title: l.TrString("asc_shell.palette_cmd_restack"), Sub: l.TrString("asc_shell.palette_cmd_restack_sub", num),
					Href: pull + "/asc/confirm?op=restack",
				}},
				ascPaletteCommand{verb: "merge", item: AscPaletteItem{
					Title: l.TrString("asc_shell.palette_cmd_merge"), Sub: l.TrString("asc_shell.palette_cmd_merge_sub", num),
					Href: pull + "/asc/confirm?op=queue",
				}})
			if pr.HeadRepoID == repo.ID {
				target = pr.HeadBranch
			}
		}
	}
	if ascagentui.FeaturesEnabled(ctx, ctx.Doer) {
		href := link + "/asc/agents/new"
		on := repo.DefaultBranch
		if target != "" {
			href += "?" + url.Values{"target": {target}}.Encode()
			on = target
		}
		out = append(out, ascPaletteCommand{verb: "ask", item: AscPaletteItem{
			Title: l.TrString("asc_shell.palette_cmd_agent"), Sub: l.TrString("asc_shell.palette_cmd_agent_sub", on), Href: href,
		}})
	}
	return out
}

// ascPaletteCommands is the Commands group for q on the page at, and whether q starts with a command verb. With a
// verb the group holds the commands of that verb; otherwise the commands whose title holds q.
func ascPaletteCommands(ctx *context.Context, q, at string) (AscPaletteGroup, bool) {
	g := AscPaletteGroup{Key: "commands", Label: ctx.Locale.TrString("asc_shell.palette_commands")}
	verb := ascPaletteVerb(q)
	lower := strings.ToLower(q)
	for _, c := range ascPaletteCommandsAt(ctx, at) {
		if c.verb == verb || (verb == "" && strings.Contains(strings.ToLower(c.item.Title), lower)) {
			g.Items = append(g.Items, c.item)
		}
	}
	return g, verb != ""
}
