package mainserver

import (
	"encoding/binary"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/freaks/openfreaks/internal/config"
	"github.com/freaks/openfreaks/internal/protocol"
)

func TestRawMainRejectsOversizedHeaderBeforeReadingBody(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	cfg := config.Default().Main
	s := &Server{cfg: cfg, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	conn := &Conn{Conn: server, Server: s}
	done := make(chan struct{})
	go func() { conn.readLoop(); close(done) }()
	header := make([]byte, protocol.TCPHeaderSize)
	binary.BigEndian.PutUint16(header, uint16(protocol.CClientHello))
	binary.BigEndian.PutUint32(header[2:], uint32(32<<20))
	if _, err := client.Write(header); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("raw TCP allocated an unauthenticated 32 MiB body and waited for it")
	}
}
