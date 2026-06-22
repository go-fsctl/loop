// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, go-fsctl

//go:build linux

package loop

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// Indirection seams over the operating-system and ioctl primitives this package
// drives. They exist so the error branches of every kernel call — which only
// trigger on real ioctl failures that are impractical to provoke against a live
// device — can be exercised deterministically by fault-injecting fakes in tests.
// Production code uses the real implementations assigned here; tests swap a var,
// run, and restore it. The root-only integration test still drives the genuine
// LOOP_* ioctls for end-to-end confidence.
var (
	osOpenFile  = os.OpenFile
	osStat      = os.Stat
	osReadDir   = os.ReadDir
	osReadFile  = os.ReadFile
	filepathAbs = filepath.Abs

	ioctlRetInt          = unix.IoctlRetInt
	ioctlSetInt          = unix.IoctlSetInt
	ioctlLoopConfigure   = unix.IoctlLoopConfigure
	ioctlLoopGetStatus64 = unix.IoctlLoopGetStatus64
	ioctlLoopSetStatus64 = unix.IoctlLoopSetStatus64
	ioctlRetIntArgFn     = ioctlRetIntArg
)
