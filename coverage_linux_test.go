// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, go-fsctl

//go:build linux

package loop

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// These tests drive every branch of the loop_linux.go kernel paths through the
// indirection seams in seams_linux.go, fault-injecting both success and each
// errno without needing root or a real loop device. The root-only
// integration_linux_test.go exercises the genuine ioctls end to end.

// snapshotSeams captures the production seam values and returns a restore func.
func snapshotSeams() func() {
	a, b, c, d, e := osOpenFile, osStat, osReadDir, osReadFile, filepathAbs
	f, g, h, i, j, k := ioctlRetInt, ioctlSetInt, ioctlLoopConfigure, ioctlLoopGetStatus64, ioctlLoopSetStatus64, ioctlRetIntArgFn
	return func() {
		osOpenFile, osStat, osReadDir, osReadFile, filepathAbs = a, b, c, d, e
		ioctlRetInt, ioctlSetInt, ioctlLoopConfigure, ioctlLoopGetStatus64, ioctlLoopSetStatus64, ioctlRetIntArgFn = f, g, h, i, j, k
	}
}

// openOK returns an osOpenFile seam that hands back a fresh real temp file
// (so .Fd()/.Close() work) regardless of the requested path.
func openOK(t *testing.T) func(string, int, os.FileMode) (*os.File, error) {
	t.Helper()
	dir := t.TempDir()
	n := 0
	return func(string, int, os.FileMode) (*os.File, error) {
		n++
		f, err := os.CreateTemp(dir, "fd")
		if err != nil {
			t.Fatalf("temp: %v", err)
		}
		return f, nil
	}
}

var errInjected = errors.New("injected")

func TestAvailable(t *testing.T) {
	defer snapshotSeams()()
	osStat = func(string) (os.FileInfo, error) { return nil, nil }
	if !Available() {
		t.Fatal("want available")
	}
	osStat = func(string) (os.FileInfo, error) { return nil, errInjected }
	if Available() {
		t.Fatal("want unavailable")
	}
}

func TestAttachOpenControlError(t *testing.T) {
	defer snapshotSeams()()
	osOpenFile = func(string, int, os.FileMode) (*os.File, error) { return nil, errInjected }
	if _, err := Attach("img", Options{}); err == nil {
		t.Fatal("want error")
	}
}

func TestAttachGetFreeError(t *testing.T) {
	defer snapshotSeams()()
	osOpenFile = openOK(t)
	ioctlRetInt = func(int, uint) (int, error) { return 0, errInjected }
	if _, err := Attach("img", Options{}); err == nil {
		t.Fatal("want error")
	}
}

func TestAttachBackingOpenError(t *testing.T) {
	defer snapshotSeams()()
	calls := 0
	osOpenFile = func(name string, flag int, perm os.FileMode) (*os.File, error) {
		calls++
		if calls == 2 { // ctl ok, backing fails
			return nil, errInjected
		}
		f, _ := os.CreateTemp(t.TempDir(), "fd")
		return f, nil
	}
	ioctlRetInt = func(int, uint) (int, error) { return 3, nil }
	if _, err := Attach("img", Options{ReadOnly: true}); err == nil {
		t.Fatal("want error")
	}
}

func TestAttachDevOpenError(t *testing.T) {
	defer snapshotSeams()()
	calls := 0
	osOpenFile = func(name string, flag int, perm os.FileMode) (*os.File, error) {
		calls++
		if calls == 3 { // ctl ok, backing ok, dev fails
			return nil, errInjected
		}
		f, _ := os.CreateTemp(t.TempDir(), "fd")
		return f, nil
	}
	ioctlRetInt = func(int, uint) (int, error) { return 3, nil }
	if _, err := Attach("img", Options{}); err == nil {
		t.Fatal("want error")
	}
}

