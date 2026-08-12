# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased](https://github.com/pilebones/go-udev/compare/v0.9.2...master)

### Security

- Uevents which do not come from the kernel or from udevd are now dropped and reported as the new
  `ErrUntrustedSender`, following what libudev does: the sender port id, its multicast group and the
  credentials attached by `SO_PASSCRED` are checked on every message. Forging an uevent, hence making every
  consumer act on a device which does not exist, used to only require being able to write on the socket.

### Added

- `UEventConn.MonitorWithContext` monitors until its `context.Context` is cancelled. `Monitor` is kept as
  a wrapper for existing callers but the context based API should be preferred.

### Changed

- **Requirement**: `go.mod`, the builder image and the CI toolchain move from Go 1.24 to Go 1.26, so building
  the module now needs a Go 1.26 toolchain. The modernizations `go fix` suggests for that language version are
  applied: `interface{}` becomes `any`, a counting loop becomes `for range 2`, and the two `String` methods
  concatenating in a loop use a `strings.Builder`. Tests derive their context from `t.Context()`, so a worker
  cannot outlive its test.
- Cancelling a monitoring now stops its worker within 200 ms even when no event happens, where closing the
  `Monitor` quit channel used to be noticed only when the next event woke the worker up, which on an idle
  machine could be never. `ReadMsg` and `ReadUEvent` still block until a message arrives.
- Reading a uevent now takes a single `recvmsg` syscall instead of a `MSG_PEEK` probe, one syscall per
  buffer enlargement and a final read. A message longer than 32 KiB, which the kernel never produces, is
  reported as `ErrTruncatedMessage` and dropped instead of being handed over partially parsed.
- `Connect` enlarges the socket receive queue to 2 MiB, configurable with the new
  `UEventConn.ReceiveBufferSize` field, to avoid the kernel dropping events during a burst.
- The command line tool moved from the root package to `cmd/go-udev`, install it with
  `go install github.com/pilebones/go-udev/cmd/go-udev@latest` and build it with `go build ./cmd/go-udev`.
- The `github.com/kr/pretty` dependency is dropped, the module now has no external dependency at all. Uevents are
  printed by a local formatter which sorts the env vars, so the output is stable from one run to the next.

### Fixed

- `netlink`: `TestParseUdevEvent` embedded libudev headers captured on a little endian machine, so it was skipped on
  `s390x` and failed outright on `ppc64`, `mips` and `mips64`, which the skip list did not cover. The headers are now
  built at test time in the byte order of the platform, so the udev format is really covered everywhere.

### Added

- `netlink`: a libudev header written with the opposite byte order is now rejected with an explicit error instead of
  being decoded as a bogus payload offset. The magic number is byte order agnostic, so the header size is what
  detects it.

## [v0.9.2](https://github.com/pilebones/go-udev/compare/v0.9.1...v0.9.2) - 2026-08-12

Bug fix release focused on the robustness of the parsing, of the matcher and of the shutdown paths.

### Fixed

- `netlink`: `ParseUEvent()` panicked with `slice bounds out of range` on any message not ending with a NUL byte.
  Malformed and truncated input is now reported as an error, as expected from an exported parsing function.
- `netlink`: env values containing a `=` were rejected instead of being kept as-is, only the first separator splits
  the name from the value now, consistently with the `crawler` package.
- `netlink`: `RuleDefinitions.Compile()` compiled copies of the rules and discarded the result, so every regexp of
  every rule was recompiled for each incoming uevent. Rules are now compiled once, in place.
- `netlink`: `Monitor()` and `crawler`: `ExistingDevices()` emitted the matcher error on the error channel before
  returning the quit channel, deadlocking any caller using an unbuffered channel (including the provided CLI).
- `netlink`: `UEventConn.Close()` closed file descriptor 0 (stdin) when called on a connection that was never
  connected, since 0 is the zero value of the `Fd` field.
- `crawler`: a single unreadable `uevent` file (missing permission, device unplugged while crawling) aborted the
  whole walk of `/sys/devices`. Such errors are now reported and the walk continues; a missing sysfs root stays fatal.
- `cli`: running the binary without `-monitor` nor `-info` exited silently with status 0 instead of printing the usage.
- `cli`: the error reported for an empty rules file contained a `%!w(<nil>)` placeholder.
- `docker`: the image could not be built at all, the builder image was still Go 1.18 while `go.mod` requires Go 1.24.

### Changed

- **API**: `RuleDefinition` and `RuleDefinitions` methods now use pointer receivers, which is what makes the compiled
  rules persist. Code evaluating a non-addressable value, such as `RuleDefinitions{...}.Evaluate(e)`, must take a
  pointer instead. Values stored in a variable are unaffected.
- **API**: `Monitor()` and `ExistingDevices()` now close the event queue when their worker stops, whatever the reason,
  so callers can detect the end of the processing. Callers reading the queue with `select` should check the second
  return value.
- **API**: `Close()` returns the new `ErrNotConnected` error instead of `EBADF` when no socket is held.
- `netlink`: the libudev payload offset is read with `binary.NativeEndian` instead of `unsafe.Pointer`, removing the
  last use of `unsafe` from the module.
- `docker`: the misspelled `Dockfile.multistage` is renamed `Dockerfile`, the runtime image is pinned and the binary
  runs as an unprivileged user.

### Added

- `netlink`: the package now builds on non-Linux platforms. Uevent parsing and matching stay available, while
  `UEventConn` methods return the new `ErrNotSupported` error. The CI checks the darwin and windows builds.
- Regression tests for the parsing of malformed messages, for the persistence of compiled rules and for the matcher
  error path of both workers.

### Security

- `netlink`: the netlink socket is created with `SOCK_CLOEXEC` so it is no longer leaked to child processes.
- `docker`: the container no longer runs as root; receiving netlink uevents and reading `/sys` require no privilege.

### CI

- Deprecated actions bumped (`checkout@v4`, `setup-go@v5`, `golangci-lint-action@v8`), tests run with `-race`,
  `gofmt` and cross-platform builds are checked, and the linter set is pinned in `.golangci.yml`.

## [v0.9.1](https://github.com/pilebones/go-udev/compare/v0.9.0...v0.9.1) - 2025-04-21

Maintenance release, no change in the library behaviour.

### Changed

- CI moved from Travis to GitHub Actions, with a dedicated `golangci-lint` workflow and Coveralls reporting.
- `go.mod` and the CI toolchain bumped to Go 1.24.
- Multi-stage Docker image and related documentation added.
- README badges refreshed.

### Added

- Additional tests, mainly around uevent equality and env parsing from a data stream.

## [v0.9.0](https://github.com/pilebones/go-udev/releases/tag/v0.9.0) - 2022-02-17

First tagged release. See the git history for the details.
