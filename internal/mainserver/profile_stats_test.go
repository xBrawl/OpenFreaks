package mainserver

import (
	"testing"

	"github.com/freaks/openfreaks/internal/protocol"
)

// Хвост статистики получает только клиент, поднявший FeatureProfileStats:
// старые сборки читают профиль строго и порвали бы соединение.
func TestProfileStatsOnlyForFlaggedClients(t *testing.T) {
	s, c, a := metaServer(t)
	meta := s.store.Meta.Player(a.PlayerID)
	meta.Career.Kills, meta.Career.Deaths, meta.Career.Battles, meta.Career.PlaySeconds = 7, 3, 4, 600

	read := func() *protocol.Reader {
		frames(c, 0)
		s.dispatch(c, protocol.CPlayerDataRequest, longPayload(a.PlayerID))
		list := frames(c, protocol.SPlayerDataResponse)
		if len(list) != 1 {
			t.Fatal("нет ответа профиля")
		}
		r := protocol.NewReader(list[0])
		r.ReadLong()
		r.ReadString()
		for i := 0; i < 5; i++ {
			r.ReadInt() // трофеи, победы соло/дуо/трио, статус дружбы
		}
		if has, _ := r.ReadBool(); has {
			t.Fatal("у тестового игрока нет шайки")
		}
		return r
	}

	if r := read(); !r.Ended() {
		t.Fatal("старому клиенту ушёл хвост")
	}

	c.profileStats.Store(true)
	r := read()
	var got [5]int32
	for i := range got {
		got[i], _ = r.ReadInt()
	}
	if !r.Ended() || got[0] != 7 || got[1] != 3 || got[2] < 4 || got[3] != 600 || got[4] < 1 {
		t.Fatalf("статистика %v, ended=%v", got, r.Ended())
	}
}
