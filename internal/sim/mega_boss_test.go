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
	if boss.Definition.Int("hit_points") <= hitPointsThresh {
		t.Fatal("boss catalog HP must select the extended snapshot field on the client")
	}
	if w.DurationTicks != 3600 || boss.MaxHP != 145000 || w.Endless {
		t.Fatalf("wrong three-player tuning: ticks=%d hp=%d endless=%v", w.DurationTicks, boss.MaxHP, w.Endless)
	}
	_, sixBoss := megaBossWorld(t, 6)
	if sixBoss.MaxHP != 290000 {
		t.Fatalf("six-player boss hp=%d", sixBoss.MaxHP)
	}
	_, _, practiceBoss := karityFixture(t)
	if practiceBoss.MaxHP != 36000 {
		t.Fatalf("test arena boss hp=%d", practiceBoss.MaxHP)
	}
	w.TickNum = int32(w.TotalTicks() - 1)
	w.Advance()
	if !w.Finished || w.WinnerTeam != 1 || w.WinReason != "boss_survived" {
		t.Fatalf("wrong timeout: finished=%v winner=%d reason=%s", w.Finished, w.WinnerTeam, w.WinReason)
	}
}

// titanPhase доводит бой до фазы 2: шар убит, титан встал.
func titanPhase(t *testing.T, w *World, ball *Player) *Player {
	t.Helper()
	ball.SpawnShield = false
	for ball.Alive {
		DealDamage(w, w.Players[0], ball, ball.HP)
	}
	for i := 0; i < transitionTicks+5 && !w.Finished; i++ {
		w.Advance()
	}
	if w.karity == nil || w.karity.titan == nil || w.BossPhase != BossPhaseTitan {
		t.Fatalf("titan phase did not start: phase=%d", w.BossPhase)
	}
	return w.karity.titan
}

func TestMegaBossBallDownStartsTitanPhase(t *testing.T) {
	w, ball := megaBossWorld(t, 3)
	ball.SpawnShield = false
	for ball.Alive {
		DealDamage(w, w.Players[0], ball, ball.HP)
	}
	if w.Finished || w.BossPhase != BossPhaseTransition || w.Endless || !KarityTimeless(w) {
		t.Fatalf("ball death must start the transition: finished=%v phase=%d endless=%v", w.Finished, w.BossPhase, w.Endless)
	}
	for i := 0; i < titanSpawnDelay+1; i++ {
		w.Advance()
	}
	titan := w.karity.titan
	if titan == nil || !titan.Alive || titan.MaxHP != 174000 {
		t.Fatalf("titan not spawned right: %+v", titan)
	}
	if titan.X != w.Tiles.PixW/2 || titan.Y != w.Tiles.PixH/2 {
		t.Fatalf("titan must lie at the arena centre, got %.0f,%.0f", titan.X, titan.Y)
	}
	hp := titan.HP
	DealDamage(w, w.Players[0], titan, 5000)
	if titan.HP != hp {
		t.Fatal("rising titan must be invulnerable")
	}
	for i := 0; i < transitionTicks; i++ {
		w.Advance()
	}
	if w.BossPhase != BossPhaseTitan || ball.Alive {
		t.Fatalf("phase=%d ballAlive=%v", w.BossPhase, ball.Alive)
	}
	// Фаза 2 без таймера: давно прошедшие 3 минуты боя ничего не решают.
	w.TickNum = int32(w.TotalTicks() + 10)
	w.Advance()
	if w.Finished {
		t.Fatalf("titan phase ended by time: %s", w.WinReason)
	}
	for titan.Alive {
		w.EmojiShieldLeft = 0
		DealDamage(w, w.Players[0], titan, titan.HP)
	}
	if !w.Finished || w.WinnerTeam != 0 || w.WinReason != "boss_killed" {
		t.Fatalf("wrong titan death: finished=%v winner=%d reason=%s", w.Finished, w.WinnerTeam, w.WinReason)
	}
}

