package main

import "flag"

var (
	host *string
	port *int
)

func init() {
	host = flag.String("host", "0.0.0.0", "服务器通信ip")
	port = flag.Int("port", 9960, "通信端口")
}

func main() {
	flag.Parse()

	bridgeServer := &BridgeServer{*host, *port}
	bridgeServer.Start()
}