func TestAttachConfigureErrorTearsDown(t *testing.T) {
	defer snapshotSeams()()
	osOpenFile = openOK(t)
	ioctlRetInt = func(int, uint) (int, error) { return 7, nil }
	ioctlLoopConfigure = func(int, *unix.LoopConfig) error { return errInjected } // non-ENOTTY/EINVAL
	cleared := false
	ioctlSetInt = func(_ int, req uint, _ int) error {
		if req == unix.LOOP_CLR_FD {
			cleared = true
		}
		return nil
	}
	if _, err := Attach("img", Options{}); err == nil {
		t.Fatal("want error")
	}
	if !cleared {
		t.Fatal("want CLR_FD teardown")
	}
}

func TestAttachSuccess(t *testing.T) {
	defer snapshotSeams()()
	osOpenFile = openOK(t)
	ioctlRetInt = func(int, uint) (int, error) { return 5, nil }
	ioctlLoopConfigure = func(int, *unix.LoopConfig) error { return nil }
	dev, err := Attach("img", Options{Offset: 1 << 20})
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if dev != "/dev/loop5" {
		t.Fatalf("dev=%q", dev)
	}
}

func TestConfigureFallbackPaths(t *testing.T) {
	defer snapshotSeams()()

	t.Run("configure non-fallback error", func(t *testing.T) {
		ioctlLoopConfigure = func(int, *unix.LoopConfig) error { return errInjected }
		if err := configure(1, 2, "img", Options{}); err == nil {
			t.Fatal("want error")
		}
	})

	t.Run("ENOTTY then SET_FD fails", func(t *testing.T) {
		ioctlLoopConfigure = func(int, *unix.LoopConfig) error { return unix.ENOTTY }
		ioctlSetInt = func(int, uint, int) error { return errInjected }
		if err := configure(1, 2, "img", Options{}); err == nil {
			t.Fatal("want error")
		}
	})

	t.Run("EINVAL, no status needed", func(t *testing.T) {
		ioctlLoopConfigure = func(int, *unix.LoopConfig) error { return unix.EINVAL }
		ioctlSetInt = func(int, uint, int) error { return nil }
		if err := configure(1, 2, "img", Options{}); err != nil {
			t.Fatalf("want nil, got %v", err)
		}
	})

	t.Run("ENOTTY, SET_STATUS64 fails, teardown", func(t *testing.T) {
		ioctlLoopConfigure = func(int, *unix.LoopConfig) error { return unix.ENOTTY }
		cleared := false
		ioctlSetInt = func(_ int, req uint, _ int) error {
			if req == unix.LOOP_CLR_FD {
				cleared = true
			}
			return nil
		}
		ioctlLoopSetStatus64 = func(int, *unix.LoopInfo64) error { return errInjected }
		if err := configure(1, 2, "img", Options{Offset: 4096}); err == nil {
			t.Fatal("want error")
		}
		if !cleared {
			t.Fatal("want CLR_FD teardown")
		}
	})

	t.Run("ENOTTY, SET_STATUS64 ok", func(t *testing.T) {
		ioctlLoopConfigure = func(int, *unix.LoopConfig) error { return unix.ENOTTY }
		ioctlSetInt = func(int, uint, int) error { return nil }
		ioctlLoopSetStatus64 = func(int, *unix.LoopInfo64) error { return nil }
		if err := configure(1, 2, "img", Options{SizeLimit: 8192}); err != nil {
			t.Fatalf("want nil, got %v", err)
		}
	})
}

func TestDetach(t *testing.T) {
	defer snapshotSeams()()
	osOpenFile = func(string, int, os.FileMode) (*os.File, error) { return nil, errInjected }
	if err := Detach("/dev/loop0"); err == nil {
		t.Fatal("want open error")
	}
	osOpenFile = openOK(t)
	ioctlSetInt = func(int, uint, int) error { return errInjected }
	if err := Detach("/dev/loop0"); err == nil {
		t.Fatal("want ioctl error")
	}
	ioctlSetInt = func(int, uint, int) error { return nil }
	if err := Detach("/dev/loop0"); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}

