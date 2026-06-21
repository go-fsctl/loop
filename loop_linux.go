// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, go-fsctl

//go:build linux

// Package loop drives Linux loop devices directly through /dev/loop-control and
// the LOOP_* ioctls — the same kernel interface util-linux's losetup uses —
// with no cgo and no shelling out to an external binary.
//
// It is the loop-device member of the go-fsctl family alongside
// github.com/go-fsctl/zfs and github.com/go-fsctl/btrfs: where those packages
// talk to the OpenZFS and btrfs kernel modules via their native control paths,
// this package allocates, configures, and tears down loop devices via
// /dev/loop-control (LOOP_CTL_GET_FREE / ADD / REMOVE) and the per-device
// LOOP_* ioctls (LOOP_CONFIGURE, LOOP_SET_FD, LOOP_SET_STATUS64,
// LOOP_GET_STATUS64, LOOP_CLR_FD, LOOP_SET_CAPACITY).
//
// All operations require CAP_SYS_ADMIN (in practice, root).
package loop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	devLoopControl = "/dev/loop-control"
	sysBlock       = "/sys/block"
)

// ErrUnsupported is returned by the non-Linux build of this package. It is
// declared here too so callers can reference loop.ErrUnsupported on any
// platform; on Linux it is never returned.
var ErrUnsupported = errors.New("loop: LOOP_* ioctls are only supported on Linux")

// Options configures a loop-device attachment. The zero value attaches the
// whole backing file read-write with no special flags.
type Options struct {
	// Offset is the byte offset into the backing file at which the loop
	// device begins (lo_offset).
	Offset uint64
	// SizeLimit caps the device size in bytes (lo_sizelimit). Zero means the
	// device spans from Offset to the end of the backing file.
	SizeLimit uint64
	// ReadOnly opens the backing file O_RDONLY and sets LO_FLAGS_READ_ONLY.
	ReadOnly bool
	// Autoclear sets LO_FLAGS_AUTOCLEAR so the kernel detaches the device
	// automatically when the last opener closes it.
	Autoclear bool
	// PartScan sets LO_FLAGS_PARTSCAN so the kernel scans the backing file
	// for a partition table and creates /dev/loopNpM partition nodes.
	PartScan bool
}

// flags folds the Options booleans into the LO_FLAGS_* bitmask the kernel
// stores in lo_flags.
func (o Options) flags() uint32 {
	var f uint32
	if o.ReadOnly {
		f |= LO_FLAGS_READ_ONLY
	}
	if o.Autoclear {
		f |= LO_FLAGS_AUTOCLEAR
	}
	if o.PartScan {
		f |= LO_FLAGS_PARTSCAN
	}
	return f
}

// Info is the decoded status of a loop device (LOOP_GET_STATUS64).
type Info struct {
	// Number is the loop device index N in /dev/loopN (lo_number).
	Number uint32
	// Offset is the backing-file byte offset (lo_offset).
	Offset uint64
	// SizeLimit is the device size cap in bytes, 0 if unlimited (lo_sizelimit).
	SizeLimit uint64
	// Flags is the raw LO_FLAGS_* bitmask (lo_flags).
	Flags uint32
	// BackingFile is the path of the backing file (lo_file_name), as recorded
	// by the kernel. It may be truncated to 64 bytes.
	BackingFile string
}

// ReadOnly reports whether LO_FLAGS_READ_ONLY is set.
func (i Info) ReadOnly() bool { return i.Flags&LO_FLAGS_READ_ONLY != 0 }

// Autoclear reports whether LO_FLAGS_AUTOCLEAR is set.
func (i Info) Autoclear() bool { return i.Flags&LO_FLAGS_AUTOCLEAR != 0 }

// PartScan reports whether LO_FLAGS_PARTSCAN is set.
func (i Info) PartScan() bool { return i.Flags&LO_FLAGS_PARTSCAN != 0 }

// Available reports whether the loop control interface is usable: that
// /dev/loop-control exists. It does not check for the CAP_SYS_ADMIN privilege
// the actual ioctls require.
func Available() bool {
	_, err := os.Stat(devLoopControl)
	return err == nil
}

