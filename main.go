package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/pilebones/go-udev/netlink"

	"github.com/kr/pretty"
)

var (
	filePath      *string
	monitor, info *bool
)

func init() {
	filePath = flag.String("file", "", "Optionnal input file path with matcher-rules (default: no matcher)")
	monitor = flag.Bool("monitor", false, "Enable monitor mode")
	info = flag.Bool("info", false, "Enable monitor mode")
}

func main() {
	flag.Parse()

	matcher, err := getOptionnalMatcher()
	if err != nil {
		log.Fatalln(err.Error())
	}

	if monitor == nil && info == nil {
		log.Fatalln("You should use only one mode:", os.Args[0], "-monitor|-info")
	}

	if (monitor != nil && *monitor) && (info != nil && *info) {
		log.Fatalln("Unable to enable both mode : monitor & info")
	}

	if *monitor {
		log.Println("Monitoring UEvent kernel message to user-space...")
		conn := new(netlink.UEventConn)
		if err = conn.Connect(); err != nil {
			log.Fatalln("Unable to connect to Netlink Kobject UEvent socket")
		}
		defer conn.Close()

		queue := make(chan netlink.UEvent)
		quit := conn.Monitor(queue, matcher)

		// Signal handler to quit properly monitor mode
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
		go func() {
			<-signals
			log.Println("Exiting monitor mode...")
			quit <- true
			os.Exit(0)
		}()

		// Handling message from queue
		for {
			select {
			case uevent := <-queue:
				log.Printf("Handle %s\n", pretty.Sprint(uevent))
			}
		}
	}

	if *info {

	}
}

// getOptionnalMatcher Parse and load config file which contains rules for matching
func getOptionnalMatcher() (matcher netlink.Matcher, err error) {
	if filePath == nil || *filePath == "" {
		return nil, nil
	}

	stream, err := ioutil.ReadFile(*filePath)
	if err != nil {
		return nil, err
	}

	if stream == nil {
		return nil, fmt.Errorf("Empty, no rules provided in \"%s\", err: %s", *filePath, err.Error())
	}

	var rules netlink.RuleDefinitions
	if err := json.Unmarshal(stream, &rules); err != nil {
		return nil, fmt.Errorf("Wrong rule syntax in \"%s\", err: %s", *filePath, err.Error())
	}

	return &rules, nil
}