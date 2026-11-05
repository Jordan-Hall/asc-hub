// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"strconv"

	"forgejo.org/modules/git"
)

// The Graph view of History (board History): the same rows as the List view, with the lines between the
// changes of the page drawn beside them. The layout uses only the page's own commits and their parents, so
// it works for every storage backend; a parent that is not on the page continues its line to the bottom of
// the row.

const (
	// historyLaneGap is the distance between two lanes, historyLaneX the centre of the first.
	historyLaneGap = 14
	historyLaneX   = 8
	// historyNodeY is the centre of a row's node, from the top of the row: the line of the title.
	historyNodeY = 25
	// historyMaxLanes lanes are drawn apart; further lanes share the last one.
	historyMaxLanes = 8
)

// historyLaneLine is one line of a row's picture. Y values are SVG lengths: "0", the node's line or "100%"
// (the bottom of the row, whatever its height).
type historyLaneLine struct {
	X1, X2 int
	Y1, Y2 string
	Color  int
}

// historyLaneRow is the picture beside one commit.
type historyLaneRow struct {
	Lines     []historyLaneLine
	NodeX     int
	NodeColor int
	Merge     bool
}

func historyLaneAt(col int) int {
	return historyLaneX + min(col, historyMaxLanes-1)*historyLaneGap
}

// historyLanes lays out commits, newest first, in lanes. It returns the picture of each commit by ID and
// the width the widest row needs.
func historyLanes(commits []*git.Commit) (map[string]*historyLaneRow, int) {
	rows := make(map[string]*historyLaneRow, len(commits))
	var lanes []string // the commit each lane waits for; "" is a free lane
	widest := 1
	node := strconv.Itoa(historyNodeY)
	free := func(skip int) int {
		for i, id := range lanes {
			if id == "" && i != skip {
				return i
			}
		}
		lanes = append(lanes, "")
		return len(lanes) - 1
	}
	for _, c := range commits {
		id := c.ID.String()
		col := -1
		var into []int
		for i, want := range lanes {
			if want == id {
				into = append(into, i)
				if col < 0 {
					col = i
				}
			}
		}
		if col < 0 {
			col = free(-1)
		}
		row := &historyLaneRow{NodeX: historyLaneAt(col), NodeColor: col % historyMaxLanes, Merge: len(c.Parents) > 1}
		for _, i := range into {
			row.Lines = append(row.Lines, historyLaneLine{X1: historyLaneAt(i), Y1: "0", X2: row.NodeX, Y2: node, Color: i % historyMaxLanes})
			lanes[i] = ""
		}
		// the other lanes pass by
		for i, want := range lanes {
			if want != "" {
				x := historyLaneAt(i)
				row.Lines = append(row.Lines, historyLaneLine{X1: x, Y1: "0", X2: x, Y2: "100%", Color: i % historyMaxLanes})
			}
		}
		for n, p := range c.Parents {
			pid := p.String()
			to := -1
			for i, want := range lanes {
				if want == pid {
					to = i
					break
				}
			}
			switch {
			case to >= 0 && n > 0:
				// a merge into a line that is already open
			case n == 0:
				if to < 0 {
					to = col
				}
				lanes[to] = pid
			default:
				to = free(col)
				lanes[to] = pid
			}
			row.Lines = append(row.Lines, historyLaneLine{X1: row.NodeX, Y1: node, X2: historyLaneAt(to), Y2: "100%", Color: to % historyMaxLanes})
		}
		for len(lanes) > 0 && lanes[len(lanes)-1] == "" {
			lanes = lanes[:len(lanes)-1]
		}
		widest = max(widest, len(lanes), col+1)
		rows[id] = row
	}
	return rows, historyLaneX*2 + (min(widest, historyMaxLanes)-1)*historyLaneGap
}
