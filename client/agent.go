package main

import (
	"log"
	"net"
	"strconv"
)

//Agent 客户端到目标端口链路
type Agent struct {
	conn      net.Conn
	channelID uint32
	localHost string
	localPort int
	TunnelChannelClosed bool

	bridgeClient *BridgeClient
}

//NewAgent 新建本地连接
func NewAgent(channelID uint32, host string, port int, bridgeClient *BridgeClient) (*Agent, error) {
	agent := &Agent{channelID: channelID, localHost: host, localPort: port, bridgeClient: bridgeClient, TunnelChannelClosed: false}

	addr, err := net.ResolveTCPAddr("tcp", host+":"+strconv.Itoa(port))

	conn, err := net.DialTCP("tcp", nil, addr)
	if err != nil {
		log.Println("connect failed", err)
		return nil, err
	}
	agent.conn = conn

	go agent.handleConnection(conn)

	return agent, nil
}

func (agent *Agent) handleConnection(conn net.Conn) {

	defer conn.Close()
	defer func() {
		if !agent.TunnelChannelClosed {
			agent.bridgeClient.NotifyAgentChannelClosed(agent.channelID)
		}
	}()
	defer agent.bridgeClient.DeleteAgentChannel(agent.channelID)
	for {
		buffer := make([]byte, 1024)

		readLen, err := conn.Read(buffer)
		if err != nil {
			log.Println("read from client error", err)
			break
		}
		data := buffer[0:readLen]
		agent.bridgeClient.ForwardToTunnel(agent.channelID, data)
	}
}

//ForwardToAgentChannel 转发数据到本地端口
func (agent *Agent) ForwardToAgentChannel(data []byte) {
	agent.conn.Write(data)
}

//CloseAgent 主动关闭客户端到目标的连接
func (agent *Agent) CloseAgent() {
	log.Printf("close agent, channelID:%d, address:%s\n", agent.channelID, agent.conn.RemoteAddr().String())
	agent.TunnelChannelClosed = true
	agent.conn.Close()
}
