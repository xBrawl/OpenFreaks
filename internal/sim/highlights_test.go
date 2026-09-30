package sim

import (
	"testing"

	"github.com/freaks/openfreaks/internal/protocol"
)

func findHighlight(w *World, kind int) *Highlight {
	for _, h := range w.Highlights() {
		if h.Kind == kind {
			h := h
			return &h
		}
	}
	return nil
}

func TestHighlightsTripleKillIsOneMoment(t *testing.T) {
	w := modeWorld(t, "knockout_angles", 6, halves, 3)
	for _, p := range w.Players {
		p.IsBot = false
	}
	advanceTicks(w, 20+knockoutIntroPause+1)
	hero := w.Players[0]
	for _, p := range w.Players {
		if p.Team == 1 {
			p.SpawnShield = false
			Kill(w, hero, p)
			w.Advance()
		}
	}
	advanceTicks(w, 2)
	all := w.Highlights()
	if len(all) != 1 {
		t.Fatalf("one episode must give one moment, got %+v", all)
	}
	h := all[0]
	if h.Kind != HighlightAce || h.Player != hero.Index || h.Count != 3 {
		t.Fatalf("expected ace by %d, got %+v", hero.Index, h)
	}
}

func TestHighlightsMultiKillAndSpacing(t *testing.T) {
	w := modeWorld(t, "knockout_angles", 6, halves, 4)
	for _, p := range w.Players {
		p.IsBot = false
	}
	advanceTicks(w, 20+knockoutIntroPause+1)
	hero := w.Players[0]
	enemies := []*Player{}
	for _, p := range w.Players {
		if p.Team == 1 {
			p.SpawnShield = false
			enemies = append(enemies, p)
		}
	}
	Kill(w, hero, enemies[0])
	w.Advance()
	Kill(w, hero, enemies[1])
	h := findHighlight(w, HighlightMultiKill)
	if h == nil || h.Count != 2 || h.Player != hero.Index {
		t.Fatalf("double kill missing: %+v", w.Highlights())
	}
	// Через 10 секунд — уже новая серия, отдельное убийство.
	advanceTicks(w, 200)
	if w.Finished {
		t.Skip("round ended by time")
	}
	w.noteKill(hero, enemies[2])
	own := 0
	for _, h := range w.hl().list {
		if h.Player == hero.Index {
			own++
		}
	}
	if own != 2 {
		t.Fatalf("expected two moments of the hero, got %+v", w.hl().list)
	}
}

func TestNightmareVictimLearnsDeathCauseAndRoles(t *testing.T) {
	w, killer, victim := nightmarePair(t)
	s := w.nm()
	putAt(killer, s.boxX-1500, s.boxY+1500)
	putAt(victim, s.boxX-1300, s.boxY+1500)
	TryCast(w, killer, killer.Skills[0], protocol.PlayerAction{X: 200}, false)
	advanceTicks(w, nmKnifeWindup+2)
	if victim.Alive {
		t.Fatal("knife did not kill")
	}
	st := s.of(victim)
	if st.deathCause != nmDeathKnife || !st.knowsKiller {
		t.Fatalf("victim must know: cause=%d knows=%v", st.deathCause, st.knowsKiller)
	}
	if h := findHighlight(w, HighlightNightmareKill); h == nil || h.Player != killer.Index {
		t.Fatalf("killer strike is a moment: %+v", w.Highlights())
	}
	nightmareFinish(w, nightmareKillerSide, "test")
	if w.PlayerRoles[killer.Index] != RoleKiller || w.PlayerRoles[victim.Index] == RoleKiller {
		t.Fatalf("roles: %v", w.PlayerRoles)
	}
	snapshotAll(t, w)
}

func TestInfectionRolesAndPatientZero(t *testing.T) {
	w := modeWorld(t, "infection_lake", 6, zero, 5)
	advanceTicks(w, 20+infQuarantineTicks+2)
	s := w.inf()
	if s.alpha < 0 {
		t.Fatal("no patient zero")
	}
	if h := findHighlight(w, HighlightPatientZero); h == nil || h.Player != s.alpha {
		t.Fatalf("patient zero moment: %+v", w.Highlights())
	}
	infectionFinish(w, 0, "test")
	if w.PlayerRoles[s.alpha] != RoleAlpha {
		t.Fatalf("roles: %v", w.PlayerRoles)
	}
	for _, p := range w.Players {
		if p.Index != s.alpha && w.PlayerRoles[p.Index] != RoleSurvivor {
			t.Fatalf("roles: %v", w.PlayerRoles)
		}
	}
}
