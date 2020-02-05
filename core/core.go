package core

import (
	"bytes"
	"encoding/binary"
	"strconv"
	"strings"
	"sync"
)

const (
	//CurrentVersion 当前版本
	CurrentVersion = byte(1)
	//MaxChannelDataSize 最大数据包长度
	MaxChannelDataSize int32 = 65536
	//RequestFlag 请求标记
	RequestFlag = uint8(1)
	//ResponseFlag 返回标记
	ResponseFlag = uint8(2)
	//NotifyFlag 不需要返回的请求
	NotifyFlag = uint8(3)

	TunnelTypeTcp = "tcp"

	TunnelTypeUdp = "udp"

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

var byteOrder = binary.LittleEndian


//Packet 数据包
type Packet struct {
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
	BindHost   string
	BindPort   int
	LocalHost  string
	LocalPort  int
	TunnelType string
}

//PacketToBytes 封包
func PacketToBytes(packet *Packet) []byte {
	buffer := new(bytes.Buffer)
	buffer.WriteByte(packet.Ver)
	buffer.WriteByte(packet.Flag)
	buffer.Write(Uint32ToBytes(packet.Req))
	if packet.Flag != ResponseFlag {
		buffer.WriteByte(packet.Cid)
	} else {
		buffer.WriteByte(packet.Success)
	}
	buffer.Write(packet.Data)
	return buffer.Bytes()
}

func BinaryToPacket(data []byte) Packet  {
	buffer := bytes.NewBuffer(data)
	packet := Packet{}

	packet.Ver, _ = buffer.ReadByte()
	packet.Flag, _ = buffer.ReadByte()
	reqBytes := make([]byte, 4)
	buffer.Read(reqBytes)
	packet.Req = BytesToUInt32(reqBytes)
	if packet.Flag != ResponseFlag {
		packet.Cid, _ = buffer.ReadByte()
	} else {
		packet.Success, _ = buffer.ReadByte()
	}
	packet.Data = data[7:]
	return packet
}

//BytesToUInt32 x
func BytesToUInt32(b []byte) uint32 {
	buf := bytes.NewBuffer(b)
	var tmp uint32
	binary.Read(buf, byteOrder, &tmp)
	return tmp
}

//BytesToUInt8 x
func BytesToUInt8(b []byte) uint8 {
	buf := bytes.NewBuffer(b)
	var tmp uint8
	binary.Read(buf, byteOrder, &tmp)
	return tmp
}

//Uint32ToBytes x
func Uint32ToBytes(i uint32) []byte {
	b := make([]byte, 4)
	byteOrder.PutUint32(b, i)
	return b
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

func SplitArray(array []byte, size int) [][]byte {
	if len(array) <= size {
		return [][]byte{array}
	}
	var divided [][]byte

	for i := 0; i < len(array); i += size {
		end := i + size

		if end > len(array) {
			end = len(array)
		}

		divided = append(divided, array[i:end])
	}

	return divided
}
