//go:build linux

package netlink

import (
	"context"
	"os"
	"testing"
	"time"
)

// triggerCandidates are inert devices whose uevent file can be written to ask the kernel for a change
// event. The only side effect is udevd applying again the rules of a device which has none worth mentioning.
var triggerCandidates = []string{
	"/sys/class/mem/null/uevent",
	"/sys/class/mem/zero/uevent",
	"/sys/devices/virtual/mem/null/uevent",
}

// TestMonitorReceivesKernelEvent proves the sender checks let a genuine kernel event through. Asking the
// kernel for one requires the privilege to write in sysfs, so the test is skipped when running
// unprivileged, as it does on the CI.
func TestMonitorReceivesKernelEvent(t *testing.T) {
	var trigger string
	for _, candidate := range triggerCandidates {
		// Opening is enough to know whether writing is allowed, and it does not emit any event yet.
		file, err := os.OpenFile(candidate, os.O_WRONLY, 0)
		if err != nil {
			continue
		}

		_ = file.Close()
		trigger = candidate
		break
	}

	if trigger == "" {
		t.Skip("no writable sysfs uevent file, asking the kernel for an event requires root")
	}

	conn := new(UEventConn)
	if err := conn.Connect(KernelEvent); err != nil {
		t.Fatal("unable to subscribe to netlink uevent, err:", err)
	}
	defer func() {
		_ = conn.Close()
	}()

	queue := make(chan UEvent)
	errs := make(chan error, 8)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go conn.MonitorWithContext(ctx, queue, errs, nil)

	// Let the worker reach its first receive before asking for the event.
	time.Sleep(readTimeout)

	if err := os.WriteFile(trigger, []byte("change"), 0); err != nil {
		t.Fatal("unable to ask the kernel for an event, err:", err)
	}

	for {
		select {
		case uevent, more := <-queue:
			if !more {
				t.Fatalf("monitoring stopped before delivering the event triggered from %s", trigger)
			}

			t.Log("Got a genuine kernel event:", uevent.Action, uevent.KObj)
			return
		case err := <-errs:
			t.Fatalf("a genuine kernel event should not be rejected, err: %v", err)
		}
	}
}
