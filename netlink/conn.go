//go:build linux

package netlink

import (
	"errors"
	"fmt"
	"os"
	"syscall"
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

// ErrNotConnected is returned when an operation needs a socket but Connect() was not called (or already closed).
var ErrNotConnected = errors.New("netlink socket not connected")

// Generic connection
type NetlinkConn struct {
	Fd   int
	Addr syscall.SockaddrNetlink
}

type UEventConn struct {
	NetlinkConn

	// Options
	MatchedUEventLimit int // allow to stop monitor mode after X event(s) matched by the matcher
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

	c.Addr = syscall.SockaddrNetlink{
		Family: syscall.AF_NETLINK,
		Groups: uint32(mode),
	}

	if err = syscall.Bind(c.Fd, &c.Addr); err != nil {
		_ = syscall.Close(c.Fd)
		c.Fd = closedFd
	}

	return
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

func (c *UEventConn) msgPeek() (int, *[]byte, error) {
	var n int
	var err error

	if c.Fd <= 0 {
		return 0, nil, ErrNotConnected
	}

	buf := make([]byte, os.Getpagesize())
	for {
		// Just read how many bytes are available in the socket
		// WARNING: syscall.MSG_PEEK is a blocking call (no testable from CI)
		if n, _, err = syscall.Recvfrom(c.Fd, buf, syscall.MSG_PEEK); err != nil {
			return n, &buf, err
		}

		// If all message could be store inside the buffer : break
		if n < len(buf) {
			break
		}

		// Increase size of buffer if not enough
		buf = make([]byte, len(buf)+os.Getpagesize())
	}
	return n, &buf, err
}

func (c *UEventConn) msgRead(buf *[]byte) error {
	if buf == nil {
		return errors.New("empty buffer")
	}

	if c.Fd <= 0 {
		return ErrNotConnected
	}

	n, _, err := syscall.Recvfrom(c.Fd, *buf, 0)
	if err != nil {
		return err
	}

	// Extract only real data from buffer and return that
	*buf = (*buf)[:n]

	return nil
}

// ReadMsg allow to read an entire uevent msg
func (c *UEventConn) ReadMsg() (msg []byte, err error) {
	// Just read how many bytes are available in the socket
	_, buf, err := c.msgPeek()
	if err != nil {
		return nil, err
	}

	// Now read complete data
	err = c.msgRead(buf)

	return *buf, err
}

// ReadUEvent allow to read and parse an entire uevent msg
func (c *UEventConn) ReadUEvent() (*UEvent, error) {
	msg, err := c.ReadMsg()
	if err != nil {
		return nil, err
	}

	return ParseUEvent(msg)
}

// Monitor run in background a worker to read netlink msg in loop and notify
// when msg receive inside a queue using channel.
// To be notified with only relevant message, use Matcher.
// The queue is closed when the worker stops, whatever the reason, so the caller can detect the end of the monitoring.
func (c *UEventConn) Monitor(queue chan UEvent, errs chan error, matcher Matcher) chan struct{} {
	quit := make(chan struct{}, 1)

	go func() {
		// Never emit on errs before returning quit to the caller: an unbuffered errs channel
		// would block forever because the caller cannot read it yet.
		defer close(queue)

		if matcher != nil {
			if err := matcher.Compile(); err != nil {
				errs <- fmt.Errorf("wrong matcher, err: %w", err)
				return
			}
		}

		bufToRead := make(chan *[]byte, 1)
		count := 0
	loop:
		for {
			select {
			case <-quit:
				break loop // stop iteration in case of stop signal received
			case buf := <-bufToRead: // Read one by one
				err := c.msgRead(buf)
				if err != nil {
					errs <- fmt.Errorf("unable to read uevent, err: %w", err)
					break loop // stop iteration in case of error
				}

				uevent, err := ParseUEvent(*buf)
				if err != nil {
					errs <- fmt.Errorf("unable to parse uevent, err: %w", err)
					continue loop // Drop uevent if not known
				}

				if matcher != nil {
					if !matcher.Evaluate(*uevent) {
						continue loop // Drop uevent if not match
					}
				}
				select {
				case queue <- *uevent:
				case <-quit:
					break loop // stop iteration rather than blocking forever on a queue nobody reads
				}

				count++
				if c.MatchedUEventLimit > 0 && count >= c.MatchedUEventLimit {
					break loop // stop iteration when reach limit of uevent
				}
			default:
				_, buf, err := c.msgPeek()
				if err != nil {
					errs <- fmt.Errorf("unable to check available uevent, err: %w", err)
					break loop // stop iteration in case of error
				}
				bufToRead <- buf
			}
		}
	}()
	return quit
}
