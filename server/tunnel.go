package main

import (
	"log"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
)

type Tunnel interface {
	Close()

	Listen() error

	SetDataReceivedHandler(handler func(channel TunnelConn, data []byte))

	GetDataReceivedHandler()func(channel TunnelConn, data []byte)

	SetClosedHandler(handler func())

	GetClosedHandler() func()

	SetTunnelChannelClosedHandler(handler func(channel TunnelConn))

	GetTunnelChannelClosedHandler() func(channel TunnelConn)

	GetTunnelID() uint32

	SetTunnelID(tunnelID uint32)

	GenerateChannelID() uint32

	AddTunnelChannel(channelID uint32, conn TunnelConn)

	DeleteTunnelChannel(channelID uint32)

	GetTunnelChannel(channelID uint32) TunnelConn

	ForwardToTunnelChannel(channelID uint32, data []byte)

	CloseTunnelChannel(channelID uint32)
}

type TcpTunnel struct {
	TunnelProperty
 	l net.Listener
}

type UdpTunnel struct {
	Tunnel
	TunnelProperty
	udpConn net.UDPConn
}

type TunnelConn interface {
	Close()

	IsAgentClosed() bool

	GetChannelID() uint32

	SetAgentClosed(closed bool)

	GetRemoteAddStr() string

	Write(data []byte)
}

type TunnelConnProperty struct {
	AgentClosed bool
	ChannelID   uint32
}

type TcpTunnelConn struct {
	TunnelConn
	TunnelConnProperty
	conn net.Conn
}



//Tunnel 被映射的服务端通道
type TunnelProperty struct {
	Tunnel
	host                       string
	port                       int
	localHost                  string
	localPort                  int
	tunnelChannelMap           map[uint32]*TunnelConn
	tunnelChannelMapLocker     sync.RWMutex
	channelIDAtom              uint32
	TunnelID                   uint32
	ClosedHandler              func()
	DataReceivedHandler        func(channel TunnelConn, data []byte)
	TunnelChannelClosedHandler func(channel TunnelConn)
}

func (tunnel *TunnelProperty) SetDataReceivedHandler(handler func(channel TunnelConn, data []byte)) {
	tunnel.DataReceivedHandler = handler
}

func (tunnel *TunnelProperty) GetDataReceivedHandler() func(channel TunnelConn, data []byte) {
	return tunnel.DataReceivedHandler
}

func (tunnel *TunnelProperty) SetClosedHandler(handler func()) {
	tunnel.ClosedHandler = handler
}

func (tunnel *TunnelProperty) GetClosedHandler() func() {
	return tunnel.ClosedHandler
}

func (tunnel *TunnelProperty) SetTunnelChannelClosedHandler(handler func(channel TunnelConn)) {
	tunnel.TunnelChannelClosedHandler = handler
}

func (tunnel *TunnelProperty) GetTunnelChannelClosedHandler() func(channel TunnelConn) {
	return tunnel.TunnelChannelClosedHandler
}

func (tunnel *TunnelProperty) GenerateChannelID() uint32 {
	return atomic.AddUint32(&tunnel.channelIDAtom, 1)
}

func (tunnel *TunnelProperty) GetTunnelID() uint32 {
	return tunnel.TunnelID
}

func (tunnel *TunnelProperty) SetTunnelID(tunnelID uint32) {
	tunnel.TunnelID = tunnelID
}


func (tunnel *TunnelProperty) init() {
	tunnel.tunnelChannelMap = make(map[uint32]*TunnelConn)
	tunnel.channelIDAtom = uint32(1)
}

//CloseTunnelChannel 关闭映射连接通道
func (tunnel *TunnelProperty) CloseTunnelChannel(channelID uint32) {
	tunnelChannel := tunnel.GetTunnelChannel(channelID)
	if tunnelChannel != nil {
		log.Printf("close tunnel channel, ChannelID:%d, address:%s\n", channelID, tunnelChannel.GetRemoteAddStr())
		tunnelChannel.SetAgentClosed(true)
		tunnelChannel.Close()
	}
}

