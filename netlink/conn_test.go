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
	errors := make(chan error)
	quit := conn.Monitor(queue, errors, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second) // Monitor 5s then stop for testing
	defer func() {
		close(quit)
		close(errors)
		cancel()
	}()

loop:
	for {
		select {
		case <-ctx.Done():
			t.Log("Reach timeout while monitoring netlink uevent channel, everything is fine.")
			quit <- struct{}{}
			break loop // stop iteration in case of stop signal received
		case uevent := <-queue:
			t.Log("Handle", uevent.String())
		case err := <-errors:
			quit <- struct{}{}
			t.Fatalf("unable to get existing devices, err: %v", err)
		}
	}
}
