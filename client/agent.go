package main

import (
	"github.com/kidbei/easy-tunnel/core"
	"log"
	"net"
	"strconv"
)

//Agent 客户端到目标端口链路
type Agent struct {
	conn                net.Conn
	channelID           uint32
	LocalHost           string
	LocalPort           int
	TunnelChannelClosed bool
	tunnelID            uint32
	DisconnectHandler   func()
	DataReceivedHandler func([]byte)
}

//NewAgent 新建本地连接
func NewAgent(channelID uint32, host string, port int, tunnelID uint32) (*Agent, error) {
	agent := &Agent{channelID: channelID, LocalHost: host, LocalPort: port, tunnelID: tunnelID,
		TunnelChannelClosed: false}

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
	defer agent.DisconnectHandler()
	buffer := make([]byte, core.MaxChannelDataSize)

	for {
		readLen, err := conn.Read(buffer)
		if err != nil {
			log.Println("read from client error", err)
			break
		}
		data := buffer[:readLen]
		agent.DataReceivedHandler(data)
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
