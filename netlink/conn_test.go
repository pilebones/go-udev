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
