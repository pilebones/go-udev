//go:build linux

package netlink

import (
	"context"
	"errors"
	"fmt"
	"syscall"
	"time"
)

// Mode determines event source: kernel events or udev-processed events.
// See libudev/libudev-monitor.c.
type Mode int

const (
	KernelEvent Mode = 1
	// Events that are processed by udev - much richer, with more attributes (such as vendor info, serial numbers and more).
	UdevEvent Mode = 2
)

// closedFd is the value of the Fd field when the connection holds no socket.
// It is not zero because zero is a valid file descriptor (stdin), which must never be closed by mistake.
const closedFd = -1

// defaultReceiveBufferSize is the size requested for the socket receive queue when the caller does not
// provide one. The kernel default (net.core.rmem_default, usually a few hundred kilobytes) is filled by a
// single burst of uevents, for instance when a hub holding many devices is plugged, and every event which
// does not fit is silently dropped by the kernel.
const defaultReceiveBufferSize = 2 * 1024 * 1024

// readBufferSize bounds the size of one message. The kernel builds uevents in a 2 KiB buffer
// (UEVENT_BUFFER_SIZE) and libudev reads them in an 8 KiB one, so this leaves a wide margin.
const readBufferSize = 32 * 1024

// readTimeout is how long the socket waits for a message before giving the control back to the caller.
// A receive cannot be interrupted once started, so this is what bounds the delay between a monitoring
// context being cancelled and its worker actually stopping.
const readTimeout = 200 * time.Millisecond

var (
	// ErrNotConnected is returned when an operation needs a socket but Connect() was not called (or already closed).
	ErrNotConnected = errors.New("netlink socket not connected")

	// ErrTruncatedMessage is returned when a message does not fit in readBufferSize and had to be dropped.
	// It should never happen since the kernel produces much smaller messages, and it is not fatal: the
	// following messages are still readable.
	ErrTruncatedMessage = errors.New("netlink message too big, it is dropped")

	// errReadTimeout tells the receive queue stayed empty for readTimeout, which is not an error for a
	// caller waiting for hardware to show up. It never escapes this package.
	errReadTimeout = errors.New("netlink receive timed out")
)

// Generic connection
type NetlinkConn struct {
	Fd   int
	Addr syscall.SockaddrNetlink
}

type UEventConn struct {
	NetlinkConn

	// Options

	// MatchedUEventLimit allows to stop monitor mode after X event(s) matched by the matcher
	MatchedUEventLimit int

	// ReceiveBufferSize is the size, in bytes, requested for the socket receive queue. Events arriving
	// while the queue is full are dropped by the kernel, so a bigger queue lets a slow consumer survive a
	// burst of events. Zero means defaultReceiveBufferSize. The kernel caps the value to
	// net.core.rmem_max unless the process holds CAP_NET_ADMIN.
	ReceiveBufferSize int
}

// Connect allow to connect to system socket AF_NETLINK with family NETLINK_KOBJECT_UEVENT to
// catch events about block/char device
// see:
// - http://elixir.free-electrons.com/linux/v3.12/source/include/uapi/linux/netlink.h#L23
// - http://elixir.free-electrons.com/linux/v3.12/source/include/uapi/linux/socket.h#L11
func (c *UEventConn) Connect(mode Mode) (err error) {
	// SOCK_CLOEXEC avoids leaking the socket to child processes.
	if c.Fd, err = syscall.Socket(syscall.AF_NETLINK, syscall.SOCK_RAW|syscall.SOCK_CLOEXEC, syscall.NETLINK_KOBJECT_UEVENT); err != nil {
		c.Fd = closedFd
		return
	}

	// Bound the time spent inside a receive so a monitoring worker can notice its context was cancelled.
	timeout := syscall.NsecToTimeval(readTimeout.Nanoseconds())
	if err = syscall.SetsockoptTimeval(c.Fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &timeout); err != nil {
		c.closeFd()
		return
	}

	c.enlargeReceiveBuffer()

	c.Addr = syscall.SockaddrNetlink{
		Family: syscall.AF_NETLINK,
		Groups: uint32(mode),
	}

	if err = syscall.Bind(c.Fd, &c.Addr); err != nil {
		c.closeFd()
	}

	return
}

// enlargeReceiveBuffer grows the socket receive queue on a best effort basis: failing to grow it only
// means events are dropped earlier under load, which is not a reason to refuse to monitor.
func (c *UEventConn) enlargeReceiveBuffer() {
	size := c.ReceiveBufferSize
	if size <= 0 {
		size = defaultReceiveBufferSize
	}

	// SO_RCVBUFFORCE ignores net.core.rmem_max but requires CAP_NET_ADMIN, so fall back to the capped option.
	if err := syscall.SetsockoptInt(c.Fd, syscall.SOL_SOCKET, syscall.SO_RCVBUFFORCE, size); err == nil {
		return
	}

	_ = syscall.SetsockoptInt(c.Fd, syscall.SOL_SOCKET, syscall.SO_RCVBUF, size)
}

// Close allow to close file descriptor and socket bound
func (c *UEventConn) Close() error {
	// Guard against closing an unrelated descriptor: the zero value of the struct holds Fd 0 (stdin).
	if c.Fd <= 0 {
		return ErrNotConnected
	}

	err := syscall.Close(c.Fd)
	c.Fd = closedFd

	return err
}

