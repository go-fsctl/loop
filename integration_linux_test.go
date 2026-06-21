// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, go-fsctl

//go:build linux

package loop

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// requireLoop skips the test unless /dev/loop-control exists and the process is
// running as root (CAP_SYS_ADMIN is needed for every LOOP_* ioctl).
func requireLoop(t *testing.T) {
	t.Helper()
	if !Available() {
		t.Skip("skipping: /dev/loop-control not present")
	}
	if os.Geteuid() != 0 {
		t.Skip("skipping: loop ioctls require root")
	}
}

// makeImage creates a sparse size-byte backing file in the test temp dir.
func makeImage(t *testing.T, size int64) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "lo.img")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create image: %v", err)
	}
	if err := f.Truncate(size); err != nil {
		f.Close()
		t.Fatalf("truncate image: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close image: %v", err)
	}
	return path
}

// TestAttachStatusDetach is the end-to-end happy path: attach a backing file
// with an offset, verify Status reflects it, do a write/read round-trip on the
// device, then detach and confirm it is gone.
func TestAttachStatusDetach(t *testing.T) {
	requireLoop(t)

	const offset = 1 << 20 // 1 MiB
	img := makeImage(t, 64<<20)

	dev, err := Attach(img, Options{Offset: offset})
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	t.Logf("attached %s -> %s", img, dev)
	defer func() {
		if err := Detach(dev); err != nil {
			t.Errorf("cleanup Detach(%s): %v", dev, err)
		}
	}()

	info, err := Status(dev)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if info.Offset != offset {
		t.Errorf("Status.Offset = %d, want %d", info.Offset, offset)
	}
	abs, _ := filepath.Abs(img)
	if info.BackingFile != abs {
		t.Errorf("Status.BackingFile = %q, want %q", info.BackingFile, abs)
	}

	// FindByBacking should locate the device by its backing file.
	found, err := FindByBacking(img)
	if err != nil {
		t.Fatalf("FindByBacking: %v", err)
	}
	if len(found) == 0 {
		t.Errorf("FindByBacking(%s) returned nothing, want %s", img, dev)
	} else {
		var ok bool
		for _, d := range found {
			if d == dev {
				ok = true
			}
		}
		if !ok {
			t.Errorf("FindByBacking = %v, want to contain %s", found, dev)
		}
	}

	// Write/read round-trip through the loop device.
	want := []byte("go-fsctl/loop round-trip marker")
	f, err := os.OpenFile(dev, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open %s: %v", dev, err)
	}
	defer f.Close()
	if _, err := f.WriteAt(want, 0); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := f.Sync(); err != nil {
		t.Fatalf("sync: %v", err)
	}
	got := make([]byte, len(want))
	if _, err := f.ReadAt(got, 0); err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("round-trip = %q, want %q", got, want)
	}
}

// TestReadOnlyAttach checks the read-only flag is honored end-to-end.
func TestReadOnlyAttach(t *testing.T) {
	requireLoop(t)
	img := makeImage(t, 16<<20)

	dev, err := Attach(img, Options{ReadOnly: true})
	if err != nil {
		t.Fatalf("Attach ro: %v", err)
	}
	defer Detach(dev)

	info, err := Status(dev)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !info.ReadOnly() {
		t.Errorf("Status.ReadOnly() = false, want true (flags=%#x)", info.Flags)
	}
}

// TestDetachNonLoop confirms detaching a non-loop path fails cleanly (the
// LOOP_CLR_FD ioctl returns ENOTTY) rather than panicking.
func TestDetachNonLoop(t *testing.T) {
	requireLoop(t)
	if err := Detach("/dev/null"); err == nil {
		t.Errorf("Detach(/dev/null) = nil, want error")
	}
}
