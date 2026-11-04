// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package webhook

import (
	"strings"
	"testing"

	webhook_model "forgejo.org/models/webhook"
	webhook_module "forgejo.org/modules/webhook"

	"github.com/stretchr/testify/assert"
)

func TestDelivery(t *testing.T) {
	d := &Delivery{&webhook_model.HookTask{
		UUID:           "7f3a9c21-0b4e-4d2a-9e51-2c7d8f0a1b33",
		EventType:      webhook_module.HookEventRelease,
		PayloadContent: `{"action":"published","release":{"tag_name":"v2.18.0"}}`,
		DurationMS:     1234,
		ResponseInfo:   &webhook_model.HookResponse{Status: 502, Body: "bad gateway"},
	}}
	assert.Equal(t, "7f3a9c21", d.ShortID())
	assert.Equal(t, "release published", d.Event())
	assert.Equal(t, "502 Bad Gateway", d.StatusLine())
	assert.Equal(t, "1.2", d.Seconds())

	// the request part falls back to the stored payload, indented
	lines, more := d.Body("request")
	assert.Equal(t, []string{"{", `  "action": "published",`, `  "release": {`, `    "tag_name": "v2.18.0"`, "  }", "}"}, lines)
	assert.Zero(t, more)
	assert.Equal(t, []string{"bad gateway"}, d.BodyLines("response"))

	// a pull request comment without an action names the event only; a waiting task has no status
	d = &Delivery{&webhook_model.HookTask{EventType: webhook_module.HookEventPullRequestComment, PayloadContent: "{}"}}
	assert.Equal(t, "pull request comment", d.Event())
	assert.Zero(t, d.Status())
	assert.Nil(t, d.BodyLines("response"))

	// an ASC event names itself
	d = &Delivery{&webhook_model.HookTask{EventType: webhook_module.HookEventAgentRunFinished, PayloadContent: `{"event":"agent_run.finished"}`}}
	assert.Equal(t, "agent run finished", d.Event())

	// a long body is cut
	d.PayloadContent = strings.Repeat("x\n", deliveryLines+5)
	assert.Len(t, d.BodyLines("request"), deliveryLines)
	assert.Equal(t, 5, d.BodyMore("request"))
}
