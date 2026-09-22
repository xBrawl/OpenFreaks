package mainserver

import (
	"context"
	"github.com/coder/websocket"
	"github.com/freaks/openfreaks/internal/protocol"
	"github.com/freaks/openfreaks/internal/webtransport"
	"io"
	"net"
	"net/http"
	"time"
)

// Each WebSocket binary message carries exactly one existing TCP frame.
// The net.Pipe boundary preserves the normal authentication, persistence and
// account connection limits; no new login or command handler is introduced.
func (s *Server) WebSocketHandler(parent context.Context, origins []string) http.Handler {
	accept := &webtransport.Acceptor{Origins: origins}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, ctx, cleanup, ok := accept.Accept(parent, w, r, "openfreaks.main.v1")
		if !ok {
			return
		}
		defer cleanup()
		ws.SetReadLimit(64 << 10)
		client, server := net.Pipe()
		defer client.Close()
		defer server.Close()
		stop := context.AfterFunc(ctx, func() { client.Close(); server.Close() })
		defer stop()
		defer ws.CloseNow()
		go func() {
			defer client.Close()
			defer ws.CloseNow()
			for {
				kind, frame, err := ws.Read(ctx)
				if err != nil {
					return
				}
				if kind != websocket.MessageBinary || len(frame) < 6 {
					return
				}
				_, size, err := protocol.DecodeTCPHeader(frame[:6])
				if err != nil || int(size) != len(frame)-6 || size > 64<<10 {
					return
				}
				client.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if _, err = client.Write(frame); err != nil {
					return
				}
			}
		}()
		go func() {
			defer client.Close()
			defer ws.CloseNow()
			for {
				header := make([]byte, 6)
				if _, err := io.ReadFull(client, header); err != nil {
					return
				}
				_, size, err := protocol.DecodeTCPHeader(header)
				if err != nil || size > 32<<20 {
					return
				}
				frame := make([]byte, 6+size)
				copy(frame, header)
				if _, err = io.ReadFull(client, frame[6:]); err != nil {
					return
				}
				writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				err = ws.Write(writeCtx, websocket.MessageBinary, frame)
				cancel()
				if err != nil {
					return
				}
			}
		}()
		s.handleRaw(server)
	})
}
