package mainserver

import (
	"fmt"
	"github.com/freaks/openfreaks/internal/battleserver"
	"github.com/freaks/openfreaks/internal/sim"
	"github.com/freaks/openfreaks/internal/store"
	"testing"
	"time"
)

type arenaAllocator struct {
	rooms map[string]*battleserver.AllocateRequest
}

func (a *arenaAllocator) Allocate(r *battleserver.AllocateRequest) (map[int]int64, string, int, string) {
	a.rooms[r.BattleID] = r
	sessions := map[int]int64{}
	for _, p := range r.Participants {
		sessions[p.Index] = int64(p.Index + 100)
	}
	return sessions, "127.0.0.1", 30610, ""
}
func arenaFixture(t *testing.T) (*Server, *Conn, *store.Account, *store.ArenaTournament) {
	s, c, account := metaServer(t)
	s.mm = testMatchmaker(s)
	s.battle = &arenaAllocator{rooms: map[string]*battleserver.AllocateRequest{}}
	loc := s.catData.LocByID[int(testLocation(s, "duel_duo_showdown_lobby"))]
	mode := s.catData.ModeByName[loc.Mode]
	a := &store.Allocation{LocationID: int32(loc.Id), ModeID: int32(mode.Id), StartedAt: time.Now().UnixMilli()}
	for i := 0; i < mode.MaxPlayers; i++ {
		pid := int64(-1000 - i)
		bot := true
		if i == 0 {
			pid = account.PlayerID
			bot = false
		}
		a.Players = append(a.Players, store.BattleParticipant{PlayerID: pid, IsBot: bot, Index: i, Team: i % mode.TeamCount, CharacterModelID: account.SelectedChar, HPLevel: 1, DamageLevel: 1, SpeedLevel: 1, StarPowerID: -1, DashID: -1, UltID: -1})
	}
	s.store.Meta.Allocations["first-room"] = a
	s.beginArena("first-room", a)
	c.BattleID = "first-room"
	return s, c, account, s.store.Meta.Arenas[a.ArenaID]
}
func TestArenaChoiceIsOfferedOnceAndRerollsAreBounded(t *testing.T) {
	s, c, a, tournament := arenaFixture(t)
	p := tournament.Players[a.PlayerID]
	original := p.Choices[0]
	s.onAugmentSelect(c, intPayload(123456))
	if p.Selected || len(p.Augments) != 0 {
		t.Fatal("forged augment accepted")
	}
	s.onAugmentReroll(c, intPayload(0))
	replacement := p.Choices[0]
	if replacement == original || p.Rerolls[0] != 0 {
		t.Fatal("reroll did not replace choice")
	}
	s.onAugmentReroll(c, intPayload(0))
	if p.Choices[0] != replacement {
		t.Fatal("unlimited rerolls")
	}
	s.onAugmentSelect(c, intPayload(replacement))
	s.onAugmentSelect(c, intPayload(p.Choices[1]))
	if len(p.Augments) != 1 || !p.Selected {
		t.Fatal("multiple augments per round")
	}
}
func TestArenaThreeLivesTransitionsRestoresAndRewardsOnce(t *testing.T) {
	s, c, a, tournament := arenaFixture(t)
	id := tournament.ID
	roomCount := 0
	restored := false
	var last *battleserver.Result
	for phase := 0; phase < 100 && s.store.Meta.Arenas[id] != nil; phase++ {
		tournament = s.store.Meta.Arenas[id]
		rooms := []string{}
		for room := range tournament.Battles {
			rooms = append(rooms, room)
		}
		if len(rooms) == 0 {
			t.Fatal("tournament stuck with no rooms")
		}
		for _, room := range rooms {
			allocation := s.store.Meta.Allocations[room]
			winner := 0
			for _, slot := range allocation.Players {
				if slot.PlayerID == a.PlayerID {
					winner = slot.Team
				}
			}
			r := &battleserver.Result{BattleID: room, ModeID: allocation.ModeID, FinalTick: 220, WinnerTeam: winner}
			for _, slot := range allocation.Players {
				p := &sim.Player{PlayerID: slot.PlayerID, IsBot: slot.IsBot, Index: slot.Index, Team: slot.Team, ModelID: slot.CharacterModelID, Alive: slot.Team == winner, HeroType: "hero", HP: 100, DeathTick: 200}
				if !p.IsBot {
					p.DamageDealt = 1000
					p.Kills = 1
				}
				r.Players = append(r.Players, p)
			}
			if !s.mm.OnBattleResult(r) {
				t.Fatalf("room result rejected %s", room)
			}
			last = r
			roomCount++
		}
		if !restored && phase == 2 {
			s.store.RestoreState(s.store.DumpState())
			restored = true
		}
	}
	if s.store.Meta.Arenas[id] != nil || roomCount < 10 {
		t.Fatal("tournament did not finish after lives ran out")
	}
	meta := s.store.Meta.Player(a.PlayerID)
	if meta.Mastery[int(a.SelectedChar)].Matches != 1 {
		t.Fatal("practice/rounds awarded duplicate mastery")
	}
	current, _ := s.store.GetAccount(a.PlayerID)
	trophies := current.Trophies
	if trophies != 8 || c.BattleID != "" {
		t.Fatalf("final award or session incorrect: %d %s", trophies, c.BattleID)
	}
	if !s.mm.OnBattleResult(last) || current.Trophies != trophies {
		t.Fatal("repeated room result awarded again")
	}
	for _, req := range s.battle.(*arenaAllocator).rooms {
		if req.ArenaRound {
			if len(req.Participants) > 4 {
				t.Fatal("tournament teams mixed in one duel")
			}
			for _, p := range req.Participants {
				if len(p.AugmentIDs) == 0 {
					t.Fatal("chosen augment missing in battle")
				}
			}
		}
	}
}

