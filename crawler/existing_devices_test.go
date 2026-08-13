package crawler

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pilebones/go-udev/netlink"
)

// writeFakeSysfs builds a directory holding what a crawler meets under /sys/devices: uevent files, files to
// ignore and the subsystem symlink. A tree of our own is the only way to exercise the cases a real sysfs
// cannot be made to produce on demand, and it makes the tests runnable outside of Linux.
func writeFakeSysfs(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	files := map[string]string{
		"pci0000:00/uevent":              "DEVTYPE=pci_device\n",
		"pci0000:00/0000:00:14.0/uevent": "DEVTYPE=usb_device\nMAJOR=189\nMINOR=32\n",
		"virtual/mem/null/uevent":        "MAJOR=1\nMINOR=3\nDEVNAME=null\n",
		"virtual/mem/null/dev":           "1:3\n", // not a uevent file, hence ignored
	}

	for path, content := range files {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal("unable to build the fake sysfs, err:", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal("unable to build the fake sysfs, err:", err)
		}
	}

	// The crawler follows this link to fill the SUBSYSTEM env var, which no uevent file carries.
	if err := os.Symlink("../../../class/mem", filepath.Join(root, "virtual/mem/null/subsystem")); err != nil {
		t.Fatal("unable to build the fake sysfs, err:", err)
	}

	return root
}

// crawl drains the crawler until it closes the queue and returns everything it reported.
func crawl(t *testing.T, root string, matcher netlink.Matcher) ([]Device, []error) {
	t.Helper()

	queue := make(chan Device)
	errs := make(chan error)
	quit := existingDevices(root, queue, errs, matcher)
	defer close(quit)

	var (
		devices  []Device
		failures []error
	)

	for {
		select {
		case device, more := <-queue:
			if !more {
				return devices, failures
			}
			devices = append(devices, device)
		case err := <-errs:
			failures = append(failures, err)
		case <-time.After(10 * time.Second):
			t.Fatal("crawling did not finish")
		}
	}
}

func TestExistingDevicesFromFakeSysfs(t *testing.T) {
	root := writeFakeSysfs(t)

	devices, failures := crawl(t, root, nil)
	if len(failures) != 0 {
		t.Fatalf("crawling a readable sysfs should not report any error, got: %v", failures)
	}

	found := make(map[string]map[string]string, len(devices))
	for _, device := range devices {
		found[device.KObj] = device.Env
	}

	if len(found) != 3 {
		t.Fatalf("every uevent file should give a device (got: %d, expected: 3), found: %v", len(found), found)
	}

	null, ok := found[filepath.Join(root, "virtual/mem/null")]
	if !ok {
		t.Fatalf("the device holding the uevent file is missing, found: %v", found)
	}
	if got := null["DEVNAME"]; got != "null" {
		t.Fatalf(`the uevent file content should be parsed (got DEVNAME: %q, expected: "null")`, got)
	}
	if got := null["SUBSYSTEM"]; got != "mem" {
		t.Fatalf(`the subsystem link should fill SUBSYSTEM (got: %q, expected: "mem")`, got)
	}

	if _, ok := found[filepath.Join(root, "virtual/mem")]; ok {
		t.Fatal("a directory without any uevent file should not give a device")
	}
}

func TestExistingDevicesWithMatcher(t *testing.T) {
	root := writeFakeSysfs(t)
	matcher := &netlink.RuleDefinitions{Rules: []netlink.RuleDefinition{{
		Env: map[string]string{"DEVTYPE": "usb_device"},
	}}}

	devices, failures := crawl(t, root, matcher)
	if len(failures) != 0 {
		t.Fatalf("crawling a readable sysfs should not report any error, got: %v", failures)
	}

	if len(devices) != 1 {
		t.Fatalf("only the matching device should be delivered (got: %d, expected: 1), got: %v", len(devices), devices)
	}
	if expected := filepath.Join(root, "pci0000:00/0000:00:14.0"); devices[0].KObj != expected {
		t.Fatalf("wrong device delivered (got: %s, expected: %s)", devices[0].KObj, expected)
	}
}

