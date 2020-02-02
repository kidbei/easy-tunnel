package main

import (
	"encoding/json"
	"errors"
	"github.com/kidbei/easy-tunnel/core"
	"github.com/xtaci/kcp-go"
	"log"
	"net"
	"strconv"
	"sync"
)

//BridgeServer 连接客户端通道
type BridgeServer struct {
	Host string
	Port int
}

//BridgeChannel 客户端连接
type BridgeChannel struct {
	conn                 net.Conn
	tunnelIDTunnelMap    map[uint32]*Tunnel
	tunnelIDTunnelLocker sync.Mutex
	protocolHandler      *core.ProtocolHandler
}

//Start start bridge server
func (bridgeServer BridgeServer) Start() {
	bridgeServer.startTcpServer()
}

func (bridgeServer *BridgeServer) startTcpServer() {
	server, e := net.Listen("tcp", bridgeServer.Host+":"+strconv.Itoa(bridgeServer.Port))
	if e != nil {
		log.Panicln("start bridge server failed", e)
	}

	log.Printf("start bridge server success at %s:%d\n", bridgeServer.Host, bridgeServer.Port)

	defer server.Close()
	for {
		conn, err := server.Accept()
		if err != nil {
			log.Println("accept error", err)
		} else {
			go bridgeServer.handleBridgeConnection(conn, core.NewProtocolHandler(conn))
		}
	}
}

func (bridgeServer *BridgeServer) startKcpServer() {
	listen, err := kcp.Listen(bridgeServer.Host + ":" + strconv.Itoa(bridgeServer.Port))
	if err != nil {
		log.Panicln("failed to start kcp server", err)
	}
	log.Printf("start kcp server at %s:%d\n", bridgeServer.Host, bridgeServer.Port)

	defer listen.Close()

	for {
		conn, e := listen.Accept()
		if e != nil {
			log.Println("accept error", e)
		} else {
			go bridgeServer.handleBridgeConnection(conn, core.NewProtocolHandler(conn))
		}
	}
}

func (bridgeServer *BridgeServer) handleBridgeConnection(conn net.Conn, protocolHandler *core.ProtocolHandler) {
	channel := &BridgeChannel{tunnelIDTunnelMap: make(map[uint32]*Tunnel)}
	channel.conn = conn
	channel.protocolHandler = protocolHandler
	channel.protocolHandler.RequestHandler = channel.handleRequest
	channel.protocolHandler.DisconnectHandler = channel.disconnectHandler
	channel.protocolHandler.Conn = conn
	channel.protocolHandler.NotifyHandler = channel.handleNotify
	channel.protocolHandler.ReadPacket(conn)
}

func (bridgeChannel *BridgeChannel) disconnectHandler() {
	bridgeChannel.tunnelIDTunnelLocker.Lock()
	defer bridgeChannel.tunnelIDTunnelLocker.Unlock()
	for _, tunnel := range bridgeChannel.tunnelIDTunnelMap {
		tunnel.CloseTunnel()
	}
	bridgeChannel.tunnelIDTunnelMap = make(map[uint32]*Tunnel)
}

//handleRequest 处理请求
func (bridgeChannel *BridgeChannel) handleRequest(packet *core.Packet) (data []byte, err error) {
	switch packet.Cid {
	case core.CommandOpenTunnel:
		return bridgeChannel.handleOpenTunnel(packet)
	default:
		log.Printf("unkown cid %d\n", strconv.Itoa(int(packet.Cid)))
		return []byte("unknown cid"), errors.New("unkown cid:" + strconv.Itoa(int(packet.Cid)))
	}
}

func (bridgeChannel *BridgeChannel) handleNotify(packet *core.Packet) {
	switch packet.Cid {
	case core.CommandPing:
		log.Printf("got ping from %s\n", bridgeChannel.conn.RemoteAddr().String())
		bridgeChannel.protocolHandler.Notify(core.CommandPong, nil)
	case core.CommandAgentChannelClosed:
		bridgeChannel.handleAgentChannelClosed(packet)
	case core.CommandForwardToTunnel:
		bridgeChannel.handleForwardToTunnel(packet)
	default:
		log.Printf("invalid notify request:%+v\n", packet)
	}
}

