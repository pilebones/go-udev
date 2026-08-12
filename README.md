# go-udev [![GitHub Repo stars](https://img.shields.io/github/stars/pilebones/go-udev)](https://somsubhra.github.io/github-release-stats/?username=pilebones&repository=go-udev)

![Build Status](https://github.com/pilebones/go-udev/workflows/CI/badge.svg) [![Linters Status](https://github.com/pilebones/go-udev/actions/workflows/golangci-lint.yml/badge.svg)](https://github.com/pilebones/go-udev/actions/workflows/golangci-lint.yml) [![Coverage Status](https://coveralls.io/repos/github/pilebones/go-udev/badge.svg?branch=master)](https://coveralls.io/github/pilebones/go-udev?branch=master)
 [![GoDoc](https://godoc.org/github.com/pilebones/go-udev?status.svg)](https://godoc.org/github.com/pilebones/go-udev) [![Release](https://img.shields.io/github/release/pilebones/go-udev.svg)](https://github.com/pilebones/go-udev/releases/latest) [![License](https://img.shields.io/github/license/pilebones/go-udev)](/LICENSE)

Simple udev implementation in Golang developed from scratch.
This library allow to listen and manage Linux-kernel (since version 2.6.10) Netlink messages to user space (ie: `NETLINK_KOBJECT_UEVENT`).

Like [`udev`](https://en.wikipedia.org/wiki/Udev) you will be able to monitor, display and manage devices plug to the system.

Monitoring requires a Linux kernel: on other platforms the module still builds and the uevent parsing/matching API stays
available, but `UEventConn` methods return `netlink.ErrNotSupported`.

## How to

### Get sources

Use the library:

```
go get github.com/pilebones/go-udev
```

Or install the command line tool:

```
go install github.com/pilebones/go-udev/cmd/go-udev@latest
```

The library itself has no external dependency.

### Unit test

```
go test ./...
```

### Compile

```
go build ./cmd/go-udev
```

### Usage

```
./go-udev -<mode> [-file=<absolute_path>]
```

Allowed mode: `info` or `monitor`
File should contains matcher rules (see: "Advanced usage" section)

### Info Mode

Crawl /sys/devices uevent struct to detect plugged devices:

```
./go-udev -info
```

### Monitor Mode

Handle all kernel message to detect change about plugged or unplugged devices:

```
./go-udev -monitor
```

Only the events sent by the kernel or by udevd are delivered, the others are dropped and reported as
`netlink.ErrUntrustedSender`: an uevent is trivial to forge for a process able to write on the socket, and
consumers usually act on the devices it announces.

#### Examples

Example of output when a USB storage is plugged:

```
2017/10/20 23:47:23 Handle UEvent{Action: add, KObj: /devices/pci0000:00/0000:00:14.0/usb1/1-1, Env: {ACTION="add", BUSNUM="001", DEVNAME="bus/usb/001/005", DEVNUM="005", DEVPATH="/devices/pci0000:00/0000:00:14.0/usb1/1-1", DEVTYPE="usb_device", MAJOR="189", MINOR="4", PRODUCT="58f/6387/10b", SEQNUM="2511", SUBSYSTEM="usb", TYPE="0/0/0"}}
2017/10/20 23:47:23 Handle UEvent{Action: add, KObj: /devices/pci0000:00/0000:00:14.0/usb1/1-1/1-1:1.0, Env: {ACTION="add", DEVPATH="/devices/pci0000:00/0000:00:14.0/usb1/1-1/1-1:1.0", DEVTYPE="usb_interface", INTERFACE="8/6/80", MODALIAS="usb:v058Fp6387d010Bdc00dsc00dp00ic08isc06ip50in00", PRODUCT="58f/6387/10b", SEQNUM="2512", SUBSYSTEM="usb", TYPE="0/0/0"}}
2017/10/20 23:47:23 Handle UEvent{Action: add, KObj: /module/usb_storage, Env: {ACTION="add", DEVPATH="/module/usb_storage", SEQNUM="2513", SUBSYSTEM="module"}}
[...]
```

Example of output when a USB storage is unplugged:

```
[...]
2017/10/20 23:47:29 Handle UEvent{Action: remove, KObj: /devices/pci0000:00/0000:00:14.0/usb1/1-1/1-1:1.0/host4/target4:0:0/4:0:0:0/block/sdb, Env: {ACTION="remove", DEVNAME="sdb", DEVPATH="/devices/pci0000:00/0000:00:14.0/usb1/1-1/1-1:1.0/host4/target4:0:0/4:0:0:0/block/sdb", DEVTYPE="disk", MAJOR="8", MINOR="16", SEQNUM="2543", SUBSYSTEM="block"}}
[...]
2017/10/20 23:47:29 Handle UEvent{Action: remove, KObj: /devices/pci0000:00/0000:00:14.0/usb1/1-1/1-1:1.0, Env: {ACTION="remove", DEVPATH="/devices/pci0000:00/0000:00:14.0/usb1/1-1/1-1:1.0", DEVTYPE="usb_interface", INTERFACE="8/6/80", MODALIAS="usb:v058Fp6387d010Bdc00dsc00dp00ic08isc06ip50in00", PRODUCT="58f/6387/10b", SEQNUM="2548", SUBSYSTEM="usb", TYPE="0/0/0"}}
2017/10/20 23:47:29 Handle UEvent{Action: remove, KObj: /devices/pci0000:00/0000:00:14.0/usb1/1-1, Env: {ACTION="remove", BUSNUM="001", DEVNAME="bus/usb/001/005", DEVNUM="005", DEVPATH="/devices/pci0000:00/0000:00:14.0/usb1/1-1", DEVTYPE="usb_device", MAJOR="189", MINOR="4", PRODUCT="58f/6387/10b", SEQNUM="2549", SUBSYSTEM="usb", TYPE="0/0/0"}}
```

Note: To implement your own monitoring system, please see `cmd/go-udev/main.go` as a simple example.

### Advanced usage

Is it possible to filter uevents/devices with a Matcher.

A Matcher is a list of your own rules to match only relevant uevent kernel message (see: `matcher.sample`).

You could pass this file using for both mode:

```
./go-udev -file  matcher.sample [...]

```

### Library usage

Monitoring stops as soon as the context is cancelled, and the queue is closed when the worker returns:

```go
conn := new(netlink.UEventConn)
if err := conn.Connect(netlink.UdevEvent); err != nil {
	log.Fatalln("unable to subscribe to netlink uevent, err:", err)
}
defer conn.Close()

ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
defer stop()

queue, errs := make(chan netlink.UEvent), make(chan error)
go conn.MonitorWithContext(ctx, queue, errs, nil) // pass a netlink.Matcher to filter the events

for {
	select {
	case uevent, more := <-queue:
		if !more {
			return
		}
		log.Println("handle", uevent.Action, uevent.KObj)
	case err := <-errs:
		// Reading errors are not fatal, a dropped message does not stop the monitoring
		log.Println("ERROR:", err)
	}
}
```

## Docker usage

### How to build docker image

```
docker build . -t go-udev
```

### How to execute `go-udev -info` using docker image

```
docker run --rm -t go-udev -info
```

### How to execute `go-udev -monitor` using docker image

```
docker run --rm -v=/dev:/dev -v /run/udev:/run/udev:ro --net=host -t go-udev -monitor
```

Warning: unsafe method because we share host /dev directory to the container.

*__TODO__: unsafe and `-monitor` is not fully functional in docker, fix working in progress...*

## Troubleshooting

Don't hesitate to notice if you detect a problem with this tool or library.

## Links

- Netlink Manual: <http://man7.org/linux/man-pages/man7/netlink.7.html>
- Linux source code about:
  - Struct sockaddr_netlink: <http://elixir.free-electrons.com/linux/v3.12/source/lib/kobject_uevent.c#L45>
  - KObject action: <http://elixir.free-electrons.com/linux/v3.12/source/lib/kobject_uevent.c#L45>

## Documentation

- [GoDoc Reference](http://godoc.org/github.com/pilebones/go-udev).

## License

go-udev is available under the [GNU GPL v3 - Clause License](https://opensource.org/licenses/GPL-3.0).
