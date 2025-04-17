package crawler

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pilebones/go-udev/netlink"
)

const (
	BASE_DEVPATH = "/sys/devices"
)

type Device struct {
	KObj string
	Env  map[string]string
}

var ErrAbortReceived = errors.New("abort signal received")

// ExistingDevices return all plugged devices matched by the matcher
// All uevent files inside /sys/devices is crawled to match right env values
func ExistingDevices(queue chan Device, errs chan error, matcher netlink.Matcher) chan struct{} {
	quit := make(chan struct{}, 1)

	if matcher != nil {
		if err := matcher.Compile(); err != nil {
			errs <- fmt.Errorf("wrong matcher, err: %w", err)
			quit <- struct{}{}
			close(queue)
			return quit
		}
	}

	go func() {
		err := filepath.Walk(BASE_DEVPATH, func(path string, info os.FileInfo, err error) error {
			select {
			case <-quit:
				return ErrAbortReceived
			default:
				if err != nil {
					return err
				}

				if info.IsDir() || info.Name() != "uevent" {
					return nil
				}

				env, err := getEventFromUEventFile(path)
				if err != nil {
					return err
				}

				kObj := filepath.Dir(path)

				// Append to env subsystem if existing
				if link, err := os.Readlink(kObj + "/subsystem"); err == nil {
					env["SUBSYSTEM"] = filepath.Base(link)
				}

				if matcher == nil || matcher.EvaluateEnv(env) {
					queue <- Device{
						KObj: kObj,
						Env:  env,
					}
				}
				return nil
			}
		})
		if err != nil {
			errs <- err
		}

		close(queue)
	}()
	return quit
}

// getEventFromUEventFile return all variables defined in /sys/**/uevent files
func getEventFromUEventFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return getEventFromUEventData(data)
}

// syntax: name=value for each line
func getEventFromUEventData(data []byte) (map[string]string, error) {
	rv := make(map[string]string)
	buf := bufio.NewScanner(bytes.NewBuffer(data))
	for buf.Scan() {
		field := strings.SplitN(buf.Text(), "=", 2)
		if len(field) != 2 {
			return rv, fmt.Errorf("cannot parse uevent data: did not find '=' in '%s'", buf.Text())
		}
		rv[field[0]] = field[1]
	}
	return rv, nil
}
