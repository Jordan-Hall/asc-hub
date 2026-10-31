// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	"os"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// The host gauges of the health page (board AdminHealth): CPU and disk, next to the memory gauge the hub already had.
// Both are read from the host the hub runs on, cheaply and without a sample window: the CPU gauge is the one-minute load
// average per core, and the disk gauge is how full the file system holding the hub's data is. A host that does not give
// the number (no /proc, no statfs) has no gauge.

// hostGauge is one gauge: Known says whether the host gave the number.
type hostGauge struct {
	Known   bool
	Percent int // 0 to 100
}

// loadPercent turns the text of /proc/loadavg into the one-minute load per core, as a percent capped at 100.
func loadPercent(text string, cores int) hostGauge {
	fields := strings.Fields(text)
	if len(fields) < 1 || cores < 1 {
		return hostGauge{}
	}
	load, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || load < 0 {
		return hostGauge{}
	}
	return hostGauge{Known: true, Percent: min(100, int(load*100/float64(cores)+0.5))}
}

// diskPercent is how full a file system is: blocks in use of the blocks a user may use (what df shows).
func diskPercent(total, free, avail uint64) hostGauge {
	if total == 0 || free > total {
		return hostGauge{}
	}
	used := total - free
	usable := used + avail
	if usable == 0 {
		return hostGauge{}
	}
	return hostGauge{Known: true, Percent: min(100, int(used*100/usable))}
}

func cpuGauge() hostGauge {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return hostGauge{}
	}
	return loadPercent(string(b), runtime.NumCPU())
}

func diskGauge(path string) hostGauge {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return hostGauge{}
	}
	bs := uint64(st.Bsize) //nolint:gosec // a block size is positive
	return diskPercent(st.Blocks*bs, st.Bfree*bs, st.Bavail*bs)
}