func TestMegaBossEmojiShield(t *testing.T) {
	w, ball := megaBossWorld(t, 3)
	titan := titanPhase(t, w, ball)
	for titan.HP*2 > titan.MaxHP {
		DealDamage(w, w.Players[0], titan, 30000) // один удар — не больше 32767
	}
	w.Advance()
	if w.EmojiShieldTotal != 15 || w.EmojiShieldLeft != 15 {
		t.Fatalf("shield for three humans: left=%d total=%d", w.EmojiShieldLeft, w.EmojiShieldTotal)
	}
	hp := titan.HP
	DealDamage(w, w.Players[0], titan, 5000)
	if titan.HP != hp {
		t.Fatal("emoji shield must block damage")
	}
	w.KarityEmote(ball.Index) // босс не считается
	if w.EmojiShieldLeft != 15 {
		t.Fatal("boss emote cracked the shield")
	}
	for i := 0; i < 15; i++ {
		w.KarityEmote(i % 3)
	}
	if w.EmojiShieldLeft != 0 || w.karity.staggerUntil <= w.TickNum {
		t.Fatalf("shield not broken: left=%d", w.EmojiShieldLeft)
	}
	DealDamage(w, w.Players[0], titan, 1000)
	if got := hp - titan.HP; got <= 1000 {
		t.Fatalf("staggered titan must take bonus damage, took %d", got)
	}
}

func TestMegaBossTitanWipesTeam(t *testing.T) {
	w, ball := megaBossWorld(t, 3)
	titanPhase(t, w, ball)
	for _, p := range w.Players[:3] {
		p.SpawnShield = false
		Kill(w, nil, p)
		if p.RespawnLeft != titanRespawnTicks {
			t.Fatalf("respawn in titan phase = %d", p.RespawnLeft)
		}
	}
	w.Advance()
	if !w.Finished || w.WinnerTeam != 1 || w.WinReason != "team_wiped" {
		t.Fatalf("wipe: finished=%v winner=%d reason=%s", w.Finished, w.WinnerTeam, w.WinReason)
	}
}

func TestMegaBossTitanSweepHitsBandOnly(t *testing.T) {
	w, ball := megaBossWorld(t, 3)
	titan := titanPhase(t, w, ball)
	inBand, under, far := w.Players[0], w.Players[1], w.Players[2]
	place := func(p *Player, x, y float64) {
		p.SpawnShield = false
		p.X, p.Y, p.ExactX, p.ExactY = titan.X+x, titan.Y+y, titan.X+x, titan.Y+y
		p.HasMoveTarget = false
		p.MaxHP, p.HP = 10000, 10000
	}
	place(inBand, -1000, -1800)
	place(under, -1000, 0)
	place(far, 3500, 3500)
	k := w.karity
	k.action, k.actionTick, k.hit = "sweep_front", w.TickNum, map[*Player]bool{}
	for i := 0; i < sweepWarnTicks+sweepMoveTicks+2; i++ {
		tickTitanAction(w, k, titan)
		w.TickNum++
	}
	if inBand.HP != 10000-sweepDamage {
		t.Fatalf("hero in the band took %d", 10000-inBand.HP)
	}
	if under.HP != 10000 || far.HP != 10000 {
		t.Fatalf("heroes outside the band were hit: under=%d far=%d", under.HP, far.HP)
	}
}

func TestMegaBossGrabberDragsToMouth(t *testing.T) {
	w, ball := megaBossWorld(t, 3)
	titan := titanPhase(t, w, ball)
	hero := w.Players[0]
	hero.SpawnShield = false
	hero.MaxHP, hero.HP = 20000, 20000
	hx, hy := w.Tiles.FindOpen(titan.X+1500, titan.Y+2500, hero.CollisionRadius)
	hero.X, hero.Y, hero.ExactX, hero.ExactY = hx, hy, hx, hy
	g := SpawnMinion(w, titan, karityGrabberName, hx, hy)
	if g == nil {
		t.Fatal("grabber not spawned")
	}
	gx, gy := w.Tiles.FindOpen(hx+120, hy, g.CollisionRadius)
	g.X, g.Y, g.ExactX, g.ExactY = gx, gy, gx, gy
	mouthX, mouthY := w.karity.mouthX, w.karity.mouthY
	start := Dist(hero.X, hero.Y, mouthX, mouthY)
	w.karity.nextAction = w.TickNum + 1000 // титан не мешает
	for i := 0; i < 60; i++ {
		w.Advance()
	}
	if g.KarityGrab != hero {
		t.Fatal("grabber did not latch")
	}
	if Dist(hero.X, hero.Y, mouthX, mouthY) > start-500 {
		t.Fatalf("hero was not dragged: %.0f → %.0f", start, Dist(hero.X, hero.Y, mouthX, mouthY))
	}
}

