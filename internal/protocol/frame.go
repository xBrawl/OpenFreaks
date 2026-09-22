package protocol

import (
	"encoding/binary"
	"errors"
)

var ErrFrameTooBig = errors.New("protocol: frame payload exceeds 32MiB")

func EncodeTCPFrame(id int16, payload []byte) []byte {
	pkt := make([]byte, TCPHeaderSize+len(payload))
	binary.BigEndian.PutUint16(pkt[0:2], uint16(id))
	binary.BigEndian.PutUint32(pkt[2:6], uint32(len(payload)))
	copy(pkt[6:], payload)
	return pkt
}

func DecodeTCPHeader(h []byte) (int16, int32, error) {
	if len(h) < TCPHeaderSize {
		return 0, 0, ErrEndOfStream
	}
	id := int16(binary.BigEndian.Uint16(h[0:2]))
	n := int32(binary.BigEndian.Uint32(h[2:6]))
	if n < 0 || n > MaxTCPFramePayload {
		return 0, 0, ErrFrameTooBig
	}
	return id, n, nil
}

func EncodeBattleServerFrame(id int16, tick int32, payload []byte) []byte {
	pkt := make([]byte, 10+len(payload))
	binary.BigEndian.PutUint16(pkt[0:2], uint16(id))
	binary.BigEndian.PutUint32(pkt[2:6], uint32(len(payload)))
	binary.BigEndian.PutUint32(pkt[6:10], uint32(tick))
	copy(pkt[10:], payload)
	return pkt
}

func EncodeBattleClientFrame(id int16, payload []byte) []byte {
	pkt := make([]byte, 6+len(payload))
	binary.BigEndian.PutUint16(pkt[0:2], uint16(id))
	binary.BigEndian.PutUint32(pkt[2:6], uint32(len(payload)))
	copy(pkt[6:], payload)
	return pkt
}

func TryReadBattleClientFrame(b []byte) (id int16, length int, ok bool) {
	if len(b) < 6 {
		return 0, 0, false
	}
	length = int(binary.BigEndian.Uint32(b[2:6]))
	if length < 0 || length > len(b)-6 {
		return 0, 0, false
	}
	id = int16(binary.BigEndian.Uint16(b[0:2]))
	return id, length, true
}
