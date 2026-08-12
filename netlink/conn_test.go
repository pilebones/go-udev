//go:build linux

package netlink

import (
	"context"
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second) // Monitor 5s then stop for testing
	// The errs channel is left open on purpose: the worker may still emit while the test returns.
	defer func() {
		close(quit)
		cancel()
	}()

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

	ctx, cancel := context.WithCancel(context.Background())
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

// TestReadMsgWaitsForMessage ensures the receive timeout used to stop the monitoring stays invisible to
// ReadMsg, which is documented as blocking until a message arrives.
func TestReadMsgWaitsForMessage(t *testing.T) {
	conn := new(UEventConn)
	if err := conn.Connect(UdevEvent); err != nil {
		t.Fatal("unable to subscribe to netlink uevent, err:", err)
	}
	defer func() {
		_ = conn.Close()
	}()

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