type lifecycleAllocator struct {
	*arenaAllocator
	failLeave bool
	leaves    int
	missing   bool
}

func (a *lifecycleAllocator) Resume(string, int64) (*battleserver.BattleGrant, error) {
	return nil, fmt.Errorf("room unavailable")
}
func (a *lifecycleAllocator) Leave(string, int64) bool { a.leaves++; return !a.failLeave }
func (a *lifecycleAllocator) RoomStatus(ids []string) (map[string]bool, error) {
	result := map[string]bool{}
	for _, id := range ids {
		result[id] = !a.missing
	}
	return result, nil
}
func TestFailedLeaveRetriesAndEliminatesAbandonedArenaTeam(t *testing.T) {
	s, c, account, tournament := arenaFixture(t)
	backend := &lifecycleAllocator{arenaAllocator: s.battle.(*arenaAllocator), failLeave: true}
	s.battle = backend
	room := c.BattleID
	s.leaveBattle(c)
	member := tournament.Players[account.PlayerID]
	if !member.Slot.Abandoned || tournament.Teams[member.Slot.Team].Lives != 0 {
		t.Fatal("abandoned human team kept alive")
	}
	if c.BattleID != "" || backend.leaves != 1 {
		t.Fatal("leave was not dispatched")
	}
	backend.failLeave = false
	s.retryBattleLeaves()
	if backend.leaves != 2 {
		t.Fatal("failed leave did not retry")
	}
	for _, p := range s.store.Meta.Allocations[room].Players {
		if p.PlayerID == account.PlayerID && (!p.Abandoned || !p.LeaveDelivered) {
			t.Fatal("leave state not retained")
		}
	}
	s.leaveCheckedAt = time.Time{}
	s.retryBattleLeaves()
	if backend.leaves != 2 {
		t.Fatal("acknowledged leave retried")
	}
}
func TestArenaMissingRoomReplacedWithoutLifeLoss(t *testing.T) {
	s, c, _, tournament := arenaFixture(t)
	backend := &lifecycleAllocator{arenaAllocator: s.battle.(*arenaAllocator), missing: true}
	s.battle = backend
	room := c.BattleID
	s.recoverArenaRooms()
	if s.store.Meta.Allocations[room] == nil {
		t.Fatal("missing room replaced before grace")
	}
	s.arenaCheckedAt = time.Time{}
	s.arenaMissing[room] = time.Now().Add(-31 * time.Second)
	s.recoverArenaRooms()
	if s.store.Meta.Allocations[room] != nil || len(tournament.PendingTeams) != 1 {
		t.Fatal("missing round not scheduled again")
	}
	for _, team := range tournament.Teams {
		if team.Lives != 3 {
			t.Fatal("recovery consumed a life")
		}
	}
	if s.store.Meta.Receipts["battle:"+room] == 0 {
		t.Fatal("late old result not fenced")
	}
	s.allocateArenaPending(tournament)
	if len(tournament.Battles) != 1 || len(tournament.PendingTeams) != 0 {
		t.Fatal("replacement not allocated")
	}
}
