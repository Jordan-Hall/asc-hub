// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"forgejo.org/modules/log"
	"forgejo.org/services/ascagentdir"
	"forgejo.org/services/context"
)

// ascProfileAgentsMax is how many agents the "Agents <name> runs" card names.
const ascProfileAgentsMax = 5

// AscProfileAgents is the "Agents <name> runs" card of a profile.
type AscProfileAgents struct {
	Agents []*ascagentdir.Agent
	Total  int
	More   int
	Self   bool
}

// ascProfileAgents returns the agents the person started or created runs of, in the asc repositories of the person
// and those the person can reach, as far as the viewer can read them; nil when the viewer gets no agents, asc could
// not be asked, or there are none and the viewer is not the person (the person gets the card with New agent).
func ascProfileAgents(ctx *context.Context) *AscProfileAgents {
	person := ctx.ContextUser
	if person.IsOrganization() || !ascagentdir.Enabled(ctx, ctx.Doer) {
		return nil
	}
	repos, err := ascagentdir.Repos(ctx, ctx.Doer, person.ID, true)
	if err != nil {
		log.Error("profile: the asc repositories of %s: %v", person.Name, err)
		return nil
	}
	agents, err := ascagentdir.Summarize(ctx, repos, person.Name)
	if err != nil {
		log.Warn("profile: the agents of %s (request %s): %v", person.Name, ctx.RequestIDForLog(), err)
		return nil
	}
	self := ctx.Doer.ID == person.ID
	if len(agents) == 0 && !self {
		return nil
	}
	shown := agents[:min(len(agents), ascProfileAgentsMax)]
	return &AscProfileAgents{Agents: shown, Total: len(agents), More: len(agents) - len(shown), Self: self}
}
