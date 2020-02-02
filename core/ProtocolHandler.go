package core

import (
	"encoding/binary"
	"github.com/smallnest/goframe"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

type ProtocolHandler struct {
	PacketLen         uint32
	DataBuffer        []byte
	RequestHandler    func(*Packet) (data []byte, err error)
	NotifyHandler     func(*Packet)
	DisconnectHandler func()
	Conn              net.Conn
	CurReqID          uint32
	PendingMap        map[uint32]*Packet
	encodeConfig      goframe.EncoderConfig
	decodeConfig      goframe.DecoderConfig
	frameConn         goframe.FrameConn
}

func NewProtocolHandler(conn net.Conn) *ProtocolHandler {
	protocolHandler := &ProtocolHandler{}
	protocolHandler.PendingMap = make(map[uint32]*Packet)

	protocolHandler.encodeConfig = goframe.EncoderConfig{
		ByteOrder:                       binary.BigEndian,
		LengthFieldLength:               2,
		LengthAdjustment:                0,
		LengthIncludesLengthFieldLength: false,
	}

	protocolHandler.decodeConfig = goframe.DecoderConfig{
		ByteOrder:           binary.BigEndian,
		LengthFieldLength:   2,
		LengthFieldOffset:   0,
		LengthAdjustment:    0,
		InitialBytesToStrip: 2,
	}

	protocolHandler.frameConn =
		goframe.NewLengthFieldBasedFrameConn(protocolHandler.encodeConfig, protocolHandler.decodeConfig, conn)

	return protocolHandler
}

func (protocolHandler *ProtocolHandler) ReadPacket(conn net.Conn) {
	defer conn.Close()
	for {
		b, err := protocolHandler.frameConn.ReadFrame()
		if err != nil {
			log.Printf("bridge channel disconnect:%s, %+v\n", conn.RemoteAddr().String(), err)
			if protocolHandler.DisconnectHandler != nil {
				protocolHandler.DisconnectHandler()
			}
			return
		}
		packet := &Packet{}
		packet.Ver = BytesToUInt8(b[0:1])
		packet.Flag = BytesToUInt8(b[1:2])
		packet.Req = BytesToUInt32(b[2:6])
		if packet.Flag != ResponseFlag {
			packet.Cid = BytesToUInt8(b[6:7])
		} else {
			packet.Success = BytesToUInt8(b[6:7])
		}
		packet.Data = b[7:]
		protocolHandler.gotPacket(packet)
	}
}

//gotPacket 解出完整数据包
func (protocolHandler *ProtocolHandler) gotPacket(packet *Packet) {
	if packet.Flag == RequestFlag {
		data, err := protocolHandler.RequestHandler(packet)
		var success uint8
		if err != nil {
			success = uint8(0)
		} else {
			success = uint8(1)
		}
		protocolHandler.SendResponse(success, data, packet.Req)
	} else if packet.Flag == ResponseFlag {
		requestPacket := protocolHandler.PendingMap[packet.Req]
		if requestPacket != nil {
			requestPacket.Data = packet.Data
			requestPacket.Flag = ResponseFlag
			requestPacket.Success = packet.Success
			requestPacket.Wg.Done()
		} else {
			log.Printf("got invalid response, may be it's timeout. %+v\n", packet)
		}
	} else if packet.Flag == NotifyFlag {
		protocolHandler.NotifyHandler(packet)
	}
}

func (protocolHandler *ProtocolHandler) generateID() uint32 {
	return atomic.AddUint32(&protocolHandler.CurReqID, 1)
}

//Send 发送请求等待返回
func (protocolHandler *ProtocolHandler) Send(cid uint8, data []byte) (*Packet, error) {
	wg := sync.WaitGroup{}
	ver := CurrentVersion
	flag := RequestFlag
	reqID := protocolHandler.generateID()
	sendPacket := &Packet{ Ver: ver, Flag: flag, Req: reqID, Cid: cid, Data: data, Wg: &wg, ReqTime: time.Now().Unix()}
	packetData := PacketToBytes(sendPacket)
	protocolHandler.PendingMap[reqID] = sendPacket
	wg.Add(1)
	protocolHandler.frameConn.WriteFrame(packetData)
	wg.Wait()
	return sendPacket, nil
}

//Notify 发送请求，不需要返回
func (protocolHandler *ProtocolHandler) Notify(cid uint8, data []byte) {
	ver := CurrentVersion
	flag := NotifyFlag
	reqID := protocolHandler.generateID()
	packet := &Packet{Ver: ver, Flag: flag, Req: reqID, Cid: cid, Data: data, ReqTime: time.Now().Unix()}
	packetData := PacketToBytes(packet)
	protocolHandler.frameConn.WriteFrame(packetData)
}

func (protocolHandler *ProtocolHandler) SendResponse(success uint8, data []byte, reqID uint32) {
	ver := CurrentVersion
	flag := ResponseFlag
	packet := &Packet{Ver: ver, Flag: flag, Success: success, Req: reqID, Data: data}
	packetData := PacketToBytes(packet)
	protocolHandler.frameConn.WriteFrame(packetData)
}
