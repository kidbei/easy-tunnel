package core

import (
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
	CurReqID          uint32
	PendingMap        map[uint32]*Packet
	codec			  LengthFieldCodec
}

func NewProtocolHandler(conn net.Conn) *ProtocolHandler {
	protocolHandler := &ProtocolHandler{}
	protocolHandler.PendingMap = make(map[uint32]*Packet)

	protocolHandler.codec = *NewLengthFieldCodec(conn, 4)

	return protocolHandler
}

func (protocolHandler *ProtocolHandler) ReadPacket(conn net.Conn) {
	for {
		b, err := protocolHandler.codec.Decode()
		if err != nil {
			log.Printf("bridge channel disconnect:%s, %+v\n", conn.RemoteAddr().String(), err)
			if protocolHandler.DisconnectHandler != nil {
				protocolHandler.DisconnectHandler()
			}
			return
		}
		if len(b) == 0 {
			log.Printf("got empty data from socket %s\n", conn.RemoteAddr().String())
			continue
		}
		packet := BinaryToPacket(b)
		protocolHandler.gotPacket(&packet)
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
		if err := protocolHandler.SendResponse(success, data, packet.Req); err != nil {
			log.Printf("send response error:%+v\n", err)
		}
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

func (protocolHandler *ProtocolHandler) write(data []byte) (n int, err error) {
	out, e := protocolHandler.codec.Encode(data)
	if e != nil {
		return 0, e
	}
	return protocolHandler.codec.conn.Write(out)
}

//Send 发送请求等待返回
func (protocolHandler *ProtocolHandler) Send(cid uint8, data []byte) (*Packet, error) {
	wg := sync.WaitGroup{}
	ver := CurrentVersion
	flag := RequestFlag
	reqID := protocolHandler.generateID()
	sendPacket := &Packet{Ver: ver, Flag: flag, Req: reqID, Cid: cid, Data: data, Wg: &wg, ReqTime: time.Now().Unix()}
	packetData := PacketToBytes(sendPacket)
	protocolHandler.PendingMap[reqID] = sendPacket
	wg.Add(1)
	n, err := protocolHandler.write(packetData)
	if n < len(packetData) || err != nil {
		wg.Done()
		return nil, err
	}
	wg.Wait()
	return sendPacket, nil
}



//Notify notify发送请求，不需要返回
func (protocolHandler *ProtocolHandler) Notify(cid uint8, data []byte) error {
	ver := CurrentVersion
	flag := NotifyFlag
	reqID := protocolHandler.generateID()
	packet := &Packet{Ver: ver, Flag: flag, Req: reqID, Cid: cid, Data: data, ReqTime: time.Now().Unix()}
	packetData := PacketToBytes(packet)
	n, err := protocolHandler.write(packetData)
	if n < len(packetData) || err != nil {
		return err
	}
	return nil
}

func (protocolHandler *ProtocolHandler) SendResponse(success uint8, data []byte, reqID uint32) error {
	ver := CurrentVersion
	flag := ResponseFlag
	packet := &Packet{Ver: ver, Flag: flag, Success: success, Req: reqID, Data: data}
	packetData := PacketToBytes(packet)
	n, err := protocolHandler.write(packetData)
	if n < len(packetData) || err != nil {
		return err
	}
	return nil
}
