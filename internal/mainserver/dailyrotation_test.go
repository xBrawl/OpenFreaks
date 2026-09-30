package mainserver

import (
	"testing"
	"time"

	"github.com/freaks/openfreaks/internal/content"
)

func TestDailyPublicLocationIsStableAndRotates(t *testing.T) {
	locations := []*content.Location{
		{Id: 1, Name: "a", Mode: "showdown"},
		{Id: 2, Name: "b", Mode: "showdown"},
		{Id: 3, Name: "hidden", Mode: "showdown", Hidden: true},
		{Id: 4, Name: "duo", Mode: "duo_showdown"},
	}
	cat := &content.CatalogData{Locations: locations, LocByID: map[int]*content.Location{}}
	for _, loc := range locations {
		cat.LocByID[loc.Id] = loc
	}
	day := time.Unix(100*86400, 0).UTC()
	first := dailyPublicLocation(cat, 1, day)
	if first == nil || first.Id != 1 {
		t.Fatalf("day 100 picked %v", first)
	}
	if same := dailyPublicLocation(cat, 4, day); same == nil || same.Id != first.Id {
		t.Fatalf("variant escaped public rotation: %v != %v", same, first)
	}
	if next := dailyPublicLocation(cat, 1, day.Add(24*time.Hour)); next == nil || next.Id != 2 {
		t.Fatalf("next UTC day did not rotate: %v", next)
	}
}

func TestDailyPublicLocationRejectsHiddenAndUnknown(t *testing.T) {
	hidden := &content.Location{Id: 1, Mode: "bounty", Hidden: true}
	cat := &content.CatalogData{Locations: []*content.Location{hidden}, LocByID: map[int]*content.Location{1: hidden}}
	if got := dailyPublicLocation(cat, 1, time.Now()); got != nil {
		t.Fatalf("hidden location accepted: %v", got)
	}
	if got := dailyPublicLocation(cat, 99, time.Now()); got != nil {
		t.Fatalf("unknown location accepted: %v", got)
	}
}

// Карта, снятая с ротации, но оставшаяся выбранной у клиента, ведёт на карту
// дня того же режима, а не отменяет поиск.
func TestDailyPublicLocationRedirectsHiddenToVisibleSibling(t *testing.T) {
	hidden := &content.Location{Id: 1, Mode: "bounty", Hidden: true}
	visible := &content.Location{Id: 2, Mode: "bounty"}
	cat := &content.CatalogData{Locations: []*content.Location{hidden, visible}, LocByID: map[int]*content.Location{1: hidden, 2: visible}}
	if got := dailyPublicLocation(cat, 1, time.Now()); got != visible {
		t.Fatalf("hidden location was not redirected: %v", got)
	}
}
