package core

import (
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net"
)

var (
	// errProtocolNotSupported occurs when trying to use protocol that is not supported.
	errProtocolNotSupported = errors.New("not supported protocol on this platform")
	// errServerShutdown occurs when server is closing.
	errServerShutdown = errors.New("server is going to be shutdown")
	// ErrInvalidFixedLength occurs when the output data have invalid fixed length.
	ErrInvalidFixedLength = errors.New("invalid fixed length of bytes")
	// ErrUnexpectedEOF occurs when no enough data to read by codecc.
	ErrUnexpectedEOF = errors.New("there is no enough data")
	// ErrDelimiterNotFound occurs when no such a delimiter is in input data.
	ErrDelimiterNotFound = errors.New("there is no such a delimiter")
	// ErrCRLFNotFound occurs when a CRLF is not found by codecc.
	ErrCRLFNotFound = errors.New("there is no CRLF")
	// ErrUnsupportedLength occurs when unsupported lengthFieldLength is from input data.
	ErrUnsupportedLength = errors.New("unsupported lengthFieldLength. (expected: 1, 2, 3, 4, or 8)")
	// ErrTooLessLength occurs when adjusted frame length is less than zero.
	ErrTooLessLength = errors.New("adjusted frame length is less than zero")
)

type LengthFieldCodec struct {
	conn              net.Conn
	LengthFieldLength int
	ByteOrder         binary.ByteOrder
}


func NewLengthFieldCodec(conn net.Conn, lengthFieldLength int) *LengthFieldCodec {
	l := &LengthFieldCodec{conn:conn, LengthFieldLength:lengthFieldLength}
	l.ByteOrder = binary.LittleEndian
	return l
}

func (cc *LengthFieldCodec) Encode(data []byte) (out []byte, err error) {
	dataLen := len(data)
	length := dataLen + cc.LengthFieldLength

	switch cc.LengthFieldLength {
	case 1:
		if length >= 256 {
			return nil, fmt.Errorf("length does not fit into a byte: %d", length)
		}
		out = []byte{byte(dataLen)}
	case 2:
		if length >= 65536 {
			return nil, fmt.Errorf("length does not fit into a short integer: %d", length)
		}
		out = make([]byte, 2)
		cc.ByteOrder.PutUint16(out, uint16(dataLen))
	case 3:
		if length >= 16777216 {
			return nil, fmt.Errorf("length does not fit into a medium integer: %d", length)
		}
		out = writeUint24(cc.ByteOrder, dataLen)
	case 4:
		out = make([]byte, 4)
		cc.ByteOrder.PutUint32(out, uint32(dataLen))
	case 8:
		out = make([]byte, 8)
		cc.ByteOrder.PutUint64(out, uint64(dataLen))
	default:
		return nil, ErrUnsupportedLength
	}

	out = append(out, data...)
	return
}

func (cc *LengthFieldCodec) Decode() ([]byte, error) {
	lenBuf, size, err := cc.getUnadjustedFrameLength()
	if err != nil {
		return nil, err
	}
	if size == 0 {
		return nil, ErrUnexpectedEOF
	}

	if size > 16777216 {
		log.Printf("ssss#######:%d,buf:%+v\n", size, lenBuf)
	}

	n, data := cc.ReadN(int(size))
	if n == 0 {
		return nil, ErrUnexpectedEOF
	}
	if n != int(size) {
		return nil, ErrUnexpectedEOF
	}
	return data[:n], nil
}

func (cc *LengthFieldCodec) getUnadjustedFrameLength() (lenBuf []byte, n uint64, err error) {
	switch cc.LengthFieldLength {
	case 1:
		size, b := cc.ReadN(1)
		if size == 0 {
			return nil, 0, ErrUnexpectedEOF
		}
		return b, uint64(b[0]), nil
	case 2:
		size, lenBuf := cc.ReadN(2)
		if size == 0 {
			return nil, 0, ErrUnexpectedEOF
		}
		return lenBuf, uint64(cc.ByteOrder.Uint16(lenBuf)), nil
	case 3:
		size, lenBuf := cc.ReadN(3)
		if size == 0 {
			return nil, 0, ErrUnexpectedEOF
		}
		return lenBuf, readUint24(cc.ByteOrder, lenBuf), nil
	case 4:
		size, lenBuf := cc.ReadN(4)
		if size == 0 {
			return nil, 0, ErrUnexpectedEOF
		}
		return lenBuf, uint64(cc.ByteOrder.Uint32(lenBuf)), nil
	case 8:
		size, lenBuf := cc.ReadN(8)
		if size == 0 {
			return nil, 0, ErrUnexpectedEOF
		}
		return lenBuf, cc.ByteOrder.Uint64(lenBuf), nil
	default:
		return nil, 0, ErrUnsupportedLength
	}
}

func (cc *LengthFieldCodec) ReadN(n int) (size int, data []byte) {
	buf := make([]byte, n)
	readLen, err := cc.conn.Read(buf)
	if err != nil {
		return 0, nil
	}
	return readLen, buf[:readLen]
}

func readUint24(byteOrder binary.ByteOrder, b []byte) uint64 {
	_ = b[2]
	if byteOrder == binary.LittleEndian {
		return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16
	}
	return uint64(b[2]) | uint64(b[1])<<8 | uint64(b[0])<<16
}

func writeUint24(byteOrder binary.ByteOrder, v int) []byte {
	b := make([]byte, 3)
	if byteOrder == binary.LittleEndian {
		b[0] = byte(v)
		b[1] = byte(v >> 8)
		b[2] = byte(v >> 16)
	} else {
		b[2] = byte(v)
		b[1] = byte(v >> 8)
		b[0] = byte(v >> 16)
	}
	return b
}
