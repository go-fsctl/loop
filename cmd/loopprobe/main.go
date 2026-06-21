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
	"os"

	"github.com/go-fsctl/loop"
)

func main() {
	if !loop.Available() {
		fmt.Println("FAIL: /dev/loop-control not present")
		os.Exit(1)
	}

	var img string
	var cleanup bool
	if len(os.Args) > 1 {
		img = os.Args[1]
	} else {
		f, err := os.CreateTemp("", "loopprobe-*.img")
		if err != nil {
			fail("create temp image", err)
		}
		if err := f.Truncate(64 << 20); err != nil {
			fail("truncate", err)
		}
		f.Close()
		img = f.Name()
		cleanup = true
		defer os.Remove(img)
	}

	const offset = 1 << 20
	dev, err := loop.Attach(img, loop.Options{Offset: offset})
	if err != nil {
		fail("Attach", err)
	}
	fmt.Printf("Attach(%s, offset=%d) -> %s\n", img, offset, dev)

	info, err := loop.Status(dev)
	if err != nil {
		fail("Status", err)
	}
	fmt.Printf("Status: number=%d offset=%d sizelimit=%d flags=%#x backing=%q ro=%t\n",
		info.Number, info.Offset, info.SizeLimit, info.Flags, info.BackingFile, info.ReadOnly())

	found, err := loop.FindByBacking(img)
	if err != nil {
		fail("FindByBacking", err)
	}
	fmt.Printf("FindByBacking(%s) -> %v\n", img, found)

	marker := []byte("loopprobe pure-Go ioctl round-trip")
	df, err := os.OpenFile(dev, os.O_RDWR, 0)
	if err != nil {
		fail("open device", err)
	}
	if _, err := df.WriteAt(marker, 0); err != nil {
		fail("write", err)
	}
	df.Sync()
	got := make([]byte, len(marker))
	if _, err := df.ReadAt(got, 0); err != nil {
		fail("read", err)
	}
	df.Close()
	if !bytes.Equal(got, marker) {
		fmt.Printf("FAIL: round-trip mismatch: got %q\n", got)
		os.Exit(1)
	}
	fmt.Printf("round-trip OK: %q\n", got)

	if err := loop.Detach(dev); err != nil {
		fail("Detach", err)
	}
	fmt.Printf("Detach(%s) OK\n", dev)

	if cleanup {
		fmt.Printf("(removed temp image %s)\n", img)
	}
	fmt.Println("PASS")
}

func fail(what string, err error) {
	fmt.Printf("FAIL: %s: %v\n", what, err)
	os.Exit(1)
}