func TestMegaBossTitanHitCircles(t *testing.T) {
	w, ball := megaBossWorld(t, 3)
	titan := titanPhase(t, w, ball)
	if d := bodyDist(titan, titan.X-2880, titan.Y-420); d != 0 {
		t.Fatalf("head must be hittable, dist=%.0f", d)
	}
	if d := bodyDist(titan, titan.X+3000, titan.Y+3000); d < 1000 {
		t.Fatalf("far corner counted as body, dist=%.0f", d)
	}
}

func TestMegaBossActiveThreePlayerCalibration(t *testing.T) {
	w, boss := megaBossWorld(t, 3)
	for _, p := range w.Players[:3] {
		p.BotControlled = true
	}
	for i := 0; i < w.TotalTicks() && w.BossPhase == BossPhaseBall && !w.Finished; i++ {
		w.Advance()
	}
	if w.BossPhase != BossPhaseTransition || w.TickNum < 1400 || w.TickNum > 2900 { // волк ослаблен 25.09 — бой дольше
		t.Fatalf("three active heroes took %.1fs to sink the ball, hp=%d/%d, phase=%d reason=%s", float64(w.TickNum)/20, boss.HP, boss.MaxHP, w.BossPhase, w.WinReason)
	}
}

// Живой бой второй фазы с ботами-героями: каждый тик кодируется снимок для
// каждого участника. Хватальщик с навыком-заглушкой (перезарядка длиннее
// 255 × 50 мс) ронял бой паникой на записи снимка.
func TestMegaBossTitanPhaseSnapshotsEncode(t *testing.T) {
	w, ball := megaBossWorld(t, 3)
	for _, p := range w.Players[:3] {
		p.BotControlled = true
	}
	titanPhase(t, w, ball)
	grabbers := 0
	for i := 0; i < 4000 && !w.Finished; i++ {
		w.Advance()
		for idx := range w.Players {
			EncodeSnapshot(w, idx, 0)
		}
		for _, m := range w.Minions {
			if m.Alive && isGrabber(m) {
				grabbers++
				break
			}
		}
	}
	if grabbers == 0 {
		t.Fatal("titan never summoned grabbers during the soak")
	}
}

// Шар стоит, копит замах и выпускает две волны по burstBullets смайлов.
func TestMegaBossEmojiBurst(t *testing.T) {
	w, ball := megaBossWorld(t, 3)
	for i, p := range w.Players[:3] {
		p.X, p.Y = ball.X+900+float64(i)*200, ball.Y
	}
	w.karity = &karityState{nextRoll: 1 << 30, nextBurst: w.TickNum}
	count := func() int {
		n := 0
		for _, pr := range w.Projectiles {
			if pr.Definition.String("name") == burstName {
				n++
			}
		}
		return n
	}
	// Залп ждёт, пока шар доиграет текущий навык.
	for i := 0; i < 200 && w.karity.burstTick == 0; i++ {
		w.Advance()
	}
	if w.karity.burstTick == 0 || !ball.KarityCasting {
		t.Fatal("burst did not start")
	}
	startX, startY := ball.X, ball.Y
	for i := 0; i < burstWarnTicks-2; i++ {
		w.Advance()
	}
	if count() != 0 {
		t.Fatalf("projectiles before the wind-up ended: %d", count())
	}
	if Dist(startX, startY, ball.X, ball.Y) > 1 {
		t.Fatal("ball moved while winding up the burst")
	}
	for i := 0; i < burstWaveGap+2; i++ {
		w.Advance()
	}
	if got := count(); got < burstBullets*burstWaves-4 {
		t.Fatalf("burst fired %d projectiles", got)
	}
	if ball.KarityCasting {
		t.Fatal("ball still casting after both waves")
	}
}

