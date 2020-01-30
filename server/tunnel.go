package main

import (
	"fmt"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
)

//Tunnel 被映射的服务端通道
type Tunnel struct {
	host                   string
	port                   int
	localHost              string
	localPort              int
	bridgeChannel          *BridgeChannel
	tunnelChannelMap       map[uint32]*net.Conn
	tunnelChannelMapLocker sync.RWMutex
	channelIDAtom          uint32
}

//NewTunnel 开启端口映射
func NewTunnel(host string, port int, localHost string, localPort int,
	bridgeChannel *BridgeChannel) (*Tunnel, error) {

	tunnel := &Tunnel{host: host, port: port, bridgeChannel: bridgeChannel,
		tunnelChannelMap: make(map[uint32]*net.Conn), channelIDAtom: uint32(1),
		localHost: localHost, localPort: localPort}

	server, err := net.Listen("tcp", host+":"+strconv.Itoa(port))
	if err != nil {
		return nil, err
	}
	go func() {
		defer server.Close()

		for {
			conn, e := server.Accept()
			if e != nil {
				fmt.Println("accept error", e)
				continue
			}
			go tunnel.handleConnection(conn)
		}
	}()

	return tunnel, nil
}

func (tunnel *Tunnel) handleConnection(conn net.Conn) {
	fmt.Printf("new tunnel connection %s\n", conn.RemoteAddr().String())

	channelID := atomic.AddUint32(&tunnel.channelIDAtom, 1)
	tunnel.AddTunnelChannel(channelID, &conn)
	tunnel.bridgeChannel.AddChannelTunnel(channelID, tunnel)

	defer conn.Close()
	defer tunnel.DeleteTunnelChannel(channelID)
	defer tunnel.bridgeChannel.DeleteChannelTunnel(channelID)
	for {
		buffer := make([]byte, 1024)

		readLen, err := conn.Read(buffer)
		if err != nil {
			fmt.Println("read from client error", err)
			break
		}
		data := buffer[0:readLen]
		tunnel.bridgeChannel.ForwardDataToLocal(channelID, tunnel.localHost, tunnel.localPort, data)
	}
}

//CloseTunnelChannel 关闭映射连接通道
func (tunnel *Tunnel) CloseTunnelChannel(channelID uint32) {
	conn := *tunnel.GetTunnelChannel(channelID)
	if conn != nil {
		conn.Close()
	}
}

//ForwardToTunnel x
func (tunnel *Tunnel) ForwardToTunnel(channelID uint32, data []byte) {
	conn := *tunnel.GetTunnelChannel(channelID)
	if conn == nil {
		fmt.Printf("tunnel channel is not found for channelID:%d\n", channelID)
		return
	}
	conn.Write(data)
}

//AddTunnelChannel x
func (tunnel *Tunnel) AddTunnelChannel(channelID uint32, conn *net.Conn) {
	tunnel.tunnelChannelMapLocker.Lock()
	defer tunnel.tunnelChannelMapLocker.Unlock()
	tunnel.tunnelChannelMap[channelID] = conn
}

//DeleteTunnelChannel x
func (tunnel *Tunnel) DeleteTunnelChannel(channelID uint32) {
	tunnel.tunnelChannelMapLocker.Lock()
	defer tunnel.tunnelChannelMapLocker.Unlock()
	delete(tunnel.tunnelChannelMap, channelID)
}

//GetTunnelChannel x
func (tunnel *Tunnel) GetTunnelChannel(channelID uint32) *net.Conn {
	return tunnel.tunnelChannelMap[channelID]
}
