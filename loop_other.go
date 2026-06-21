// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, go-fsctl

//go:build !linux

// Package loop drives Linux loop devices via /dev/loop-control and the LOOP_*
// ioctls. That kernel control path only exists on Linux; on other platforms
// every operation returns ErrUnsupported. The ABI definitions and ioctl-number
// derivation in abi.go remain available everywhere for testing and tooling.
package loop

import "errors"

// ErrUnsupported is returned by all kernel operations on non-Linux platforms.
var ErrUnsupported = errors.New("loop: LOOP_* ioctls are only supported on Linux")

// Options configures a loop-device attachment. See the Linux build for field
// semantics.
type Options struct {
	Offset    uint64
	SizeLimit uint64
	ReadOnly  bool
	Autoclear bool
	PartScan  bool
}

// Info is the decoded status of a loop device. See the Linux build for field
// semantics.
type Info struct {
	Number      uint32
	Offset      uint64
	SizeLimit   uint64
	Flags       uint32
	BackingFile string
}

// ReadOnly reports whether LO_FLAGS_READ_ONLY is set.
func (i Info) ReadOnly() bool { return i.Flags&LO_FLAGS_READ_ONLY != 0 }

// Autoclear reports whether LO_FLAGS_AUTOCLEAR is set.
func (i Info) Autoclear() bool { return i.Flags&LO_FLAGS_AUTOCLEAR != 0 }

// PartScan reports whether LO_FLAGS_PARTSCAN is set.
func (i Info) PartScan() bool { return i.Flags&LO_FLAGS_PARTSCAN != 0 }

// Available reports false off Linux.
func Available() bool { return false }

// Attach is unsupported off Linux.
func Attach(imagePath string, opt Options) (string, error) { return "", ErrUnsupported }

// Detach is unsupported off Linux.
func Detach(devPath string) error { return ErrUnsupported }

// SetCapacity is unsupported off Linux.
func SetCapacity(devPath string) error { return ErrUnsupported }

// Status is unsupported off Linux.
func Status(devPath string) (Info, error) { return Info{}, ErrUnsupported }

// FindByBacking is unsupported off Linux.
func FindByBacking(path string) ([]string, error) { return nil, ErrUnsupported }

// CtlAdd is unsupported off Linux.
func CtlAdd(n int) (int, error) { return 0, ErrUnsupported }

// CtlRemove is unsupported off Linux.
func CtlRemove(n int) error { return ErrUnsupported }

// DeviceNumber is unsupported off Linux.
func DeviceNumber(devPath string) (int, error) { return 0, ErrUnsupported }
