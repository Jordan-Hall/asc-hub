// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package byoweb

import (
	"forgejo.org/modules/web"
	svc "forgejo.org/services/ascbyo"
	"forgejo.org/services/context"
)

// Resolver builds the Level of a request: it checks that the signed-in user administers the scope and
// returns nil after writing the refusal when not. It must fail closed.
type Resolver func(ctx *context.Context) *Level

// Routes registers the pages below "/asc" on m. m is the settings group of the level, which already
// requires a signed-in user (and, for an organisation or a repository, its owner or administrator); the
// resolver checks again.
//
//	GET  /asc/storage                              list and add form
//	POST /asc/storage/targets                      add
//	GET  /asc/storage/targets/{id}                 details, edit form
//	POST /asc/storage/targets/{id}                 edit
//	POST /asc/storage/targets/{id}/verify          write, read and delete a probe object
//	POST /asc/storage/targets/{id}/enable          use the target
//	POST /asc/storage/targets/{id}/disable         stop using the target
//	POST /asc/storage/targets/{id}/delete          remove the target
//
// and, for a repository only:
//
//	POST /asc/storage/assign                       new objects go to a target
//	POST /asc/storage/migrations                   move the existing objects
//	POST /asc/storage/migrations/{id}/cancel       stop a migration
//	GET  /asc/storage/migrations/{id}/status       progress as JSON
//
//	GET  /asc/ai                                   list and add form
//	POST /asc/ai/keys                              add
//	GET  /asc/ai/keys/{id}                         details, edit form, usage
//	POST /asc/ai/keys/{id}                         edit
//	POST /asc/ai/keys/{id}/test                    one small request to the provider
//	POST /asc/ai/keys/{id}/enable                  use the key again
//	POST /asc/ai/keys/{id}/disable                 stop using the key
//	POST /asc/ai/keys/{id}/delete                  remove the key
//	POST /asc/ai/keys/{id}/shares                  share the key (account and organization keys; aishares.go)
//	POST /asc/ai/keys/{id}/shares/{share}/revoke   take a share back
//	POST /asc/ai/keys/{id}/caps                    warning percentage and stop rule of the cap
func Routes(m *web.Route, resolve Resolver) {
	h := func(fn func(*context.Context, *Level)) func(*context.Context) {
		return func(ctx *context.Context) {
			lvl := resolve(ctx)
			if lvl == nil {
				return
			}
			fn(ctx, lvl)
		}
	}
	with := func(fn func(*context.Context, *Level, bool), v bool) func(*context.Context, *Level) {
		return func(ctx *context.Context, lvl *Level) { fn(ctx, lvl, v) }
	}
	m.Group("/asc", func() {
		m.Group("/storage", func() {
			m.Get("", h(StorageIndex))
			m.Post("/targets", h(StorageCreate))
			m.Group("/targets/{id}", func() {
				m.Get("", h(StorageTarget))
				m.Post("", h(StorageUpdate))
				m.Post("/verify", h(StorageVerify))
				m.Post("/enable", h(with(StorageEnable, true)))
				m.Post("/disable", h(with(StorageEnable, false)))
				m.Post("/delete", h(StorageDelete))
			})
			m.Post("/assign", h(StorageAssign))
			m.Post("/migrations", h(MigrationStart))
			m.Post("/migrations/{id}/cancel", h(MigrationCancel))
			m.Get("/migrations/{id}/status", h(MigrationStatus))
		})
		m.Group("/ai", func() {
			m.Get("", h(AIIndex))
			m.Post("/keys", h(AICreate))
			m.Group("/keys/{id}", func() {
				m.Get("", h(AIKey))
				m.Post("", h(AIUpdate))
				m.Post("/test", h(AITest))
				m.Post("/enable", h(with(AIDisable, false)))
				m.Post("/disable", h(with(AIDisable, true)))
				m.Post("/delete", h(AIDelete))
				m.Post("/shares", h(AIShare))
				m.Post("/shares/{share}/revoke", h(AIShareRevoke))
				m.Post("/caps", h(AICaps))
			})
		})
	})
}

// NewLevel builds a Level and checks the scope name.
func NewLevel(kind svc.Kind, name, label, base string) (*Level, error) {
	sc, err := svc.NewScope(kind, name)
	if err != nil {
		return nil, err
	}
	return &Level{Scope: sc, Base: base, ScopeLabel: label}, nil
}
