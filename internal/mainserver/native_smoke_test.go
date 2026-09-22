package mainserver

import (
	"encoding/binary"
	"github.com/freaks/openfreaks/internal/protocol"
	kcp "github.com/xtaci/kcp-go/v5"
	"net"
	"strconv"
	"testing"
	"time"
)

func assertNativeBattleSnapshot(t *testing.T, host string, port int, session int64) {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(host, fmtPort(port)))
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := kcp.NewKCP(0xaabbccdd, func(data []byte, size int) {
		packet := make([]byte, 9+size)
		packet[0] = 1
		binary.BigEndian.PutUint64(packet[1:], uint64(session))
		copy(packet[9:], data[:size])
		_, _ = conn.Write(packet)
	})
	client.SetMtu(1191)
	client.WndSize(512, 512)
	client.NoDelay(1, 10, 2, 1)

	client.Send([]byte{1})
	buffer := make([]byte, 65535)
	deadline := time.Now().Add(45 * time.Second)
	authed := false
	isSnapshot := func(data []byte) bool {
		if len(data) < 11 || data[0] != 4 {
			return false
		}
		frame := data[1:]
		return binary.BigEndian.Uint16(frame[:2]) == 2 && int(binary.BigEndian.Uint32(frame[2:6])) == len(frame)-10
	}
	for time.Now().Before(deadline) {
		client.Update()
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
		n, err := conn.Read(buffer)
		if err == nil && n > 9 && int64(binary.BigEndian.Uint64(buffer[1:9])) == session {
			if buffer[0] == 1 {
				client.Input(buffer[9:n], kcp.IKCP_PACKET_REGULAR, true)
			} else if buffer[0] == 2 && isSnapshot(buffer[9:n]) {
				return
			}
		}
		for client.PeekSize() > 0 {
			message := make([]byte, client.PeekSize())
			count := client.Recv(message)
			if count < 1 {
				break
			}
			message = message[:count]
			if message[0] == 1 && !authed {
				authed = true
				body := protocol.EncodeBattleClientFrame(protocol.UdpPlayerActions, protocol.EncodePlayerActions([]protocol.PlayerAction{{Type: protocol.ActionWalk, X: 1000, Y: 1000}}))
				client.Send(append([]byte{4}, body...))
			}
			if message[0] == 2 {
				client.Send(append([]byte{3}, message[1:]...))
			}
			if isSnapshot(message) {
				return
			}
		}
	}
	t.Fatal("native KCP snapshot not received")
}
func fmtPort(port int) string {
	return strconv.Itoa(port)
}
