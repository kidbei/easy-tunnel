package main

import (
	"encoding/json"
	"errors"
	"github.com/kidbei/easy-tunnel/core"
	"log"
	"net"
	"os"
	"strconv"
	"sync"
	"time"
)

//BridgeClient 客户端
type BridgeClient struct {
	tickerChan       chan bool
	protocolHandler  *core.ProtocolHandler
	conn             net.Conn
	remoteTunnelMap  map[uint32]RemoteTunnel
	locker           sync.Mutex
	ExitOnDisconnect bool
}

type RemoteTunnel struct {
	TunnelId        uint32
	LocalHost       string
	LocalPort       int
	agentChannelMap map[uint32]*Agent
	locker          sync.Mutex
	TunnelType      string
}

func NewBridgeClient() *BridgeClient {
	client := &BridgeClient{remoteTunnelMap: make(map[uint32]RemoteTunnel)}

	return client
}

//Connect 连接服务端
func (bridgeClient *BridgeClient) Connect(host string, port int) (bool, error) {
	var (
		conn net.Conn
		err  error
	)

	conn, err = net.Dial("tcp", host+":"+strconv.Itoa(port))

	if err != nil {
		log.Println("connect failed", err)
		return false, err
	}
	bridgeClient.conn = conn
	bridgeClient.protocolHandler = core.NewProtocolHandler(conn)
	bridgeClient.protocolHandler.NotifyHandler = bridgeClient.handleNotify
	bridgeClient.protocolHandler.RequestHandler = bridgeClient.handleRequest
	bridgeClient.protocolHandler.DisconnectHandler = bridgeClient.handleDisconnect
	bridgeClient.protocolHandler.Conn = conn
	go bridgeClient.protocolHandler.ReadPacket(conn)
	bridgeClient.startPing()
	return true, nil
}

func (bridgeClient *BridgeClient) Close() {
	bridgeClient.conn.Close()
}

func (bridgeClient *BridgeClient) startPing() {
	log.Println("start ping task")
	ticker := time.NewTicker(time.Second * 10)
	stopChan := make(chan bool)
	go func(ticker *time.Ticker) {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				log.Println("send ping request")
				bridgeClient.Notify(core.CommandPing, nil)
			case stop := <-stopChan:
				if stop {
					log.Println("stop ping task")
					return
				}
			}
		}
	}(ticker)
	bridgeClient.tickerChan = stopChan
}

func (bridgeClient *BridgeClient) stopPing() {
	if bridgeClient.tickerChan != nil {
		log.Println("stop ping")
		bridgeClient.tickerChan <- true
	}
}

//Send 往服务端发送消息，并等待返回
func (bridgeClient *BridgeClient) Send(cid uint8, data []byte) (*core.Packet, error) {
	return bridgeClient.protocolHandler.Send(cid, data)
}

//Notify 发送请求，不需要返回
func (bridgeClient *BridgeClient) Notify(cid uint8, data []byte) {
	bridgeClient.protocolHandler.Notify(cid, data)
}

//ForwardToTunnelChannel 转发数据到服务端
func (bridgeClient *BridgeClient) ForwardToTunnel(channelID uint32, tunnelID uint32, data []byte) {
	packetData := append(append(core.Uint32ToBytes(channelID), core.Uint32ToBytes(tunnelID)...), data...)
	log.Printf("forward to tunnel channel, channelID:%d, tunnelID:%d, data length:%d\n", channelID, tunnelID, len(data))
	bridgeClient.protocolHandler.Notify(core.CommandForwardToTunnel, packetData)
}

//NotifyAgentChannelClosed x
func (bridgeClient *BridgeClient) NotifyAgentChannelClosed(channelID uint32, tunnelID uint32) {
	log.Printf("notify agent channel closed event for channelID:%d, tunnelID:%d\n", channelID, tunnelID)
	data := append(core.Uint32ToBytes(channelID), core.Uint32ToBytes(tunnelID)...)
	bridgeClient.protocolHandler.Notify(core.CommandAgentChannelClosed, data)
}

//OpenTunnel 开启端口映射
func (bridgeClient *BridgeClient) OpenTunnel(remoteBindHost string, remoteBindPort int,
	localHost string, localPort int, tunnelType string) (tunnel *RemoteTunnel, err error) {

	openTunnelReq := core.OpenTunnelReq{BindHost: remoteBindHost, BindPort: remoteBindPort,
		LocalHost: localHost, LocalPort: localPort, TunnelType: tunnelType}
	bytes, _ := json.Marshal(openTunnelReq)
	packet, e := bridgeClient.Send(core.CommandOpenTunnel, bytes)
	if e != nil {
		return nil, e
	}
	if packet.Success == 0 {
		return nil, errors.New(string(packet.Data))
	}
	log.Printf("open tunnel success%s\n", string(bytes))

	tunnelID := core.BytesToUInt32(packet.Data)
	remoteTunnel := RemoteTunnel{TunnelId: tunnelID, LocalHost: localHost, LocalPort: localPort, TunnelType: tunnelType}
	remoteTunnel.agentChannelMap = make(map[uint32]*Agent)

	bridgeClient.addRemoteTunnel(tunnelID, remoteTunnel)

	return &remoteTunnel, nil
}

