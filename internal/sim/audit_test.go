package sim

import (
	"testing"

	"github.com/freaks/openfreaks/internal/protocol"
)

func stunSkill(castingTimeMs int) *Skill {
	return &Skill{
		Definition: spec(map[string]any{
			"casting_time":                     castingTimeMs,
			"execute_first_attack_immediately": true,
			"behaviour": map[string]any{
				"attack": map[string]any{"attack_type": "direct"},
			},
		}),
		Behaviour: "attack", AttackType: "direct",
		Damage: 100, RangePx: 3000, ActiveMs: 50, MsBetween: 50,
	}
}

func TestStunBlocksCastIssuedBeforeEffectsTick(t *testing.T) {
	w := arena(20, 20)
	w.TickNum = 100
	attacker, victim := fighter(0, 0, 600, 600), fighter(1, 1, 1200, 600)
	attacker.Skills = []*Skill{stunSkill(0)}
	w.Players = []*Player{attacker, victim}

	AddStatus(w, attacker, "stun", 0, 20, victim.ObjectIndex)
	if TryCast(w, attacker, attacker.Skills[0], protocol.PlayerAction{X: 600}, false) {
		t.Fatal("оглушённый персонаж начал каст")
	}
	if victim.HP != victim.MaxHP {
		t.Fatalf("цель получила урон во время стана: hp=%d", victim.HP)
	}
}

func TestStunCancelsPendingDelayedCast(t *testing.T) {
	w := arena(20, 20)
	w.TickNum = 100
	attacker, victim := fighter(0, 0, 600, 600), fighter(1, 1, 1200, 600)
	attacker.Skills = []*Skill{stunSkill(100)}
	w.Players = []*Player{attacker, victim}

	if !TryCast(w, attacker, attacker.Skills[0], protocol.PlayerAction{X: 600}, false) {
		t.Fatal("каст с задержкой не начался")
	}
	TickExecutions(w, attacker)
	AddStatus(w, attacker, "stun", 0, 20, victim.ObjectIndex)
	TickExecutions(w, attacker)
	TickExecutions(w, attacker)

	if victim.HP != victim.MaxHP {
		t.Fatalf("отложенный каст нанёс урон сквозь стан: hp=%d", victim.HP)
	}
	if len(attacker.Executions) != 0 {
		t.Fatal("отменённый каст остался в очереди исполнений")
	}
}

func TestEntityIndexAllocatorsStayInWireRange(t *testing.T) {
	w := arena(20, 20)
	w.Players = []*Player{fighter(0, 0, 600, 600)}

	w.NextEffectIndex = maxEntityIndex - 1
	for i := 0; i < 8; i++ {
		id := w.AllocEffectIndex()
		if id < minEffectIndex || id > maxEntityIndex {
			t.Fatalf("индекс эффекта вне диапазона: %d", id)
		}
	}

	w.NextCharacterIndex = maxEntityIndex - 1
	for i := 0; i < 8; i++ {
		id := w.AllocCharacterIndex()
		if id < minCharacterIndex || id > maxEntityIndex {
			t.Fatalf("индекс персонажа вне диапазона: %d", id)
		}
	}
}

func TestEntityIndexAllocatorsSkipLiveIds(t *testing.T) {
	w := arena(20, 20)
	minion := fighter(0, 0, 900, 900)
	minion.ObjectIndex = minCharacterIndex
	w.Minions = []*Player{minion}
	w.NextCharacterIndex = minCharacterIndex

	if id := w.AllocCharacterIndex(); id == minCharacterIndex {
		t.Fatal("аллокатор выдал индекс живого миньона")
	}

	w.Areas = []*Area{{LocalIndex: minEffectIndex}}
	w.NextEffectIndex = minEffectIndex
	if id := w.AllocEffectIndex(); id == minEffectIndex {
		t.Fatal("аллокатор выдал индекс живой области")
	}
}

func TestHiddenEnemyPositionIsFrozenInSnapshot(t *testing.T) {
	w := arena(20, 20)
	w.TickNum = 100
	w.MaxPlayers = 2
	w.Tiles.HidesHero = make([]bool, 400)
	observer, enemy := fighter(0, 0, 600, 600), fighter(1, 1, 2400, 600)
	w.Players = []*Player{observer, enemy}

	EncodeSnapshot(w, observer.Index, protocol.ParticipantPlayer)

	w.Tiles.HidesHero[2*20+8] = true
	if w.CanSee(observer, enemy) {
		t.Fatal("куст не скрыл противника — тест ничего не проверяет")
	}

	seenX, seenY := enemy.X, enemy.Y

	w.Tiles.HidesHero[10*20+14] = true
	enemy.X, enemy.Y = 4200, 3000
	enemy.HP = 1200
	if w.CanSee(observer, enemy) {
		t.Fatal("новая позиция оказалась на виду — тест ничего не проверяет")
	}
	EncodeSnapshot(w, observer.Index, protocol.ParticipantPlayer)

	view := w.hiddenViewFor(observer.Index, enemy)
	if view.X != seenX || view.Y != seenY {
		t.Fatalf("скрытый противник отдал свежие координаты: %v,%v", view.X, view.Y)
	}
	if view.HP == enemy.HP {
		t.Fatal("скрытый противник отдал актуальные HP")
	}
}

func TestTrainingDamagePerSecondIsComputed(t *testing.T) {
	w := arena(20, 20)
	w.DPSEnabled = true
	attacker, dummy := fighter(0, 0, 600, 600), fighter(1, 1, 1200, 600)
	dummy.MaxHP, dummy.HP = 1<<30, 1<<30
	w.Players = []*Player{attacker, dummy}

	w.TickDPS()
	if got := w.DamagePerSecond(attacker); got != 0 {
		t.Fatalf("без урона DPS должен быть нулём, получено %d", got)
	}

	attacker.DamageDealt = 5000
	w.TickNum = 10
	w.TickDPS()
	if got := w.DamagePerSecond(attacker); got <= 0 {
		t.Fatalf("нанесённый урон не попал в DPS: %d", got)
	}
}
