//go:build linux

package netlink

import (
	"context"
	"errors"
	"syscall"
	"testing"
	"time"
)

// credentials builds the control message the kernel attaches to a received message thanks to SO_PASSCRED.
func credentials(pid int32, uid, gid uint32) []byte {
	return syscall.UnixCredentials(&syscall.Ucred{Pid: pid, Uid: uid, Gid: gid})
}

func TestCheckSender(t *testing.T) {
	kernel := &syscall.SockaddrNetlink{Family: syscall.AF_NETLINK, Pid: 0, Groups: uint32(KernelEvent)}
	udevd := &syscall.SockaddrNetlink{Family: syscall.AF_NETLINK, Pid: 4242, Groups: uint32(UdevEvent)}

	for _, test := range []struct {
		name     string
		from     syscall.Sockaddr
		oob      []byte
		accepted bool
	}{{
		name:     "kernel event",
		from:     kernel,
		oob:      credentials(0, 0, 0),
		accepted: true,
	}, {
		name:     "udevd event",
		from:     udevd,
		oob:      credentials(4242, 0, 0),
		accepted: true,
	}, {
		// The kernel uid is translated to the overflow uid in a user namespace where root is not mapped,
		// such an event must still be delivered.
		name:     "kernel event seen from a user namespace",
		from:     kernel,
		oob:      credentials(0, 65534, 65534),
		accepted: true,
	}, {
		name: "unicast message",
		from: &syscall.SockaddrNetlink{Family: syscall.AF_NETLINK, Pid: 4242},
		oob:  credentials(4242, 0, 0),
	}, {
		name: "kernel group message sent from user space",
		from: &syscall.SockaddrNetlink{Family: syscall.AF_NETLINK, Pid: 4242, Groups: uint32(KernelEvent)},
		oob:  credentials(4242, 0, 0),
	}, {
		name: "udev group message sent by an unprivileged process",
		from: udevd,
		oob:  credentials(4242, 1000, 1000),
	}, {
		name: "message without credentials",
		from: udevd,
		oob:  nil,
	}, {
		name: "message with unrelated control data only",
		from: udevd,
		oob:  syscall.UnixRights(0),
	}, {
		name: "sender address of another family",
		from: &syscall.SockaddrInet4{},
		oob:  credentials(0, 0, 0),
	}} {
		t.Run(test.name, func(t *testing.T) {
			err := checkSender(test.from, test.oob)
			switch {
			case test.accepted && err != nil:
				t.Fatalf("message should have been accepted, got: %v", err)
			case !test.accepted && err == nil:
				t.Fatal("message should have been rejected")
			case !test.accepted && !errors.Is(err, ErrUntrustedSender):
				t.Fatalf("rejection should wrap ErrUntrustedSender, got: %v", err)
			case !test.accepted:
				t.Log("Got expected rejection:", err)
			}
		})
	}
}

// TestMonitorIgnoresForgedUEvent sends an uevent from another socket of this very process, the way a
// process would to make consumers act on a device which does not exist, and ensures it never reaches the
// queue. Recent kernels already refuse that send without CAP_NET_ADMIN, in which case there is nothing to
// reproduce and the test is skipped.
func TestMonitorIgnoresForgedUEvent(t *testing.T) {
	conn := new(UEventConn)
	if err := conn.Connect(UdevEvent); err != nil {
		t.Fatal("unable to subscribe to netlink uevent, err:", err)
	}
	defer func() {
		_ = conn.Close()
	}()

	// The port id assigned at bind time is what a forged message has to target.
	name, err := syscall.Getsockname(conn.Fd)
	if err != nil {
		t.Fatal("unable to get the socket name, err:", err)
	}

	target, ok := name.(*syscall.SockaddrNetlink)
	if !ok {
		t.Fatalf("unexpected socket address type: %T", name)
	}

	sender, err := syscall.Socket(syscall.AF_NETLINK, syscall.SOCK_RAW|syscall.SOCK_CLOEXEC, syscall.NETLINK_KOBJECT_UEVENT)
	if err != nil {
		t.Fatal("unable to open the sending socket, err:", err)
	}
	defer func() {
		_ = syscall.Close(sender)
	}()

	const forgedKObj = "/devices/forged"
	forged := UEvent{
		Action: ADD,
		KObj:   forgedKObj,
		Env:    map[string]string{"ACTION": "add", "DEVPATH": forgedKObj, "SUBSYSTEM": "block"},
	}

	to := &syscall.SockaddrNetlink{Family: syscall.AF_NETLINK, Pid: target.Pid}
	if err := syscall.Sendto(sender, forged.Bytes(), 0, to); err != nil {
		if errors.Is(err, syscall.EPERM) {
			t.Skip("the kernel refuses to send an uevent without CAP_NET_ADMIN, nothing to reproduce")
		}
		t.Fatal("unable to send the forged uevent, err:", err)
	}

	queue := make(chan UEvent)
	errs := make(chan error, 8)

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	go conn.MonitorWithContext(ctx, queue, errs, nil)

	for {
		select {
		case uevent, more := <-queue:
			if !more {
				return // monitoring stopped without ever delivering the forged event
			}
			if uevent.KObj == forgedKObj {
				t.Fatal("a forged uevent was delivered to the queue")
			}
			t.Log("Ignoring a genuine event about", uevent.KObj)
		case err := <-errs:
			if !errors.Is(err, ErrUntrustedSender) {
				t.Fatalf("unexpected error while monitoring: %v", err)
			}
			t.Log("Got expected rejection:", err)
		}
	}
}
