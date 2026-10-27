// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"google.golang.org/grpc"
)

// MaxQuarantined bounds the quarantined groups the merge queue page lists.
const MaxQuarantined = 20

// Quarantined is the list of failure groups in quarantine: the failures that do not hold up a landing because a
// maintainer said so (and the policy of the repository allows it). The merge queue page shows them with the note that
// was written when the group was quarantined, so a person who wonders why a red check did not stop a change can read
// why. More says the repository has more than MaxQuarantined.
type Quarantined struct {
	Rows []GroupRow
	More bool
}

// LoadQuarantined reads the quarantined failure groups, most recently seen first.
func LoadQuarantined(ctx context.Context, conn grpc.ClientConnInterface, repoName string) (*Quarantined, error) {
	cctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	resp, err := ascv1.NewCiInsightsClient(conn).ListFailureGroups(cctx, &ascv1.ListFailureGroupsRequest{
		Repo:   RepoRef(repoName),
		States: []ascv1.FailureGroupState{ascv1.FailureGroupState_FAILURE_GROUP_STATE_QUARANTINED},
		Limit:  MaxQuarantined,
	})
	if err != nil {
		return nil, err
	}
	page, err := GroupsPageOf(resp)
	if err != nil {
		return nil, err
	}
	if len(page.Rows) > MaxQuarantined {
		return nil, ErrBadAnswer
	}
	q := &Quarantined{More: page.Next != ""}
	for _, r := range page.Rows {
		// A group that is not in the state asked for is an answer that cannot be trusted.
		if r.StateKey != "quarantined" {
			return nil, ErrBadAnswer
		}
		q.Rows = append(q.Rows, r)
	}
	return q, nil
}