//handleOpenTunnel 开启端口映射
func (bridgeChannel *BridgeChannel) handleOpenTunnel(packet *core.Packet) (data []byte, err error) {
	openTunnelReq := &core.OpenTunnelReq{}
	e := json.Unmarshal(packet.Data, openTunnelReq)
	if e != nil {
		log.Printf("invalid request params:%s,error:%+v\n", string(packet.Data), e)
		return []byte("invalid request params"), e
	}
	log.Printf("open tunnel:%+v\n", openTunnelReq)
	tunnel, openErr := NewTunnel(openTunnelReq.BindHost, openTunnelReq.BindPort,
		openTunnelReq.LocalHost, openTunnelReq.LocalPort)
	if openErr != nil {
		log.Printf("open tunnel failed:%+v\n", openErr)
		return []byte("open tunnel error"), openErr
	}
	tunnel.DataReceivedHandler = func(channel TunnelChannel, data []byte) {
		bridgeChannel.ForwardDataToAgent(channel.ChannelID, tunnel.TunnelID, data)
	}
	tunnel.ClosedHandler = func() {
		bridgeChannel.DeleteChannelTunnel(tunnel.TunnelID)
	}
	tunnel.TunnelChannelClosedHandler = func(channel TunnelChannel) {
		bridgeChannel.NotifyTunnelChannelClosed(channel.ChannelID, tunnel.TunnelID)
	}
	bridgeChannel.AddTunnel(tunnel.TunnelID, tunnel)
	return core.Uint32ToBytes(tunnel.TunnelID), nil
}

//handleAgentChannelClosed 客户端通道关闭
func (bridgeChannel *BridgeChannel) handleAgentChannelClosed(packet *core.Packet) {
	if packet.Data == nil {
		log.Printf("invalid request, ChannelID is nil:%+v\n", packet)
		return
	}
	channelID := core.BytesToUInt32(packet.Data[0:4])
	tunnelID := core.BytesToUInt32(packet.Data[4:8])

	tunnel := bridgeChannel.GetChannelTunnel(tunnelID)
	if tunnel != nil {
		log.Printf("close tunnel channel:%d\n", channelID)
		tunnel.CloseTunnelChannel(channelID)
	} else {
		log.Printf("tunnel is not found for channel:%d\n", channelID)
	}
}

//handleForwardToTunnel 转发数据到映射通道
func (bridgeChannel *BridgeChannel) handleForwardToTunnel(packet *core.Packet) {
	channelID := core.BytesToUInt32(packet.Data[0:4])
	tunnelID := core.BytesToUInt32(packet.Data[4:8])
	data := packet.Data[8:]
	tunnel := bridgeChannel.GetChannelTunnel(tunnelID)
	if tunnel == nil {
		log.Printf("tunnel not found for ChannelID:%d, ignore to forward\n", channelID)
		return
	}
	tunnel.ForwardToTunnelChannel(channelID, data)
}

//ForwardDataToAgent 转发数据到客户端本地
func (bridgeChannel *BridgeChannel) ForwardDataToAgent(channelID uint32, tunnelID uint32, data []byte) {
	packetData := append(append(core.Uint32ToBytes(channelID), core.Uint32ToBytes(tunnelID)...), data...)
	bridgeChannel.protocolHandler.Notify(core.CommandForwardToLocal, packetData)
}

//NotifyTunnelChannelClosed 映射连接上的连接主动关闭后触发
func (bridgeChannel *BridgeChannel) NotifyTunnelChannelClosed(channelID uint32, tunnelID uint32) {
	log.Printf("notify tunnel channel closed to local agent, ChannelID:%d, tunnelID:%d\n", channelID, tunnelID)
	data := append(core.Uint32ToBytes(channelID), core.Uint32ToBytes(tunnelID)...)
	bridgeChannel.protocolHandler.Notify(core.CommandTunnelChannelClosed, data)
}

//AddChannelTunnel x
func (bridgeChannel *BridgeChannel) AddTunnel(tunnelID uint32, tunnel *Tunnel) {
	bridgeChannel.tunnelIDTunnelLocker.Lock()
	defer bridgeChannel.tunnelIDTunnelLocker.Unlock()
	bridgeChannel.tunnelIDTunnelMap[tunnelID] = tunnel
}

//DeleteChannelTunnel x
func (bridgeChannel *BridgeChannel) DeleteChannelTunnel(tunnelID uint32) {
	bridgeChannel.tunnelIDTunnelLocker.Lock()
	defer bridgeChannel.tunnelIDTunnelLocker.Unlock()
	delete(bridgeChannel.tunnelIDTunnelMap, tunnelID)
}

//GetChannelTunnel x
func (bridgeChannel *BridgeChannel) GetChannelTunnel(tunnelID uint32) *Tunnel {
	return bridgeChannel.tunnelIDTunnelMap[tunnelID]
}
