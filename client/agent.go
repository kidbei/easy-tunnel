package main

import (
	"fmt"
	"net"
	"strconv"
)

//Agent 客户端到目标端口链路
type Agent struct {
	conn      net.Conn
	channelID uint32
	localHost string
	localPort int

	bridgeClient *BridgeClient
}

//NewAgent 新建本地连接
func NewAgent(channelID uint32, host string, port int, bridgeClient *BridgeClient) (*Agent, error) {
	agent := &Agent{channelID: channelID, localHost: host, localPort: port, bridgeClient: bridgeClient}

	raddr, err := net.ResolveTCPAddr("tcp", host+":"+strconv.Itoa(port))

	conn, err := net.DialTCP("tcp", nil, raddr)
	if err != nil {
		fmt.Println("connect failed", err)
		return nil, err
	}
	agent.conn = conn

	go agent.handleConnection(conn)

	return agent, nil
}

func (agent *Agent) handleConnection(conn net.Conn) {

	defer conn.Close()
	defer agent.bridgeClient.NotifyAgentChannelClosed(agent.channelID)
	defer agent.bridgeClient.DeleteAgentChannel(agent.channelID)
	for {
		buffer := make([]byte, 1024)

		readLen, err := conn.Read(buffer)
		if err != nil {
			fmt.Println("read from client error", err)
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
