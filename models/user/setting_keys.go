// Copyright 2021 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package user

const (
	// SettingsKeyHiddenCommentTypes is the setting key for hidden comment types
	SettingsKeyHiddenCommentTypes = "issue.hidden_comment_types"
	// SettingsKeyHideResolved is the setting key whether resolved conversations start collapsed ("false": expanded;
	// anything else, including no value: collapsed, as before the setting existed)
	SettingsKeyHideResolved = "asc.hide_resolved"
	// SettingsKeyDiffWhitespaceBehavior is the setting key for whitespace behavior of diff
	SettingsKeyDiffWhitespaceBehavior = "diff.whitespace_behaviour"
	// SettingsKeyShowOutdatedComments is the setting key whether or not to show outdated comments in PRs
	SettingsKeyShowOutdatedComments = "comment_code.show_outdated"
	// UserActivityPubPrivPem is user's private key
	UserActivityPubPrivPem = "activitypub.priv_pem"
	// UserActivityPubPubPem is user's public key
	UserActivityPubPubPem = "activitypub.pub_pem"
)
