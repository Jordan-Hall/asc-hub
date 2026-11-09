// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package structs

// AscDoctorCheck is one check of a machine report.
// swagger:model
type AscDoctorCheck struct {
	// the name of the check, as `asc doctor` prints it (real-git, shim-dir, daemon, ...)
	Name string `json:"name"`
	// ok, info, warn or fail
	Level string `json:"level"`
}

// AscDoctorReport is what `asc doctor --report` says about the machine of the authenticated user. It holds no
// path and no detail text.
// swagger:model
type AscDoctorReport struct {
	// the version of asc
	Asc string `json:"asc"`
	// the version of git asc uses, when found
	Git string `json:"git,omitempty"`
	// the operating system (windows, linux, macos)
	OS string `json:"os"`
	// the processor architecture
	Arch string `json:"arch,omitempty"`
	// the checks, at most 64
	Checks []AscDoctorCheck `json:"checks"`
}
