package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
)

var (
	host     *string
	port     *int
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

	c := make(chan os.Signal)
	signal.Notify(c)
	s := <-c
	log.Println("exit", s)
	bridgeServer.Close()
}
