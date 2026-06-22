// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, go-fsctl

package main

import (
	"bytes"
	"errors"
	"testing"

	"github.com/go-fsctl/loop"
)

var errBoom = errors.New("boom")

// fakeTmp / fakeDev satisfy the tmpFile / devFile seams without touching disk
// or a real loop device.
type fakeTmp struct {
	name        string
	truncateErr error
}

func (f *fakeTmp) Truncate(int64) error { return f.truncateErr }
func (f *fakeTmp) Close() error         { return nil }
func (f *fakeTmp) Name() string         { return f.name }

type fakeDev struct {
	buf      []byte
	writeErr error
	readErr  error
	corrupt  bool
}

func (d *fakeDev) WriteAt(p []byte, _ int64) (int, error) {
	if d.writeErr != nil {
		return 0, d.writeErr
	}
	d.buf = append([]byte(nil), p...)
	return len(p), nil
}

func (d *fakeDev) ReadAt(p []byte, _ int64) (int, error) {
	if d.readErr != nil {
		return 0, d.readErr
	}
	src := d.buf
	if d.corrupt {
		src = bytes.Repeat([]byte{'x'}, len(p))
	}
	copy(p, src)
	return len(p), nil
}

func (d *fakeDev) Sync() error  { return nil }
func (d *fakeDev) Close() error { return nil }

// restore snapshots every seam and returns a deferred restore.
func restore() func() {
	a, b, c, d, e := available, attach, status, findByBacking, detach
	f, g, h := createTemp, openDevice, removeFile
	o, w := osExit, stdout
	return func() {
		available, attach, status, findByBacking, detach = a, b, c, d, e
		createTemp, openDevice, removeFile = f, g, h
		osExit, stdout = o, w
	}
}

// happy installs an all-succeeding set of seams; individual tests then break one.
func happy(t *testing.T) *fakeDev {
	t.Helper()
	dev := &fakeDev{}
	available = func() bool { return true }
	createTemp = func(string, string) (tmpFile, error) { return &fakeTmp{name: "/tmp/x.img"}, nil }
	removeFile = func(string) error { return nil }
	attach = func(string, loop.Options) (string, error) { return "/dev/loop7", nil }
	status = func(string) (loop.Info, error) { return loop.Info{Number: 7}, nil }
	findByBacking = func(string) ([]string, error) { return []string{"/dev/loop7"}, nil }
	openDevice = func(string) (devFile, error) { return dev, nil }
	detach = func(string) error { return nil }
	return dev
}

func runWith(args ...string) int {
	var buf bytes.Buffer
	stdout = &buf
	return run(args)
}

func TestRunSuccessTempImage(t *testing.T) {
	defer restore()()
	happy(t)
	if rc := runWith("loopprobe"); rc != 0 {
		t.Fatalf("rc=%d, want 0", rc)
	}
}

func TestRunSuccessProvidedImage(t *testing.T) {
	defer restore()()
	happy(t)
	// With an explicit arg, no temp file / cleanup path is taken.
	if rc := runWith("loopprobe", "/tmp/given.img"); rc != 0 {
		t.Fatalf("rc=%d, want 0", rc)
	}
}

func TestRunUnavailable(t *testing.T) {
	defer restore()()
	happy(t)
	available = func() bool { return false }
	if rc := runWith("loopprobe"); rc != 1 {
		t.Fatalf("rc=%d, want 1", rc)
	}
}

func TestRunCreateTempError(t *testing.T) {
	defer restore()()
	happy(t)
	createTemp = func(string, string) (tmpFile, error) { return nil, errBoom }
	if rc := runWith("loopprobe"); rc != 1 {
		t.Fatalf("rc=%d, want 1", rc)
	}
}

func TestRunTruncateError(t *testing.T) {
	defer restore()()
	happy(t)
	createTemp = func(string, string) (tmpFile, error) {
		return &fakeTmp{name: "/tmp/x.img", truncateErr: errBoom}, nil
	}
	if rc := runWith("loopprobe"); rc != 1 {
		t.Fatalf("rc=%d, want 1", rc)
	}
}

func TestRunAttachError(t *testing.T) {
	defer restore()()
	happy(t)
	attach = func(string, loop.Options) (string, error) { return "", errBoom }
	if rc := runWith("loopprobe", "/tmp/x.img"); rc != 1 {
		t.Fatalf("rc=%d, want 1", rc)
	}
}

func TestRunStatusError(t *testing.T) {
	defer restore()()
	happy(t)
	status = func(string) (loop.Info, error) { return loop.Info{}, errBoom }
	if rc := runWith("loopprobe", "/tmp/x.img"); rc != 1 {
		t.Fatalf("rc=%d, want 1", rc)
	}
}

func TestRunFindError(t *testing.T) {
	defer restore()()
	happy(t)
	findByBacking = func(string) ([]string, error) { return nil, errBoom }
	if rc := runWith("loopprobe", "/tmp/x.img"); rc != 1 {
		t.Fatalf("rc=%d, want 1", rc)
	}
}

func TestRunOpenDeviceError(t *testing.T) {
	defer restore()()
	happy(t)
	openDevice = func(string) (devFile, error) { return nil, errBoom }
	if rc := runWith("loopprobe", "/tmp/x.img"); rc != 1 {
		t.Fatalf("rc=%d, want 1", rc)
	}
}

func TestRunWriteError(t *testing.T) {
	defer restore()()
	dev := happy(t)
	dev.writeErr = errBoom
	if rc := runWith("loopprobe", "/tmp/x.img"); rc != 1 {
		t.Fatalf("rc=%d, want 1", rc)
	}
}

func TestRunReadError(t *testing.T) {
	defer restore()()
	dev := happy(t)
	dev.readErr = errBoom
	if rc := runWith("loopprobe", "/tmp/x.img"); rc != 1 {
		t.Fatalf("rc=%d, want 1", rc)
	}
}

func TestRunRoundTripMismatch(t *testing.T) {
	defer restore()()
	dev := happy(t)
	dev.corrupt = true
	if rc := runWith("loopprobe", "/tmp/x.img"); rc != 1 {
		t.Fatalf("rc=%d, want 1", rc)
	}
}

func TestRunDetachError(t *testing.T) {
	defer restore()()
	happy(t)
	detach = func(string) error { return errBoom }
	if rc := runWith("loopprobe", "/tmp/x.img"); rc != 1 {
		t.Fatalf("rc=%d, want 1", rc)
	}
}

// TestDefaultSeams exercises the real createTemp/openDevice seam closures (the
// production code paths that the fault-injecting tests above replace), so no
// statement is left uncovered. It uses ordinary files, not a loop device.
func TestDefaultSeams(t *testing.T) {
	f, err := createTemp(t.TempDir(), "seam-*.img")
	if err != nil {
		t.Fatalf("createTemp: %v", err)
	}
	name := f.Name()
	if err := f.Truncate(0); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	f.Close()

	d, err := openDevice(name)
	if err != nil {
		t.Fatalf("openDevice: %v", err)
	}
	d.Close()
}

// TestMainInvokesRun drives the thin main() wrapper through the osExit seam.
func TestMainInvokesRun(t *testing.T) {
	defer restore()()
	happy(t)
	var buf bytes.Buffer
	stdout = &buf
	code := -1
	osExit = func(c int) { code = c }
	main()
	if code != 0 {
		t.Fatalf("main exit=%d, want 0", code)
	}
}
