// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascclient

import ascv1 "forgejo.org/modules/ascproto/ascv1"

// MergeQueue returns the client of the asc.v1.MergeQueue service.
func (c *Client) MergeQueue() ascv1.MergeQueueClient { return ascv1.NewMergeQueueClient(c) }

// Stacks returns the client of the asc.v1.Stacks service (restack, revert of a landed stack, ref log, undo).
func (c *Client) Stacks() ascv1.StacksClient { return ascv1.NewStacksClient(c) }
