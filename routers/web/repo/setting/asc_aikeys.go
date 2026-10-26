// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

// The "AI keys" page of a repository (/{owner}/{repo}/settings/asc/ai) is served by the same resolver and
// routes as the Storage page (asc_storage.go): AI provider keys that only the agents and workflows of this
// repository use. Its template is tplSettingsAscAI.
