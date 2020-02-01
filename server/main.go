package main

import (
	"flag"
	"log"
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

type Parent struct {
}
func (p *Parent) Test() {
	log.Println("parent method")
}

type Child struct {
	Parent
}

func (c *Child) Test(){
	log.Println("child method")
}




func main() {

	flag.Parse()

	bridgeServer := &BridgeServer{*host, *port}
	bridgeServer.Start()
}
