// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package structs

import "time"

// AscInboxEntry is one pull request in the inbox of the authenticated user.
// swagger:model
type AscInboxEntry struct {
	// the id of the pull request
	PullRequestID int64 `json:"pull_request_id"`
	// the number of the pull request in its repository
	Number int64 `json:"number"`
	// the title of the pull request
	Title string `json:"title"`
	// the web address of the pull request
	HTMLURL string `json:"html_url"`
	// the full name (owner/name) of the repository the pull request is made against
	Repository string `json:"repository"`
	// whose turn it is: needs_review, returned, ready, blocked, waiting, drafts, queued or merged
	Section string `json:"section"`
	// whether the authenticated user pinned the pull request
	Pinned bool `json:"pinned"`
	// until when the authenticated user snoozed the pull request; null when not snoozed
	// swagger:strfmt date-time
	SnoozedUntil *time.Time `json:"snoozed_until"`
	// when the pull request was last updated
	// swagger:strfmt date-time
	Updated time.Time `json:"updated_at"`
}
