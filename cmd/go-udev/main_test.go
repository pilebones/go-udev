package main

import (
	"testing"

	"github.com/pilebones/go-udev/netlink"
)

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
	for i := 0; i < 2; i++ {
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
