package pgstore

import (
	"context"
	"fmt"
	"github.com/freaks/openfreaks/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// First Go startup imports the live C# progression. After migration the atomic
// checkpoint is canonical; running C# and Go writers on the same DB is forbidden.
func loadLegacyMeta(ctx context.Context, pool *pgxpool.Pool, d *store.Dump) error {
	m := store.NewMeta()
	d.Meta = m
	query := func(sql string, scan func(pgx.Rows) error) error {
		rows, err := pool.Query(ctx, sql)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if err = scan(rows); err != nil {
				return err
			}
		}
		return rows.Err()
	}
	tasks := []struct {
		sql  string
		scan func(pgx.Rows) error
	}{
		{`SELECT player_id,character_model_id,points,matches,wins,damage,role_score,style_score FROM character_mastery`, func(r pgx.Rows) error {
			var p int64
			var c int
			v := &store.Mastery{}
			if err := r.Scan(&p, &c, &v.Points, &v.Matches, &v.Wins, &v.Damage, &v.RoleScore, &v.StyleScore); err != nil {
				return err
			}
			m.Player(p).Mastery[c] = v
			return nil
		}},
		{`SELECT player_id,dialogue_id,completed_at FROM completed_character_dialogues`, func(r pgx.Rows) error {
			var p, at int64
			var id string
			if err := r.Scan(&p, &id, &at); err != nil {
				return err
			}
			m.Player(p).Completed[id] = at
			return nil
		}},
		{`SELECT player_id,current_streak,best_streak,last_visit_day,last_free_spin_day FROM daily_engagement`, func(r pgx.Rows) error {
			var p int64
			var streak, best, visit, spin int
			if err := r.Scan(&p, &streak, &best, &visit, &spin); err != nil {
				return err
			}
			v := m.Player(p)
			v.Streak, v.Best, v.LastVisit, v.LastSpin = streak, best, visit, spin
			return nil
		}},
		{`SELECT player_id,milestone,claimed_at FROM daily_streak_rewards`, func(r pgx.Rows) error {
			var p, at int64
			var n int
			if err := r.Scan(&p, &n, &at); err != nil {
				return err
			}
			m.Player(p).Milestones[n] = at
			return nil
		}},
		{`SELECT player_low,player_high,bond_type,created_at,matches,wins,custom_title,pinned_battle_id FROM player_bonds`, func(r pgx.Rows) error {
			b := &store.Bond{}
			if err := r.Scan(&b.Low, &b.High, &b.Type, &b.CreatedAt, &b.Matches, &b.Wins, &b.Title, &b.PinnedBattle); err != nil {
				return err
			}
			m.Bonds[store.BondKey(b.Low, b.High)] = b
			return nil
		}},
		{`SELECT requester_id,target_id,bond_type,created_at FROM bond_requests`, func(r pgx.Rows) error {
			b := &store.Bond{}
			var target int64
			if err := r.Scan(&b.Requester, &target, &b.Type, &b.CreatedAt); err != nil {
				return err
			}
			b.Low, b.High = min(b.Requester, target), max(b.Requester, target)
			key := store.BondKey(b.Low, b.High)
			if m.Bonds[key] == nil {
				m.Bonds[key] = b
			}
			return nil
		}},
		{`SELECT alliance_id,season_key,points FROM alliance_reactors`, func(r pgx.Rows) error {
			var id int64
			var season, points int
			if err := r.Scan(&id, &season, &points); err != nil {
				return err
			}
			m.Reactor(id, season).Points = points
			return nil
		}},
		{`SELECT alliance_id,season_key,player_id,points,matches,claimed FROM alliance_reactor_members`, func(r pgx.Rows) error {
			var id, pid int64
			var season int
			v := &store.ReactorMember{}
			if err := r.Scan(&id, &season, &pid, &v.Points, &v.Matches, &v.Claimed); err != nil {
				return err
			}
			m.Reactor(id, season).Members[pid] = v
			if v.Claimed {
				m.Player(pid).ReactorClaims[season] = 1
			}
			return nil
		}},
		{`SELECT alliance_id,season_key,stage,unlocked_at FROM alliance_reactor_unlocks`, func(r pgx.Rows) error {
			var id, at int64
			var season, stage int
			if err := r.Scan(&id, &season, &stage, &at); err != nil {
				return err
			}
			m.Reactor(id, season).Unlocks[stage] = at
			return nil
		}},
		{`SELECT player_id,offer_model_id,purchased_at FROM offer_purchases`, func(r pgx.Rows) error {
			var pid, at int64
			var id int
			if err := r.Scan(&pid, &id, &at); err != nil {
				return err
			}
			m.Player(pid).Offers[id] = at
			return nil
		}},
		{`SELECT player_id,season_index,season_end_timestamp,purchased_tier,claimed_free,claimed_premium,claimed_premium_plus FROM battle_pass_progress`, func(r pgx.Rows) error {
			var pid int64
			var p store.PassProgress
			if err := r.Scan(&pid, &p.Season, &p.EndsAt, &p.Tier, &p.ClaimedFree, &p.ClaimedPremium, &p.ClaimedPremiumPlus); err != nil {
				return err
			}
			m.Player(pid).Pass = p
			return nil
		}},
		{`SELECT player_id,mission_model_id,duration_type,target_game_mode_model_id,target_character_model_id,reward,current_progress,target_progress,start_timestamp,end_timestamp FROM mission_progress`, func(r pgx.Rows) error {
			var pid int64
			v := &store.MissionProgress{}
			if err := r.Scan(&pid, &v.ModelID, &v.DurationType, &v.ModeID, &v.CharacterID, &v.Reward, &v.Progress, &v.Target, &v.Start, &v.End); err != nil {
				return err
			}
			v.Claimed = v.Progress >= v.Target
			m.Player(pid).Missions[fmt.Sprintf("%d:%d", v.ModelID, v.DurationType)] = v
			return nil
		}},
		{`SELECT DISTINCT battle_id FROM battle_results`, func(r pgx.Rows) error {
			var id string
			if err := r.Scan(&id); err != nil {
				return err
			}
			m.Receipts["battle:"+id] = 1
			return nil
		}},
	}
	for i, t := range tasks {
		if err := query(t.sql, t.scan); err != nil {
			return fmt.Errorf("legacy progression import %d: %w", i, err)
		}
	}
	return nil
}
