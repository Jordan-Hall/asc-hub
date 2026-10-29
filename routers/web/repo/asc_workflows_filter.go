// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import "strings"

// ascWorkflowCount is what the state chips of the workflow list show.
type ascWorkflowCount struct{ All, On, Off, Invalid int }

func ascWorkflowCounts(defs []ascWorkflowDef) ascWorkflowCount {
	c := ascWorkflowCount{All: len(defs)}
	for _, d := range defs {
		switch {
		case !d.Valid:
			c.Invalid++
		case d.Enabled:
			c.On++
		default:
			c.Off++
		}
	}
	return c
}

// ascWorkflowFilter keeps the definitions in a state ("on", "off", "invalid"; empty is all) whose name, description or
// path contains q (case-insensitive).
func ascWorkflowFilter(defs []ascWorkflowDef, state, q string) []ascWorkflowDef {
	q = strings.ToLower(strings.TrimSpace(q))
	if state == "" && q == "" {
		return defs
	}
	var out []ascWorkflowDef
	for _, d := range defs {
		switch state {
		case "on":
			if !d.Valid || !d.Enabled {
				continue
			}
		case "off":
			if !d.Valid || d.Enabled {
				continue
			}
		case "invalid":
			if d.Valid {
				continue
			}
		}
		if q != "" && !strings.Contains(strings.ToLower(d.Name+"\n"+d.Description+"\n"+d.Path), q) {
			continue
		}
		out = append(out, d)
	}
	return out
}

// ascWorkflowChain is "scan > update > review": the ids of the steps in order.
func ascWorkflowChain(steps []ascWorkflowStep) []string {
	out := make([]string, 0, len(steps))
	for _, s := range steps {
		out = append(out, s.ID)
	}
	return out
}
