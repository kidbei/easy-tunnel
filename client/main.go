package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"time"
)

var (
	remoteHost     *string
	remotePort     *int
	remoteBindHost *string
	remoteBindPort *int
	localHost      *string
	localPort      *int
	protocol	   *string
)

func init() {
	remoteHost = flag.String("h", "127.0.0.1", "远程服务器通信ip")
	remotePort = flag.Int("p", 9960, "远程服务器通信端口")
	remoteBindHost = flag.String("bh", "0.0.0.0", "开启映射后绑定ip")
	remoteBindPort = flag.Int("bp", -1, "远程开启的映射端口,必填")
	localHost = flag.String("fh", "127.0.0.1", "转发目标ip")
	protocol = flag.String("protocol", "tcp", "通信协议\n  tcp\n  kcp:浪费30%左右的带宽提高20%的性能")
	localPort = flag.Int("fp", -1, "转发目标端口,必填")

	log.SetPrefix("TRACE: ")
	log.SetFlags(log.Ldate | log.Lmicroseconds | log.Lshortfile)
}

func main() {
	flag.Parse()

	if *localPort == -1 {
		panic("fport参数必填")
	}
	if *remoteBindPort == -1 {
		panic("bport参数必填")
	}

	bridgeClient := NewBridgeClient(*protocol)
	success, err := bridgeClient.Connect(*remoteHost, *remotePort)
	if !success {
		panic(err)
	}
	msg, e := bridgeClient.OpenTunnel(*remoteBindHost, *remoteBindPort, *localHost, *localPort)
	if e != nil {
		panic(msg)
	}

	time.Sleep(time.Hour * 1)

	c := make(chan os.Signal)
	signal.Notify(c)
	s := <-c
	log.Println("exit", s)
	bridgeClient.Close()
}
