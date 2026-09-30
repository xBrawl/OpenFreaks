package sim

import (
	"math"
	"testing"

	"github.com/freaks/openfreaks/internal/content"
)

func TestTestBalanceChangesAuthoritativeStats(t *testing.T) {
	cat := mustCatalog(t)
	ch := cat.Heroes()[0]
	w := &World{Catalog: cat}
	base := buildPlayer(cat, w, BuildPlayer{HPLevel: 1, DamageLevel: 1, SpeedLevel: 1}, ch)
	edited := buildPlayer(cat, w, BuildPlayer{HPLevel: 1, DamageLevel: 1, SpeedLevel: 1, TestBalance: content.TestBalance{HP: 150, Damage: 200, Speed: 125}}, ch)
	if edited.HP != base.HP*150/100 || edited.MaxHP != edited.HP || edited.DamageScaleBP != 20000 || edited.DamageAddPct != base.DamageAddPct {
		t.Fatal("test profile not applied to simulation")
	}
	// Скорость — через SpeedAdd: клиент видит только model.speed и SpeedAdd.
	want := DisplayedSpeed(base) * 1.25
	if edited.BaseSpeed != ch.Speed || math.Abs(DisplayedSpeed(edited)-want) > want*.01 {
		t.Fatalf("test speed: base %d add %d → %.0f, want %.0f", edited.BaseSpeed, edited.SpeedAdd, DisplayedSpeed(edited), want)
	}
	fast := buildPlayer(cat, w, BuildPlayer{HPLevel: 1, DamageLevel: 1, SpeedLevel: 1,
		TestBalance: content.TestBalance{HP: 10000, Damage: 10000, Speed: 21429, BasisPoints: true}}, ch)
	client := float64(ch.Speed) * (1 + float64(fast.SpeedAdd)/100)
	if math.Abs(client-DisplayedSpeed(fast)) > 1 || math.Abs(client-DisplayedSpeed(base)*2.1429) > DisplayedSpeed(base)*.02 {
		t.Fatalf("client predicts %.0f, server runs %.0f", client, DisplayedSpeed(fast))
	}
}
