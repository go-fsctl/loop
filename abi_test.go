// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, go-fsctl

package loop

import (
	"testing"
	"unsafe"
)

// TestLoopIoctlNumbers pins the LOOP_* request numbers derived in abi.go to the
// values published in linux/loop.h (magic 'L' = 0x4c, encoding (0x4c<<8)|nr).
// These are the same numbers exported by golang.org/x/sys/unix.
func TestLoopIoctlNumbers(t *testing.T) {
	for _, c := range []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"SET_FD", LOOP_SET_FD, 0x4c00},
		{"CLR_FD", LOOP_CLR_FD, 0x4c01},
		{"SET_STATUS", LOOP_SET_STATUS, 0x4c02},
		{"GET_STATUS", LOOP_GET_STATUS, 0x4c03},
		{"SET_STATUS64", LOOP_SET_STATUS64, 0x4c04},
		{"GET_STATUS64", LOOP_GET_STATUS64, 0x4c05},
		{"CHANGE_FD", LOOP_CHANGE_FD, 0x4c06},
		{"SET_CAPACITY", LOOP_SET_CAPACITY, 0x4c07},
		{"SET_DIRECT_IO", LOOP_SET_DIRECT_IO, 0x4c08},
		{"SET_BLOCK_SIZE", LOOP_SET_BLOCK_SIZE, 0x4c09},
		{"CONFIGURE", LOOP_CONFIGURE, 0x4c0a},
		{"CTL_ADD", LOOP_CTL_ADD, 0x4c80},
		{"CTL_REMOVE", LOOP_CTL_REMOVE, 0x4c81},
		{"CTL_GET_FREE", LOOP_CTL_GET_FREE, 0x4c82},
	} {
		if c.got != c.want {
			t.Errorf("%s = %#x, want %#x", c.name, c.got, c.want)
		}
	}
}

// TestLoopIOHelper checks the loopIO derivation helper independently.
func TestLoopIOHelper(t *testing.T) {
	if got := loopIO(0x05); got != 0x4c05 {
		t.Errorf("loopIO(0x05) = %#x, want 0x4c05", got)
	}
	if got := loopIO(0x82); got != 0x4c82 {
		t.Errorf("loopIO(0x82) = %#x, want 0x4c82", got)
	}
}

// TestLoopFlags pins the LO_FLAGS_* bits to linux/loop.h.
func TestLoopFlags(t *testing.T) {
	for _, c := range []struct {
		name string
		got  uint32
		want uint32
	}{
		{"READ_ONLY", LO_FLAGS_READ_ONLY, 0x1},
		{"AUTOCLEAR", LO_FLAGS_AUTOCLEAR, 0x4},
		{"PARTSCAN", LO_FLAGS_PARTSCAN, 0x8},
		{"DIRECT_IO", LO_FLAGS_DIRECT_IO, 0x10},
	} {
		if c.got != c.want {
			t.Errorf("LO_FLAGS_%s = %#x, want %#x", c.name, c.got, c.want)
		}
	}
}

// TestStructSizes pins the ioctl struct sizes to the C sizeof() values from
// linux/loop.h on a 64-bit kernel. struct loop_info64 is 232 bytes
// (8*5 fixed + 4*4 ints + 64 + 64 + 32 names/key + 8*2 init) and struct
// loop_config is 304 bytes (4 + 4 + 232 + 8*8 reserved). A mismatch means the
// Go struct diverges from the kernel ABI.
func TestStructSizes(t *testing.T) {
	if got := unsafe.Sizeof(loopInfo64{}); got != 232 {
		t.Errorf("sizeof(loop_info64) = %d, want 232", got)
	}
	if got := unsafe.Sizeof(loopConfig{}); got != 304 {
		t.Errorf("sizeof(loop_config) = %d, want 304", got)
	}
}

// TestStructOffsets pins the byte offsets of the fields we actually read/write
// (offset, sizelimit, flags, file name) inside struct loop_info64, and the fd
// field inside struct loop_config, to their kernel ABI positions.
func TestStructOffsets(t *testing.T) {
	var li loopInfo64
	for _, c := range []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"lo_device", unsafe.Offsetof(li.Device), 0},
		{"lo_offset", unsafe.Offsetof(li.Offset), 24},
		{"lo_sizelimit", unsafe.Offsetof(li.Sizelimit), 32},
		{"lo_number", unsafe.Offsetof(li.Number), 40},
		{"lo_flags", unsafe.Offsetof(li.Flags), 52},
		{"lo_file_name", unsafe.Offsetof(li.FileName), 56},
		{"lo_crypt_name", unsafe.Offsetof(li.CryptName), 120},
		{"lo_encrypt_key", unsafe.Offsetof(li.EncryptKey), 184},
		{"lo_init", unsafe.Offsetof(li.Init), 216},
	} {
		if c.got != c.want {
			t.Errorf("offsetof(loop_info64.%s) = %d, want %d", c.name, c.got, c.want)
		}
	}

	var lc loopConfig
	for _, c := range []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"fd", unsafe.Offsetof(lc.Fd), 0},
		{"block_size", unsafe.Offsetof(lc.BlockSize), 4},
		{"info", unsafe.Offsetof(lc.Info), 8},
		{"__reserved", unsafe.Offsetof(lc.Reserved), 240},
	} {
		if c.got != c.want {
			t.Errorf("offsetof(loop_config.%s) = %d, want %d", c.name, c.got, c.want)
		}
	}
}