//CloseTunnel 关闭所有连接
func (tunnel *TunnelProperty) CloseTunnel() {
	tunnel.tunnelChannelMapLocker.Lock()
	defer tunnel.tunnelChannelMapLocker.Unlock()
	for channelID, _ := range tunnel.tunnelChannelMap {
		tunnel.CloseTunnelChannel(channelID)
	}
}

//ForwardToTunnelChannel x
func (tunnel *TunnelProperty) ForwardToTunnelChannel(channelID uint32, data []byte) {
	tunnelChannel := tunnel.GetTunnelChannel(channelID)
	if tunnelChannel == nil {
		log.Printf("tunnel channel is not found for ChannelID:%d\n", channelID)
		return
	}
	tunnelChannel.Write(data)
}

//AddTunnelChannel x
func (tunnel *TunnelProperty) AddTunnelChannel(channelID uint32, conn TunnelConn) {
	tunnel.tunnelChannelMapLocker.Lock()
	defer tunnel.tunnelChannelMapLocker.Unlock()
	tunnel.tunnelChannelMap[channelID] = &conn
}

//DeleteTunnelChannel x
func (tunnel *TunnelProperty) DeleteTunnelChannel(channelID uint32) {
	tunnel.tunnelChannelMapLocker.Lock()
	defer tunnel.tunnelChannelMapLocker.Unlock()
	delete(tunnel.tunnelChannelMap, channelID)
}

//GetTunnelChannel x
func (tunnel *TunnelProperty) GetTunnelChannel(channelID uint32) TunnelConn {
	if tunnel, exist := tunnel.tunnelChannelMap[channelID]; exist {
		return *tunnel
	} else {
		return nil
	}
}

//tcp tunnel

func (tunnel *TcpTunnelConn) Close() {
	tunnel.conn.Close()
}

func (tunnel *TcpTunnelConn) IsAgentClosed() bool {
	return tunnel.AgentClosed
}

func (tunnel *TcpTunnelConn) GetChannelID() uint32 {
	return tunnel.ChannelID
}

func (tunnel *TcpTunnelConn) SetAgentClosed(closed bool) {
	tunnel.AgentClosed = closed
}

func (tunnel *TcpTunnelConn) GetRemoteAddStr() string {
	return tunnel.conn.RemoteAddr().String()
}

func (tunnel *TcpTunnelConn) Write(data []byte) {
	tunnel.conn.Write(data)
}

func (tunnel *TcpTunnel) Close() {
	log.Printf("close tcp tunnel:%s:%d\n", tunnel.host, tunnel.port)

	tunnel.l.Close()
	tunnel.TunnelProperty.CloseTunnel()
}

func (tunnel *TcpTunnel) Listen() error {
	tunnel.TunnelProperty.init()

	address := tunnel.host + ":" + strconv.Itoa(tunnel.port)
	l, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	tunnel.l = l
	go func() {
		defer tunnel.ClosedHandler()
		for {
			conn, e := l.Accept()
			if e != nil {
				log.Println("tunnel closed", e)
				break
			}
			go tunnel.handleConnection(conn)
		}
		log.Printf("tunnel closed:%s:%d\n", tunnel.localHost, tunnel.localPort)
	}()

	return nil
}

func (tunnel *TcpTunnel) handleConnection(conn net.Conn) {
	log.Printf("new tunnel connection %s\n", conn.RemoteAddr().String())

	channelID := tunnel.GenerateChannelID()
	tunnelChannel := &TcpTunnelConn{TunnelConnProperty: TunnelConnProperty{AgentClosed: false, ChannelID: channelID}, conn: conn}
	tunnel.AddTunnelChannel(channelID, tunnelChannel)

	defer conn.Close()
	defer tunnel.DeleteTunnelChannel(channelID)
	defer tunnel.GetTunnelChannelClosedHandler()(tunnelChannel)
	for {
		buffer := make([]byte, 1024)

		readLen, err := conn.Read(buffer)
		if err != nil {
			log.Println("read from client error", err)
			break
		}
		data := buffer[0:readLen]
		tunnel.GetDataReceivedHandler()(tunnelChannel, data)
	}
	if !tunnelChannel.AgentClosed {
		tunnel.GetTunnelChannelClosedHandler()(tunnelChannel)
	}
}


