package crawler

import (
	"context"
	"os"
	"reflect"
	"testing"
	"time"
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
		tmp, err := os.CreateTemp("", "uevent-file")
		if err != nil {
			t.Fatalf("Test %d failed, unable to create temp file for uevent data", k)
		}
		defer os.Remove(tmp.Name())

		if _, err := tmp.WriteString(tcase.got); err != nil {
			t.Fatalf("Test %d failed, unable to append uevent data in temp file", k)
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
	queue := make(chan Device)
	errors := make(chan error)
	quit := ExistingDevices(queue, errors, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	defer func() {
		close(quit)
		close(errors)
		cancel()
	}()

	for {
		select {
		case <-ctx.Done():
			t.Fatalf("reach timeout while getting existing devices, err: %v", ctx.Err())
			quit <- struct{}{}
		case _, more := <-queue:
			if !more {
				t.Log("Finished processing existing devices")
				quit <- struct{}{}
				return // without error
			}
			// t.Logf("Detect device at %s with env %v", device.KObj, device.Env)
		case err := <-errors:
			t.Fatalf("unable to get existing devices, err: %v", err)
			quit <- struct{}{}
		}
	}
}
