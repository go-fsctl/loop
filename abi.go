// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, go-fsctl

package loop

import "unsafe"

// The Linux loop driver's request numbers are defined in the kernel uapi
// header linux/loop.h. Unlike most modern ioctls they are NOT _IO/_IOR/_IOW
// macros that fold in a direction and a struct size; they are plain
// sequential constants:
//
//	#define LOOP_SET_FD       0x4C00
//	#define LOOP_CLR_FD       0x4C01
//	#define LOOP_SET_STATUS   0x4C02
//	#define LOOP_GET_STATUS   0x4C03
//	#define LOOP_SET_STATUS64 0x4C04
//	#define LOOP_GET_STATUS64 0x4C05
//	#define LOOP_CHANGE_FD    0x4C06
//	#define LOOP_SET_CAPACITY 0x4C07
//	#define LOOP_SET_DIRECT_IO 0x4C08
//	#define LOOP_SET_BLOCK_SIZE 0x4C09
//	#define LOOP_CONFIGURE    0x4C0A
//	#define LOOP_CTL_ADD      0x4C80
//	#define LOOP_CTL_REMOVE   0x4C81
//	#define LOOP_CTL_GET_FREE 0x4C82
//
// i.e. (0x4C << 8) | nr, where 0x4C == 'L' is the loop magic. We recompute
// them in Go from the magic + nr rather than hard-coding the hex so the
// derivation is self-documenting and unit-testable; the expected hex (and the
// values published by golang.org/x/sys/unix) is pinned in abi_test.go.
//
// These same numbers come from golang.org/x/sys/unix (unix.LOOP_SET_FD etc.);
// loop_linux.go uses the unix constants directly. The derivation below exists
// so the ABI is auditable in pure Go and so the numbers are available on every
// platform for tooling and tests.

// loopMagic is the loop ioctl type byte, 'L'.
const loopMagic = 0x4c

// loopIO derives a loop request number the way linux/loop.h does: the magic
// byte 'L' in the high 8 bits, the sequential nr in the low 8 bits.
func loopIO(nr uintptr) uintptr { return (loopMagic << 8) | nr }

// Loop ioctl request numbers, derived from linux/loop.h.
const (
	LOOP_SET_FD         = (loopMagic << 8) | 0x00 // 0x4c00
	LOOP_CLR_FD         = (loopMagic << 8) | 0x01 // 0x4c01
	LOOP_SET_STATUS     = (loopMagic << 8) | 0x02 // 0x4c02
	LOOP_GET_STATUS     = (loopMagic << 8) | 0x03 // 0x4c03
	LOOP_SET_STATUS64   = (loopMagic << 8) | 0x04 // 0x4c04
	LOOP_GET_STATUS64   = (loopMagic << 8) | 0x05 // 0x4c05
	LOOP_CHANGE_FD      = (loopMagic << 8) | 0x06 // 0x4c06
	LOOP_SET_CAPACITY   = (loopMagic << 8) | 0x07 // 0x4c07
	LOOP_SET_DIRECT_IO  = (loopMagic << 8) | 0x08 // 0x4c08
	LOOP_SET_BLOCK_SIZE = (loopMagic << 8) | 0x09 // 0x4c09
	LOOP_CONFIGURE      = (loopMagic << 8) | 0x0a // 0x4c0a

	LOOP_CTL_ADD      = (loopMagic << 8) | 0x80 // 0x4c80
	LOOP_CTL_REMOVE   = (loopMagic << 8) | 0x81 // 0x4c81
	LOOP_CTL_GET_FREE = (loopMagic << 8) | 0x82 // 0x4c82
)

// LO_FLAGS_* are the loop device flag bits from linux/loop.h. They are mirrored
// here so callers and tests can reference them without importing x/sys on
// non-Linux platforms.
const (
	LO_FLAGS_READ_ONLY = 0x1  // device is read-only
	LO_FLAGS_AUTOCLEAR = 0x4  // detach automatically on last close
	LO_FLAGS_PARTSCAN  = 0x8  // scan the backing file for partitions
	LO_FLAGS_DIRECT_IO = 0x10 // use O_DIRECT to the backing file
)

// loNameSize is LO_NAME_SIZE from linux/loop.h: the length of the file-name and
// crypt-name byte arrays inside struct loop_info64.
const loNameSize = 64

// loKeySize is LO_KEY_SIZE from linux/loop.h: the length of the encryption-key
// byte array inside struct loop_info64.
const loKeySize = 32

// loopInfo64 mirrors the kernel's struct loop_info64 (linux/loop.h). It is
// layout-compatible with golang.org/x/sys/unix.LoopInfo64; we keep an explicit
// copy here so the ABI is documented and size/offset-tested in this package.
//
//	struct loop_info64 {
//		__u64 lo_device;
//		__u64 lo_inode;
//		__u64 lo_rdevice;
//		__u64 lo_offset;
//		__u64 lo_sizelimit;
//		__u32 lo_number;
//		__u32 lo_encrypt_type;
//		__u32 lo_encrypt_key_size;
//		__u32 lo_flags;
//		__u8  lo_file_name[LO_NAME_SIZE];
//		__u8  lo_crypt_name[LO_NAME_SIZE];
//		__u8  lo_encrypt_key[LO_KEY_SIZE];
//		__u64 lo_init[2];
//	};
type loopInfo64 struct {
	Device         uint64
	Inode          uint64
	Rdevice        uint64
	Offset         uint64
	Sizelimit      uint64
	Number         uint32
	EncryptType    uint32
	EncryptKeySize uint32
	Flags          uint32
	FileName       [loNameSize]uint8
	CryptName      [loNameSize]uint8
	EncryptKey     [loKeySize]uint8
	Init           [2]uint64
}

// loopConfig mirrors the kernel's struct loop_config (linux/loop.h), the
// argument to LOOP_CONFIGURE. It is layout-compatible with
// golang.org/x/sys/unix.LoopConfig.
//
//	struct loop_config {
//		__u32 fd;
//		__u32 block_size;
//		struct loop_info64 info;
//		__u64 __reserved[8];
//	};
type loopConfig struct {
	Fd        uint32
	BlockSize uint32
	Info      loopInfo64
	Reserved  [8]uint64
}

// abiSizeofLoopInfo64 / abiSizeofLoopConfig are recorded so abi_test.go can pin
// them against the kernel's C sizeof() values on a 64-bit kernel.
var (
	abiSizeofLoopInfo64 = unsafe.Sizeof(loopInfo64{})
	abiSizeofLoopConfig = unsafe.Sizeof(loopConfig{})
)
