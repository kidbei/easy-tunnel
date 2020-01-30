package core

import (
	"bytes"
	"encoding/binary"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	//CurrentVersion 当前版本
	CurrentVersion = byte(1)
	//MaxChannelDataSize 最大数据包长度
	MaxChannelDataSize int32 = 1024 * 1024
	//RequestFlag 请求标记
	RequestFlag = uint8(1)
	//ResponseFlag 返回标记
	ResponseFlag = uint8(2)
	//NotifyFlag 不需要返回的请求
	NotifyFlag = uint8(3)
	//ProtocolReqHeaderLen 请求协议头部长度
	ProtocolReqHeaderLen = 11
	//ProtocolRespHeaderLen 返回协议头部长度
	ProtocolRespHeaderLen = 11
	//CommandPing ping请求指令
	CommandPing = 1
	//CommandPong pong返回指令
	CommandPong = 2
	//CommandForwardToLocal 转发数据到客户端本地端口指令
	CommandForwardToLocal = 3
	//CommandOpenTunnel 开启端口映射
	CommandOpenTunnel = 4
	//CommandAgentChannelClosed 客户端agent通道关闭
	CommandAgentChannelClosed = 5
	//CommandForwardToTunnel 转发数据到映射通道
	CommandForwardToTunnel = 6
	//CommandTunnelChannelClosed 映射通道上的连接主动关闭
	CommandTunnelChannelClosed = 7
)

//Packet 数据包
type Packet struct {
	//包长 包含当前4字节
	Len uint32
	//协议版本号
	Ver uint8
	//标志位，用来标记是否需要返回等
	Flag uint8
	//请求序列号
	Req uint32
	//指令ID
	Cid uint8
	//是否成功
	Success uint8
	//数据
	Data []byte
	//异步等待器
	Wg *sync.WaitGroup
	//请求时间
	ReqTime int64
}

//OpenTunnelReq 开启端口映射请求体
type OpenTunnelReq struct {
	BindHost  string
	BindPort  int
	LocalHost string
	LocalPort int
}

//ProtocolHandler 解包类
type ProtocolHandler struct {
	packetLen         uint32
	dataBuffer        []byte
	RequestHandler    func(*Packet) (data []byte, err error)
	NotifyHandler     func(*Packet)
	DisconnectHandler func()
	Conn              net.Conn
	curReqID          uint32
	pendingMap        map[uint32]*Packet
}

func (protocolHandler *ProtocolHandler) Init() {
	protocolHandler.pendingMap = make(map[uint32]*Packet)
}

