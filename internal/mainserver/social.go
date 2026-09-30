package mainserver

// Аватарки, эмодзи в чате шайки и системные сообщения чата.
//
// Совместимость. Байт SServerHello, который раньше всегда был нулём, теперь
// несёт protocol.FeatureSocialV2. Клиент, увидевший флаг, дописывает свои
// возможности в конец Login — только таким соединениям сервер добавляет
// аватарку в записи друзей, шайки, чата, профиля и домашних данных
// (protocol.Social). Старая сборка байт приветствия читает не глядя и шлёт
// прежний Login: её формат не меняется ни в одном сообщении.

import (
	"regexp"
	"strconv"
	"time"

	"github.com/freaks/openfreaks/internal/protocol"
	"github.com/freaks/openfreaks/internal/store"
)

// Системные события чата — порядок как в клиентском AllianceMessageComponent.MessageEventType.
const (
	allianceEventCreated int32 = iota
	allianceEventJoin
	allianceEventAccept
	allianceEventLeave
	allianceEventKick
	allianceEventPromote
	allianceEventDemote
)

const allianceMessageKeep = 100

// Эмодзи в чате — токены «:eN:» прямо в тексте. Номер — индекс картинки в
// клиентском наборе ChatEmoji; всё, что вне набора, вырезается.
const chatEmojiCount = 12

// Не больше стольких эмодзи в одном сообщении: стена рожиц — тот же флуд.
const chatEmojiPerMessage = 12

var chatEmojiToken = regexp.MustCompile(`:e(\d{1,2}):`)

// normalizeChatText оставляет в тексте только известные эмодзи и не больше
// chatEmojiPerMessage. Возвращает текст и длину для лимита: эмодзи — один знак.
func normalizeChatText(text string) (string, int) {
	count := 0
	out := chatEmojiToken.ReplaceAllStringFunc(text, func(token string) string {
		n, err := strconv.Atoi(token[2 : len(token)-1])
		if err != nil || n < 0 || n >= chatEmojiCount || count >= chatEmojiPerMessage {
			return ""
		}
		count++
		return ":e" + strconv.Itoa(n) + ":"
	})
	plain := chatEmojiToken.ReplaceAllString(out, "")
	return out, len([]rune(plain)) + count
}

// legacyChatText — текст для клиента без эмодзи: рожица станет «*», а не кодом.
func legacyChatText(text string) string {
	return chatEmojiToken.ReplaceAllString(text, "*")
}

// avatarOf — модель героя на аватарке: выбранная игроком, пока он ею владеет,
// иначе текущий герой.
func avatarOf(a *store.Account) int32 {
	if a == nil {
		return 0
	}
	if a.AvatarID > 0 {
		if _, owned := a.Characters[a.AvatarID]; owned {
			return a.AvatarID
		}
	}
	return a.SelectedChar
}

// socialFor — хвост записи игрока pid для соединения c.
func (s *Server) socialFor(c *Conn, pid int64) protocol.Social {
	if c == nil || !c.social.Load() {
		return protocol.Social{}
	}
	a, _ := s.store.GetAccount(pid)
	social := protocol.Social{Enabled: true, Avatar: avatarOf(a), Staff: c.staff.Load()}
	if a != nil {
		social.Role = a.Role
	}
	return social
}

func (s *Server) onSetAvatar(c *Conn, payload []byte) bool {
	model, err := protocol.DecodeIntOnly(payload)
	if err != nil {
		return false
	}
	a, ok := s.store.GetAccount(c.PlayerID)
	if !ok {
		return false
	}
	_, owned := a.Characters[model]
	if model != 0 && !owned {
		return c.Send(protocol.SSetAvatarResponse, protocol.EncodeSetAvatar(false, avatarOf(a)))
	}
	a.AvatarID = model
	c.Send(protocol.SSetAvatarResponse, protocol.EncodeSetAvatar(true, avatarOf(a)))
	// Аватарка видна в шайке и у друзей: обновляем их открытые экраны.
	if al, ok := s.playerAlliance(c.PlayerID); ok {
		s.pushAlliance(al)
	}
	for _, id := range s.store.FriendList(c.PlayerID) {
		s.pushFriendUpdate(id, c.PlayerID)
	}
	return true
}

// allianceMessageFor кодирует сообщение чата под возможности соединения.
func (s *Server) allianceMessageFor(c *Conn, m store.AllianceMessage) *protocol.AllianceMessage {
	msg := &protocol.AllianceMessage{
		Type: m.Type, PlayerID: m.PlayerID, PlayerName: m.PlayerName, RoleModelID: max(1, m.RoleModelID),
		Message: m.Message, CreationTimestamp: unixSeconds(m.CreationTime),
		EventType: m.EventType, EventPlayerID: m.EventPlayerID, EventPlayerName: m.EventPlayerName,
		EventPlayerRoleModel: max(1, m.EventPlayerRole),
		Social:               s.socialFor(c, m.PlayerID),
	}
	if !msg.Social.Enabled {
		msg.Message = legacyChatText(msg.Message)
	}
	return msg
}

// postAllianceMessage сохраняет сообщение в истории шайки и рассылает его
// всем участникам в сети.
func (s *Server) postAllianceMessage(al *store.Alliance, message store.AllianceMessage) {
	al.Messages = append(al.Messages, message)
	if len(al.Messages) > allianceMessageKeep {
		al.Messages = al.Messages[len(al.Messages)-allianceMessageKeep:]
	}
	for pid := range al.Members {
		if conn, ok := s.ConnByPlayer(pid); ok {
			w := protocol.NewWriter()
			s.allianceMessageFor(conn, message).Encode(w)
			conn.Send(protocol.SAllianceMessage, w.Bytes())
		}
	}
}

// allianceEvent пишет в чат системное сообщение: кто (actor) что сделал и с кем (target).
func (s *Server) allianceEvent(al *store.Alliance, kind int32, actor, target int64) {
	message := store.AllianceMessage{Type: 1, PlayerID: actor, RoleModelID: al.Members[actor],
		EventType: kind, EventPlayerID: target, EventPlayerRole: al.Members[target], CreationTime: time.Now().Unix()}
	if a, ok := s.store.GetAccount(actor); ok {
		message.PlayerName = a.Name
	}
	if a, ok := s.store.GetAccount(target); ok {
		message.EventPlayerName = a.Name
	}
	s.postAllianceMessage(al, message)
}
