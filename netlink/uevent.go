package netlink

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

// See: http://elixir.free-electrons.com/linux/v3.12/source/lib/kobject_uevent.c#L45

const (
	ADD     KObjAction = "add"
	REMOVE  KObjAction = "remove"
	CHANGE  KObjAction = "change"
	MOVE    KObjAction = "move"
	ONLINE  KObjAction = "online"
	OFFLINE KObjAction = "offline"
	BIND    KObjAction = "bind"
	UNBIND  KObjAction = "unbind"
)

// The magic value used by udev, see https://github.com/systemd/systemd/blob/v239/src/libudev/libudev-monitor.c#L57
const libudevMagic = 0xfeedcafe

// Size of the udev_monitor_netlink_header struct: the "libudev\0" prefix followed by 8 32-bits fields.
const libudevHeaderLength = 40

// Offsets of the header fields go-udev relies on, relative to the start of the header.
const (
	libudevMagicOffset      = 8
	libudevHeaderSizeOffset = 12
	libudevPayloadOffOffset = 16
)

var ErrWrongUEventFormat = errors.New("wrong uevent format")

type KObjAction string

func (a KObjAction) String() string {
	return string(a)
}

func ParseKObjAction(raw string) (a KObjAction, err error) {
	a = KObjAction(raw)
	switch a {
	case ADD, REMOVE, CHANGE, MOVE, ONLINE, OFFLINE, BIND, UNBIND:
	default:
		err = fmt.Errorf("unknown kobject action (got: %s)", raw)
	}
	return
}

type UEvent struct {
	Action KObjAction
	KObj   string
	Env    map[string]string
}

func (e UEvent) String() string {
	var rv strings.Builder
	fmt.Fprintf(&rv, "%s@%s\000", e.Action.String(), e.KObj)
	for k, v := range e.Env {
		rv.WriteString(k + "=" + v + "\000")
	}
	return rv.String()
}

func (e UEvent) Bytes() []byte {
	return []byte(e.String())
}

func (e UEvent) Equal(e2 UEvent) (bool, error) {
	if e.Action != e2.Action {
		return false, fmt.Errorf("wrong action (got: %s, wanted: %s)", e.Action, e2.Action)
	}

	if e.KObj != e2.KObj {
		return false, fmt.Errorf("wrong kobject (got: %s, wanted: %s)", e.KObj, e2.KObj)
	}

	if len(e.Env) != len(e2.Env) {
		return false, fmt.Errorf("wrong length of env (got: %d, wanted: %d)", len(e.Env), len(e2.Env))
	}

	for k, v := range e.Env {
		if v2, found := e2.Env[k]; !found || v != v2 {
			return false, fmt.Errorf("unable to find %s=%s env var from uevent", k, v)
		}
	}
	return true, nil
}

// Parse udev event created by udevd.
// The format of the data header is internal to udev and defined in libudev-monitor.c - see the udev_monitor_netlink_header struct.
// go-udev only looks at the "magic" number to filter out possibly invalid packets, and at the payload offset. Other fields of the header
// are ignored.
// Note, only some of the fields of the header use network byte order, for the rest udev uses native byte order of the platform.
// This works on both big and little endian platforms because udevd runs on the same host, hence shares its byte order.
// A message produced by a machine using the opposite byte order is rejected rather than decoded incorrectly.
func parseUdevEvent(raw []byte) (e *UEvent, err error) {
	if len(raw) < libudevHeaderLength {
		return nil, fmt.Errorf("cannot parse libudev event: truncated header")
	}

	// the magic number is stored in network byte order.
	magic := binary.BigEndian.Uint32(raw[libudevMagicOffset:])
	if magic != libudevMagic {
		return nil, fmt.Errorf("cannot parse libudev event: magic number mismatch")
	}

	// The header size is stored in native byte order and cannot exceed the message itself. Since the magic
	// number is byte order agnostic, this is what tells a corrupted header from one written by a machine
	// using the opposite byte order, which we have no way to decode.
	headerSize := binary.NativeEndian.Uint32(raw[libudevHeaderSizeOffset:])
	if headerSize < libudevHeaderLength || headerSize > uint32(len(raw)) {
		return nil, fmt.Errorf("cannot parse libudev event: invalid header size (got: %d)", headerSize)
	}

	// the payload offset int is stored in native byte order.
	payloadoff := binary.NativeEndian.Uint32(raw[libudevPayloadOffOffset:])
	if payloadoff >= uint32(len(raw)) {
		return nil, fmt.Errorf("cannot parse libudev event: invalid data offset")
	}

	// The payload is a list of NUL-terminated strings, hence the trailing empty field to drop.
	fields := bytes.Split(raw[payloadoff:], []byte{0x00}) // 0x00 = end of string
	if len(fields) < 2 {
		err = fmt.Errorf("cannot parse libudev event: data missing")
		return
	}

	envdata := make(map[string]string)
	for _, envs := range fields[0 : len(fields)-1] {
		env := bytes.SplitN(envs, []byte("="), 2)
		if len(env) != 2 {
			err = fmt.Errorf("cannot parse libudev event: invalid env data")
			return
		}
		envdata[string(env[0])] = string(env[1])
	}

	var action KObjAction
	action, err = ParseKObjAction(strings.ToLower(envdata["ACTION"]))
	if err != nil {
		return
	}

	// XXX: do we need kobj?
	kobj := envdata["DEVPATH"]

	e = &UEvent{
		Action: action,
		KObj:   kobj,
		Env:    envdata,
	}

	return
}

func ParseUEvent(raw []byte) (*UEvent, error) {
	if len(raw) > libudevHeaderLength && bytes.HasPrefix(raw, []byte("libudev\x00")) {
		return parseUdevEvent(raw)
	}

	// The kernel format is "<action>@<devpath>\0" followed by NUL-terminated env vars,
	// so a valid message always splits into at least the header plus a trailing empty field.
	fields := bytes.Split(raw, []byte{0x00}) // 0x00 = end of string
	if len(fields) < 2 {
		return nil, ErrWrongUEventFormat
	}

	headers := bytes.Split(fields[0], []byte("@")) // 0x40 = @
	if len(headers) != 2 {
		return nil, ErrWrongUEventFormat
	}

	action, err := ParseKObjAction(string(headers[0]))
	if err != nil {
		return nil, ErrWrongUEventFormat
	}

	e := &UEvent{
		Action: action,
		KObj:   string(headers[1]),
		Env:    make(map[string]string),
	}

	for _, envs := range fields[1 : len(fields)-1] {
		env := bytes.SplitN(envs, []byte("="), 2)
		if len(env) != 2 {
			return nil, ErrWrongUEventFormat
		}
		e.Env[string(env[0])] = string(env[1])
	}

	return e, nil
}
