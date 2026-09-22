package pgstore

import (
	"encoding/json"
	"fmt"
	"github.com/freaks/openfreaks/internal/store"
)

type historyPlayer struct {
	PlayerID                                int64 `json:"PlayerId"`
	PlayerName                              string
	CharacterModelID                        int32 `json:"CharacterModelId"`
	HitPointsLevel, DamageLevel, SpeedLevel int32
	Team                                    int
	Rank, Trophies, TrophyDelta             int32
}

func encodeHistoryPlayers(b store.BattleRecord) (string, error) {
	rows := make([]historyPlayer, 0, len(b.Players))
	for _, p := range b.Players {
		rows = append(rows, historyPlayer{p.PlayerID, p.Name, p.CharacterModelID, p.HPLevel, p.DamageLevel, p.SpeedLevel, p.Team, p.Rank, p.Trophies, b.TrophyDeltas[p.PlayerID]})
	}
	data, err := json.Marshal(rows)
	return string(data), err
}
func decodeHistoryPlayers(b *store.BattleRecord, data string) error {
	var rows []historyPlayer
	if err := json.Unmarshal([]byte(data), &rows); err != nil {
		return fmt.Errorf("battle %s history: %w", b.BattleID, err)
	}
	for i, p := range rows {
		b.Players = append(b.Players, store.BattleParticipant{PlayerID: p.PlayerID, Name: p.PlayerName, CharacterModelID: p.CharacterModelID, HPLevel: p.HitPointsLevel, DamageLevel: p.DamageLevel, SpeedLevel: p.SpeedLevel, Team: p.Team, Index: i, Rank: p.Rank, Trophies: p.Trophies, IsBot: p.PlayerID <= 0})
		if p.PlayerID > 0 {
			b.TrophyDeltas[p.PlayerID] = p.TrophyDelta
			b.Ranks[p.PlayerID] = p.Rank
		}
	}
	return nil
}
