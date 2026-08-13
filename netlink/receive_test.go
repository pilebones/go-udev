//go:build linux

package netlink

import (
	"bytes"
	"errors"
	"strings"
	"syscall"
	"testing"
)

// The receive checks are hard to observe on a real uevent socket: the kernel only lets a process holding
// CAP_NET_ADMIN write on it, and it never produces the malformed messages the checks are there for. Since
// the descriptor is a field of the connection, the tests below point it at a socket of their own and feed it
// exactly the message they want to see rejected.

// receiverOptions applies to an injected socket what Connect applies to a uevent one: the credentials of the
// sender are passed, and a receive gives up rather than hanging the whole test binary.
func receiverOptions(t *testing.T, fd int) {
	t.Helper()

	if err := syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_PASSCRED, 1); err != nil {
		t.Fatal("unable to ask for the sender credentials, err:", err)
	}

	timeout := syscall.NsecToTimeval(readTimeout.Nanoseconds())
	if err := syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &timeout); err != nil {
		t.Fatal("unable to set the receive timeout, err:", err)
	}
}

// unixDatagramPair returns a connected pair of unix datagram sockets, the receiving end being set up like a
// uevent socket.
func unixDatagramPair(t *testing.T) (receiver, sender int) {
	t.Helper()

	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_DGRAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal("unable to open a socket pair, err:", err)
	}
	t.Cleanup(func() {
		_ = syscall.Close(fds[0])
		_ = syscall.Close(fds[1])
	})

	receiverOptions(t, fds[0])

	return fds[0], fds[1]
}

// netlinkUserSocket returns a bound NETLINK_USERSOCK socket. That family is the one the kernel leaves to user
// space, so an unprivileged process may write on it, where NETLINK_KOBJECT_UEVENT refuses.
func netlinkUserSocket(t *testing.T) int {
	t.Helper()

	fd, err := syscall.Socket(syscall.AF_NETLINK, syscall.SOCK_RAW|syscall.SOCK_CLOEXEC, syscall.NETLINK_USERSOCK)
	if err != nil {
		t.Fatal("unable to open a netlink socket, err:", err)
	}
	t.Cleanup(func() {
		_ = syscall.Close(fd)
	})

	receiverOptions(t, fd)

	if err := syscall.Bind(fd, &syscall.SockaddrNetlink{Family: syscall.AF_NETLINK}); err != nil {
		t.Fatal("unable to bind the netlink socket, err:", err)
	}

	return fd
}

// TestReceiveRejectsTruncatedMessage ensures a message which does not fit is reported instead of being parsed
// as if it were complete, which would turn it into an event about the wrong device.
func TestReceiveRejectsTruncatedMessage(t *testing.T) {
	receiver, sender := unixDatagramPair(t)
	conn := &UEventConn{NetlinkConn: NetlinkConn{Fd: receiver}}

	if _, err := syscall.Write(sender, bytes.Repeat([]byte("a"), 64)); err != nil {
		t.Fatal("unable to send the message, err:", err)
	}

	_, err := conn.receive(make([]byte, 8), make([]byte, oobBufferSize))
	if !errors.Is(err, ErrTruncatedMessage) {
		t.Fatalf("a message longer than the buffer should be dropped, err: %v", err)
	}
	t.Log("Got expected rejection:", err)
}

// TestReceiveRejectsTruncatedCredentials ensures a message whose credentials could not be read entirely is
// dropped rather than trusted, since there is then no way to tell who sent it.
func TestReceiveRejectsTruncatedCredentials(t *testing.T) {
	receiver, sender := unixDatagramPair(t)
	conn := &UEventConn{NetlinkConn: NetlinkConn{Fd: receiver}}

	if _, err := syscall.Write(sender, []byte("add@/devices/foo\x00")); err != nil {
		t.Fatal("unable to send the message, err:", err)
	}

	// One byte of control data is not enough to hold the credentials the kernel attaches.
	_, err := conn.receive(make([]byte, readBufferSize), make([]byte, 1))
	if !errors.Is(err, ErrUntrustedSender) {
		t.Fatalf("credentials which cannot be read should not be trusted, err: %v", err)
	}
	if !strings.Contains(err.Error(), "control data truncated") {
		t.Fatalf("the truncated control data should be the reason, err: %v", err)
	}
	t.Log("Got expected rejection:", err)
}

// TestReceiveRejectsNonNetlinkSender ensures a message which does not even come from a netlink socket is
// dropped, the sender checks having nothing to work with otherwise.
func TestReceiveRejectsNonNetlinkSender(t *testing.T) {
	receiver, sender := unixDatagramPair(t)
	conn := &UEventConn{NetlinkConn: NetlinkConn{Fd: receiver}}

	if _, err := syscall.Write(sender, []byte("add@/devices/foo\x00")); err != nil {
		t.Fatal("unable to send the message, err:", err)
	}

	_, err := conn.receive(make([]byte, readBufferSize), make([]byte, oobBufferSize))
	if !errors.Is(err, ErrUntrustedSender) {
		t.Fatalf("a sender which is not a netlink one should be refused, err: %v", err)
	}
	if !strings.Contains(err.Error(), "not a netlink one") {
		t.Fatalf("the sender address should be the reason, err: %v", err)
	}
	t.Log("Got expected rejection:", err)
}

// TestReceiveRejectsUnicastMessage ensures a message addressed to us alone is dropped: uevents are always
// multicast, so a unicast one was written by a process which picked us as a target.
func TestReceiveRejectsUnicastMessage(t *testing.T) {
	receiver := netlinkUserSocket(t)
	sender := netlinkUserSocket(t)

	name, err := syscall.Getsockname(receiver)
	if err != nil {
		t.Fatal("unable to get the socket name, err:", err)
	}
	target, ok := name.(*syscall.SockaddrNetlink)
	if !ok {
		t.Fatalf("unexpected socket address type: %T", name)
	}

	to := &syscall.SockaddrNetlink{Family: syscall.AF_NETLINK, Pid: target.Pid}
	if err := syscall.Sendto(sender, []byte("add@/devices/foo\x00"), 0, to); err != nil {
		t.Skip("unable to send a netlink message, nothing to reproduce, err: ", err)
	}

	conn := &UEventConn{NetlinkConn: NetlinkConn{Fd: receiver}}

	_, err = conn.receive(make([]byte, readBufferSize), make([]byte, oobBufferSize))
	if !errors.Is(err, ErrUntrustedSender) {
		t.Fatalf("a unicast message should be refused, err: %v", err)
	}
	if !strings.Contains(err.Error(), "unicast message sent by pid") {
		t.Fatalf("the unicast delivery should be the reason, err: %v", err)
	}
	t.Log("Got expected rejection:", err)
}
