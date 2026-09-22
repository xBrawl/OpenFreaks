package sim

import "testing"

func karityFixture(t *testing.T) (*World, *Player, *Player) {
	t.Helper()
	cat := mustCatalog(t)
	boss := cat.CharByName["karity_boss_character"]
	if boss == nil {
		t.Fatal("в каталоге нет karity_boss_character")
	}
	w, err := BuildWorld(cat, BuildParams{
		LocationID: cat.LocByName["experimental_test_arena"].Id,
		Players: []BuildPlayer{
			{PlayerID: 1, Index: 0, Team: 0, CharacterID: cat.Heroes()[0].Id, DashID: -1, UltUpgradeID: -1, StarPowerID: -1},
			{PlayerID: -1009, Index: 1, Team: 1, IsBot: true, CharacterID: boss.Id, DashID: -1, UltUpgradeID: -1, StarPowerID: -1},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	hero, villain := w.Players[0], w.Players[1]

	centreX, centreY := w.Tiles.PixW/2, w.Tiles.PixH/2
	hero.X, hero.Y, hero.ExactX, hero.ExactY = centreX, centreY, centreX, centreY
	villain.X, villain.Y, villain.ExactX, villain.ExactY = centreX, centreY+1500, centreX, centreY+1500
	return w, hero, villain
}

func TestKarityUsesAllThreeAbilities(t *testing.T) {
	w, hero, villain := karityFixture(t)

	spat, silenced := false, false
	for tick := 0; tick < 1200 && !(spat && silenced); tick++ {

		hero.HP = hero.MaxHP
		hero.X, hero.Y, hero.ExactX, hero.ExactY = w.Tiles.PixW/2, w.Tiles.PixH/2, w.Tiles.PixW/2, w.Tiles.PixH/2
		w.Advance()
		for _, m := range w.Minions {
			if m.Definition.String("name") == "karity_minion_character" {
				spat = true
			}
		}
		if hero.ControlRemoved {
			silenced = true
		}
	}

	if !spat {
		t.Error("босс ни разу не выплюнул миньона")
	}
	if !silenced {
		t.Error("прыжок с запретом ни разу не снял управление с героя")
	}
	if villain.HP <= 0 {
		t.Error("босс умер сам по себе")
	}
}

func TestKarityIsNotPlayable(t *testing.T) {
	cat := mustCatalog(t)
	for _, hero := range cat.Heroes() {
		if hero.Name == "karity_boss_character" || hero.Name == "karity_minion_character" {
			t.Fatalf("%s попал в список героев", hero.Name)
		}
	}
}

func TestKarityCarriesEveryAbility(t *testing.T) {
	_, _, villain := karityFixture(t)
	want := map[string]bool{"karity_spit": false, "karity_jump": false, "karity_silence": false}
	for _, skill := range villain.Skills {
		if _, ok := want[skill.Name]; ok {
			want[skill.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("у босса нет навыка %s", name)
		}
	}
}

func TestKarityMinionChasesPlayers(t *testing.T) {
	w, hero, villain := karityFixture(t)
	hero.X, hero.Y, hero.ExactX, hero.ExactY = w.Tiles.PixW/2, w.Tiles.PixH/2,
		w.Tiles.PixW/2, w.Tiles.PixH/2
	villain.X, villain.Y = hero.X, hero.Y+1200
	minion := SpawnMinion(w, villain, "karity_minion_character", villain.X, villain.Y)
	if minion == nil {
		t.Fatal("миньон Karity не создался")
	}
	startX, startY := minion.X, minion.Y
	for i := 0; i < 80; i++ {
		hero.HP = hero.MaxHP
		w.Advance()
	}
	if Dist(startX, startY, minion.X, minion.Y) < 100 {
		t.Fatalf("миньон не пошёл к игроку: start=(%.0f,%.0f) now=(%.0f,%.0f)",
			startX, startY, minion.X, minion.Y)
	}
	if Dist(minion.X, minion.Y, hero.X, hero.Y) >= Dist(startX, startY, hero.X, hero.Y) {
		t.Fatal("миньон двигается не к вражескому игроку")
	}
}

func TestKarityHasAtMostThreeLivingMinions(t *testing.T) {
	w, _, villain := karityFixture(t)
	for i := 0; i < 8; i++ {
		SpawnMinion(w, villain, "karity_minion_character", villain.X, villain.Y)
	}
	living := 0
	for _, minion := range w.Minions {
		if minion.Alive && minion.Owner == villain &&
			minion.Definition.String("name") == "karity_minion_character" {
			living++
		}
	}
	if living != 3 {
		t.Fatalf("живых миньонов Карити %d, ожидалось 3", living)
	}
}

func TestKarityMinionExpiresByHealthDrain(t *testing.T) {
	w, hero, villain := karityFixture(t)
	minion := SpawnMinion(w, villain, "karity_minion_character", villain.X, villain.Y)
	if minion == nil {
		t.Fatal("миньон не создался")
	}
	for i := 0; i < 600 && minion.Alive; i++ {
		hero.HP = hero.MaxHP
		w.Advance()
	}
	if minion.Alive {
		t.Fatalf("миньон не самоуничтожился: hp=%d", minion.HP)
	}
}
