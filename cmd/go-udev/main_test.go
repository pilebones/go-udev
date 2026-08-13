package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pilebones/go-udev/netlink"
)

// TestGetOptionnalMatcher covers what the CLI does with the rule file it is given, from the absence of one to
// a rule set it has to hand over to the library.
func TestGetOptionnalMatcher(t *testing.T) {
	// The flag lives in a package level pointer, restore it so the other tests keep seeing the real flags.
	parsed := filePath
	t.Cleanup(func() { filePath = parsed })

	use := func(path string) { filePath = &path }

	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal("unable to write the rule file, err:", err)
		}
		return path
	}

	use("")
	if matcher, err := getOptionnalMatcher(); matcher != nil || err != nil {
		t.Fatalf("without a file there is no matcher (got: %v, err: %v)", matcher, err)
	}

	use(filepath.Join(dir, "absent.json"))
	if _, err := getOptionnalMatcher(); err == nil {
		t.Fatal("a missing rule file should be reported")
	}

	use(write("empty.json", ""))
	if _, err := getOptionnalMatcher(); err == nil {
		t.Fatal("an empty rule file should be reported")
	}

	use(write("invalid.json", "{"))
	if _, err := getOptionnalMatcher(); err == nil {
		t.Fatal("a rule file which is not valid JSON should be reported")
	}

	use(write("rules.json", `{"Rules":[{"action":"add","env":{"SUBSYSTEM":"usb"}}]}`))
	matcher, err := getOptionnalMatcher()
	if err != nil {
		t.Fatalf("a valid rule file should be accepted, err: %v", err)
	}
	if err := matcher.Compile(); err != nil {
		t.Fatalf("the rules read from the file should compile, err: %v", err)
	}
	if !matcher.Evaluate(netlink.UEvent{Action: netlink.ADD, Env: map[string]string{"SUBSYSTEM": "usb"}}) {
		t.Fatal("the rule read from the file should match the event it describes")
	}
}

func TestFormat(t *testing.T) {
	uevent := netlink.UEvent{
		Action: netlink.ADD,
		KObj:   "/devices/pci0000:00/0000:00:14.0/usb1/1-1",
		Env: map[string]string{
			"SUBSYSTEM": "usb",
			"ACTION":    "add",
			"DEVPATH":   "/devices/pci0000:00/0000:00:14.0/usb1/1-1",
			"MAJOR":     "189",
		},
	}

	expected := `UEvent{Action: add, KObj: /devices/pci0000:00/0000:00:14.0/usb1/1-1, ` +
		`Env: {ACTION="add", DEVPATH="/devices/pci0000:00/0000:00:14.0/usb1/1-1", MAJOR="189", SUBSYSTEM="usb"}}`

	// Called twice to ensure the env vars order does not depend on the map iteration order
	for range 2 {
		if got := format(uevent); got != expected {
			t.Fatalf("wrong output\n got: %s\nwant: %s", got, expected)
		}
	}
}

func TestFormatWithoutEnv(t *testing.T) {
	expected := "UEvent{Action: remove, KObj: /module/usb_storage, Env: {}}"
	if got := format(netlink.UEvent{Action: netlink.REMOVE, KObj: "/module/usb_storage"}); got != expected {
		t.Fatalf("wrong output\n got: %s\nwant: %s", got, expected)
	}
}
