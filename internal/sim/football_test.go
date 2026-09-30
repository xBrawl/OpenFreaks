package sim

import (
	"math"
	"testing"

	"github.com/freaks/openfreaks/internal/protocol"
)

func footballFixture(t *testing.T) *World {
	cat := mustCatalog(t)
	w, e := BuildWorld(cat, BuildParams{LocationID: cat.LocByName["experimental_football"].Id, Players: []BuildPlayer{{PlayerID: 1, Index: 0, Team: 0, CharacterID: cat.Heroes()[0].Id}, {PlayerID: 2, Index: 1, Team: 1, CharacterID: cat.Heroes()[1].Id}}})
	if e != nil {
		t.Fatal(e)
	}
	return w
}
// footballSpot — точка у ворот, в которые бьёт команда team: depth от края поля
// вдоль оси «ворота — ворота» и side поперёк от середины створа.
func footballSpot(w *World, team int, depth, side float64) (float64, float64) {
	m := ballMouth(w, team)
	if w.BallHorizontal() {
		x := depth
		if m.x > w.Tiles.PixW/2 {
			x = w.Tiles.PixW - depth
		}
		return x, w.Tiles.PixH/2 + side
	}
	y := depth
	if m.y > w.Tiles.PixH/2 {
		y = w.Tiles.PixH - depth
	}
	return w.Tiles.PixW/2 + side, y
}

// Поле горизонтальное, как в настоящем футболе: команда 0 слева бьёт вправо.
func TestFootballPitchIsHorizontal(t *testing.T) {
	w := footballFixture(t)
	if !w.BallHorizontal() {
		t.Fatalf("поле %.0fx%.0f не горизонтальное", w.Tiles.PixW, w.Tiles.PixH)
	}
	blue, red := w.Players[0], w.Players[1]
	if blue.X >= w.Tiles.PixW/2 || red.X <= w.Tiles.PixW/2 {
		t.Fatalf("команды не по бокам: %.0f и %.0f", blue.X, red.X)
	}
	if ballMouth(w, 0).x <= w.Tiles.PixW/2 {
		t.Fatal("команда 0 должна бить в правые ворота")
	}
}

func TestFootballGoalsResetAndOvertime(t *testing.T) {
	w := footballFixture(t)
	p := w.Players[0]
	p.X, p.Y = footballSpot(w, 0, 1100, 0)
	p.CarryableObjectID = w.Ball.ObjectIndex
	BallTick(w)
	if w.GetPoint(0) != 1 || w.RoundResetTicks != 40 {
		t.Fatal("carried goal not scored")
	}
	for i := 0; i < 40; i++ {
		w.Advance()
	}
	if p.CarryableObjectID != 0 || p.HP != p.MaxHP || w.Ball.Y != w.Tiles.PixH/2 || w.Ball.X != w.Tiles.PixW/2 {
		t.Fatal("round did not reset")
	}
	w.TeamPoints = []int{1, 1}
	w.FinishByTime()
	if w.Finished || !w.BallOvertime {
		t.Fatal("no overtime")
	}
	w.Ball.X, w.Ball.Y = footballSpot(w, 1, 1100, 0)
	BallTick(w)
	if !w.Finished || w.WinnerTeam != 1 {
		t.Fatal("overtime goal did not win")
	}
}
func TestFootballNeedsConfiguredGoals(t *testing.T) {
	w := footballFixture(t)
	need := w.GoalsForWin()
	if need < 2 {
		t.Fatalf("goals_to_win=%d, матч кончится с первого гола", need)
	}
	for goal := 1; goal <= need; goal++ {
		w.Ball.X, w.Ball.Y = footballSpot(w, 0, 1100, 0)
		w.RoundResetTicks = 0
		BallTick(w)
		if (goal < need) == w.Finished {
			t.Fatalf("finished=%v после гола %d из %d", w.Finished, goal, need)
		}
	}
}

func TestFootballGoalsDefaultWhenUnset(t *testing.T) {
	w := footballFixture(t)
	w.GoalsToWin = 0
	if w.GoalsForWin() != 3 {
		t.Fatalf("GoalsForWin=%d", w.GoalsForWin())
	}
}