func (protocolHandler *ProtocolHandler) ReadAndUnpack(conn net.Conn) {
	defer conn.Close()

readLoop:
	for {
		buffer := make([]byte, 65535)
		readLen, err := conn.Read(buffer)
		if err != nil {
			log.Printf("bridge channel disconnect:%s, %+v\n", conn.RemoteAddr().String(), err)
			if protocolHandler.DisconnectHandler != nil {
				protocolHandler.DisconnectHandler()
			}
			break
		}
		if protocolHandler.dataBuffer == nil {
			protocolHandler.dataBuffer = buffer[0:readLen]
		} else {
			protocolHandler.dataBuffer = append(protocolHandler.dataBuffer, buffer[0:readLen]...)
		}
	unpack:
		for {
			if protocolHandler.packetLen == 0 {
				protocolHandler.packetLen = BytesToUInt32(protocolHandler.dataBuffer[0:4])
				if protocolHandler.packetLen == 0 {
				}
			}
			if len(protocolHandler.dataBuffer) < int(protocolHandler.packetLen) {
				continue readLoop
			}
			packet := &Packet{}
			packet.Len = protocolHandler.packetLen
			packet.Ver = BytesToUInt8(protocolHandler.dataBuffer[4:5])
			packet.Flag = BytesToUInt8(protocolHandler.dataBuffer[5:6])
			packet.Req = BytesToUInt32(protocolHandler.dataBuffer[6:10])
			if packet.Flag != ResponseFlag {
				packet.Cid = BytesToUInt8(protocolHandler.dataBuffer[10:11])
			} else {
				packet.Success = BytesToUInt8(protocolHandler.dataBuffer[10:11])
			}
			packet.Data = protocolHandler.dataBuffer[11:protocolHandler.packetLen]

			protocolHandler.gotPacket(packet)

			packetLen := int(protocolHandler.packetLen)
			protocolHandler.packetLen = 0
			if packetLen == len(protocolHandler.dataBuffer) {
				protocolHandler.dataBuffer = nil
				break unpack
			} else {
				protocolHandler.dataBuffer = protocolHandler.dataBuffer[packetLen:len(protocolHandler.dataBuffer)]
			}
		}
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
		requestPacket := protocolHandler.pendingMap[packet.Req]
		if requestPacket != nil {
			requestPacket.Data = packet.Data
			requestPacket.Flag = ResponseFlag
			requestPacket.Len = packet.Len
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
	return atomic.AddUint32(&protocolHandler.curReqID, 1)
}

//Send 发送请求等待返回
func (protocolHandler *ProtocolHandler) Send(cid uint8, data []byte) (*Packet, error) {
	wg := sync.WaitGroup{}
	tLen := ProtocolReqHeaderLen + len(data)
	ver := CurrentVersion
	flag := RequestFlag
	reqID := protocolHandler.generateID()
	sendPacket := &Packet{Len: uint32(tLen), Ver: ver, Flag: flag, Req: reqID, Cid: cid, Data: data, Wg: &wg, ReqTime: time.Now().Unix()}
	packetData := PacketToBytes(sendPacket)
	protocolHandler.pendingMap[reqID] = sendPacket
	wg.Add(1)
	protocolHandler.Conn.Write(packetData)
	wg.Wait()
	return sendPacket, nil
}

//Notify 发送请求，不需要返回
func (protocolHandler *ProtocolHandler) Notify(cid uint8, data []byte) {
	tLen := ProtocolReqHeaderLen + len(data)
	ver := CurrentVersion
	flag := NotifyFlag
	reqID := protocolHandler.generateID()
	packet := &Packet{Len: uint32(tLen), Ver: ver, Flag: flag, Req: reqID, Cid: cid, Data: data, ReqTime: time.Now().Unix()}
	packetData := PacketToBytes(packet)
	protocolHandler.Conn.Write(packetData)
}

func (protocolHandler *ProtocolHandler) SendResponse(success uint8, data []byte, reqID uint32) {
	len := ProtocolRespHeaderLen + len(data)
	ver := CurrentVersion
	flag := ResponseFlag
	packet := &Packet{Len: uint32(len), Ver: ver, Flag: flag, Success: success, Req: reqID, Data: data}
	packetData := PacketToBytes(packet)
	protocolHandler.Conn.Write(packetData)
}

//PacketToBytes 封包
func PacketToBytes(packet *Packet) []byte {
	buffer := new(bytes.Buffer)
	buffer.Write(Uint32ToBytes(packet.Len))
	buffer.Write([]byte{packet.Ver})
	buffer.Write([]byte{packet.Flag})
	buffer.Write(Uint32ToBytes(packet.Req))
	if packet.Flag != ResponseFlag {
		buffer.Write([]byte{packet.Cid})
	} else {
		buffer.Write([]byte{packet.Success})
	}
	buffer.Write(packet.Data)
	return buffer.Bytes()
}

//BytesToInt32 x
func BytesToInt32(b []byte) int {
	buf := bytes.NewBuffer(b)
	var tmp uint32
	binary.Read(buf, binary.BigEndian, &tmp)
	return int(tmp)
}

//BytesToUInt32 x
func BytesToUInt32(b []byte) uint32 {
	buf := bytes.NewBuffer(b)
	var tmp uint32
	binary.Read(buf, binary.BigEndian, &tmp)
	return tmp
}

//BytesToUInt8 x
func BytesToUInt8(b []byte) uint8 {
	buf := bytes.NewBuffer(b)
	var tmp uint8
	binary.Read(buf, binary.LittleEndian, &tmp)
	return tmp
}

//Uint32ToBytes x
func Uint32ToBytes(i uint32) []byte {
	buf := bytes.NewBuffer([]byte{})
	tmp := i
	binary.Write(buf, binary.BigEndian, tmp)
	return buf.Bytes()
}

//Int64ToBytes x
func Int64ToBytes(i int64) []byte {
	buf := bytes.NewBuffer([]byte{})
	tmp := i
	binary.Write(buf, binary.BigEndian, tmp)
	return buf.Bytes()
}

//Int32ToBytes x
func Int32ToBytes(i int32) []byte {
	buf := bytes.NewBuffer([]byte{})
	tmp := i
	binary.Write(buf, binary.BigEndian, tmp)
	return buf.Bytes()
}

//BytesToInt64 x
func BytesToInt64(b []byte) int64 {
	buf := bytes.NewBuffer(b)
	var tmp int64
	binary.Read(buf, binary.LittleEndian, &tmp)
	return tmp
}

//StringIPToInt x
func StringIPToInt(ipstring string) int {
	ipSegs := strings.Split(ipstring, ".")
	var ipInt int = 0
	var pos uint = 24
	for _, ipSeg := range ipSegs {
		tempInt, _ := strconv.Atoi(ipSeg)
		tempInt = tempInt << pos
		ipInt = ipInt | tempInt
		pos -= 8
	}
	return ipInt
}

//IPIntToString x
func IPIntToString(ipInt int) string {
	ipSegs := make([]string, 4)
	var len int = len(ipSegs)
	buffer := bytes.NewBufferString("")
	for i := 0; i < len; i++ {
		tempInt := ipInt & 0xFF
		ipSegs[len-i-1] = strconv.Itoa(tempInt)
		ipInt = ipInt >> 8
	}
	for i := 0; i < len; i++ {
		buffer.WriteString(ipSegs[i])
		if i < len-1 {
			buffer.WriteString(".")
		}
	}
	return buffer.String()
}
