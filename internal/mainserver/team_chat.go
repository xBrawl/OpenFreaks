package mainserver

// Чат отряда — пока игроки собираются в команду перед боем. Тот же текст и те
// же эмодзи, что в чате шайки; история живёт вместе с командой и исчезает,
// когда команда распадается. Сообщения получают только клиенты с
// protocol.FeatureSocialV2 — у старых сборок для них нет ни экрана, ни декодера.

import (
	"time"

	"github.com/freaks/openfreaks/internal/protocol"
	"github.com/freaks/openfreaks/internal/store"
)

const (
	teamMessageKeep   = 30
	teamMessageLength = 200
)

func (s *Server) teamMessageProto(m store.TeamMessage, withRole bool) *protocol.TeamMessage {
	a, _ := s.store.GetAccount(m.PlayerID)
	msg := &protocol.TeamMessage{PlayerID: m.PlayerID, PlayerName: m.Name, Avatar: avatarOf(a), Text: m.Text, Timestamp: m.Time, WithRole: withRole}
	if a != nil {
		msg.Role = a.Role
	}
	return msg
}

func (s *Server) onTeamMessage(c *Conn, payload []byte) bool {
	text, _, err := protocol.DecodeStringOnly(payload)
	if err != nil {
		return false
	}
	text, length := normalizeChatText(cleanSocialText(text))
	t := s.store.TeamForPlayer(c.PlayerID)
	if t == nil || length < 1 || length > teamMessageLength {
		return true
	}
	a, _ := s.store.GetAccount(c.PlayerID)
	message := store.TeamMessage{PlayerID: c.PlayerID, Text: text, Time: time.Now().Unix()}
	if a != nil {
		message.Name = a.Name
	}
	t.Messages = append(t.Messages, message)
	if len(t.Messages) > teamMessageKeep {
		t.Messages = t.Messages[len(t.Messages)-teamMessageKeep:]
	}
	var payloads [2][]byte
	for i := range payloads {
		w := protocol.NewWriter()
		s.teamMessageProto(message, i == 1).Encode(w)
		payloads[i] = w.Bytes()
	}
	for pid := range t.Members {
		if conn, ok := s.ConnByPlayer(pid); ok && conn.social.Load() {
			variant := 0
			if conn.staff.Load() {
				variant = 1
			}
			conn.Send(protocol.STeamMessage, payloads[variant])
		}
	}
	return true
}

func (s *Server) onTeamMessages(c *Conn) bool {
	w := protocol.NewWriter()
	t := s.store.TeamForPlayer(c.PlayerID)
	if t == nil {
		w.WriteInt(0)
	} else {
		w.WriteInt(int32(len(t.Messages)))
		for _, m := range t.Messages {
			s.teamMessageProto(m, c.staff.Load()).Encode(w)
		}
	}
	return c.Send(protocol.STeamMessagesResponse, w.Bytes())
}
