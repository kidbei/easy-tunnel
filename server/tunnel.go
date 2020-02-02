package main

import (
	"log"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
)

//Tunnel 被映射的服务端通道
type Tunnel struct {
	host                       string
	port                       int
	localHost                  string
	localPort                  int
	tunnelChannelMap           map[uint32]*TunnelChannel
	tunnelChannelMapLocker     sync.RWMutex
	channelIDAtom              uint32
	listener                   net.Listener
	TunnelID                   uint32
	ClosedHandler              func()
	DataReceivedHandler        func(channel TunnelChannel, data []byte)
	TunnelChannelClosedHandler func(channel TunnelChannel)
}

var tunnelIDAtom = uint32(1)

type TunnelChannel struct {
	Conn        net.Conn
	AgentClosed bool
	ChannelID   uint32
}

//NewTunnel 开启端口映射
func NewTunnel(host string, port int, localHost string, localPort int) (*Tunnel, error) {

	tunnel := &Tunnel{host: host, port: port,
		tunnelChannelMap: make(map[uint32]*TunnelChannel), channelIDAtom: uint32(1),
		localHost: localHost, localPort: localPort, TunnelID: atomic.AddUint32(&tunnelIDAtom, 1)}

	l, err := net.Listen("tcp", host+":"+strconv.Itoa(port))
	if err != nil {
		return nil, err
	}
	tunnel.listener = l
	go func() {
		defer tunnel.ClosedHandler()
		for {
			conn, e := l.Accept()
			if e != nil {
				log.Println("tunnel closed", e)
				break
			}
			go tunnel.handleConnection(conn)
		}
		log.Printf("tunnel closed:%s:%d\n", tunnel.localHost, tunnel.localPort)
	}()

	return tunnel, nil
}

func (tunnel *Tunnel) handleConnection(conn net.Conn) {
	log.Printf("new tunnel connection %s\n", conn.RemoteAddr().String())

	channelID := atomic.AddUint32(&tunnel.channelIDAtom, 1)
	tunnelChannel := tunnel.AddTunnelChannel(channelID, conn)

	defer conn.Close()
	defer tunnel.DeleteTunnelChannel(channelID)
	defer tunnel.TunnelChannelClosedHandler(*tunnelChannel)
	for {
		buffer := make([]byte, 1024)

		readLen, err := conn.Read(buffer)
		if err != nil {
			log.Println("read from client error", err)
			break
		}
		data := buffer[0:readLen]
		tunnel.DataReceivedHandler(*tunnelChannel, data)
	}
	if !tunnelChannel.AgentClosed {
		tunnel.TunnelChannelClosedHandler(*tunnelChannel)
	}
}

//CloseTunnelChannel 关闭映射连接通道
func (tunnel *Tunnel) CloseTunnelChannel(channelID uint32) {
	tunnelChannel := tunnel.GetTunnelChannel(channelID)
	if tunnelChannel != nil {
		log.Printf("close tunnel channel, ChannelID:%d, address:%s\n", channelID, tunnelChannel.Conn.RemoteAddr().String())
		tunnelChannel.AgentClosed = true
		tunnelChannel.Conn.Close()
	}
}

//CloseTunnel 关闭所有连接
func (tunnel *Tunnel) CloseTunnel() {
	log.Printf("close tunnel:%s\n", tunnel.listener.Addr().String())
	tunnel.tunnelChannelMapLocker.Lock()
	defer tunnel.tunnelChannelMapLocker.Unlock()
	for channelID, _ := range tunnel.tunnelChannelMap {
		tunnel.CloseTunnelChannel(channelID)
	}
	tunnel.listener.Close()
}

//ForwardToTunnelChannel x
func (tunnel *Tunnel) ForwardToTunnelChannel(channelID uint32, data []byte) {
	tunnelChannel := tunnel.GetTunnelChannel(channelID)
	if tunnelChannel == nil {
		log.Printf("tunnel channel is not found for ChannelID:%d\n", channelID)
		return
	}
	tunnelChannel.Conn.Write(data)
}

//AddTunnelChannel x
func (tunnel *Tunnel) AddTunnelChannel(channelID uint32, conn net.Conn) *TunnelChannel {
	tunnel.tunnelChannelMapLocker.Lock()
	defer tunnel.tunnelChannelMapLocker.Unlock()
	tunnelChannel := &TunnelChannel{Conn: conn, AgentClosed: false, ChannelID: channelID}
	tunnel.tunnelChannelMap[channelID] = tunnelChannel
	return tunnelChannel
}

//DeleteTunnelChannel x
func (tunnel *Tunnel) DeleteTunnelChannel(channelID uint32) {
	tunnel.tunnelChannelMapLocker.Lock()
	defer tunnel.tunnelChannelMapLocker.Unlock()
	delete(tunnel.tunnelChannelMap, channelID)
}

//GetTunnelChannel x
func (tunnel *Tunnel) GetTunnelChannel(channelID uint32) *TunnelChannel {
	if tunnel, exist := tunnel.tunnelChannelMap[channelID]; exist {
		return tunnel
	} else {
		return nil
	}
}