func TestFootballKillsDoNotCountAsGoals(t *testing.T) {
	w := footballFixture(t)
	killer, victim := w.Players[0], w.Players[1]
	Kill(w, killer, victim)
	if killer.Kills != 1 || w.GetPoint(killer.Team) != 0 {
		t.Fatalf("kills=%d goals=%d", killer.Kills, w.GetPoint(killer.Team))
	}
}
func TestFootballThrowRejectsStunAndUnchargedUltimate(t *testing.T) {
	w := footballFixture(t)
	p := w.Players[0]
	p.CarryableObjectID = w.Ball.ObjectIndex
	a := protocol.PlayerAction{Y: -2000}
	if ThrowBall(w, p, a, true) {
		t.Fatal("free super shot")
	}
	p.UltCharge = 4000
	p.ControlRemoved = true
	if ThrowBall(w, p, a, true) {
		t.Fatal("stunned throw")
	}
	p.ControlRemoved = false
	if !ThrowBall(w, p, a, true) || p.UltCharge != 0 {
		t.Fatal("super charge not spent")
	}
}

func footballTeamFixture(t *testing.T) *World {
	cat := mustCatalog(t)
	h := cat.Heroes()
	w, e := BuildWorld(cat, BuildParams{LocationID: cat.LocByName["experimental_football"].Id, Players: []BuildPlayer{
		{PlayerID: 1, Index: 0, Team: 0, CharacterID: h[0].Id},
		{PlayerID: 2, Index: 1, Team: 0, CharacterID: h[1].Id},
		{PlayerID: 3, Index: 2, Team: 1, CharacterID: h[0].Id},
		{PlayerID: 4, Index: 3, Team: 1, CharacterID: h[1].Id},
	}})
	if e != nil {
		t.Fatal(e)
	}
	w.TickNum = int32(w.IntroTicks)
	return w
}

func TestFootballBotsKeepADefenderHome(t *testing.T) {
	w := footballTeamFixture(t)
	near, far := w.Players[0], w.Players[1]
	w.Ball.X, w.Ball.Y = w.Tiles.PixW/2, w.Tiles.PixH/2
	near.X, near.Y = w.Ball.X, w.Ball.Y+200
	far.X, far.Y = w.Ball.X, w.Tiles.PixH-2500

	if d := ballDefender(w, 0); d != far {
		t.Fatalf("в защите оказался не дальний игрок: %v", d)
	}
	own := ballMouth(w, 1)
	if post := footballGoal(w, far, 0); Dist(post.x, post.y, own.x, own.y) > ballKeeperDepth+1 {
		t.Fatalf("защитник ушёл от своих ворот: %v", post)
	}
	if chase := footballGoal(w, near, 0); Dist(chase.x, chase.y, w.Ball.X, w.Ball.Y) > 1 {
		t.Fatalf("ближний не пошёл за мячом: %v", chase)
	}
}

func TestFootballBotDoesNotShootFromMidfield(t *testing.T) {
	w := footballTeamFixture(t)
	bot := w.Players[0]
	bot.CarryableObjectID = w.Ball.ObjectIndex
	mouth := ballMouth(w, bot.Team)
	dx, dy := w.Tiles.PixW/2-mouth.x, w.Tiles.PixH/2-mouth.y
	d := math.Hypot(dx, dy)
	dx, dy = dx/d, dy/d

	bot.X, bot.Y = mouth.x+dx*(ballShotRange+400), mouth.y+dy*(ballShotRange+400)
	shootBall(w, bot, mouth, 0)
	if bot.CarryableObjectID != w.Ball.ObjectIndex {
		t.Fatal("бот пробил с середины поля")
	}

	bot.X, bot.Y = mouth.x+dx*(ballShotRange-200), mouth.y+dy*(ballShotRange-200)
	shootBall(w, bot, mouth, 0)
	if bot.CarryableObjectID == w.Ball.ObjectIndex {
		t.Fatal("бот не бьёт даже с ударной позиции")
	}
}

func TestFootballBotAimIsDeterministic(t *testing.T) {
	first := footballShotTarget(t)
	if second := footballShotTarget(t); first != second {
		t.Fatalf("бросок разошёлся: %v != %v", first, second)
	}
}

func footballShotTarget(t *testing.T) [2]float64 {
	w := footballTeamFixture(t)
	bot := w.Players[0]
	bot.CarryableObjectID = w.Ball.ObjectIndex
	mouth := ballMouth(w, 0)
	bot.X, bot.Y = mouth.x, mouth.y+600
	shootBall(w, bot, mouth, 37)
	return [2]float64{w.Ball.TargetX, w.Ball.TargetY}
}

