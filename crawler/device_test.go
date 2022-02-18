package crawler

import (
	"reflect"
	"testing"
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
		evt := getEventFromUEventData([]byte(tcase.got))
		if !reflect.DeepEqual(evt, tcase.expected) {
			t.Fatalf("Test %d failed (got: %v, expected: %v)", k, evt, tcase.expected)
		}
	}
}
