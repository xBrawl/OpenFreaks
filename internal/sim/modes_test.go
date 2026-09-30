package sim

import (
	"testing"

	"github.com/freaks/openfreaks/internal/content"
	"github.com/freaks/openfreaks/internal/protocol"
)

func locByName(t *testing.T, cat *content.CatalogData, name string) *content.Location {
	t.Helper()
	for _, loc := range cat.Locations {
		if loc.Name == name {
			return loc
		}
	}
	t.Fatalf("no location %s", name)
	return nil
}

func modeWorld(t *testing.T, name string, players int, teams func(i int) int, seed int64) *World {
	t.Helper()
	cat := mustCatalog(t)
	loc := locByName(t, cat, name)
	heroes := cat.Heroes()
	var bp []BuildPlayer
	for i := 0; i < players; i++ {
		bp = append(bp, BuildPlayer{PlayerID: int64(-1000 - i), IsBot: true, Index: i, Team: teams(i), Name: "bot",
			CharacterID: heroes[i%len(heroes)].Id, HPLevel: 1, DamageLevel: 1, SpeedLevel: 1, DashID: -1, UltUpgradeID: -1, StarPowerID: -1})
	}
	w, err := BuildWorld(cat, BuildParams{BattleID: "t-" + name, LocationID: loc.Id, IntroTicks: 20, Seed: seed, Players: bp,
		// Кошмар скрыт из игры (28.09), но код режима держим живым в тестах.
		ArenaRound: loc.Hidden})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func solo(i int) int   { return i }
func zero(int) int     { return 0 }
func halves(i int) int { return i % 2 }

func advanceTicks(w *World, n int) {
	for i := 0; i < n && !w.Finished; i++ {
		w.Advance()
	}
}

func snapshotAll(t *testing.T, w *World) {
	t.Helper()
	for _, p := range w.Players {
		if len(EncodeSnapshot(w, p.Index, protocol.ParticipantPlayer)) == 0 {
			t.Fatalf("empty snapshot for %d", p.Index)
		}
	}
	if len(EncodeSnapshot(w, 0, protocol.ParticipantReplayWatcher)) == 0 {
		t.Fatal("empty replay snapshot")
	}
}

// ── Кошмар ─────────────────────────────────────────────────────────────

func TestNightmareEveryoneIsExplorerWithSecretRole(t *testing.T) {
	w := modeWorld(t, "nightmare_catacombs", 8, solo, 7)
	s := w.nm()
	explorer := w.Catalog.CharByName[nmExplorerName]
	killers, detectives := 0, 0
	for _, p := range w.Players {
		if p.ModelID != int32(explorer.Id) || !p.WireHero || p.HeroType != "hero" {
			t.Fatalf("player %d is not an explorer: model=%d", p.Index, p.ModelID)
		}
		if p.RosterModelID == p.ModelID || p.ResultModel() != p.RosterModelID {
			t.Fatalf("roster model lost: %d", p.RosterModelID)
		}
		if p.DashModelID >= 0 {
			t.Fatalf("explorer must not have a dash")
		}
		switch s.of(p).role {
		case nmKiller:
			killers++
		case nmDetective:
			detectives++
		}
		if Dist(p.X, p.Y, s.boxX, s.boxY) > 900 {
			t.Fatalf("player %d spawned outside the hall", p.Index)
		}
	}
	if killers != 1 || detectives != 1 {
		t.Fatalf("roles: killers=%d detectives=%d", killers, detectives)
	}
	if len(s.fuses) != 0 {
		t.Fatal("fuses must appear only after a minute")
	}
	for _, p := range w.Players {
		p.IsBot = false
	}
	snapshotAll(t, w)
	advanceTicks(w, 20+nmFusesAfter+1)
	if len(s.vents) != 6 || len(s.fuses) != 4 || len(s.panels) != 4 || len(s.gateTiles) != 12 || s.box == nil {
		t.Fatalf("props: vents=%d fuses=%d panels=%d gates=%d", len(s.vents), len(s.fuses), len(s.panels), len(s.gateTiles))
	}
	snapshotAll(t, w)
}

func nightmarePair(t *testing.T) (*World, *Player, *Player) {
	w := modeWorld(t, "nightmare_catacombs", 8, solo, 11)
	advanceTicks(w, 25)
	s := w.nm()
	killer := s.killerPlayer(w)
	var victim *Player
	for _, p := range w.Players {
		if p != killer && s.of(p).role == nmInnocent {
			victim = p
			break
		}
	}
	for _, p := range w.Players {
		p.IsBot = false
		p.SpawnShield = false
		p.UltActive = false
		s.of(p).lightWanted = false
		s.of(p).knowsKiller = false
	}
	return w, killer, victim
}

func putAt(p *Player, x, y float64) {
	p.X, p.Y, p.ExactX, p.ExactY = x, y, x, y
	p.HasMoveTarget = false
}

func TestNightmareKnifeKillsAfterWindupWithoutKillFeed(t *testing.T) {
	w, killer, victim := nightmarePair(t)
	s := w.nm()
	// Отводим остальных в угол зала, чтобы не было свидетелей рядом.
	putAt(killer, s.boxX-1500, s.boxY+1500)
	putAt(victim, s.boxX-1300, s.boxY+1500)
	if !TryCast(w, killer, killer.Skills[0], protocol.PlayerAction{X: 200}, false) {
		t.Fatal("knife not drawn")
	}
	advanceTicks(w, nmKnifeWindup-2)
	if !victim.Alive {
		t.Fatal("victim died before the windup finished")
	}
	advanceTicks(w, 4)
	if victim.Alive {
		t.Fatal("knife did not kill")
	}
	if len(killer.PendingKills) != 0 || killer.Kills != 0 {
		t.Fatal("nightmare must not announce the killer")
	}
	body := false
	for _, a := range w.Areas {
		if a.Definition.String("name") == "nightmare_body" {
			body = true
		}
	}
	if !body {
		t.Fatal("no body left behind")
	}
	if TryCast(w, killer, killer.Skills[0], protocol.PlayerAction{X: 200}, false) {
		t.Fatal("knife must be on cooldown")
	}
}

func TestNightmareDetectiveShootsInnocentAndDiesDroppingRevolver(t *testing.T) {
	w, killer, victim := nightmarePair(t)
	s := w.nm()
	var detective *Player
	for _, p := range w.Players {
		if s.of(p).role == nmDetective {
			detective = p
		}
	}
	for _, p := range w.Players {
		putAt(p, s.vents[0][0], s.vents[0][1])
	}
	putAt(killer, s.boxX+2400, s.boxY)
	putAt(detective, s.boxX-600, s.boxY)
	putAt(victim, s.boxX+300, s.boxY)
	if !TryCast(w, detective, detective.Skills[0], protocol.PlayerAction{X: 900, Y: 0}, false) {
		t.Fatal("detective did not shoot")
	}
	advanceTicks(w, 20)
	if victim.Alive || detective.Alive {
		t.Fatalf("innocent shot: victim alive=%v detective alive=%v", victim.Alive, detective.Alive)
	}
	if s.revolver == nil || s.detective != -1 {
		t.Fatal("revolver not dropped")
	}
	// Любой выживший подбирает револьвер и становится детективом.
	var finder *Player
	for _, p := range w.Players {
		if p.Alive && p != killer {
			finder = p
			break
		}
	}
	putAt(finder, s.revolver.X+50, s.revolver.Y)
	advanceTicks(w, 2)
	if s.detective != finder.Index || !s.hasBullet {
		t.Fatalf("revolver not picked up: detective=%d bullet=%v", s.detective, s.hasBullet)
	}
	if w.Finished {
		t.Fatal("match must go on")
	}
}

func TestNightmareShootingKillerWinsForSurvivors(t *testing.T) {
	w, killer, _ := nightmarePair(t)
	s := w.nm()
	var detective *Player
	for _, p := range w.Players {
		if s.of(p).role == nmDetective {
			detective = p
		}
	}
	putAt(killer, s.boxX+600, s.boxY)
	putAt(detective, s.boxX-600, s.boxY)
	for _, p := range w.Players {
		if p != killer && p != detective {
			putAt(p, s.boxX, s.boxY+1200)
		}
	}
	TryCast(w, detective, detective.Skills[0], protocol.PlayerAction{X: 900}, false)
	advanceTicks(w, 20)
	if !w.Finished || w.WinReason != "killer_down" {
		t.Fatalf("finished=%v reason=%s", w.Finished, w.WinReason)
	}
	if w.PlayerRanks[killer.Index] != 2 || w.PlayerRanks[detective.Index] != 1 {
		t.Fatalf("ranks: %v", w.PlayerRanks)
	}
}

func TestNightmareFusesTurnOnLightsAndSurvivorsWin(t *testing.T) {
	w, killer, runner := nightmarePair(t)
	s := w.nm()
	s.fusesAt = w.TickNum + 1
	advanceTicks(w, 2)
	putAt(killer, s.vents[0][0], s.vents[0][1])
	for _, f := range append([]*Area(nil), s.fuses...) {
		putAt(runner, f.X, f.Y)
		advanceTicks(w, 2)
		if s.of(runner).carried != 1 {
			t.Fatalf("carried %d fuses", s.of(runner).carried)
		}
		putAt(runner, s.boxX, s.boxY)
		advanceTicks(w, nmInsertTicks/2)
		if s.of(runner).carried != 1 {
			t.Fatal("inserting takes two seconds")
		}
		advanceTicks(w, nmInsertTicks/2+2)
	}
	if !s.lightsOn() {
		t.Fatal("lights did not come on")
	}
	if TryCast(w, killer, killer.Skills[0], protocol.PlayerAction{X: 100}, false) {
		t.Fatal("blinded killer must not attack")
	}
	if w.nightmareVisibility(runner, killer) != 15 {
		t.Fatal("in emergency light everybody sees everybody")
	}
	advanceTicks(w, nmLightsWinTicks+2)
	if !w.Finished || w.WinReason != "lights" || w.PlayerRanks[killer.Index] != 2 {
		t.Fatalf("finished=%v reason=%s ranks=%v", w.Finished, w.WinReason, w.PlayerRanks)
	}
}

func TestNightmareDarknessAndFlashlight(t *testing.T) {
	w, killer, a := nightmarePair(t)
	s := w.nm()
	var b *Player
	for _, p := range w.Players {
		if p != killer && p != a {
			b = p
			break
		}
	}
	// Все трое в длинном коридоре внутреннего кольца.
	y := 7*300.0 + 150
	putAt(a, 8*300+150, y)
	putAt(b, 13*300+150, y)
	putAt(killer, 20*300+150, y)
	for _, p := range w.Players {
		if p != a && p != b && p != killer {
			putAt(p, s.boxX, s.boxY)
		}
	}
	a.AttackAngle = 0
	if w.nightmareVisibility(a, b) != 0 {
		t.Fatal("in the dark nobody is seen at 5 tiles")
	}
	nightmareFlashlight(w, a, true)
	advanceTicks(w, 1)
	a.AttackAngle = 0
	if !a.UltActive || w.nightmareVisibility(a, b) != 15 {
		t.Fatalf("flashlight must reveal b: light=%v", a.UltActive)
	}
	if v := w.nightmareVisibility(killer, a); v != 1 {
		t.Fatalf("killer must see the beam from afar, got %d", v)
	}
	a.AttackAngle = 180
	if w.nightmareVisibility(a, b) != 0 {
		t.Fatal("behind the beam is darkness")
	}
	// Ночное зрение убийцы: 4 клетки — видно.
	putAt(killer, b.X-1200, y)
	if w.nightmareVisibility(killer, b) != 15 {
		t.Fatal("killer night vision")
	}
	// Батарея садится за 15 секунд.
	a.AttackAngle = 0
	advanceTicks(w, nmBatteryMs/TickMs+5)
	if a.UltActive || !s.of(a).lightLocked {
		t.Fatal("battery must run out after 15 s")
	}
	nightmareFlashlight(w, a, true)
	advanceTicks(w, 1)
	if a.UltActive {
		t.Fatal("empty flashlight must stay off")
	}
}

func TestNightmareVentAndGatesSabotage(t *testing.T) {
	w, killer, _ := nightmarePair(t)
	s := w.nm()
	putAt(killer, s.vents[0][0], s.vents[0][1])
	if !TryCast(w, killer, killer.Skills[1], protocol.PlayerAction{}, true) {
		t.Fatal("vent not used")
	}
	if Dist(killer.X, killer.Y, s.vents[0][0], s.vents[0][1]) < 1000 {
		t.Fatal("killer did not travel")
	}
	var gate *nmPanel
	for _, p := range s.panels {
		if p.gate {
			gate = p
		}
	}
	putAt(killer, gate.x, gate.y)
	if !TryCast(w, killer, killer.Skills[1], protocol.PlayerAction{}, true) {
		t.Fatal("gate sabotage failed")
	}
	if len(w.activeGateTiles()) != 12 || !w.Tiles.Blocked[s.gateTiles[0]] {
		t.Fatal("gates not closed")
	}
	snapshotAll(t, w)
	advanceTicks(w, nmGateTicks+2)
	if len(w.activeGateTiles()) != 0 || w.Tiles.Blocked[s.gateTiles[0]] {
		t.Fatal("gates did not reopen")
	}
}

func TestNightmareBotsFinishTheMatch(t *testing.T) {
	for seed := int64(1); seed <= 4; seed++ {
		w := modeWorld(t, "nightmare_catacombs", 8, solo, seed)
		for i := 0; i < 4000 && !w.Finished; i++ {
			w.Advance()
			if i%97 == 0 {
				snapshotAll(t, w)
			}
		}
		if !w.Finished {
			t.Fatalf("seed %d: match did not end", seed)
		}
		if len(w.PlayerRanks) != 8 {
			t.Fatalf("seed %d: ranks for %d players", seed, len(w.PlayerRanks))
		}
		dead := 0
		for _, p := range w.Players {
			if !p.Alive {
				dead++
			}
		}
		t.Logf("seed %d: %s at tick %d, dead %d", seed, w.WinReason, w.TickNum, dead)
	}
}

// ── Заражение ──────────────────────────────────────────────────────────

func TestInfectionQuarantineAlphaAndConversion(t *testing.T) {
	w := modeWorld(t, "infection_lake", 10, zero, 5)
	s := w.inf()
	for _, p := range w.Players {
		p.IsBot = false
	}
	advanceTicks(w, 30)
	p0 := w.Players[0]
	p0.UltCharge = 4000
	if TryCast(w, p0, p0.Skills[0], protocol.PlayerAction{X: 500}, false) {
		t.Fatal("no shooting in quarantine")
	}
	advanceTicks(w, infQuarantineTicks)
	if s.phase != infPhaseHunt || s.alpha < 0 {
		t.Fatalf("phase=%d alpha=%d", s.phase, s.alpha)
	}
	var alpha *Player
	for _, p := range w.Players {
		if p.Index == s.alpha {
			alpha = p
		}
	}
	if alpha.Team != 1 || alpha.MaxHP != 14000 || alpha.Definition.String("name") != infAlphaName || alpha.ObjectIndex < 1000 {
		t.Fatalf("alpha: team=%d hp=%d name=%s obj=%d", alpha.Team, alpha.MaxHP, alpha.Definition.String("name"), alpha.ObjectIndex)
	}
	if alpha.ResultTeam() != 0 || alpha.ResultModel() == alpha.ModelID {
		t.Fatal("roster team/model must survive the mutation")
	}
	// Альфа убивает выжившего — тот встаёт заражённым.
	var victim *Player
	for _, p := range w.Players {
		if p != alpha {
			victim = p
			break
		}
	}
	alpha.SpawnShield = false
	victim.SpawnShield = false
	Kill(w, alpha, victim)
	if !s.of(victim).pending {
		t.Fatal("victim not marked for infection")
	}
	advanceTicks(w, 70)
	if !victim.Alive || victim.Team != 1 || victim.Definition.String("name") != infZombieName || victim.MaxHP != 5500 {
		t.Fatalf("victim: alive=%v team=%d name=%s", victim.Alive, victim.Team, victim.Definition.String("name"))
	}
	if s.of(alpha).infections != 1 {
		t.Fatal("infection not counted")
	}
	snapshotAll(t, w)
	// Урон по заражённому копит ему ульту.
	victim.UltCharge = 0
	victim.SpawnShield = false
	var shooter *Player
	for _, p := range w.Players {
		if p.Team == 0 && p.Alive {
			shooter = p
		}
	}
	DealDamage(w, shooter, victim, 1750)
	if victim.UltCharge < 1900 {
		t.Fatalf("damage taken must charge the pounce, got %d", victim.UltCharge)
	}
}

func TestInfectionLastStandAndSurvivorsWin(t *testing.T) {
	w := modeWorld(t, "infection_river", 10, zero, 9)
	s := w.inf()
	for _, p := range w.Players {
		p.IsBot = false
	}
	advanceTicks(w, 20+infQuarantineTicks+2)
	var last *Player
	for _, p := range w.Players {
		if p.Index == s.alpha {
			continue
		}
		if last == nil {
			last = p
			continue
		}
		p.SpawnShield = false
		Kill(w, nil, p)
	}
	advanceTicks(w, 3)
	if s.phase != infPhaseLastStand || s.lastStand != last.Index {
		t.Fatalf("phase=%d last=%d", s.phase, s.lastStand)
	}
	if last.MaxHP < infLastStandHP || last.UltCharge != 4000 {
		t.Fatal("last stand buffs missing")
	}
	if left := w.TotalTicks() - int(w.TickNum); left > infLastStandTicks || left < infLastStandTicks-10 {
		t.Fatalf("timer not reset to 30 s: %d ticks", left)
	}
	// Никого не трогаем 30 секунд — выживший побеждает.
	for _, p := range w.Players {
		if p != last {
			putAt(p, 300, 300)
			p.ControlRemoved = true
		}
	}
	for i := 0; i < infLastStandTicks+40 && !w.Finished; i++ {
		for _, p := range w.Players {
			if p != last {
				AddStatus(w, p, "stun", 1, 3, -5)
			}
		}
		w.Advance()
	}
	if !w.Finished || w.WinnerTeam != 0 || w.PlayerRanks[last.Index] != 1 || w.PlayerRanks[s.alpha] != 2 {
		t.Fatalf("finished=%v winner=%d ranks=%v", w.Finished, w.WinnerTeam, w.PlayerRanks)
	}
}

func TestInfectionBotsFinishTheMatch(t *testing.T) {
	for _, loc := range []string{"infection_lake", "infection_default"} {
		w := modeWorld(t, loc, 10, zero, 3)
		for i := 0; i < 4000 && !w.Finished; i++ {
			w.Advance()
			if i%101 == 0 {
				snapshotAll(t, w)
			}
		}
		if !w.Finished || len(w.PlayerRanks) != 10 {
			t.Fatalf("%s: finished=%v ranks=%d", loc, w.Finished, len(w.PlayerRanks))
		}
		zombies := 0
		for _, p := range w.Players {
			if w.inf().of(p).zombie {
				zombies++
			}
		}
		t.Logf("%s: %s at tick %d, zombies %d", loc, w.WinReason, w.TickNum, zombies)
	}
}

// ── Нокаут ─────────────────────────────────────────────────────────────

func TestKnockoutRoundsAndNoRespawn(t *testing.T) {
	w := modeWorld(t, "knockout_angles", 6, halves, 2)
	s := w.ko()
	for _, p := range w.Players {
		p.IsBot = false
	}
	advanceTicks(w, 20+knockoutIntroPause+1)
	if !knockoutFighting(w) || s.round != 1 {
		t.Fatalf("round 1 did not start: state=%d", s.state)
	}
	wipe := func(team int) {
		for _, p := range w.Players {
			if p.Team == team {
				p.SpawnShield = false
				Kill(w, nil, p)
			}
		}
	}
	w.Players[0].UltCharge = 3000
	wipe(1)
	advanceTicks(w, 2)
	if s.wins[0] != 1 || s.state != knockoutStateOver {
		t.Fatalf("round 1: wins=%v state=%d", s.wins, s.state)
	}
	advanceTicks(w, 40)
	for _, p := range w.Players {
		if p.Team == 1 && p.Alive {
			t.Fatal("no respawn inside a round")
		}
	}
	advanceTicks(w, knockoutOverPause+knockoutIntroPause)
	if s.round != 2 || !knockoutFighting(w) {
		t.Fatalf("round 2 did not start: round=%d state=%d", s.round, s.state)
	}
	for _, p := range w.Players {
		if !p.Alive || p.UltCharge != 0 {
			t.Fatalf("round reset: alive=%v ult=%d", p.Alive, p.UltCharge)
		}
	}
	snapshotAll(t, w)
	wipe(1)
	advanceTicks(w, 2)
	if !w.Finished || w.WinnerTeam != 0 || w.TeamPoints[0] != 2 {
		t.Fatalf("match: finished=%v winner=%d points=%v", w.Finished, w.WinnerTeam, w.TeamPoints)
	}
}

// Команды Нокаута начинают снизу (0) и сверху (1) длинной карты, а не по бокам.
func TestKnockoutSpawnsBottomAndTop(t *testing.T) {
	for _, loc := range []string{"knockout_angles", "knockout_river", "knockout_onebridge", "knockout_lake"} {
		w := modeWorld(t, loc, 6, halves, 1)
		third := w.Tiles.PixH / 3
		for _, p := range w.Players {
			if p.Team == 0 && p.SpawnY > third || p.Team == 1 && p.SpawnY < w.Tiles.PixH-third {
				t.Fatalf("%s: team %d spawn y=%.0f of %.0f", loc, p.Team, p.SpawnY, w.Tiles.PixH)
			}
		}
	}
}

func TestKnockoutPoisonAndTimeoutByDamage(t *testing.T) {
	w := modeWorld(t, "knockout_river", 6, halves, 4)
	s := w.ko()
	for _, p := range w.Players {
		p.IsBot = false
	}
	advanceTicks(w, 20+knockoutIntroPause+1)
	corner := func() (float64, float64) { return 450, 450 }
	x, y := corner()
	if w.PoisonedAt(x, y, w.TickNum) {
		t.Fatal("no smoke at round start")
	}
	if !w.PoisonedAt(x, y, s.roundStart+knockoutPoisonDelay+200) {
		t.Fatal("smoke must reach the corner")
	}
	// Команда 1 наносит больше урона — раунд её по таймеру.
	w.Players[1].DamageDealt += 5000
	for i := 0; i < knockoutRoundTicks+5 && s.state == knockoutStateFight; i++ {
		for _, p := range w.Players {
			p.HP = p.MaxHP
		}
		w.Advance()
	}
	if s.lastWinner != 1 || s.wins[1] != 1 {
		t.Fatalf("timeout winner=%d wins=%v", s.lastWinner, s.wins)
	}
}

func TestKnockoutBotsFinishTheMatch(t *testing.T) {
	w := modeWorld(t, "knockout_onebridge", 6, halves, 8)
	for i := 0; i < 8000 && !w.Finished; i++ {
		w.Advance()
		if i%103 == 0 {
			snapshotAll(t, w)
		}
	}
	if !w.Finished {
		t.Fatal("knockout did not end")
	}
	t.Logf("knockout: winner %d wins %v rounds %d tick %d", w.WinnerTeam, w.ko().wins, w.ko().round, w.TickNum)
}

// ── Футбол 3 на 3 ──────────────────────────────────────────────────────

func TestFootball3v3Field(t *testing.T) {
	w := modeWorld(t, "experimental_football_3v3", 6, halves, 1)
	if w.Ball == nil || !w.HasCarryable || !w.BallHorizontal() || w.GoalsForWin() != 5 {
		t.Fatalf("ball=%v carry=%v horizontal=%v goals=%d", w.Ball != nil, w.HasCarryable, w.BallHorizontal(), w.GoalsForWin())
	}
	for i := 0; i < 6000 && !w.Finished; i++ {
		w.Advance()
		if i%211 == 0 {
			snapshotAll(t, w)
		}
	}
	t.Logf("football 3v3: %d:%d finished=%v tick %d", w.GetPoint(0), w.GetPoint(1), w.Finished, w.TickNum)
	if w.GetPoint(0)+w.GetPoint(1) == 0 {
		t.Fatal("bots never scored in 5 minutes")
	}
}