// closeFd releases the socket when the connection could not be set up completely.
func (c *UEventConn) closeFd() {
	_ = syscall.Close(c.Fd)
	c.Fd = closedFd
}

// receive reads the next message into buf and returns its length.
// It needs a single syscall where the previous implementation needed at least three: one MSG_PEEK to
// measure the message, one more per buffer enlargement, and a last one to actually read it. recvmsg
// reports a truncation in its output flags, which is all we need to detect and report the messages that
// do not fit instead of silently handing over a partial one.
func (c *UEventConn) receive(buf []byte) (int, error) {
	if c.Fd <= 0 {
		return 0, ErrNotConnected
	}

	n, _, recvflags, _, err := syscall.Recvmsg(c.Fd, buf, nil, 0)
	if err != nil {
		// EWOULDBLOCK is EAGAIN on Linux, both mean SO_RCVTIMEO expired on an empty queue.
		if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EINTR) {
			return 0, errReadTimeout
		}

		return 0, err
	}

	if recvflags&syscall.MSG_TRUNC != 0 {
		return 0, fmt.Errorf("%w: read %d bytes out of a longer message", ErrTruncatedMessage, n)
	}

	return n, nil
}

// ReadMsg allow to read an entire uevent msg
// It blocks until a message is available, the underlying receive timeout is an implementation detail
// of the monitoring shutdown and is retried here.
func (c *UEventConn) ReadMsg() ([]byte, error) {
	buf := make([]byte, readBufferSize)

	for {
		n, err := c.receive(buf)
		if errors.Is(err, errReadTimeout) {
			continue
		}

		if err != nil {
			return nil, err
		}

		return buf[:n], nil
	}
}

// ReadUEvent allow to read and parse an entire uevent msg
func (c *UEventConn) ReadUEvent() (*UEvent, error) {
	msg, err := c.ReadMsg()
	if err != nil {
		return nil, err
	}

	return ParseUEvent(msg)
}

// MonitorWithContext reads netlink msg in loop and notifies when msg receive inside a queue using channel.
// To be notified with only relevant message, use Matcher.
//
// It blocks until ctx is cancelled, until the MatchedUEventLimit is reached or until the socket fails, so
// it is meant to be started with the go keyword. The queue is closed when it returns, whatever the reason,
// so the caller can detect the end of the monitoring.
//
// Cancelling ctx stops the worker within readTimeout, even when no event happens, where the Monitor quit
// channel used to be noticed only once the next event woke the worker up.
func (c *UEventConn) MonitorWithContext(ctx context.Context, queue chan UEvent, errs chan error, matcher Matcher) {
	defer close(queue)

	if matcher != nil {
		if err := matcher.Compile(); err != nil {
			c.notify(ctx, errs, fmt.Errorf("wrong matcher, err: %w", err))
			return
		}
	}

	// The buffer is reused for every message because parsing copies everything it keeps.
	buf := make([]byte, readBufferSize)
	count := 0

	for ctx.Err() == nil {
		n, err := c.receive(buf)
		switch {
		case errors.Is(err, errReadTimeout):
			continue // No event so far, loop to check the context again
		case errors.Is(err, ErrTruncatedMessage):
			// The message is lost but the socket is still usable, keep going.
			if !c.notify(ctx, errs, err) {
				return
			}
			continue
		case err != nil:
			c.notify(ctx, errs, fmt.Errorf("unable to read uevent, err: %w", err))
			return
		}

		uevent, err := ParseUEvent(buf[:n])
		if err != nil {
			if !c.notify(ctx, errs, fmt.Errorf("unable to parse uevent, err: %w", err)) {
				return
			}
			continue // Drop uevent if not known
		}

		if matcher != nil && !matcher.Evaluate(*uevent) {
			continue // Drop uevent if not match
		}

		select {
		case queue <- *uevent:
		case <-ctx.Done():
			return // stop iteration rather than blocking forever on a queue nobody reads
		}

		count++
		if c.MatchedUEventLimit > 0 && count >= c.MatchedUEventLimit {
			return // stop iteration when reach limit of uevent
		}
	}
}

// Monitor runs MonitorWithContext in background, closing (or writing to) the returned channel stops it.
// It is kept for callers written before the context based API, which should be preferred.
func (c *UEventConn) Monitor(queue chan UEvent, errs chan error, matcher Matcher) chan struct{} {
	quit := make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(context.Background())

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		c.MonitorWithContext(ctx, queue, errs, matcher)
	}()

	// Translate the quit channel into a context cancellation. Waiting for the worker as well keeps this
	// goroutine from leaking when it stops on its own, for instance on MatchedUEventLimit.
	go func() {
		defer cancel()
		select {
		case <-quit:
		case <-stopped:
		}
	}()

	return quit
}

// notify reports an error to the caller, giving up when the context is done so that a consumer which
// stopped reading errs cannot keep the worker alive forever.
func (c *UEventConn) notify(ctx context.Context, errs chan error, err error) bool {
	select {
	case errs <- err:
		return true
	case <-ctx.Done():
		return false
	}
}
