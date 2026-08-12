//go:build !linux

package netlink

import (
	"context"
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

var (
	// ErrNotConnected is returned when an operation needs a socket but Connect() was not called (or already closed).
	ErrNotConnected = errors.New("netlink socket not connected")

	// ErrTruncatedMessage exists for API parity with Linux, where a message too big to be read is dropped.
	ErrTruncatedMessage = errors.New("netlink message too big, it is dropped")

	// ErrUntrustedSender exists for API parity with Linux, where a message which does not come from the
	// kernel or from udevd is dropped.
	ErrUntrustedSender = errors.New("netlink message from an untrusted sender, it is dropped")

	// ErrNotSupported is returned by every operation requiring a NETLINK_KOBJECT_UEVENT socket, which only exists on Linux.
	// The uevent parsing and matching API remains usable on other platforms.
	ErrNotSupported = errors.New("netlink kobject uevent is only supported on linux, got: " + runtime.GOOS)
)

// Generic connection
type NetlinkConn struct {
	Fd int
}

type UEventConn struct {
	NetlinkConn

	// Options

	// MatchedUEventLimit allows to stop monitor mode after X event(s) matched by the matcher
	MatchedUEventLimit int

	// ReceiveBufferSize is the size, in bytes, requested for the socket receive queue on Linux.
	ReceiveBufferSize int
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

// MonitorWithContext reports ErrNotSupported then returns immediately on non-Linux platforms.
func (c *UEventConn) MonitorWithContext(ctx context.Context, queue chan UEvent, errs chan error, matcher Matcher) {
	defer close(queue)

	select {
	case errs <- ErrNotSupported:
	case <-ctx.Done():
	}
}

// Monitor reports ErrNotSupported then stops immediately on non-Linux platforms.
func (c *UEventConn) Monitor(queue chan UEvent, errs chan error, matcher Matcher) chan struct{} {
	quit := make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(context.Background())

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		c.MonitorWithContext(ctx, queue, errs, matcher)
	}()

	go func() {
		defer cancel()
		select {
		case <-quit:
		case <-stopped:
		}
	}()

	return quit
}
