// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package webhook

import webhook_model "forgejo.org/models/webhook"

// ErrPayloadURLNotHTTPS is returned for a payload address that is not https and is not a private or loopback target
// the instance lets webhooks reach.
var ErrPayloadURLNotHTTPS = webhook_model.ErrPayloadURLNotHTTPS

// CheckPayloadURL is the transport rule of an ASC hub for the address of a webhook (see the model package, which also
// enforces it when a webhook is created or updated, so no entry point can skip it). Handlers call it first to answer
// with the form's own error.
func CheckPayloadURL(raw string) error { return webhook_model.CheckPayloadURL(raw) }
