package main

import (
	"encoding/json"
	"errors"
	"github.com/kidbei/easy-tunnel/core"
	"log"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
)

//BridgeServer 连接客户端通道
type BridgeServer struct {
	Host     string
	Port     int
	channels []*BridgeChannel
	l        net.Listener
	locker   sync.Mutex
}

//BridgeChannel 客户端连接
type BridgeChannel struct {
	conn              net.Conn
	tunnelIDTunnelMap map[uint32]*Tunnel
	locker            sync.Mutex
	protocolHandler   *core.ProtocolHandler
	tunnelIDAtom      uint32
}

//Start start bridge server
func (bridgeServer *BridgeServer) Start() {
	go bridgeServer.startTcpServer()
}

func (bridgeServer *BridgeServer) Close() {
	bridgeServer.locker.Lock()
	defer bridgeServer.locker.Unlock()

	bridgeServer.l.Close()

	for idx, _ := range bridgeServer.channels {
		bridgeChannel := bridgeServer.channels[idx]
		bridgeChannel.Close()
	}

}

func (bridgeServer *BridgeServer) startTcpServer() {
	server, e := net.Listen("tcp", bridgeServer.Host+":"+strconv.Itoa(bridgeServer.Port))
	if e != nil {
		log.Panicln("start bridge server failed", e)
	}

	log.Printf("start bridge server success at %s:%d\n", bridgeServer.Host, bridgeServer.Port)
	bridgeServer.l = server

	for {
		conn, err := server.Accept()
		if err != nil {
			log.Println("tunnel connection disconnect", err)
			break
		}
		go bridgeServer.handleBridgeConnection(conn, core.NewProtocolHandler(conn))
	}
}

func (bridgeServer *BridgeServer) handleBridgeConnection(conn net.Conn, protocolHandler *core.ProtocolHandler) {
	channel := &BridgeChannel{tunnelIDTunnelMap: make(map[uint32]*Tunnel), tunnelIDAtom: uint32(1)}
	channel.conn = conn
	channel.protocolHandler = protocolHandler
	channel.protocolHandler.RequestHandler = channel.handleRequest
	channel.protocolHandler.DisconnectHandler = channel.disconnectHandler
	channel.protocolHandler.Conn = conn
	channel.protocolHandler.NotifyHandler = channel.handleNotify
	channel.protocolHandler.ReadPacket(conn)
}

func (bridgeChannel *BridgeChannel) disconnectHandler() {
	bridgeChannel.Close()
}

func (bridgeChannel *BridgeChannel) Close() {
	bridgeChannel.locker.Lock()
	defer bridgeChannel.locker.Unlock()
	for tunnelID, _ := range bridgeChannel.tunnelIDTunnelMap {
		tunnel := *bridgeChannel.GetChannelTunnel(tunnelID)
		tunnel.Close()
	}
	bridgeChannel.tunnelIDTunnelMap = make(map[uint32]*Tunnel)
}

//handleRequest 处理请求
func (bridgeChannel *BridgeChannel) handleRequest(packet *core.Packet) (data []byte, err error) {
	switch packet.Cid {
	case core.CommandOpenTunnel:
		return bridgeChannel.handleOpenTunnel(packet)
	default:
		log.Printf("unkown cid %d\n", packet.Cid)
		return []byte("unknown cid"), errors.New("unknown cid:" + strconv.Itoa(int(packet.Cid)))
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

func (bridgeChannel *BridgeChannel) generateTunnelID() uint32 {
	return atomic.AddUint32(&bridgeChannel.tunnelIDAtom, 1)
}

//handleOpenTunnel 开启端口映射
func (bridgeChannel *BridgeChannel) handleOpenTunnel(packet *core.Packet) (data []byte, err error) {
	openTunnelReq := &core.OpenTunnelReq{}
	e := json.Unmarshal(packet.Data, openTunnelReq)
	if e != nil {
		log.Printf("invalid request params:%s,error:%+v\n", string(packet.Data), e)
		return []byte("invalid request params"), e
	}
	log.Printf("open tunnel:%s\n", string(packet.Data))

	var tunnel Tunnel

	if &openTunnelReq.TunnelType == nil {
		return nil, errors.New("invalid tunnel type")
	}
	if openTunnelReq.TunnelType == core.TunnelTypeTcp {
		tunnel = &TcpTunnel{TunnelProperty: TunnelProperty{host: openTunnelReq.BindHost, port: openTunnelReq.BindPort, localHost: openTunnelReq.LocalHost,
			localPort: openTunnelReq.LocalPort}}
	} else {
		log.Printf("unknown tunnel type:%s\n", openTunnelReq.TunnelType)
		return nil, errors.New("invalid tunnel type")
	}
	tunnel.SetTunnelID(bridgeChannel.generateTunnelID())
	openErr := tunnel.Listen()
	if openErr != nil {
		log.Printf("open tunnel failed:%+v\n", openErr)
		return []byte("open tunnel error"), openErr
	}
	tunnel.SetDataReceivedHandler(func(channel TunnelConn, data []byte) {
		bridgeChannel.ForwardDataToAgent(channel.GetChannelID(), tunnel.GetTunnelID(), data)
	})
	tunnel.SetClosedHandler(func() {
		bridgeChannel.DeleteChannelTunnel(tunnel.GetTunnelID())
	})
	tunnel.SetTunnelChannelClosedHandler(func(channel TunnelConn) {
		bridgeChannel.NotifyTunnelChannelClosed(channel.GetChannelID(), tunnel.GetTunnelID())
	})
	bridgeChannel.AddTunnel(tunnel.GetTunnelID(), &tunnel)
	return core.Uint32ToBytes(tunnel.GetTunnelID()), nil
}

//handleAgentChannelClosed 客户端通道关闭
func (bridgeChannel *BridgeChannel) handleAgentChannelClosed(packet *core.Packet) {
	if packet.Data == nil {
		log.Printf("invalid request, ChannelID is nil:%+v\n", packet)
		return
	}
	channelID := core.BytesToUInt32(packet.Data[0:4])
	tunnelID := core.BytesToUInt32(packet.Data[4:8])

	tunnel := *bridgeChannel.GetChannelTunnel(tunnelID)
	if tunnel != nil {
		log.Printf("handle agent channel closed event:%d\n", channelID)
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
	tunnel := *bridgeChannel.GetChannelTunnel(tunnelID)
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
	bridgeChannel.locker.Lock()
	defer bridgeChannel.locker.Unlock()
	bridgeChannel.tunnelIDTunnelMap[tunnelID] = tunnel
}

//DeleteChannelTunnel x
func (bridgeChannel *BridgeChannel) DeleteChannelTunnel(tunnelID uint32) {
	bridgeChannel.locker.Lock()
	defer bridgeChannel.locker.Unlock()
	delete(bridgeChannel.tunnelIDTunnelMap, tunnelID)
}

//GetChannelTunnel x
func (bridgeChannel *BridgeChannel) GetChannelTunnel(tunnelID uint32) *Tunnel {
	return bridgeChannel.tunnelIDTunnelMap[tunnelID]
}
