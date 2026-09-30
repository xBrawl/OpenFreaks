package battleserver

import (
	"log/slog"
	"time"
)

const (
	// Клиент шлёт пакет каждый кадр, пока игрок идёт: 120 FPS на телефоне,
	// 144 Гц и выше в браузере (там лимита кадров нет). Бюджет с запасом выше,
	// иначе честный игрок теряет часть пакетов — вместе с атаками.
	inputPacketsPerSecond = 180
	inputPacketBurst      = 360
	inputBytesPerSecond   = 32 * 1024
	inputByteBurst        = 64 * 1024
	inputActionsPerSecond = 240
	inputActionBurst      = 480
	inputQueuePerPlayer   = 32
	inputErrorThreshold   = 5
	inputErrorWindow      = 10 * time.Second
	maxActionBodyBytes    = 256 // 31 действий занимают не более 236 байт; оставляем запас.
)

// time.Time сохраняет монотонную составляющую: перевод системных часов
// не восстанавливает бюджет и не продлевает окно ошибок.
type inputBucket struct {
	at     time.Time
	tokens float64
}

func (b *inputBucket) allow(now time.Time, cost, rate, burst int) bool {
	if b.at.IsZero() {
		b.at, b.tokens = now, float64(burst)
	} else if now.After(b.at) {
		b.tokens = min(float64(burst), b.tokens+now.Sub(b.at).Seconds()*float64(rate))
		b.at = now
	}
	if cost < 0 || float64(cost) > b.tokens {
		return false
	}
	b.tokens -= float64(cost)
	return true
}

// Всё состояние ввода, кроме pendingWalk, защищено Match.actionMu.
// Бюджеты и блокировка живут до конца боя, в том числе при переподключении.
type inputGuard struct {
	packets, bytes, actions inputBucket
	errorTimes              [inputErrorThreshold]time.Time
	errorNext               int
	blocked                 bool
	queued                  int
}

func (m *Match) inputState(index int) *inputGuard {
	g := m.inputGuards[index]
	if g == nil {
		g = &inputGuard{}
		m.inputGuards[index] = g
	}
	return g
}

func (g *inputGuard) allowPacket(now time.Time, size int) bool {
	packets := g.packets.allow(now, 1, inputPacketsPerSecond, inputPacketBurst)
	bytes := g.bytes.allow(now, size, inputBytesPerSecond, inputByteBurst)
	return packets && bytes
}

// Одна ошибка на пакет, а не на действие. Разрозненные ошибки не копятся
// весь матч. Блокируем только ввод этого боя, без автобана аккаунта.
func (m *Match) inputError(index int, reason string, now time.Time) {
	g := m.inputState(index)
	if g.blocked {
		return
	}
	g.errorTimes[g.errorNext] = now
	g.errorNext = (g.errorNext + 1) % len(g.errorTimes)
	oldest := g.errorTimes[g.errorNext]
	if !oldest.IsZero() && now.Sub(oldest) < inputErrorWindow {
		g.blocked = true
		slog.Warn("battle input blocked", "battle", m.ID, "index", index,
			"player", m.parts[index].PlayerID, "reason", reason, "packets", inputErrorThreshold,
			"window_ms", now.Sub(oldest).Milliseconds())
	}
}

func (m *Match) inputBlocked(index int) bool {
	g := m.inputGuards[index]
	return m.cheatFlagged(index) || g != nil && g.blocked
}

// Резервируем место для остальных участников даже при остановившемся тике.
// Переполнение никогда не вытесняет уже принятую чужую команду.
func (m *Match) enqueueInput(it inputItem) {
	g := m.inputState(it.index)
	limit := min(inputQueuePerPlayer, cap(m.inputs)/max(1, len(m.parts)))
	if g.queued >= limit {
		return
	}
	select {
	case m.inputs <- it:
		g.queued++
	default:
	}
}
