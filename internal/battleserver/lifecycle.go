package battleserver

import "fmt"

func (s *Server) RoomStatus(ids []string) (map[string]bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]bool{}
	for _, id := range ids {
		out[id] = s.matches[id] != nil
	}
	return out, nil
}

type BattleGrant struct {
	BattleID          string
	SessionID         int64
	LocationName      string
	Host              string
	Port              int
	OwnIndex, OwnTeam int
	Players           []*Participant
}

func (s *Server) Resume(battleID string, playerID int64) (*BattleGrant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.matches[battleID]
	if m == nil {
		return nil, fmt.Errorf("battle no longer active")
	}
	m.peerMu.Lock()
	defer m.peerMu.Unlock()
	grant := &BattleGrant{BattleID: battleID, LocationName: m.World.LocationName, Host: s.cfg.AdvertisedHost, Port: s.cfg.AdvertisedPort}
	found := false
	for i := 0; i < len(m.parts); i++ {
		p := m.parts[i]
		if p == nil {
			continue
		}
		grant.Players = append(grant.Players, p)
		if p.PlayerID == playerID && !p.IsBot && !m.departed[p.Index] {
			found = true
			grant.SessionID = m.SessionIDs[p.Index]
			grant.OwnIndex = p.Index
			grant.OwnTeam = p.Team
		}
	}
	if !found {
		return nil, fmt.Errorf("participant unavailable")
	}
	return grant, nil
}
// Emote — игрок кинул эмоут в бою (эмоуты идут через главный сервер). Бою он
// нужен для щита эмодзи титана Карити (sim.KarityEmote).
func (s *Server) Emote(battleID string, playerID int64) bool {
	s.mu.Lock()
	m := s.matches[battleID]
	s.mu.Unlock()
	if m == nil {
		return false
	}
	index := -1
	for _, p := range m.parts {
		if p.PlayerID == playerID && !p.IsBot {
			index = p.Index
			break
		}
	}
	if index < 0 {
		return false
	}
	m.worldMu.Lock()
	defer m.worldMu.Unlock()
	m.World.KarityEmote(index)
	return true
}

func (s *Server) Leave(battleID string, playerID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.matches[battleID]
	if m == nil {
		return true
	}
	m.peerMu.Lock()
	defer m.peerMu.Unlock()
	for _, p := range m.parts {
		if p.PlayerID == playerID && !p.IsBot {
			m.departed[p.Index] = true
			sid := m.SessionIDs[p.Index]
			delete(s.grants, sid)
			if peer := s.peers[sid]; peer != nil {
				peer.Close()
				s.byAddr[peer.Addr.IP.String()]--
				delete(s.peers, sid)
			}
			return true
		}
	}
	return false
}
