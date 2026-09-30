package mainserver

import (
	"github.com/freaks/openfreaks/internal/protocol"
	"github.com/freaks/openfreaks/internal/store"
)

// profileStats — цифры экрана профиля: устранения, смерти (для эффективности),
// бои, время в бою и очки силы (сумма уровней всех бойцов).
//
// Счётчики карьеры появились позже побед, поэтому число боёв не меньше
// сыгранного по мастерству героев — оно копилось с самого начала.
func profileStats(acc *store.Account, meta *store.PlayerMeta) *protocol.ProfileStats {
	st := &protocol.ProfileStats{}
	if meta != nil {
		st.Kills = clampInt32(int64(meta.Career.Kills))
		st.Deaths = clampInt32(int64(meta.Career.Deaths))
		st.PlaySeconds = clampInt32(meta.Career.PlaySeconds)
		battles := meta.Career.Battles
		matches := 0
		for _, m := range meta.Mastery {
			if m != nil {
				matches += m.Matches
			}
		}
		st.Battles = clampInt32(int64(max(battles, matches)))
	}
	if acc != nil {
		power := int64(0)
		for _, ch := range acc.Characters {
			if ch != nil {
				power += int64(ch.ProgressionPoints) + 1
			}
		}
		st.PowerPoints = clampInt32(power)
	}
	return st
}

func clampInt32(v int64) int32 {
	return int32(min(max(v, 0), 1<<31-1))
}
