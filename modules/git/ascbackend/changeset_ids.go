// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package ascbackend

import (
	"context"
	"encoding/hex"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git"
)

// ChangesetIDs maps git commit ids to the asc changeset ids of the same commits, as lowercase hex, in
// order. It is how Actions (K14) names a run's commit to asc. A commit the server has no changeset for
// is an error, not an empty id: the caller would otherwise hand a job a blank revision.
func (b *Backend) ChangesetIDs(ctx context.Context, ids []git.ObjectID) ([]string, error) {
	for _, id := range ids {
		if err := requireSha1(id); err != nil {
			return nil, err
		}
	}
	ctx, cancel, err := b.unary(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	out := make([]string, 0, len(ids))
	for start := 0; start < len(ids); start += maxQueryItems {
		chunk := ids[start:min(start+maxQueryItems, len(ids))]
		wire := make([][]byte, len(chunk))
		for i, id := range chunk {
			wire[i] = id.RawValue()
		}
		resp, err := b.api.GitMap(ctx, &ascv1.GitMapRequest{Repo: b.ascRef(), Git: wire})
		if err != nil {
			return nil, callErr("GitMap", "", err)
		}
		if len(resp.GetGitToAsc()) != len(chunk) {
			return nil, badAnswer("GitMap answered %d ids for %d", len(resp.GetGitToAsc()), len(chunk))
		}
		for i, raw := range resp.GetGitToAsc() {
			switch len(raw) {
			case 0:
				return nil, git.ErrNotExist{ID: chunk[i].String()}
			case ascIDLen:
				out = append(out, hex.EncodeToString(raw))
			default:
				return nil, badAnswer("changeset id of %d bytes", len(raw))
			}
		}
	}
	return out, nil
}
