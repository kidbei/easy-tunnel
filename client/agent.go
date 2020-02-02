package main

import (
	"github.com/kidbei/easy-tunnel/core"
	"log"
	"net"
	"strconv"
)

type Agent interface {
	Connect(host string, port int) error

	ForwardToAgentChannel(data []byte)

	Close()

	GetRemoteAddrStr() string

	SetDisconnectHandler(handler func())

	SetDataReceivedHandler(handler func([]byte))
}

type AgentProperty struct {
	ChannelID           uint32
	LocalHost           string
	LocalPort           int
	TunnelChannelClosed bool
	TunnelID            uint32
	DisconnectHandler   func()
	DataReceivedHandler func([]byte)
}

type TcpAgent struct {
	AgentProperty
	Conn net.Conn
}

func (agent *TcpAgent) Connect(host string, port int) error {
	addr, err := net.ResolveTCPAddr("tcp", host+":"+strconv.Itoa(port))
	conn, err := net.DialTCP("tcp", nil, addr)

	if err != nil {
		log.Println("connect failed", err)
		return err
	}

	agent.Conn = conn
	go agent.handleConnection(conn)

	return nil
}

func (agent *TcpAgent) ForwardToAgentChannel(data []byte) {
	agent.Conn.Write(data)
}

func (agent *TcpAgent) Close() {
	log.Printf("close tcp agent connection:%s:%d\n", agent.LocalHost, agent.LocalPort)
	agent.TunnelChannelClosed = true
	agent.Conn.Close()
}

func (agent *TcpAgent) handleConnection(conn net.Conn) {

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

func (agent *AgentProperty) GetRemoteAddrStr() string {
	return agent.LocalHost + strconv.Itoa(agent.LocalPort)
}

func (agent *AgentProperty) SetDisconnectHandler(handler func()) {
	agent.DisconnectHandler = handler
}

func (agent *AgentProperty) SetDataReceivedHandler(handler func([]byte)) {
	agent.DataReceivedHandler = handler
}
