package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/pilebones/go-udev/crawler"
	"github.com/pilebones/go-udev/netlink"

	"github.com/kr/pretty"
)

var (
	filePath              *string
	monitorMode, infoMode *bool
)

func init() {
	filePath = flag.String("file", "", "Optionnal input file path with matcher-rules (default: no matcher)")
	monitorMode = flag.Bool("monitor", false, "Enable monitor mode")
	infoMode = flag.Bool("info", false, "Enable crawler mode")
}

func main() {
	flag.Parse()

	matcher, err := getOptionnalMatcher()
	if err != nil {
		log.Fatalln(err)
	}

	if monitorMode == nil && infoMode == nil {
		log.Fatalln("You should use only one mode:", os.Args[0], "-monitor|-info")
	}

	if (monitorMode != nil && *monitorMode) && (infoMode != nil && *infoMode) {
		log.Fatalln("Unable to enable both mode : monitor & info")
	}

	if *monitorMode {
		monitor(matcher)
	}

	if *infoMode {
		info(matcher)
	}
}

// info run info mode
func info(matcher netlink.Matcher) {
	log.Println("Get existing devices...")

	queue := make(chan crawler.Device)
	errors := make(chan error)
	quit := crawler.ExistingDevices(queue, errors, matcher)

	// Signal handler to quit properly monitor mode
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	go func() {
		<-signals
		log.Println("Exiting info mode...")
		close(quit)
		os.Exit(0)
	}()

	// Handling message from queue
	for {
		select {
		case device, more := <-queue:
			if !more {
				log.Println("Finished processing existing devices")
				return
			}
			log.Println("Detect device at", device.KObj, "with env", device.Env)
		case err := <-errors:
			log.Println("ERROR:", err)
		}
	}
}

// monitor run monitor mode
func monitor(matcher netlink.Matcher) {
	log.Println("Monitoring UEvent kernel message to user-space...")

	conn := new(netlink.UEventConn)
	if err := conn.Connect(netlink.UdevEvent); err != nil {
		log.Fatalln("Unable to connect to Netlink Kobject UEvent socket")
	}
	defer func() {
		_ = conn.Close()
	}()

	queue := make(chan netlink.UEvent)
	errors := make(chan error)
	quit := conn.Monitor(queue, errors, matcher)

	// Signal handler to quit properly monitor mode
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	go func() {
		<-signals
		log.Println("Exiting monitor mode...")
		close(quit)
		os.Exit(0)
	}()

	// Handling message from queue
	for {
		select {
		case uevent := <-queue:
			log.Println("Handle", pretty.Sprint(uevent))
		case err := <-errors:
			log.Println("ERROR:", err)
		}
	}
}

// getOptionnalMatcher Parse and load config file which contains rules for matching
func getOptionnalMatcher() (matcher netlink.Matcher, err error) {
	if filePath == nil || *filePath == "" {
		return nil, nil
	}

	stream, err := os.ReadFile(*filePath)
	if err != nil {
		return nil, err
	}

	if stream == nil {
		return nil, fmt.Errorf("empty, no rules provided in \"%s\", err: %w", *filePath, err)
	}

	var rules netlink.RuleDefinitions
	if err := json.Unmarshal(stream, &rules); err != nil {
		return nil, fmt.Errorf("wrong rule syntax, err: %w", err)
	}

	return &rules, nil
}
