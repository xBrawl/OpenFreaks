package mainserver

import (
	"strings"
	"testing"
)

func TestBotNamesAreUniqueAndNotNumbered(t *testing.T) {
	pool := newBotNamePool([]string{"Гоша"})
	seen := map[string]bool{}
	for i := 0; i < 8; i++ {
		name := pool.take(i)
		if seen[name] {
			t.Fatalf("ник повторился: %q", name)
		}
		if !strings.Contains(name, " ") || strings.HasPrefix(name, "Bot ") {
			t.Fatalf("ожидалось «прилагательное имя», получено %q", name)
		}
		seen[name] = true
	}
}

func TestBotNamePoolCoversWholeGrid(t *testing.T) {
	pool := newBotNamePool(nil)
	seen := map[string]bool{}
	for i := 0; i < botNameGrid(); i++ {
		name := pool.take(i)
		if seen[name] {
			t.Fatalf("сетка обойдена не полностью, повтор на %d: %q", i, name)
		}
		seen[name] = true
	}
	if got := pool.take(9); got != "Бот 10" {
		t.Fatalf("исчерпанная сетка должна давать запасной ник, получено %q", got)
	}
}
