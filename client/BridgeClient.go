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
	tickerChan            chan bool
	protocolHandler       *core.ProtocolHandler
	agentChannelMap       map[uint32]*Agent
	agentChannelMapLocker sync.Mutex
	conn				  net.Conn
}

//Connect 连接服务端
func (bridgeClient *BridgeClient) Connect(host string, port int) (bool, error) {
	conn, err := net.Dial("tcp", host+":"+strconv.Itoa(port))
	if err != nil {
		log.Println("connect failed", err)
		return false, err
	}
	bridgeClient.conn = conn
	bridgeClient.agentChannelMap = make(map[uint32]*Agent)
	bridgeClient.protocolHandler = &core.ProtocolHandler{RequestHandler: bridgeClient.handleRequest,
		NotifyHandler: bridgeClient.handleNotify, DisconnectHandler: bridgeClient.handleDisconnect, Conn: conn}
	bridgeClient.protocolHandler.Init()
	go bridgeClient.protocolHandler.ReadAndUnpack(conn)
	bridgeClient.startPing()
	return true, nil
}

func (bridgeClient *BridgeClient) Close()  {
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

//ForwardToTunnel 转发数据到服务端
func (bridgeClient *BridgeClient) ForwardToTunnel(channelID uint32, data []byte) {
	packetData := append(core.Uint32ToBytes(channelID), data...)
	bridgeClient.protocolHandler.Notify(core.CommandForwardToTunnel, packetData)
}

//NotifyAgentChannelClosed x
func (bridgeClient *BridgeClient) NotifyAgentChannelClosed(channelID uint32) {
	log.Printf("notify agent channel closed event for channelID:%d\n", channelID)
	bridgeClient.protocolHandler.Notify(core.CommandAgentChannelClosed, core.Uint32ToBytes(channelID))
}

//OpenTunnel 开启端口映射
func (bridgeClient *BridgeClient) OpenTunnel(remoteBindHost string, remoteBindPort int,
	localHost string, localPort int) (msg string, err error) {

	openTunnelReq := core.OpenTunnelReq{BindHost: remoteBindHost, BindPort: remoteBindPort,
		LocalHost: localHost, LocalPort: localPort}
	bytes, _ := json.Marshal(openTunnelReq)
	packet, e := bridgeClient.Send(core.CommandOpenTunnel, bytes)
	if e != nil {
		return "send open tunnel request error", e
	}
	if packet.Success == 0 {
		return string(packet.Data), errors.New(string(packet.Data))
	}
	log.Printf("open tunnel success%s\n", string(bytes))
	return "ok", nil
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
	localHost := core.IPIntToString(core.BytesToInt32(packet.Data[4:8]))
	localPort := int(core.BytesToUInt32(packet.Data[8:12]))
	data := packet.Data[12:len(packet.Data)]

	agent := bridgeClient.GetAgentChannel(channelID)
	if agent == nil {
		log.Printf("agent not found for channelID:%d\n", channelID)
		a, err := NewAgent(channelID, localHost, localPort, bridgeClient)
		if err != nil {
			log.Printf("connect to local failed, %s:%d, %+v\n", localHost, localPort, err)
			bridgeClient.NotifyAgentChannelClosed(channelID)
			return
		}
		log.Printf("new agent connection:%s:%d\n", localHost, localPort)
		agent = a
		bridgeClient.AddAgentChannel(channelID, agent)
	}
	agent.ForwardToAgentChannel(data)
}

func (bridgeClient *BridgeClient) handleTunnelChannelClosed(packet *core.Packet)  {
	channelID := core.BytesToUInt32(packet.Data)
	agent := bridgeClient.GetAgentChannel(channelID)
	if agent != nil {
		log.Printf("close agent channel:%d\n", channelID)
		agent.CloseAgent()
	}
}

//AddAgentChannel x
func (bridgeClient *BridgeClient) AddAgentChannel(channelID uint32, agent *Agent) {
	bridgeClient.agentChannelMapLocker.Lock()
	defer bridgeClient.agentChannelMapLocker.Unlock()
	bridgeClient.agentChannelMap[channelID] = agent
}

//DeleteAgentChannel x
func (bridgeClient *BridgeClient) DeleteAgentChannel(channelID uint32) {
	bridgeClient.agentChannelMapLocker.Lock()
	defer bridgeClient.agentChannelMapLocker.Unlock()
	delete(bridgeClient.agentChannelMap, channelID)
}

//GetAgentChannel x
func (bridgeClient *BridgeClient) GetAgentChannel(channelID uint32) *Agent {
	return bridgeClient.agentChannelMap[channelID]
}

func (bridgeClient *BridgeClient) handleDisconnect() {
	bridgeClient.stopPing()
	bridgeClient.agentChannelMapLocker.Lock()
	defer bridgeClient.agentChannelMapLocker.Unlock()
	for _, agent := range bridgeClient.agentChannelMap {
		agent.CloseAgent()
	}
	os.Exit(2)
}
