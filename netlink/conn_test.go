package netlink

import (
	"testing"
)

func TestConnect(t *testing.T) {
	conn := new(UEventConn)
	if err := conn.Connect(UdevEvent); err != nil {
		t.Fatal("unable to subscribe to netlink uevent, err:", err)
	}
	defer func() {
		_ = conn.Close()
	}()

	conn2 := new(UEventConn)
	if err := conn2.Connect(UdevEvent); err != nil {
		t.Fatal("unable to subscribe to netlink uevent a second time, err:", err)
	}
	defer func() {
		_ = conn2.Close()
	}()
}
