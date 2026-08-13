package crawler

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/pilebones/go-udev/netlink"
)

const (
	BASE_DEVPATH = "/sys/devices"
)

// errAbort stops the crawling when the caller asks for it, it is not reported as a crawling error.
var errAbort = errors.New("abort signal receive")

type Device struct {
	KObj string
	Env  map[string]string
}

// ExistingDevices return all plugged devices matched by the matcher
// All uevent files inside /sys/devices is crawled to match right env values
// The queue is closed when the crawling is over, whatever the reason, so the caller can detect the end of the walk.
func ExistingDevices(queue chan Device, errs chan error, matcher netlink.Matcher) chan struct{} {
	return existingDevices(BASE_DEVPATH, queue, errs, matcher)
}

// existingDevices crawls the given root, which the tests replace with a sysfs of their own to exercise the
// cases a real /sys/devices cannot be made to produce on demand.
func existingDevices(root string, queue chan Device, errs chan error, matcher netlink.Matcher) chan struct{} {
	quit := make(chan struct{}, 1)

	go func() {
		// Never emit on errs before returning quit to the caller: an unbuffered errs channel
		// would block forever because the caller cannot read it yet.
		defer close(queue)

		if matcher != nil {
			if err := matcher.Compile(); err != nil {
				errs <- fmt.Errorf("wrong matcher, err: %w", err)
				return
			}
		}

		// notify reports a non-fatal error to the caller without blocking forever once crawling is aborted.
		notify := func(err error) error {
			select {
			case errs <- err:
				return nil
			case <-quit:
				return errAbort
			}
		}

		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			select {
			case <-quit:
				return errAbort
			default:
				if err != nil {
					if path == root {
						return err // sysfs is not available at all, no point in walking further
					}

					// A single unreadable entry (missing permission, device unplugged while crawling)
					// must not abort the whole walk.
					return notify(err)
				}

				if d.IsDir() || d.Name() != "uevent" {
					return nil
				}

				env, err := getEventFromUEventFile(path)
				if err != nil {
					return notify(err)
				}

				kObj := filepath.Dir(path)

				// Append to env subsystem if existing
				if link, err := os.Readlink(kObj + "/subsystem"); err == nil {
					env["SUBSYSTEM"] = filepath.Base(link)
				}

				if matcher == nil || matcher.EvaluateEnv(env) {
					select {
					case queue <- Device{
						KObj: kObj,
						Env:  env,
					}:
					case <-quit:
						return errAbort
					}
				}
				return nil
			}
		})
		if err != nil && !errors.Is(err, errAbort) {
			_ = notify(err)
		}
	}()
	return quit
}

// getEventFromUEventFile return all env var define in file
// syntax: name=value for each line
// Fonction use for /sys/.../uevent files
func getEventFromUEventFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = f.Close()
	}()

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	return getEventFromUEventData(data), nil
}

func getEventFromUEventData(data []byte) map[string]string {
	rv := make(map[string]string)
	buf := bufio.NewScanner(bytes.NewBuffer(data))
	for buf.Scan() {
		field := strings.SplitN(buf.Text(), "=", 2)
		if len(field) != 2 {
			return rv // TODO: return error ?
		}
		rv[field[0]] = field[1]
	}
	return rv
}
