package mainserver

import (
	"time"

	"github.com/freaks/openfreaks/internal/content"
)

var megaBossRotation = []string{"karity_boss_character"}

func megaBossForDay(cat *content.CatalogData, now time.Time) *content.Character {
	if cat == nil || len(megaBossRotation) == 0 {
		return nil
	}
	day := now.UTC().Unix() / 86400
	index := day % int64(len(megaBossRotation))
	if index < 0 {
		index += int64(len(megaBossRotation))
	}
	return cat.CharByName[megaBossRotation[index]]
}
