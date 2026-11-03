// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package packages

import (
	"cmp"
	"context"
	"slices"

	"forgejo.org/models/db"
)

// TypeCount is how many packages of one type a list holds.
type TypeCount struct {
	Type  Type
	Count int64
}

// CountPackagesByType counts the packages that match the search options for each type, the most packages first
// (ties in the order of TypeList); types without a package are left out. The type of the options is ignored: the counts are those of the type chips
// of a package list, and every chip keeps the name filter.
func CountPackagesByType(ctx context.Context, opts *PackageSearchOptions) ([]TypeCount, error) {
	byType := *opts
	byType.Type = ""
	rows := make([]struct {
		Type  Type
		Count int64
	}, 0, len(TypeList))
	err := db.GetEngine(ctx).
		Select("package.type AS type, COUNT(DISTINCT package.id) AS count").
		Table("package_version").
		Join("INNER", "package", "package.id = package_version.package_id").
		Where(byType.ToConds()).
		GroupBy("package.type").
		Find(&rows)
	if err != nil {
		return nil, err
	}
	counts := make(map[Type]int64, len(rows))
	for _, row := range rows {
		counts[row.Type] = row.Count
	}
	out := make([]TypeCount, 0, len(rows))
	for _, t := range TypeList {
		if n := counts[t]; n > 0 {
			out = append(out, TypeCount{Type: t, Count: n})
		}
	}
	slices.SortStableFunc(out, func(a, b TypeCount) int { return cmp.Compare(b.Count, a.Count) })
	return out, nil
}
