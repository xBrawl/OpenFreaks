package mainserver

import (
	"github.com/freaks/openfreaks/internal/content"
	"github.com/freaks/openfreaks/internal/protocol"
)

// Experimental TCP extension: C63(character,hp%,damage%,speed%); S63(success,character,applied values).
//
// Query forms: (-1,-1,-1) answers in percents, (-1,-1,-2) answers in basis points.
// The second one is how a new client probes the server: an older server takes
// -2 for an invalid value and answers success=false instead of dropping the
// connection, which it would do on a fifth int.
func (s *Server) onTestBalance(c *Conn, payload []byte) bool {
	r := protocol.NewReader(payload)
	id, e1 := r.ReadInt()
	hp, e2 := r.ReadInt()
	dmg, e3 := r.ReadInt()
	speed, e4 := r.ReadInt()
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
		return false
	}
	// A fifth int marks a client that speaks basis points (see content.TestBalance);
	// older clients stop after four and keep whole percents.
	var unit int32 = 100
	if !r.Ended() {
		u, err := r.ReadInt()
		if err != nil || !r.Ended() || u != content.BalanceUnit {
			return false
		}
		unit = u
	}
	probe := unit == 100 && hp == -1 && dmg == -1 && speed == -2
	basis := unit == content.BalanceUnit || probe
	query := hp == -1 && dmg == -1 && (speed == -1 || probe)
	b := content.TestBalance{HP: int(hp), Damage: int(dmg), Speed: int(speed), BasisPoints: basis && !(hp == 0 && dmg == 0 && speed == 0)}
	account, exists := s.store.GetAccount(c.PlayerID)
	valid := s.cfg.Testing.EnableBalanceOverrides && exists && (query || b.Valid()) && c.BattleID == ""
	if valid {
		_, valid = account.Characters[id]
	}
	m := s.store.Meta.Player(c.PlayerID)
	if valid && !query {
		if m.TestBalance == nil {
			m.TestBalance = map[int32]content.TestBalance{}
		}
		if b == (content.TestBalance{}) {
			delete(m.TestBalance, id)
		} else {
			m.TestBalance[id] = b
		}
	}
	applied := m.TestBalance[id].Normalized()
	if basis {
		applied = m.TestBalance[id].Scaled()
	}
	w := protocol.NewWriter()
	w.WriteBool(valid)
	w.WriteInt(id)
	w.WriteInt(int32(applied.HP))
	w.WriteInt(int32(applied.Damage))
	w.WriteInt(int32(applied.Speed))
	return c.Send(63, w.Bytes())
}

func (s *Server) balanceFor(player int64, character int32) content.TestBalance {
	if !s.cfg.Testing.EnableBalanceOverrides {
		return content.TestBalance{}
	}
	return s.store.Meta.Player(player).TestBalance[character]
}
