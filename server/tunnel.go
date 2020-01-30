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
	host                   string
	port                   int
	localHost              string
	localPort              int
	bridgeChannel          *BridgeChannel
	tunnelChannelMap       map[uint32]*TunnelChannel
	tunnelChannelMapLocker sync.RWMutex
	channelIDAtom          uint32
	listener			   net.Listener
	TunnelID			   uint32
}

var tunnelIDAtom = uint32(1)

type TunnelChannel struct {
	Conn          net.Conn
	AgentClosed   bool
	bridgeChannel *BridgeChannel
	ChannelID     uint32
}

//NewTunnel 开启端口映射
func NewTunnel(host string, port int, localHost string, localPort int,
	bridgeChannel *BridgeChannel) (*Tunnel, error) {

	tunnel := &Tunnel{host: host, port: port, bridgeChannel: bridgeChannel,
		tunnelChannelMap: make(map[uint32]*TunnelChannel), channelIDAtom: uint32(1),
		localHost: localHost, localPort: localPort, TunnelID:atomic.AddUint32(&tunnelIDAtom, 1)}

	server, err := net.Listen("tcp", host+":"+strconv.Itoa(port))
	if err != nil {
		return nil, err
	}
	tunnel.listener = server
	go func() {
		defer server.Close()

		for {
			conn, e := server.Accept()
			if e != nil {
				log.Println("accept error", e)
				break
			}
			go tunnel.handleConnection(conn)
		}
	}()

	return tunnel, nil
}

func (tunnel *Tunnel) handleConnection(conn net.Conn) {
	log.Printf("new tunnel connection %s\n", conn.RemoteAddr().String())

	channelID := atomic.AddUint32(&tunnel.channelIDAtom, 1)
	tunnelChannel := tunnel.AddTunnelChannel(channelID, conn)
	tunnel.bridgeChannel.AddChannelTunnel(channelID, tunnel)

	defer conn.Close()
	defer tunnel.DeleteTunnelChannel(channelID)
	defer tunnel.bridgeChannel.DeleteChannelTunnel(channelID)
	for {
		buffer := make([]byte, 1024)

		readLen, err := conn.Read(buffer)
		if err != nil {
			log.Println("read from client error", err)
			break
		}
		data := buffer[0:readLen]
		tunnel.bridgeChannel.ForwardDataToLocal(channelID, tunnel.localHost, tunnel.localPort, data)
	}
	if !tunnelChannel.AgentClosed {
		tunnelChannel.notifyTunnelChannelClosed()
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
	tunnel.listener.Close()
	for channelID, _ := range tunnel.tunnelChannelMap {
		tunnel.CloseTunnelChannel(channelID)
	}
}

//ForwardToTunnel x
func (tunnel *Tunnel) ForwardToTunnel(channelID uint32, data []byte) {
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
	tunnelChannel := &TunnelChannel{Conn:conn, AgentClosed:false, bridgeChannel:tunnel.bridgeChannel, ChannelID:channelID}
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

func (tunnelChannel *TunnelChannel) notifyTunnelChannelClosed()  {
	tunnelChannel.bridgeChannel.NotifyTunnelChannelClosed(tunnelChannel.ChannelID)
}