// Attach binds imagePath to the first free loop device and returns its path
// (e.g. "/dev/loop3").
//
// It opens /dev/loop-control, asks the kernel for a free device number with
// LOOP_CTL_GET_FREE, opens that /dev/loopN and the backing file, and then
// configures the device. On kernels >= 5.8 it issues a single LOOP_CONFIGURE
// (struct loop_config: backing fd + loop_info64 with offset/sizelimit/flags);
// on older kernels, or if LOOP_CONFIGURE is unavailable, it falls back to
// LOOP_SET_FD followed by LOOP_SET_STATUS64.
func Attach(imagePath string, opt Options) (devPath string, err error) {
	ctl, err := os.OpenFile(devLoopControl, os.O_RDWR, 0)
	if err != nil {
		return "", fmt.Errorf("loop: open %s: %w", devLoopControl, err)
	}
	defer ctl.Close()

	n, err := unix.IoctlRetInt(int(ctl.Fd()), unix.LOOP_CTL_GET_FREE)
	if err != nil {
		return "", fmt.Errorf("loop: LOOP_CTL_GET_FREE: %w", err)
	}
	devPath = fmt.Sprintf("/dev/loop%d", n)

	backingFlags := os.O_RDWR
	if opt.ReadOnly {
		backingFlags = os.O_RDONLY
	}
	backing, err := os.OpenFile(imagePath, backingFlags, 0)
	if err != nil {
		return "", fmt.Errorf("loop: open backing file %s: %w", imagePath, err)
	}
	defer backing.Close()

	dev, err := os.OpenFile(devPath, os.O_RDWR, 0)
	if err != nil {
		return "", fmt.Errorf("loop: open %s: %w", devPath, err)
	}
	defer dev.Close()

	if err := configure(int(dev.Fd()), int(backing.Fd()), imagePath, opt); err != nil {
		// Best-effort teardown so we do not leak a half-configured device.
		_ = unix.IoctlSetInt(int(dev.Fd()), unix.LOOP_CLR_FD, 0)
		return "", err
	}
	return devPath, nil
}

// configure sets up an already-opened loop device fd against backingFd. It
// prefers the atomic LOOP_CONFIGURE and falls back to the legacy two-step
// LOOP_SET_FD + LOOP_SET_STATUS64 on ENOTTY/EINVAL (old kernel).
func configure(devFd, backingFd int, imagePath string, opt Options) error {
	cfg := unix.LoopConfig{
		Fd: uint32(backingFd),
		Info: unix.LoopInfo64{
			Offset:    opt.Offset,
			Sizelimit: opt.SizeLimit,
			Flags:     opt.flags(),
		},
	}
	copyName(&cfg.Info.File_name, imagePath)

	err := unix.IoctlLoopConfigure(devFd, &cfg)
	if err == nil {
		return nil
	}
	if !errors.Is(err, unix.ENOTTY) && !errors.Is(err, unix.EINVAL) {
		return fmt.Errorf("loop: LOOP_CONFIGURE: %w", err)
	}

	// Legacy fallback: associate the backing fd, then push status/flags.
	if err := unix.IoctlSetInt(devFd, unix.LOOP_SET_FD, backingFd); err != nil {
		return fmt.Errorf("loop: LOOP_SET_FD: %w", err)
	}
	if opt.Offset != 0 || opt.SizeLimit != 0 || opt.flags() != 0 {
		st := unix.LoopInfo64{
			Offset:    opt.Offset,
			Sizelimit: opt.SizeLimit,
			Flags:     opt.flags(),
		}
		copyName(&st.File_name, imagePath)
		if err := unix.IoctlLoopSetStatus64(devFd, &st); err != nil {
			_ = unix.IoctlSetInt(devFd, unix.LOOP_CLR_FD, 0)
			return fmt.Errorf("loop: LOOP_SET_STATUS64: %w", err)
		}
	}
	return nil
}

// copyName writes name into a fixed LO_NAME_SIZE byte array, NUL-terminated and
// truncated to fit (the trailing byte is always left as 0).
func copyName(dst *[loNameSize]uint8, name string) {
	for i := range dst {
		dst[i] = 0
	}
	b := []byte(name)
	if len(b) > len(dst)-1 {
		b = b[:len(dst)-1]
	}
	copy(dst[:], b)
}

// Detach unbinds the loop device at devPath via LOOP_CLR_FD. The kernel may
// defer the teardown until the last opener of the device closes it; in that
// case the device disappears asynchronously.
func Detach(devPath string) error {
	dev, err := os.OpenFile(devPath, os.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("loop: open %s: %w", devPath, err)
	}
	defer dev.Close()
	if err := unix.IoctlSetInt(int(dev.Fd()), unix.LOOP_CLR_FD, 0); err != nil {
		return fmt.Errorf("loop: LOOP_CLR_FD %s: %w", devPath, err)
	}
	return nil
}

// SetCapacity makes the loop device re-read the size of its backing file
// (LOOP_SET_CAPACITY). Use it after the backing file has been grown.
func SetCapacity(devPath string) error {
	dev, err := os.OpenFile(devPath, os.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("loop: open %s: %w", devPath, err)
	}
	defer dev.Close()
	if err := unix.IoctlSetInt(int(dev.Fd()), unix.LOOP_SET_CAPACITY, 0); err != nil {
		return fmt.Errorf("loop: LOOP_SET_CAPACITY %s: %w", devPath, err)
	}
	return nil
}

