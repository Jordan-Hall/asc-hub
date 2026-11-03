// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	"context"

	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascbyo"

	"golang.org/x/sync/errgroup"
)

// orgStorageAsks is how many organizations are asked about at the same time.
const orgStorageAsks = 8

// OrgStorage is what the Storage column of the organizations page says for one organization: the names of its enabled
// storage targets, none for the server default, or Unknown when asc could not be asked.
type OrgStorage struct {
	Names   []string
	Unknown bool
}

// More is the number of enabled targets after the first.
func (s OrgStorage) More() int {
	return max(len(s.Names)-1, 0)
}

// loadOrgStorage asks asc for the storage targets of each organization. Without asc every organization uses the server
// default.
func loadOrgStorage(ctx context.Context, orgs []*user_model.User) map[int64]OrgStorage {
	out := make(map[int64]OrgStorage, len(orgs))
	if len(orgs) == 0 {
		return out
	}
	svc, err := ascbyo.New()
	if err != nil {
		for _, o := range orgs {
			out[o.ID] = OrgStorage{Unknown: setting.Asc.Enabled}
		}
		return out
	}
	got := make([]OrgStorage, len(orgs))
	var g errgroup.Group
	g.SetLimit(orgStorageAsks)
	for i, o := range orgs {
		g.Go(func() error {
			got[i] = orgStorageOf(ctx, svc, o.Name)
			return nil
		})
	}
	_ = g.Wait()
	for i, o := range orgs {
		out[o.ID] = got[i]
	}
	return out
}

func orgStorageOf(ctx context.Context, svc *ascbyo.Service, name string) OrgStorage {
	sc, err := ascbyo.NewScope(ascbyo.KindOrg, name)
	if err != nil {
		return OrgStorage{Unknown: true}
	}
	targets, err := svc.ListTargets(ctx, sc)
	if err != nil {
		return OrgStorage{Unknown: true}
	}
	var s OrgStorage
	for _, t := range targets {
		if t.GetState() == ascv1.StorageTargetState_STORAGE_TARGET_STATE_ENABLED {
			s.Names = append(s.Names, t.GetName())
		}
	}
	return s
}
