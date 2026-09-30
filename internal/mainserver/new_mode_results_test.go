package mainserver

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/freaks/openfreaks/internal/battleserver"
	"github.com/freaks/openfreaks/internal/protocol"
	"github.com/freaks/openfreaks/internal/sim"
	"github.com/freaks/openfreaks/internal/store"
)

// Завершение новых режимов сохраняет исходного героя даже после превращения
// в зомби и отправляет полный состав для 3D-экрана итогов.
func TestNewModesDeliverCharacterAndDurableBattleHistory(t *testing.T) {
	for _, name := range []string{"knockout", "infection", "nightmare", "karit_grab"} {
		t.Run(name, func(t *testing.T) {
			s, c, account := metaServer(t)
			mode := s.catData.ModeByName[name]
			if mode == nil {
				t.Fatal("missing mode")
			}
			locationID := int32(0)
			for _, loc := range s.catData.LocByName {
				if loc.Mode == name {
					locationID = int32(loc.Id)
					break
				}
			}
			if locationID == 0 {
				t.Fatal("missing location")
			}
			id := "result-regression-" + name
			model := account.SelectedChar
			slot := store.BattleParticipant{PlayerID: account.PlayerID, Index: 0, Team: 0, CharacterModelID: model, Name: account.Name, HPLevel: 1, DamageLevel: 1, SpeedLevel: 1}
			s.store.Meta.Allocations[id] = &store.Allocation{ModeID: int32(mode.Id), LocationID: locationID, StartedAt: time.Now().UnixMilli(), Players: []store.BattleParticipant{slot}}
			c.BattleID = id
			player := &sim.Player{PlayerID: account.PlayerID, Index: 0, Team: 0, ModelID: model, RosterTeam: 0, RosterModelID: model}
			if name == "infection" {
				player.Team = 1
				player.ModelID = int32(s.catData.CharByName["infection_zombie_character"].Id)
			}
			r := &battleserver.Result{BattleID: id, ModeID: int32(mode.Id), FinalTick: 100, WinnerTeam: 0, Players: []*sim.Player{player}, PlayerRanks: map[int]int{0: 1}, PlayerRoles: map[int]int{0: 4}}
			m := testMatchmaker(s)
			if !m.OnBattleResult(r) {
				t.Fatal("settlement rejected")
			}
			records := s.store.BattlesFor(account.PlayerID, 20)
			if len(records) != 1 || len(records[0].Players) != 1 || records[0].Players[0].CharacterModelID != model {
				t.Fatal("missing original character in history")
			}
			mastery := s.store.Meta.Player(account.PlayerID).Mastery[int(model)]
			if mastery == nil || mastery.Matches != 1 {
				t.Fatal("mastery not credited to original hero")
			}
			if player.ModelID != model && s.store.Meta.Player(account.PlayerID).Mastery[int(player.ModelID)] != nil {
				t.Fatal("hidden transformation model received mastery")
			}
			select {
			case packet := <-c.SendCh:
				if int16(binary.BigEndian.Uint16(packet[:2])) != protocol.SBattleEnded {
					t.Fatal("missing battle ended")
				}
				reader := protocol.NewReader(packet[protocol.TCPHeaderSize:])
				for i := 0; i < 3; i++ {
					if _, err := reader.ReadInt(); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := reader.ReadBool(); err != nil {
					t.Fatal(err)
				}
				n, err := reader.ReadInt()
				if err != nil || n != 1 {
					t.Fatal("empty results roster")
				}
				got, err := reader.ReadInt()
				if err != nil || got != model {
					t.Fatal("wrong results character")
				}
			default:
				t.Fatal("result not delivered")
			}
			if !m.OnBattleResult(r) || len(s.store.BattlesFor(account.PlayerID, 20)) != 1 {
				t.Fatal("duplicate result not idempotent")
			}
		})
	}
}