// Титан роняет смайлы в круги по очереди; стоящий в круге получает удар.
func TestMegaBossEmojiRain(t *testing.T) {
	w, ball := megaBossWorld(t, 3)
	titan := titanPhase(t, w, ball)
	k := w.karity
	for _, p := range w.Players[:3] {
		p.X, p.Y = titan.X+2500, titan.Y+3200
		p.HP = p.MaxHP
	}
	k.action, k.actionTick, k.hit, k.meteors = "rain", w.TickNum, map[*Player]bool{}, nil
	before := w.Players[0].HP + w.Players[1].HP + w.Players[2].HP
	areas := map[*Area]bool{}
	for i := 0; i < (rainMeteors-1)*rainStagger+meteorWarnTicks+2; i++ {
		w.Advance()
		for _, a := range w.Areas {
			if a.Definition.String("name") == "karity_emoji_meteor" {
				areas[a] = true
			}
		}
	}
	if len(k.meteors) != rainMeteors || len(areas) != rainMeteors {
		t.Fatalf("meteors=%d areas=%d", len(k.meteors), len(areas))
	}
	after := w.Players[0].HP + w.Players[1].HP + w.Players[2].HP
	if before-after < meteorDamage {
		t.Fatalf("rain barely hurt the team: %d", before-after)
	}
	if k.action != "" {
		t.Fatalf("rain did not finish: %s", k.action)
	}
}

func TestMegaBossTitanCrushCoversTheBody(t *testing.T) {
	w, ball := megaBossWorld(t, 3)
	titan := titanPhase(t, w, ball)
	hugging, band, far := w.Players[0], w.Players[1], w.Players[2]
	place := func(p *Player, x, y float64) {
		p.SpawnShield = false
		p.X, p.Y, p.ExactX, p.ExactY = titan.X+x, titan.Y+y, titan.X+x, titan.Y+y
		p.HasMoveTarget = false
		p.MaxHP, p.HP = 10000, 10000
	}
	place(hugging, -360, -500)
	place(band, -1000, -1800)
	place(far, 3500, 3500)
	k := w.karity
	for i := 0; i < 50; i++ {
		k.count = i
		k.action = ""
		k.nextAction = w.TickNum
		startTitanAction(w, k, titan)
		if k.action == "crush" {
			break
		}
	}
	if k.action != "crush" {
		t.Fatal("титан ни разу не выбрал придавливание, хотя у тела стоит боец")
	}
	for i := 0; i < crushWarnTicks+2 && k.action != ""; i++ {
		tickTitanAction(w, k, titan)
		w.TickNum++
	}
	if hugging.HP != 10000-crushDamage {
		t.Fatalf("боец у тела получил %d", 10000-hugging.HP)
	}
	if band.HP != 10000 || far.HP != 10000 {
		t.Fatalf("придавило вне полосы: band=%d far=%d", band.HP, far.HP)
	}
	if hugging.Y-titan.Y > -800 {
		t.Fatalf("боец не отлетел от тела: dy=%.0f", hugging.Y-titan.Y)
	}
}

func TestMegaBossTitanSlamSeriesFollowsVictim(t *testing.T) {
	w, ball := megaBossWorld(t, 3)
	titan := titanPhase(t, w, ball)
	victim := w.Players[0]
	for _, p := range w.Players[1:3] {
		p.Alive = false
	}
	victim.SpawnShield = false
	victim.MaxHP, victim.HP = 20000, 20000
	victim.X, victim.Y, victim.ExactX, victim.ExactY = titan.X-1000, titan.Y-2000, titan.X-1000, titan.Y-2000
	victim.HasMoveTarget = false
	k := w.karity
	k.action, k.actionTick, k.hit = "slam", w.TickNum, map[*Player]bool{}
	k.victim, k.slams = victim, nil
	for i := 0; i < (slamSeries-1)*slamGap+slamWarnTicks+2 && k.action != ""; i++ {
		tickTitanAction(w, k, titan)
		w.TickNum++
		// Стоит на месте, но оглушение и отброс от каждой ладони — его забота:
		// возвращаем бойца в точку, чтобы проверить, что серия бьёт трижды.
		victim.X, victim.Y, victim.ExactX, victim.ExactY = titan.X-1000, titan.Y-2000, titan.X-1000, titan.Y-2000
	}
	if len(k.slams) != 0 && k.action != "" {
		t.Fatal("серия не закончилась")
	}
	if got := 20000 - victim.HP; got != slamSeries*slamDamage {
		t.Fatalf("серия сняла %d, ждали %d", got, slamSeries*slamDamage)
	}
}
