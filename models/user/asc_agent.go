// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

// IsAscAgent reports whether u is the account of an asc agent: a bot account (type bot, which cannot sign in), the
// account an agent comments, opens pull requests and is assigned as (agent-<name>, see services/ascagentui). The hub's
// built-in accounts (Ghost, Actions) are not agents. An agent cannot approve a pull request or merge one: a person does.
func (u *User) IsAscAgent() bool {
	return u != nil && u.ID > 0 && u.IsBot()
}
