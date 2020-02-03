package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
)

var (
	host *string
	port *int
)

func init() {
	host = flag.String("host", "0.0.0.0", "服务器通信ip")
	port = flag.Int("port", 9960, "通信端口")

	log.SetPrefix("TRACE: ")
	log.SetFlags(log.Ldate | log.Lmicroseconds | log.Lshortfile)
}

func main() {

	flag.Parse()

	bridgeServer := &BridgeServer{Host: *host, Port: *port}
	bridgeServer.Start()

	signalChan := make(chan os.Signal, 1)
	cleanupDone := make(chan bool)
	signal.Notify(signalChan, os.Interrupt)
	go func() {
		for _ = range signalChan {
			log.Println("exit bridge server")
			bridgeServer.Close()
			cleanupDone <- true
		}
	}()
	<-cleanupDone
}
