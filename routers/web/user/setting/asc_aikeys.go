// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

// The "AI keys" page of the account settings (/user/settings/asc/ai) is served by the same resolver and
// routes as the Storage page (asc_storage.go): the AI provider keys of the signed-in user, which only that
// user's own agents, workflows and semantic features use. Its template is tplSettingsAscAI.
