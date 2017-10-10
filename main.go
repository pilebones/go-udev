package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	conn := new(UEventConn)
	err := conn.Connect()
	if err != nil {
		log.Println("Unable to connect to Netlink Kobject UEvent socket")
		os.Exit(1)
	}
	defer conn.Close()

	queue := make(chan []byte)
	quit := conn.Monitor(queue)

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
		case msg := <-queue:
			log.Println("Handle msg:", string(msg))
		}
	}
}