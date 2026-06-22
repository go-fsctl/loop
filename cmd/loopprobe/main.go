// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, go-fsctl
//
// loopprobe is a live demonstration of github.com/go-fsctl/loop driving Linux
// loop devices purely via /dev/loop-control and LOOP_* ioctls (no cgo, no
// losetup). It attaches a backing file at an offset, reports the kernel status,
// does a write/read round-trip on the device, locates it by backing file, and
// detaches it.
//
// Usage: loopprobe [backing-file]   (default: a fresh 64 MiB temp image)
package main

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"github.com/go-fsctl/loop"
)

// tmpFile and devFile abstract the file handles loopprobe needs, so the I/O can
// be fault-injected in tests without a real loop device.
type tmpFile interface {
	Truncate(int64) error
	Close() error
	Name() string
}

type devFile interface {
	io.WriterAt
	io.ReaderAt
	Sync() error
	Close() error
}

// Seams over the loop package and the OS, overridable in tests. Production code
// uses the real implementations assigned here.
var (
	available     = loop.Available
	attach        = loop.Attach
	status        = loop.Status
	findByBacking = loop.FindByBacking
	detach        = loop.Detach

	createTemp = func(dir, pattern string) (tmpFile, error) { return os.CreateTemp(dir, pattern) }
	openDevice = func(path string) (devFile, error) { return os.OpenFile(path, os.O_RDWR, 0) }
	removeFile = os.Remove

	osExit            = os.Exit
	stdout io.Writer = os.Stdout
)

func main() { osExit(run(os.Args)) }

func run(args []string) int {
	if !available() {
		fmt.Fprintln(stdout, "FAIL: /dev/loop-control not present")
		return 1
	}

	var img string
	var cleanup bool
	if len(args) > 1 {
		img = args[1]
	} else {
		f, err := createTemp("", "loopprobe-*.img")
		if err != nil {
			return fail("create temp image", err)
		}
		if err := f.Truncate(64 << 20); err != nil {
			f.Close()
			return fail("truncate", err)
		}
		f.Close()
		img = f.Name()
		cleanup = true
		defer removeFile(img)
	}

	const offset = 1 << 20
	dev, err := attach(img, loop.Options{Offset: offset})
	if err != nil {
		return fail("Attach", err)
	}
	fmt.Fprintf(stdout, "Attach(%s, offset=%d) -> %s\n", img, offset, dev)

	info, err := status(dev)
	if err != nil {
		return fail("Status", err)
	}
	fmt.Fprintf(stdout, "Status: number=%d offset=%d sizelimit=%d flags=%#x backing=%q ro=%t\n",
		info.Number, info.Offset, info.SizeLimit, info.Flags, info.BackingFile, info.ReadOnly())

	found, err := findByBacking(img)
	if err != nil {
		return fail("FindByBacking", err)
	}
	fmt.Fprintf(stdout, "FindByBacking(%s) -> %v\n", img, found)

	marker := []byte("loopprobe pure-Go ioctl round-trip")
	df, err := openDevice(dev)
	if err != nil {
		return fail("open device", err)
	}
	if _, err := df.WriteAt(marker, 0); err != nil {
		df.Close()
		return fail("write", err)
	}
	df.Sync()
	got := make([]byte, len(marker))
	if _, err := df.ReadAt(got, 0); err != nil {
		df.Close()
		return fail("read", err)
	}
	df.Close()
	if !bytes.Equal(got, marker) {
		fmt.Fprintf(stdout, "FAIL: round-trip mismatch: got %q\n", got)
		return 1
	}
	fmt.Fprintf(stdout, "round-trip OK: %q\n", got)

	if err := detach(dev); err != nil {
		return fail("Detach", err)
	}
	fmt.Fprintf(stdout, "Detach(%s) OK\n", dev)

	if cleanup {
		fmt.Fprintf(stdout, "(removed temp image %s)\n", img)
	}
	fmt.Fprintln(stdout, "PASS")
	return 0
}

func fail(what string, err error) int {
	fmt.Fprintf(stdout, "FAIL: %s: %v\n", what, err)
	return 1
}
