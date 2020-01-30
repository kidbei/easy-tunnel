package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kidbei/easy-tunnel/core"
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
	conn                  net.Conn
	protocolHandler       *core.ProtocolHandler
	channelIDTunnelMap    map[uint32]*Tunnel
	channelIDTunnelLocker sync.Mutex
	tunnels				  map[uint32]*Tunnel
}

//Start start bridge server
func (bridgeServer BridgeServer) Start() {
	server, e := net.Listen("tcp", bridgeServer.Host+":"+strconv.Itoa(bridgeServer.Port))
	if e != nil {
		fmt.Println("start birdge server failed", e)
		panic("start bridge server failed")
	}

	defer server.Close()

	fmt.Printf("start bridge server success at %s:%d\n", bridgeServer.Host, bridgeServer.Port)

	for {
		conn, err := server.Accept()
		if err != nil {
			fmt.Println("accept error", err)
		} else {
			go bridgeServer.handleBridgeConnection(conn)
		}
	}
}

func (bridgeServer *BridgeServer) handleBridgeConnection(conn net.Conn) {
	channel := &BridgeChannel{channelIDTunnelMap: make(map[uint32]*Tunnel), tunnels:make(map[uint32]*Tunnel)}
	channel.conn = conn
	channel.protocolHandler = &core.ProtocolHandler{RequestHandler: channel.handleRequest,
		NotifyHandler: channel.handleNotify, Conn: conn, DisconnectHandler:channel.disconnectHandler}
	channel.protocolHandler.Init()

	channel.protocolHandler.ReadAndUnpack(conn)
}


func (bridgeChannel *BridgeChannel) disconnectHandler() {
	bridgeChannel.channelIDTunnelLocker.Lock()
	defer bridgeChannel.channelIDTunnelLocker.Unlock()
	for _,tunnel := range bridgeChannel.tunnels {
		tunnel.CloseTunnel()
	}
	bridgeChannel.tunnels = make(map[uint32]*Tunnel)
}

//handleRequest 处理请求
func (bridgeChannel *BridgeChannel) handleRequest(packet *core.Packet) (data []byte, err error) {
	switch packet.Cid {
	case core.CommandOpenTunnel:
		return bridgeChannel.handleOpenTunnel(packet)
	default:
		return []byte("unknown cid"), errors.New("unkown cid:" + strconv.Itoa(int(packet.Cid)))
	}
}

func (bridgeChannel *BridgeChannel) handleNotify(packet *core.Packet) {
	switch packet.Cid {
	case core.CommandPing:
		fmt.Printf("got ping from %s\n", bridgeChannel.conn.RemoteAddr().String())
		bridgeChannel.protocolHandler.Notify(core.CommandPong, nil)
	case core.CommandAgentChannelClosed:
		bridgeChannel.handleAgentChannelClosed(packet)
	case core.CommandForwardToTunnel:
		bridgeChannel.handleForwardToTunnel(packet)
	default:
		fmt.Printf("invalid notify request:%+v\n", packet)
	}
}

//handleOpenTunnel 开启端口映射
func (bridgeChannel *BridgeChannel) handleOpenTunnel(packet *core.Packet) (data []byte, err error) {
	openTunnelReq := &core.OpenTunnelReq{}
	e := json.Unmarshal(packet.Data, openTunnelReq)
	if e != nil {
		return []byte("invalid request params"), e
	}
	fmt.Printf("open tunnel:%+v\n", openTunnelReq)
	tunnel, openErr := NewTunnel(openTunnelReq.BindHost, openTunnelReq.BindPort,
		openTunnelReq.LocalHost, openTunnelReq.LocalPort, bridgeChannel)
	if openErr != nil {
		fmt.Printf("open tunnel failed:%+v\n", openErr)
		return []byte("open tunnel error"), openErr
	}
	bridgeChannel.tunnels[tunnel.TunnelID] = tunnel
	return []byte("open success"), nil
}

//handleAgentChannelClosed 客户端通道关闭
func (bridgeChannel *BridgeChannel) handleAgentChannelClosed(packet *core.Packet) {
	if packet.Data == nil {
		fmt.Printf("invalid request, ChannelID is nil:%+v\n", packet)
		return
	}
	channelID := core.BytesToUInt32(packet.Data)
	tunnel := bridgeChannel.GetChannelTunnel(channelID)
	if tunnel != nil {
		fmt.Printf("close tunnel channel:%d\n", channelID)
		tunnel.CloseTunnelChannel(channelID)
	} else {
		fmt.Printf("tunnel is not found for channel:%d\n", channelID)
	}
}

//handleForwardToTunnel 转发数据到映射通道
func (bridgeChannel *BridgeChannel) handleForwardToTunnel(packet *core.Packet) {
	channelIDBytes := packet.Data[0:4]
	channelID := core.BytesToUInt32(channelIDBytes)
	data := packet.Data[4:len(packet.Data)]
	tunnel := bridgeChannel.GetChannelTunnel(channelID)
	if tunnel == nil {
		fmt.Printf("tunnel not found for ChannelID:%d, ignore to forward\n", channelID)
		return
	}
	tunnel.ForwardToTunnel(channelID, data)
}

//ForwardDataToLocal 转发数据到客户端本地
func (bridgeChannel *BridgeChannel) ForwardDataToLocal(channelID uint32,
	localHost string, localPort int, data []byte) {
	channelIDBytes := core.Uint32ToBytes(channelID)
	packetData := append(channelIDBytes, core.Int32ToBytes(int32(core.StringIPToInt(localHost)))...)
	packetData = append(packetData, core.Uint32ToBytes(uint32(localPort))...)
	packetData = append(packetData, data...)
	bridgeChannel.protocolHandler.Notify(core.CommandForwardToLocal, packetData)
}

//NotifyTunnelChannelClosed 映射连接上的连接主动关闭后触发
func (bridgeChannel *BridgeChannel) NotifyTunnelChannelClosed(channelID uint32) {
	fmt.Printf("notify tunnel channel closed to local agent, ChannelID:%d\n", channelID)
	bridgeChannel.protocolHandler.Notify(core.CommandTunnelChannelClosed, core.Uint32ToBytes(channelID))
}

//AddChannelTunnel x
func (bridgeChannel *BridgeChannel) AddChannelTunnel(channelID uint32, tunnel *Tunnel) {
	bridgeChannel.channelIDTunnelLocker.Lock()
	defer bridgeChannel.channelIDTunnelLocker.Unlock()
	bridgeChannel.channelIDTunnelMap[channelID] = tunnel
}

//DeleteChannelTunnel x
func (bridgeChannel *BridgeChannel) DeleteChannelTunnel(channelID uint32) {
	bridgeChannel.channelIDTunnelLocker.Lock()
	defer bridgeChannel.channelIDTunnelLocker.Unlock()
	delete(bridgeChannel.channelIDTunnelMap, channelID)
}

//GetChannelTunnel x
func (bridgeChannel *BridgeChannel) GetChannelTunnel(channelID uint32) *Tunnel {
	return bridgeChannel.channelIDTunnelMap[channelID]
}
