//go:build linux

package netlink

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestConnect(t *testing.T) {
	conn := new(UEventConn)
	if err := conn.Connect(UdevEvent); err != nil {
		t.Fatal("unable to subscribe to netlink uevent, err:", err)
	}
	defer func() {
		_ = conn.Close()
	}()

	// ensure it is possible to subscribe several times in parallel
	conn2 := new(UEventConn)
	if err := conn2.Connect(UdevEvent); err != nil {
		t.Fatal("unable to subscribe to netlink uevent a second time, err:", err)
	}
	defer func() {
		_ = conn2.Close()
	}()
}

func TestMonitor(t *testing.T) {
	conn := new(UEventConn)
	if err := conn.Connect(UdevEvent); err != nil {
		t.Fatal("unable to subscribe to netlink uevent, err:", err)
	}
	defer func() {
		_ = conn.Close()
	}()

	queue := make(chan UEvent)
	errs := make(chan error)
	quit := conn.Monitor(queue, errs, nil)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second) // Monitor 5s then stop for testing
	// The errs channel is left open on purpose: the worker may still emit while the test returns.
	defer cancel()
	defer waitMonitoringStopped(t, func() { close(quit) }, queue)

loop:
	for {
		select {
		case <-ctx.Done():
			t.Log("Reach timeout while monitoring netlink uevent channel, everything is fine.")
			break loop // stop iteration in case of stop signal received
		case uevent, more := <-queue:
			if !more {
				t.Log("Monitoring stopped")
				break loop
			}
			t.Log("Handle", uevent.String())
		case err := <-errs:
			t.Fatalf("unable to monitor uevent, err: %v", err)
		}
	}
}

// waitMonitoringStopped stops the monitoring and waits for the worker to have left its last receive, which
// it signals by closing the queue. The socket can only be closed afterwards: closing it earlier would write
// the descriptor field while the worker is still reading it.
func waitMonitoringStopped(t *testing.T, stop func(), queue chan UEvent) {
	t.Helper()

	stop()
	for range queue { // drain until the worker closes the queue
	}
}

// TestMonitorStopsWithoutEvent ensures the worker notices the stop signal even when the machine is idle.
// It used to stay blocked inside recvfrom until the next event, which may never come.
func TestMonitorStopsWithoutEvent(t *testing.T) {
	conn := new(UEventConn)
	if err := conn.Connect(UdevEvent); err != nil {
		t.Fatal("unable to subscribe to netlink uevent, err:", err)
	}
	defer func() {
		_ = conn.Close()
	}()

	queue := make(chan UEvent)
	errs := make(chan error, 1)
	quit := conn.Monitor(queue, errs, nil)

	close(quit)

	deadline := time.After(5 * time.Second)
	for {
		select {
		case _, more := <-queue:
			if !more {
				return // the worker stopped as expected
			}
		case err := <-errs:
			t.Fatalf("unable to monitor uevent, err: %v", err)
		case <-deadline:
			t.Fatal("Monitor did not stop, it is probably still waiting for an event")
		}
	}
}

// TestMonitorWithContextStopsOnCancel ensures cancelling the context stops the worker on an idle machine.
func TestMonitorWithContextStopsOnCancel(t *testing.T) {
	conn := new(UEventConn)
	if err := conn.Connect(UdevEvent); err != nil {
		t.Fatal("unable to subscribe to netlink uevent, err:", err)
	}
	defer func() {
		_ = conn.Close()
	}()

	queue := make(chan UEvent)
	errs := make(chan error, 1)

	ctx, cancel := context.WithCancel(t.Context())
	go conn.MonitorWithContext(ctx, queue, errs, nil)

	cancel()

	deadline := time.After(5 * time.Second)
	for {
		select {
		case _, more := <-queue:
			if !more {
				return // the worker stopped as expected
			}
		case err := <-errs:
			t.Fatalf("unable to monitor uevent, err: %v", err)
		case <-deadline:
			t.Fatal("MonitorWithContext did not stop, it is probably still waiting for an event")
		}
	}
}

// TestReceiveTimesOut ensures the socket carries the receive timeout which lets the monitoring notice a
// cancelled context on an idle machine.
func TestReceiveTimesOut(t *testing.T) {
	conn := new(UEventConn)
	if err := conn.Connect(UdevEvent); err != nil {
		t.Fatal("unable to subscribe to netlink uevent, err:", err)
	}
	defer func() {
		_ = conn.Close()
	}()

	start := time.Now()
	_, err := conn.receive(make([]byte, readBufferSize), make([]byte, oobBufferSize))
	if err == nil {
		t.Skip("an event showed up while waiting for the timeout, nothing to observe")
	}
	if !errors.Is(err, errReadTimeout) {
		t.Fatalf("receive should time out on an idle socket, err: %v", err)
	}
	if elapsed := time.Since(start); elapsed < readTimeout {
		t.Fatalf("receive returned after %s, sooner than the %s timeout", elapsed, readTimeout)
	}
}

// TestReadMsgWaitsForMessage ensures the receive timeout used to stop the monitoring stays invisible to
// ReadMsg, which is documented as blocking until a message arrives.
func TestReadMsgWaitsForMessage(t *testing.T) {
	conn := new(UEventConn)
	if err := conn.Connect(UdevEvent); err != nil {
		t.Fatal("unable to subscribe to netlink uevent, err:", err)
	}
	// The socket is deliberately left open: the goroutine below stays inside recvmsg until an event shows up
	// and closing the descriptor under it would either race with it or hand it a descriptor reused
	// meanwhile. The kernel releases it when the test binary exits.

	// Buffered so the goroutine cannot leak once the test returned.
	done := make(chan error, 1)
	go func() {
		_, err := conn.ReadMsg()
		done <- err
	}()

	select {
	case err := <-done:
		// A real event may show up on a busy machine, only a timeout error would be a bug.
		if err != nil {
			t.Fatalf("ReadMsg should keep waiting instead of returning, err: %v", err)
		}
	case <-time.After(3 * readTimeout):
		t.Log("ReadMsg is still waiting for a message, everything is fine.")
	}
}

// TestMonitorWithInvalidMatcher ensures a broken matcher is reported through the error channel
// instead of deadlocking the caller, which cannot read the channel before Monitor returns.
func TestMonitorWithInvalidMatcher(t *testing.T) {
	conn := new(UEventConn)
	if err := conn.Connect(UdevEvent); err != nil {
		t.Fatal("unable to subscribe to netlink uevent, err:", err)
	}
	defer func() {
		_ = conn.Close()
	}()

	invalid := "([" // not a valid regexp
	matcher := &RuleDefinitions{Rules: []RuleDefinition{{Action: &invalid}}}

	queue := make(chan UEvent)
	errs := make(chan error)
	quit := conn.Monitor(queue, errs, matcher)
	defer close(quit)

	select {
	case err := <-errs:
		t.Log("Got expected error:", err)
	case <-time.After(5 * time.Second):
		t.Fatal("Monitor did not report the invalid matcher, it is probably deadlocked")
	}

	if _, more := <-queue; more {
		t.Fatal("queue should be closed when monitoring aborts")
	}
}
