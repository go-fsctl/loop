# go-fsctl/loop

[![Go Reference](https://pkg.go.dev/badge/github.com/go-fsctl/loop.svg)](https://pkg.go.dev/github.com/go-fsctl/loop)
[![License: BSD-3-Clause](https://img.shields.io/badge/license-BSD--3--Clause-blue)](LICENSE)
[![CI](https://github.com/go-fsctl/loop/actions/workflows/ci.yml/badge.svg)](https://github.com/go-fsctl/loop/actions/workflows/ci.yml)

Pure-Go Linux loop-device control: attach, configure, inspect, and detach
loop devices directly through `/dev/loop-control` and the `LOOP_*` ioctls —
the same kernel interface util-linux's `losetup` uses — with **no cgo** and
**no shelling out** to `losetup`.

This is the loop-device member of the [`go-fsctl`](https://github.com/go-fsctl)
family, alongside [`go-fsctl/zfs`](https://github.com/go-fsctl/zfs) and
[`go-fsctl/btrfs`](https://github.com/go-fsctl/btrfs): where those packages
talk to the OpenZFS and btrfs kernel modules through their native control
paths, this package allocates and tears down block-level loop devices via the
kernel's loop driver.

## Status

Validated against a live Linux 6.12 kernel (arm64): `go-fsctl/loop` attaches a
backing file, the attachment is cross-checked with `losetup -a` / `losetup -j`,
`Status()` matches, a write/read round-trip succeeds on `/dev/loopN`, and
`Detach()` removes it. The ABI structs and `LOOP_*` numbers are derived from the
kernel uapi header `linux/loop.h`; see `abi.go` and the host-runnable
`abi_test.go`.

## API

```go
import "github.com/go-fsctl/loop"

// Attach a backing file to the first free loop device (LOOP_CTL_GET_FREE +
// LOOP_CONFIGURE, falling back to LOOP_SET_FD + LOOP_SET_STATUS64):
dev, err := loop.Attach("/path/to/disk.img", loop.Options{
    Offset:    1 << 20, // start 1 MiB into the file
    SizeLimit: 0,        // 0 = to end of file
    ReadOnly:  false,
    Autoclear: false,    // LO_FLAGS_AUTOCLEAR
    PartScan:  false,    // LO_FLAGS_PARTSCAN
})
// dev == "/dev/loop3"

// Inspect it (LOOP_GET_STATUS64):
info, err := loop.Status(dev)
// info.Number, info.Offset, info.SizeLimit, info.Flags, info.BackingFile
// info.ReadOnly(), info.Autoclear(), info.PartScan()

// Find devices backed by a given file (scans /sys/block/loop*/loop/backing_file):
devs, err := loop.FindByBacking("/path/to/disk.img")

// Re-read the backing file size after growing it (LOOP_SET_CAPACITY):
err = loop.SetCapacity(dev)

// Detach (LOOP_CLR_FD):
err = loop.Detach(dev)

// Manage device nodes via /dev/loop-control (optional):
n, err := loop.CtlAdd(8)    // LOOP_CTL_ADD  -> creates /dev/loop8
err = loop.CtlRemove(8)     // LOOP_CTL_REMOVE

// Probe support without privileges:
ok := loop.Available()      // /dev/loop-control exists
```

All mutating operations require `CAP_SYS_ADMIN` (in practice, root). On
non-Linux platforms every function returns `loop.ErrUnsupported`.

## LOOP_* ioctls used

| Operation        | ioctl                              |
|------------------|------------------------------------|
| `Attach`         | `LOOP_CTL_GET_FREE`, `LOOP_CONFIGURE` (fallback `LOOP_SET_FD` + `LOOP_SET_STATUS64`) |
| `Detach`         | `LOOP_CLR_FD`                      |
| `Status`         | `LOOP_GET_STATUS64`                |
| `SetCapacity`    | `LOOP_SET_CAPACITY`                |
| `CtlAdd`         | `LOOP_CTL_ADD`                     |
| `CtlRemove`      | `LOOP_CTL_REMOVE`                  |

`FindByBacking` reads `/sys/block/loop*/loop/backing_file` rather than issuing
an ioctl.

## Testing

```sh
# Host-runnable unit tests (ioctl numbers, struct sizes/offsets, flags):
GOWORK=off go test ./...

# Integration tests are gated on /dev/loop-control + root and run on a Linux box:
sudo -E env "PATH=$PATH" go test -run Integration -v ./...   # (or just: sudo -E go test ./...)
```

There is also a live demo binary, `cmd/loopprobe`, that attaches a temp image,
prints its kernel status, does a round-trip, and detaches it.

## License

BSD-3-Clause. See [LICENSE](LICENSE).