func (bridgeClient *BridgeClient) handleRequest(packet *core.Packet) (data []byte, err error) {
	log.Printf("received:%+v\n", string(packet.Data))
	return packet.Data, nil
}

func (bridgeClient *BridgeClient) handleNotify(packet *core.Packet) {
	switch packet.Cid {
	case core.CommandPong:
		log.Println("got pong")
	case core.CommandForwardToLocal:
		bridgeClient.handleForwardToAgentChannel(packet)
	case core.CommandTunnelChannelClosed:
		bridgeClient.handleTunnelChannelClosed(packet)
	default:
		log.Printf("invalid notify request:%+v\n", packet)
	}
}

func (bridgeClient *BridgeClient) handleForwardToAgentChannel(packet *core.Packet) {
	channelID := core.BytesToUInt32(packet.Data[0:4])
	tunnelID := core.BytesToUInt32(packet.Data[4:8])
	data := packet.Data[8:]

	tunnel := bridgeClient.getRemoteTunnel(tunnelID)
	if tunnel == nil {
		log.Printf("tunnel not found:%d\n", tunnelID)
		return
	}

	agent := tunnel.GetAgentChannel(channelID)
	if agent == nil {
		//加锁
		bridgeClient.locker.Lock()
		defer bridgeClient.locker.Unlock()

		agent = tunnel.GetAgentChannel(channelID)

		if agent == nil {
			log.Printf("agent not found for channelID:%d\n", channelID)

			if tunnel.TunnelType == core.TunnelTypeTcp {
				agent = &TcpAgent{AgentProperty: AgentProperty{ChannelID: channelID, LocalHost: tunnel.LocalHost,
					LocalPort: tunnel.LocalPort, TunnelID: tunnelID}}
			} else {
				log.Panicln("not support tunnel type:", tunnel.TunnelType)
			}

			err := agent.Connect(tunnel.LocalHost, tunnel.LocalPort)

			if err != nil {
				log.Printf("connect to local failed, %s, %+v\n", agent.GetRemoteAddrStr(), err)
				bridgeClient.NotifyAgentChannelClosed(channelID, tunnelID)
				return
			}

			log.Printf("new agent connection:%s\n", agent.GetRemoteAddrStr())
			agent.SetDisconnectHandler(func() {
				if !agent.IsTunnelChannelClosed() {
					bridgeClient.NotifyAgentChannelClosed(channelID, tunnelID)
				}
				tunnel.DeleteAgentChannel(channelID)
			})
			agent.SetDataReceivedHandler(func(bytes []byte) {
				bridgeClient.ForwardToTunnel(channelID, tunnelID, bytes)
			})
			tunnel.AddAgentChannel(channelID, &agent)
		}

	}
	agent.ForwardToAgentChannel(data)
}

func (bridgeClient *BridgeClient) handleTunnelChannelClosed(packet *core.Packet) {
	channelID := core.BytesToUInt32(packet.Data[0:4])
	tunnelID := core.BytesToUInt32(packet.Data[4:8])

	tunnel := bridgeClient.getRemoteTunnel(tunnelID)

	if tunnel == nil {
		log.Printf("tunnel not found:%d\n", tunnelID)
		return
	}
	agent := tunnel.GetAgentChannel(channelID)
	if agent != nil {
		log.Printf("close agent channel:%d, tunnelID:%d\n", channelID, tunnelID)
		defer tunnel.DeleteAgentChannel(channelID)
		agent.Close()
	}
}

//AddAgentChannel x
func (tunnel *RemoteTunnel) AddAgentChannel(channelID uint32, agent *Agent) {
	tunnel.locker.Lock()
	defer tunnel.locker.Unlock()
	tunnel.agentChannelMap[channelID] = agent
}

//DeleteAgentChannel x
func (tunnel *RemoteTunnel) DeleteAgentChannel(channelID uint32) {
	tunnel.locker.Lock()
	defer tunnel.locker.Unlock()
	delete(tunnel.agentChannelMap, channelID)
}

//GetAgentChannel x
func (tunnel *RemoteTunnel) GetAgentChannel(channelID uint32) Agent {
	if agent, exist := tunnel.agentChannelMap[channelID]; exist {
		return *agent
	}
	return nil
}

func (bridgeClient *BridgeClient) addRemoteTunnel(tunnelID uint32, tunnel RemoteTunnel) {
	bridgeClient.locker.Lock()
	defer bridgeClient.locker.Unlock()
	bridgeClient.remoteTunnelMap[tunnelID] = tunnel
}

func (bridgeClient *BridgeClient) getRemoteTunnel(tunnelID uint32) *RemoteTunnel {
	if tunnel, exist := bridgeClient.remoteTunnelMap[tunnelID]; exist {
		return &tunnel
	}
	return nil
}

func (bridgeClient *BridgeClient) handleDisconnect() {
	bridgeClient.stopPing()

	bridgeClient.Close()

	for _, tunnel := range bridgeClient.remoteTunnelMap {
		for channelID, _ := range tunnel.agentChannelMap {
			agent := *tunnel.agentChannelMap[channelID]
			agent.Close()
			tunnel.DeleteAgentChannel(channelID)
		}
	}

	if bridgeClient.ExitOnDisconnect {
		os.Exit(1)
	}
}