// Status reads the configuration of the loop device at devPath via
// LOOP_GET_STATUS64 and decodes the fields callers care about.
func Status(devPath string) (Info, error) {
	dev, err := os.OpenFile(devPath, os.O_RDONLY, 0)
	if err != nil {
		return Info{}, fmt.Errorf("loop: open %s: %w", devPath, err)
	}
	defer dev.Close()

	st, err := unix.IoctlLoopGetStatus64(int(dev.Fd()))
	if err != nil {
		return Info{}, fmt.Errorf("loop: LOOP_GET_STATUS64 %s: %w", devPath, err)
	}
	return Info{
		Number:      st.Number,
		Offset:      st.Offset,
		SizeLimit:   st.Sizelimit,
		Flags:       st.Flags,
		BackingFile: cstr(st.File_name[:]),
	}, nil
}

// cstr converts a NUL-terminated (or full) C byte array to a Go string.
func cstr(b []uint8) string {
	if i := indexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}

func indexByte(b []uint8, c uint8) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}

// FindByBacking returns the loop device paths whose backing file is path. It
// resolves path to an absolute path and compares against each loop device's
// /sys/block/loopN/loop/backing_file. The returned slice is empty (not nil-
// erroring) when nothing matches.
func FindByBacking(path string) ([]string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("loop: resolve %s: %w", path, err)
	}

	entries, err := os.ReadDir(sysBlock)
	if err != nil {
		return nil, fmt.Errorf("loop: read %s: %w", sysBlock, err)
	}

	var matches []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "loop") {
			continue
		}
		bfPath := filepath.Join(sysBlock, name, "loop", "backing_file")
		data, err := os.ReadFile(bfPath)
		if err != nil {
			// Not an attached loop device (no loop/ subdir) — skip.
			continue
		}
		backing := strings.TrimRight(string(data), "\n")
		// sysfs may suffix " (deleted)" for an unlinked backing file.
		backing = strings.TrimSuffix(backing, " (deleted)")
		if backing == abs {
			matches = append(matches, filepath.Join("/dev", name))
		}
	}
	return matches, nil
}

// CtlAdd creates /dev/loopN with index n via /dev/loop-control LOOP_CTL_ADD and
// returns the device number assigned by the kernel (n). It fails with EEXIST if
// the device already exists. The index is passed as the ioctl argument.
func CtlAdd(n int) (int, error) {
	ctl, err := os.OpenFile(devLoopControl, os.O_RDWR, 0)
	if err != nil {
		return 0, fmt.Errorf("loop: open %s: %w", devLoopControl, err)
	}
	defer ctl.Close()
	ret, err := ioctlRetIntArg(int(ctl.Fd()), unix.LOOP_CTL_ADD, n)
	if err != nil {
		return 0, fmt.Errorf("loop: LOOP_CTL_ADD %d: %w", n, err)
	}
	return ret, nil
}

// ioctlRetIntArg issues an ioctl with integer argument arg and returns the
// non-negative integer the kernel reports (or the wrapped errno). x/sys's
// IoctlRetInt always passes 0 as the argument and IoctlSetInt discards the
// return value; LOOP_CTL_ADD needs both, so we make the raw syscall here.
func ioctlRetIntArg(fd int, req uint, arg int) (int, error) {
	r, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(req), uintptr(arg))
	if errno != 0 {
		return 0, errno
	}
	return int(r), nil
}

// CtlRemove destroys the unused /dev/loopN with index n via LOOP_CTL_REMOVE. It
// fails with EBUSY if the device is currently bound to a backing file.
func CtlRemove(n int) error {
	ctl, err := os.OpenFile(devLoopControl, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("loop: open %s: %w", devLoopControl, err)
	}
	defer ctl.Close()
	if err := unix.IoctlSetInt(int(ctl.Fd()), unix.LOOP_CTL_REMOVE, n); err != nil {
		return fmt.Errorf("loop: LOOP_CTL_REMOVE %d: %w", n, err)
	}
	return nil
}

// DeviceNumber parses the trailing integer N of a "/dev/loopN" path.
func DeviceNumber(devPath string) (int, error) {
	base := filepath.Base(devPath)
	const prefix = "loop"
	if !strings.HasPrefix(base, prefix) {
		return 0, fmt.Errorf("loop: %q is not a loop device path", devPath)
	}
	return strconv.Atoi(strings.TrimPrefix(base, prefix))
}
