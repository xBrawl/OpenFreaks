package sim

import (
	"github.com/freaks/openfreaks/internal/content"
	"testing"
)

func TestTestBalanceChangesAuthoritativeStats(t *testing.T) {
	cat := mustCatalog(t)
	ch := cat.Heroes()[0]
	w := &World{Catalog: cat}
	base := buildPlayer(cat, w, BuildPlayer{HPLevel: 1, DamageLevel: 1, SpeedLevel: 1}, ch)
	edited := buildPlayer(cat, w, BuildPlayer{HPLevel: 1, DamageLevel: 1, SpeedLevel: 1, TestBalance: content.TestBalance{HP: 150, Damage: 200, Speed: 125}}, ch)
	if edited.HP != base.HP*150/100 || edited.MaxHP != edited.HP || edited.BaseSpeed != base.BaseSpeed*125/100 || edited.DamageScaleBP != 20000 || edited.DamageAddPct != base.DamageAddPct {
		t.Fatal("test profile not applied to simulation")
	}
}
