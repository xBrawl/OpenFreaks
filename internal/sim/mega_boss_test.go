package sim

import "testing"

func megaBossWorld(t *testing.T, humans int) (*World, *Player) {
	t.Helper()
	cat := mustCatalog(t)
	loc := cat.LocByName["mega_boss_karity"]
	boss := cat.CharByName["karity_boss_character"]
	if loc == nil || boss == nil {
		t.Fatal("mega boss content missing")
	}
	players := make([]BuildPlayer, 0, humans+1)
	for i := 0; i < humans; i++ {
		players = append(players, BuildPlayer{PlayerID: int64(i + 1), Index: i, Team: 0, CharacterID: cat.Heroes()[i%len(cat.Heroes())].Id})
	}
	players = append(players, BuildPlayer{PlayerID: -1000, Index: humans, Team: 1, IsBot: true, CharacterID: boss.Id})
	w, err := BuildWorld(cat, BuildParams{LocationID: loc.Id, Players: players})
	if err != nil {
		t.Fatal(err)
	}
	return w, w.Players[humans]
}

func TestMegaBossThreeMinuteLimitAndHealthScaling(t *testing.T) {
	w, boss := megaBossWorld(t, 3)
	if w.DurationTicks != 3600 || boss.MaxHP != 145000 || w.Endless {
		t.Fatalf("wrong three-player tuning: ticks=%d hp=%d endless=%v", w.DurationTicks, boss.MaxHP, w.Endless)
	}
	_, sixBoss := megaBossWorld(t, 6)
	if sixBoss.MaxHP != 290000 {
		t.Fatalf("six-player boss hp=%d", sixBoss.MaxHP)
	}
	w.TickNum = int32(w.TotalTicks() - 1)
	w.Advance()
	if !w.Finished || w.WinnerTeam != 1 || w.WinReason != "boss_survived" {
		t.Fatalf("wrong timeout: finished=%v winner=%d reason=%s", w.Finished, w.WinnerTeam, w.WinReason)
	}
}

func TestMegaBossDeathWinsAndDoesNotRespawn(t *testing.T) {
	w, boss := megaBossWorld(t, 3)
	boss.SpawnShield = false
	for boss.Alive {
		DealDamage(w, w.Players[0], boss, boss.HP)
	}
	if !w.Finished || w.WinnerTeam != 0 || w.WinReason != "boss_killed" || boss.Alive {
		t.Fatalf("wrong boss death: finished=%v winner=%d reason=%s alive=%v", w.Finished, w.WinnerTeam, w.WinReason, boss.Alive)
	}
	w.Advance()
	if boss.Alive {
		t.Fatal("boss respawned after victory")
	}
}

func TestMegaBossActiveThreePlayerCalibration(t *testing.T) {
	w, boss := megaBossWorld(t, 3)
	for _, p := range w.Players[:3] {
		p.BotControlled = true
	}
	for i := 0; i < w.TotalTicks() && !w.Finished; i++ {
		w.Advance()
	}
	if w.WinnerTeam != 0 || w.TickNum < 1400 || w.TickNum > 2400 {
		t.Fatalf("three active heroes took %.1fs, boss hp=%d/%d, reason=%s", float64(w.TickNum)/20, boss.HP, boss.MaxHP, w.WinReason)
	}
}