func TestSetCapacity(t *testing.T) {
	defer snapshotSeams()()
	osOpenFile = func(string, int, os.FileMode) (*os.File, error) { return nil, errInjected }
	if err := SetCapacity("/dev/loop0"); err == nil {
		t.Fatal("want open error")
	}
	osOpenFile = openOK(t)
	ioctlSetInt = func(int, uint, int) error { return errInjected }
	if err := SetCapacity("/dev/loop0"); err == nil {
		t.Fatal("want ioctl error")
	}
	ioctlSetInt = func(int, uint, int) error { return nil }
	if err := SetCapacity("/dev/loop0"); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}

func TestStatus(t *testing.T) {
	defer snapshotSeams()()
	osOpenFile = func(string, int, os.FileMode) (*os.File, error) { return nil, errInjected }
	if _, err := Status("/dev/loop0"); err == nil {
		t.Fatal("want open error")
	}
	osOpenFile = openOK(t)
	ioctlLoopGetStatus64 = func(int) (*unix.LoopInfo64, error) { return nil, errInjected }
	if _, err := Status("/dev/loop0"); err == nil {
		t.Fatal("want ioctl error")
	}
	var info unix.LoopInfo64
	info.Number = 4
	info.Offset = 1 << 20
	info.Sizelimit = 2 << 20
	info.Flags = LO_FLAGS_READ_ONLY | LO_FLAGS_AUTOCLEAR | LO_FLAGS_PARTSCAN
	copyName(&info.File_name, "/tmp/x.img")
	ioctlLoopGetStatus64 = func(int) (*unix.LoopInfo64, error) { return &info, nil }
	got, err := Status("/dev/loop0")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if got.Number != 4 || got.BackingFile != "/tmp/x.img" || !got.ReadOnly() || !got.Autoclear() || !got.PartScan() {
		t.Fatalf("decoded wrong: %+v", got)
	}
}

func TestFindByBacking(t *testing.T) {
	defer snapshotSeams()()

	filepathAbs = func(string) (string, error) { return "", errInjected }
	if _, err := FindByBacking("rel"); err == nil {
		t.Fatal("want abs error")
	}

	filepathAbs = func(p string) (string, error) { return "/abs/x.img", nil }
	osReadDir = func(string) ([]os.DirEntry, error) { return nil, errInjected }
	if _, err := FindByBacking("x.img"); err == nil {
		t.Fatal("want readdir error")
	}

	osReadDir = func(string) ([]os.DirEntry, error) {
		return []os.DirEntry{fakeDE{"sda"}, fakeDE{"loop0"}, fakeDE{"loop1"}, fakeDE{"loop2"}}, nil
	}
	osReadFile = func(name string) ([]byte, error) {
		switch {
		case filepath.Base(filepath.Dir(filepath.Dir(name))) == "loop0":
			return nil, errInjected // unattached: skipped
		case filepath.Base(filepath.Dir(filepath.Dir(name))) == "loop1":
			return []byte("/abs/x.img (deleted)\n"), nil // match after suffix/newline trim
		default:
			return []byte("/other.img\n"), nil // no match
		}
	}
	got, err := FindByBacking("x.img")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(got) != 1 || got[0] != "/dev/loop1" {
		t.Fatalf("matches=%v", got)
	}
}

func TestCtlAdd(t *testing.T) {
	defer snapshotSeams()()
	osOpenFile = func(string, int, os.FileMode) (*os.File, error) { return nil, errInjected }
	if _, err := CtlAdd(2); err == nil {
		t.Fatal("want open error")
	}
	osOpenFile = openOK(t)
	ioctlRetIntArgFn = func(int, uint, int) (int, error) { return 0, errInjected }
	if _, err := CtlAdd(2); err == nil {
		t.Fatal("want ioctl error")
	}
	ioctlRetIntArgFn = func(_ int, _ uint, arg int) (int, error) { return arg, nil }
	if got, err := CtlAdd(9); err != nil || got != 9 {
		t.Fatalf("got=%d err=%v", got, err)
	}
}

