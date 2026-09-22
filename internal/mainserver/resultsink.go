package mainserver

import "github.com/freaks/openfreaks/internal/battleserver"

func (s *Server) HandleBattleResult(r *battleserver.Result) bool {
	if s.mm != nil {
		return s.mm.OnBattleResult(r)
	}
	return false
}
