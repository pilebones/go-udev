//go:build !linux

package netlink

import (
	"errors"
	"runtime"
)

// Mode determines event source: kernel events or udev-processed events.
// See libudev/libudev-monitor.c.
type Mode int

const (
	KernelEvent Mode = 1
	// Events that are processed by udev - much richer, with more attributes (such as vendor info, serial numbers and more).
	UdevEvent Mode = 2
)

// ErrNotConnected is returned when an operation needs a socket but Connect() was not called (or already closed).
var ErrNotConnected = errors.New("netlink socket not connected")

// ErrNotSupported is returned by every operation requiring a NETLINK_KOBJECT_UEVENT socket, which only exists on Linux.
// The uevent parsing and matching API remains usable on other platforms.
var ErrNotSupported = errors.New("netlink kobject uevent is only supported on linux, got: " + runtime.GOOS)

// Generic connection
type NetlinkConn struct {
	Fd int
}

type UEventConn struct {
	NetlinkConn

	// Options
	MatchedUEventLimit int // allow to stop monitor mode after X event(s) matched by the matcher
}

// Connect always fails on non-Linux platforms.
func (c *UEventConn) Connect(mode Mode) error {
	return ErrNotSupported
}

// Close always fails on non-Linux platforms since no socket can be opened.
func (c *UEventConn) Close() error {
	return ErrNotConnected
}

// ReadMsg always fails on non-Linux platforms.
func (c *UEventConn) ReadMsg() ([]byte, error) {
	return nil, ErrNotSupported
}

// ReadUEvent always fails on non-Linux platforms.
func (c *UEventConn) ReadUEvent() (*UEvent, error) {
	return nil, ErrNotSupported
}

// Monitor reports ErrNotSupported then stops immediately on non-Linux platforms.
func (c *UEventConn) Monitor(queue chan UEvent, errs chan error, matcher Matcher) chan struct{} {
	quit := make(chan struct{}, 1)

	go func() {
		defer close(queue)
		errs <- ErrNotSupported
	}()

	return quit
}