// TestExistingDevicesMissingRoot ensures an unusable sysfs is reported instead of looking like a machine
// without any device.
func TestExistingDevicesMissingRoot(t *testing.T) {
	devices, failures := crawl(t, filepath.Join(t.TempDir(), "absent"), nil)

	if len(devices) != 0 {
		t.Fatalf("no device can be found without a sysfs, got: %v", devices)
	}
	if len(failures) != 1 {
		t.Fatalf("a missing sysfs should be reported once (got: %d), got: %v", len(failures), failures)
	}
	t.Log("Got expected error:", failures[0])
}

// TestExistingDevicesReportsUnreadableFile ensures a single unreadable device, which happens when it is
// unplugged while crawling or when the process lacks a permission, does not abort the whole walk.
func TestExistingDevicesReportsUnreadableFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a file whatever its mode, an unreadable device cannot be simulated")
	}

	root := t.TempDir()
	for path, mode := range map[string]os.FileMode{"broken/uevent": 0o000, "readable/uevent": 0o644} {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal("unable to build the fake sysfs, err:", err)
		}
		if err := os.WriteFile(full, []byte("DEVNAME=sda\n"), mode); err != nil {
			t.Fatal("unable to build the fake sysfs, err:", err)
		}
	}

	devices, failures := crawl(t, root, nil)

	if len(failures) != 1 {
		t.Fatalf("the unreadable device should be reported once (got: %d), got: %v", len(failures), failures)
	}
	if len(devices) != 1 {
		t.Fatalf("the readable device should still be delivered (got: %d), got: %v", len(devices), devices)
	}
	t.Log("Got expected error:", failures[0])
}

// TestExistingDevicesStopsWhileDelivering ensures the crawling also stops while it is waiting for the caller
// to take the device it just found, otherwise a caller which stops reading would leave the walk hanging.
func TestExistingDevicesStopsWhileDelivering(t *testing.T) {
	root := writeFakeSysfs(t)

	queue := make(chan Device)
	errs := make(chan error)

	quit := existingDevices(root, queue, errs, nil)

	if _, more := <-queue; !more {
		t.Fatal("the fake sysfs holds devices, one should be delivered")
	}

	close(quit) // the walk now has a device on its hands and nobody to give it to

	deadline := time.After(10 * time.Second)
	for {
		select {
		case _, more := <-queue:
			if !more {
				return // the crawling stopped as expected
			}
		case err := <-errs:
			t.Fatalf("stopping the crawling should not be reported as an error, err: %v", err)
		case <-deadline:
			t.Fatal("crawling did not stop while delivering a device")
		}
	}
}

// TestEventFromUEventFileUnreadable covers a path which opens but cannot be read, as a directory does: the
// error must be reported instead of yielding a device without any env var.
func TestEventFromUEventFileUnreadable(t *testing.T) {
	env, err := getEventFromUEventFile(t.TempDir())
	if err == nil {
		t.Fatalf("reading a directory should fail, got: %v", env)
	}
	t.Log("Got expected error:", err)
}

// TestExistingDevicesStopsOnQuit ensures closing the returned channel ends the crawling without reporting it
// as a failure, which is how a caller which found what it looked for stops the walk.
func TestExistingDevicesStopsOnQuit(t *testing.T) {
	root := writeFakeSysfs(t)

	queue := make(chan Device)
	errs := make(chan error)

	quit := existingDevices(root, queue, errs, nil)
	close(quit) // asking to stop before the walk had the time to deliver everything

	for {
		select {
		case _, more := <-queue:
			if !more {
				return // the crawling stopped as expected
			}
		case err := <-errs:
			t.Fatalf("stopping the crawling should not be reported as an error, err: %v", err)
		case <-time.After(10 * time.Second):
			t.Fatal("crawling did not stop")
		}
	}
}