func TestCtlRemove(t *testing.T) {
	defer snapshotSeams()()
	osOpenFile = func(string, int, os.FileMode) (*os.File, error) { return nil, errInjected }
	if err := CtlRemove(2); err == nil {
		t.Fatal("want open error")
	}
	osOpenFile = openOK(t)
	ioctlSetInt = func(int, uint, int) error { return errInjected }
	if err := CtlRemove(2); err == nil {
		t.Fatal("want ioctl error")
	}
	ioctlSetInt = func(int, uint, int) error { return nil }
	if err := CtlRemove(2); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}

func TestDeviceNumber(t *testing.T) {
	if _, err := DeviceNumber("/dev/sda1"); err == nil {
		t.Fatal("want non-loop error")
	}
	if _, err := DeviceNumber("/dev/loopX"); err == nil {
		t.Fatal("want atoi error")
	}
	n, err := DeviceNumber("/dev/loop12")
	if err != nil || n != 12 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestCopyNameTruncates(t *testing.T) {
	var dst [loNameSize]uint8
	long := make([]byte, 200)
	for i := range long {
		long[i] = 'a'
	}
	copyName(&dst, string(long))
	if dst[loNameSize-1] != 0 {
		t.Fatal("want NUL terminator preserved")
	}
	if cstr(dst[:]) != string(long[:loNameSize-1]) {
		t.Fatal("want truncated to fit")
	}
}

func TestCstrNoNUL(t *testing.T) {
	var full [4]uint8
	copy(full[:], []byte("abcd"))
	if cstr(full[:]) != "abcd" {
		t.Fatalf("got %q", cstr(full[:]))
	}
}

func TestFlagsAndLoopIO(t *testing.T) {
	o := Options{ReadOnly: true, Autoclear: true, PartScan: true}
	if o.flags() != LO_FLAGS_READ_ONLY|LO_FLAGS_AUTOCLEAR|LO_FLAGS_PARTSCAN {
		t.Fatal("flags fold wrong")
	}
	if (Options{}).flags() != 0 {
		t.Fatal("zero flags")
	}
	if loopIO(0x82) != LOOP_CTL_GET_FREE {
		t.Fatalf("loopIO derivation wrong: %#x", loopIO(0x82))
	}
}

// TestIoctlRetIntArg covers both branches of the raw-syscall wrapper without
// root: a bogus request on a regular file fd yields ENOTTY (error branch), and
// FIOCLEX — which sets close-on-exec, ignores its integer argument, and returns
// 0 for any fd unprivileged — exercises the success branch. The success half is
// skipped under -test.short so the emulated (QEMU) CI jobs never issue a real
// ioctl; the native job (no -short) covers it.
func TestIoctlRetIntArg(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "fd")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	if _, err := ioctlRetIntArg(int(f.Fd()), 0xDEAD, 0); err == nil {
		t.Fatal("want errno from bogus ioctl request")
	}

	if testing.Short() {
		t.Skip("skip the real FIOCLEX ioctl under -short (emulated CI)")
	}
	if _, err := ioctlRetIntArg(int(f.Fd()), 0x5451 /* FIOCLEX */, 0); err != nil {
		t.Fatalf("ioctlRetIntArg FIOCLEX: %v", err)
	}
}

// fakeDE is a minimal os.DirEntry whose only meaningful method is Name.
type fakeDE struct{ name string }

func (f fakeDE) Name() string               { return f.name }
func (f fakeDE) IsDir() bool                { return true }
func (f fakeDE) Type() os.FileMode          { return os.ModeDir }
func (f fakeDE) Info() (os.FileInfo, error) { return nil, nil }
