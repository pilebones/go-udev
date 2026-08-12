package crawler

import (
	"context"
	"os"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/pilebones/go-udev/netlink"
)

func TestEventFromUEventData(t *testing.T) {
	type testcase struct {
		got      string
		expected map[string]string
	}

	// Given
	testcases := []testcase{
		{
			got:      "",
			expected: map[string]string{},
		},
		{
			got: `MODALIAS=acpi:LNXSYSTM:`,
			expected: map[string]string{
				"MODALIAS": "acpi:LNXSYSTM:",
			},
		},
		{
			got:      `MODALIASDSQDSQ:dsqsdqds`,
			expected: map[string]string{},
		},
		{
			got: `MAJOR=7
MINOR3
DEVNAMEvcs3`,
			expected: map[string]string{
				"MAJOR": "7",
			},
		},
		{
			got: `MAJOR=7
MINOR=3
DEVNAME=vcs3`,
			expected: map[string]string{
				"MAJOR":   "7",
				"MINOR":   "3",
				"DEVNAME": "vcs3",
			},
		},
	}

	for k, tcase := range testcases {
		tmp, err := os.CreateTemp(t.TempDir(), "uevent-file")
		if err != nil {
			t.Fatalf("Test %d failed, unable to create temp file for uevent data", k)
		}

		if _, err := tmp.WriteString(tcase.got); err != nil {
			t.Fatalf("Test %d failed, unable to append uevent data in temp file", k)
		}

		if err := tmp.Close(); err != nil {
			t.Fatalf("Test %d failed, unable to close temp file: %v", k, err)
		}

		evt, err := getEventFromUEventFile(tmp.Name())
		if err != nil {
			t.Fatalf("Test %d failed, unable to get event dfrom uevent file", k)
		}

		if !reflect.DeepEqual(evt, getEventFromUEventData([]byte(tcase.got))) {
			t.Fatalf("Test %d failed, uevent from file or data must be equals", k)
		}

		if !reflect.DeepEqual(evt, tcase.expected) {
			t.Fatalf("Test %d failed (got: %v, expected: %v)", k, evt, tcase.expected)
		}
	}
}

func TestExistingDevices(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skipf("crawling %s requires a linux sysfs", BASE_DEVPATH)
	}

	queue := make(chan Device)
	errs := make(chan error)
	quit := ExistingDevices(queue, errs, nil)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)

	// The errs channel is left open on purpose: the worker may still emit while the test returns.
	defer func() {
		close(quit)
		cancel()
	}()

	for {
		select {
		case <-ctx.Done():
			t.Fatalf("reach timeout while getting existing devices, err: %v", ctx.Err())
		case _, more := <-queue:
			if !more {
				t.Log("Finished processing existing devices")
				return // without error
			}
			// t.Logf("Detect device at %s with env %v", device.KObj, device.Env)
		case err := <-errs:
			t.Fatalf("unable to get existing devices, err: %v", err)
		}
	}
}

// TestExistingDevicesWithInvalidMatcher ensures a broken matcher is reported through the error channel
// instead of deadlocking the caller, which cannot read the channel before ExistingDevices returns.
func TestExistingDevicesWithInvalidMatcher(t *testing.T) {
	invalid := "(["
	matcher := &netlink.RuleDefinitions{Rules: []netlink.RuleDefinition{{Action: &invalid}}}

	queue := make(chan Device)
	errs := make(chan error)
	quit := ExistingDevices(queue, errs, matcher)
	defer close(quit)

	select {
	case err := <-errs:
		t.Log("Got expected error:", err)
	case <-time.After(30 * time.Second):
		t.Fatal("ExistingDevices did not report the invalid matcher, it is probably deadlocked")
	}

	if _, more := <-queue; more {
		t.Fatal("queue should be closed when crawling aborts")
	}
}
