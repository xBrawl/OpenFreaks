package mainserver

import (
	"fmt"
	"math/rand/v2"
	"strings"
)

var botAdjectives = []string{
	"Дерзкий", "Шустрый", "Лютый", "Хитрый", "Мохнатый", "Бешеный",
	"Колючий", "Ржавый", "Пухлый", "Наглый", "Вредный", "Смелый",
	"Кривой", "Грозный", "Липкий", "Косой", "Злой", "Быстрый",
	"Тихий", "Мрачный", "Бодрый", "Рыжий", "Чумной", "Упрямый",
}

var botNames = []string{
	"Гоша", "Витёк", "Серый", "Тимоха", "Жорик", "Бублик", "Кекс",
	"Пельмень", "Бобр", "Ёжик", "Тапок", "Валера", "Стёпа", "Боря",
	"Кактус", "Сухарь", "Фантик", "Хомяк", "Пират", "Батон", "Шуруп",
	"Маркиз", "Крендель", "Зубр", "Тюлень", "Филин", "Барсук", "Вжух",
}

const botNameStride = 53

type botNamePool struct {
	used map[string]bool
	next int
}

func newBotNamePool(taken []string) *botNamePool {
	p := &botNamePool{used: make(map[string]bool, len(taken)), next: rand.IntN(botNameGrid())}
	for _, name := range taken {
		p.used[strings.ToLower(strings.TrimSpace(name))] = true
	}
	return p
}

func botNameGrid() int { return len(botAdjectives) * len(botNames) }

func (p *botNamePool) take(index int) string {
	grid := botNameGrid()
	for i := 0; i < grid; i++ {
		slot := ((p.next % grid) + grid) % grid
		p.next += botNameStride
		name := botAdjectives[slot/len(botNames)] + " " + botNames[slot%len(botNames)]
		key := strings.ToLower(name)
		if p.used[key] {
			continue
		}
		p.used[key] = true
		return name
	}
	return fmt.Sprintf("Бот %d", index+1)
}