func TestFootballBotMatchLastsAtLeastOneMinute(t *testing.T) {
	w := footballTeamFixture(t)
	for _, p := range w.Players {
		p.IsBot = true
	}
	const oneMinuteTicks = 20 * 60
	for i := 0; i < oneMinuteTicks && !w.Finished; i++ {
		w.Advance()
	}
	if w.Finished {
		t.Fatalf("боты закончили матч слишком быстро: tick=%d score=%v", w.TickNum, w.TeamPoints)
	}
}

func TestFootballSnapshotAfterRoundResetDoesNotPanic(t *testing.T) {
	w := footballFixture(t)
	w.Ball.LastThrowTeam = -1
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("fresh ball snapshot panicked: %v", recovered)
		}
	}()
	if snapshot := EncodeSnapshot(w, 0, protocol.ParticipantPlayer); len(snapshot) == 0 {
		t.Fatal("empty football snapshot")
	}
}

func TestFootballDashTackleKnocksBallLoose(t *testing.T) {
	w := footballFixture(t)
	carrier, rival := w.Players[0], w.Players[1]
	carrier.X, carrier.Y = w.Tiles.PixW/2, w.Tiles.PixH/2
	carrier.CarryableObjectID = w.Ball.ObjectIndex
	rival.X, rival.Y = carrier.X, carrier.Y-float64(carrier.CollisionRadius+rival.CollisionRadius)
	BallTick(w)
	if carrier.CarryableObjectID != w.Ball.ObjectIndex {
		t.Fatal("касание без рывка отняло мяч")
	}
	rival.DashActiveTicks = 5
	BallTick(w)
	if carrier.CarryableObjectID != 0 || !w.Ball.HasMoveTarget || w.Ball.LastThrowTeam != rival.Team {
		t.Fatalf("рывок не выбил мяч: carrier=%d move=%v team=%d", carrier.CarryableObjectID, w.Ball.HasMoveTarget, w.Ball.LastThrowTeam)
	}
	if w.Ball.TargetY <= carrier.Y {
		t.Fatal("мяч должен лететь по ходу рывка")
	}
}

func TestFootballPickupGoesToNearest(t *testing.T) {
	w := footballFixture(t)
	near, far := w.Players[1], w.Players[0]
	w.Ball.X, w.Ball.Y = w.Tiles.PixW/2, w.Tiles.PixH/2
	w.Ball.HasMoveTarget = false
	w.Ball.ThrowCooldown = 0
	far.X, far.Y = w.Ball.X-float64(far.CollisionRadius+100), w.Ball.Y
	near.X, near.Y = w.Ball.X+20, w.Ball.Y
	BallTick(w)
	if near.CarryableObjectID != w.Ball.ObjectIndex || far.CarryableObjectID != 0 {
		t.Fatalf("near=%d far=%d", near.CarryableObjectID, far.CarryableObjectID)
	}
}

// Супер-удар: всегда на полную дальность, быстрее паса и расталкивает соперников.
func TestFootballSuperKickIsStrongerThanPass(t *testing.T) {
	flight := func(ultimate bool) (float64, int) {
		w := footballFixture(t)
		w.TickNum = int32(w.IntroTicks) + 1
		p, enemy := w.Players[0], w.Players[1]
		p.X, p.Y = w.Tiles.PixW/2, w.Tiles.PixH/2
		enemy.X, enemy.Y = p.X+400, p.Y
		p.CarryableObjectID = w.Ball.ObjectIndex
		p.UltCharge = 4000
		startX := enemy.X
		// Стик едва сдвинут вправо: пас летит недалеко, супер-удар — всё равно на всю дальность.
		if !ThrowBall(w, p, protocol.PlayerAction{X: 300, Y: 0}, ultimate) {
			t.Fatal("throw rejected")
		}
		for i := 0; i < 3; i++ {
			BallTick(w)
		}
		return w.Ball.X - p.X, int(enemy.X - startX)
	}
	pass, passPush := flight(false)
	super, superPush := flight(true)
	if super <= pass*1.5 {
		t.Fatalf("super kick %.0f is not faster than pass %.0f", super, pass)
	}
	if passPush != 0 || superPush <= 0 {
		t.Fatalf("push: pass %d, super %d", passPush, superPush)
	}
}
