// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascbyo

import (
	"context"
	"math"
	"regexp"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/timeutil"
)

// StorageUsageKind is one kind of stored bytes as shown.
type StorageUsageKind struct {
	Name    string // slug, stable for scripts
	Key     string // locale key of the label
	Bytes   uint64
	Objects uint64
}

// StorageUsage is what a repository keeps in storage and what was read from it this month.
type StorageUsage struct {
	// Kinds that hold bytes, in a fixed order.
	Kinds      []StorageUsageKind
	Total      uint64
	Objects    uint64
	ComputedAt timeutil.TimeStamp // zero when no scan has finished
	Computing  bool
	ReadBytes  uint64
	Month      string // YYYY-MM, or empty when the answer's was not one
}

var monthRe = regexp.MustCompile(`^[0-9]{4}-(0[1-9]|1[0-2])$`)

// usageKinds is the order and the labels of the kinds asc reports. A kind not named here is counted in the total
// only.
var usageKinds = []struct {
	kind ascv1.StorageUsageKind
	name string
}{
	{ascv1.StorageUsageKind_STORAGE_USAGE_KIND_HISTORY, "history"},
	{ascv1.StorageUsageKind_STORAGE_USAGE_KIND_FILES, "files"},
	{ascv1.StorageUsageKind_STORAGE_USAGE_KIND_LARGE_FILES, "large_files"},
	{ascv1.StorageUsageKind_STORAGE_USAGE_KIND_INDEXES, "indexes"},
	{ascv1.StorageUsageKind_STORAGE_USAGE_KIND_CI, "ci"},
	{ascv1.StorageUsageKind_STORAGE_USAGE_KIND_OTHER, "other"},
}

// StorageUsageFrom turns the answer into the view. The total is the sum of the rows, so the parts always add up.
func StorageUsageFrom(r *ascv1.GetRepoStorageUsageResponse) *StorageUsage {
	got := map[ascv1.StorageUsageKind]*ascv1.StorageUsageRow{}
	for _, row := range r.GetRows() {
		if _, dup := got[row.GetKind()]; !dup {
			got[row.GetKind()] = row
		}
	}
	out := &StorageUsage{Computing: r.GetComputing(), ReadBytes: r.GetReadThisMonth()}
	if at := r.GetComputedAt(); at > 0 {
		out.ComputedAt = timeutil.TimeStamp(at)
	}
	if monthRe.MatchString(r.GetMonth()) {
		out.Month = r.GetMonth()
	}
	for _, k := range usageKinds {
		row := got[k.kind]
		if row == nil || row.GetBytes() == 0 {
			continue
		}
		out.Kinds = append(out.Kinds, StorageUsageKind{
			Name: k.name, Key: "ascbyo.storage.usage.kind." + k.name, Bytes: row.GetBytes(), Objects: row.GetObjects(),
		})
		out.Total += row.GetBytes()
		out.Objects += row.GetObjects()
	}
	return out
}

// RepoUsage returns the repository's storage usage. refresh asks asc to scan again (it does so at most every five
// minutes).
func (s *Service) RepoUsage(ctx context.Context, repo Scope, refresh bool) (*StorageUsage, error) {
	if repo.Kind != KindRepo || !repo.Valid() {
		return nil, failure(FailInvalid)
	}
	cctx, cancel := s.call(ctx, repo, "GetRepoUsage", "", false)
	defer cancel()
	resp, err := s.api.Storage.GetRepoUsage(cctx, &ascv1.GetRepoStorageUsageRequest{Repo: repo.Repo(), Refresh: refresh})
	if err != nil {
		return nil, done("read repository storage usage", repo, err)
	}
	return StorageUsageFrom(resp), nil
}

// sizeOf is n as the int64 the size formatter takes, clamped.
func sizeOf(n uint64) int64 {
	if n > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(n)
}

// Size is Bytes for the size formatter.
func (k StorageUsageKind) Size() int64 { return sizeOf(k.Bytes) }

// TotalSize is Total for the size formatter.
func (u *StorageUsage) TotalSize() int64 { return sizeOf(u.Total) }

// ReadSize is ReadBytes for the size formatter.
func (u *StorageUsage) ReadSize() int64 { return sizeOf(u.ReadBytes) }
