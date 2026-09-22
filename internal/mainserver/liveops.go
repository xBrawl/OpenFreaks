package mainserver

import (
	"crypto/rand"
	"fmt"
	"github.com/freaks/openfreaks/internal/content"
	"github.com/freaks/openfreaks/internal/protocol"
	"github.com/freaks/openfreaks/internal/store"
	"math/big"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

var milestones = []int{10, 20, 30, 50, 100, 180, 365}
var reactorThresholds = []int{500, 1500, 3000, 5000, 8000}

func walletCurrency(a *store.Account, model int) *int32 {
	switch model {
	case 1:
		return &a.Wallet.Karit
	case 2:
		return &a.Wallet.Sparks
	case 3:
		return &a.Wallet.VoidGems
	case 4:
		return &a.Wallet.UpgradeTokens
	case 5:
		return &a.Wallet.PassPoints
	}
	return nil
}
func (s *Server) changeCurrency(a *store.Account, model, delta int, reason, ref string) bool {
	balance := walletCurrency(a, model)
	if balance == nil {
		return false
	}
	next := int64(*balance) + int64(delta)
	if next < 0 || next > 2147483647 {
		return false
	}
	*balance = int32(next)
	s.store.Meta.Ledger = append(s.store.Meta.Ledger, store.LedgerEntry{ID: rand.Text(), PlayerID: a.PlayerID, Reason: reason, Currency: int32(model - 1), Delta: int32(delta), Balance: *balance, Reference: ref, CreatedAt: time.Now().UnixMilli()})
	return true
}
func masteryLevel(points int) int {
	if points < 250 {
		return 1
	}
	if points < 750 {
		return 2
	}
	if points < 1500 {
		return 3
	}
	return 4
}
func (s *Server) touchDaily(a *store.Account, day int) (int, int, int) {
	p := s.store.Meta.Player(a.PlayerID)
	if p.LastVisit >= day {
		return 0, 0, 0
	}
	if p.LastVisit == day-1 {
		p.Streak++
	} else {
		p.Streak = 1
	}
	p.Best = max(p.Best, p.Streak)
	p.LastVisit = day
	rewards := map[int][2]int{10: {1, 100}, 20: {2, 50}, 30: {1, 200}, 50: {3, 20}, 100: {3, 50}, 180: {4, 100}, 365: {3, 200}}
	if r, ok := rewards[p.Streak]; ok && p.Milestones[p.Streak] == 0 {
		if s.changeCurrency(a, r[0], r[1], "daily_streak", fmt.Sprintf("daily-streak:%d", p.Streak)) {
			p.Milestones[p.Streak] = time.Now().Unix()
			return p.Streak, r[0], r[1]
		}
	}
	return 0, 0, 0
}
func (s *Server) onLiveOps(c *Conn) bool {
	a, ok := s.store.GetAccount(c.PlayerID)
	if !ok {
		return false
	}
	now := time.Now().Unix()
	milestone, model, amount := s.touchDaily(a, int(now/86400))
	p := s.store.Meta.Player(c.PlayerID)
	next := 0
	for _, n := range milestones {
		if n > p.Streak {
			next = n
			break
		}
	}
	w := protocol.NewWriter()
	w.WriteInt(int32(p.Streak))
	w.WriteInt(int32(p.Best))
	w.WriteInt(int32(next))
	w.WriteBool(p.LastSpin < int(now/86400))
	w.WriteInt(0)
	w.WriteLong(now)
	w.WriteInt(int32(milestone))
	w.WriteInt(int32(model))
	w.WriteInt(int32(amount))
	heroes := s.catData.Heroes()
	w.WriteInt(int32(len(heroes)))
	for _, ch := range heroes {
		m := p.Mastery[ch.Id]
		if m == nil {
			m = &store.Mastery{}
		}
		w.WriteInt(int32(ch.Id))
		w.WriteInt(int32(m.Points))
		w.WriteInt(int32(masteryLevel(m.Points)))
		w.WriteInt(int32(m.Matches))
		w.WriteInt(int32(m.Wins))
		w.WriteLong(m.Damage)
		ds := content.DialoguesFor(ch.Name)
		w.WriteInt(int32(len(ds)))
		for _, d := range ds {
			w.WriteString(d.ID)
			w.WriteInt(int32(ch.Id))
			w.WriteInt(int32(d.Unlock))
			w.WriteString(d.Text)
			w.WriteBool(p.Completed[d.ID] > 0)
		}
	}
	c.Send(protocol.SLiveOpsStateResponse, w.Bytes())
	if amount > 0 {
		return s.sendHome(c)
	}
	return true
}
func (s *Server) onCompleteDialogue(c *Conn, payload []byte) bool {
	r := protocol.NewReader(payload)
	charID, err := r.ReadInt()
	if err != nil {
		return false
	}
	id, _, err := r.ReadString()
	if err != nil || !r.Ended() {
		return false
	}
	p := s.store.Meta.Player(c.PlayerID)
	success := false
	a, _ := s.store.GetAccount(c.PlayerID)
	if ch := s.catData.CharByID[int(charID)]; ch != nil && a != nil && a.Characters[charID] != nil {
		points := 0
		if m := p.Mastery[ch.Id]; m != nil {
			points = m.Points
		}
		for _, d := range content.DialoguesFor(ch.Name) {
			if d.ID == id && points >= d.Unlock {
				success = true
				if p.Completed[id] == 0 {
					p.Completed[id] = time.Now().Unix()
				}
				break
			}
		}
	}
	w := protocol.NewWriter()
	w.WriteBool(success)
	w.WriteString(id)
	return c.Send(protocol.SCompleteCharacterDialogueResponse, w.Bytes())
}
func (s *Server) onDailySpin(c *Conn, payload []byte) bool {
	source, err := protocol.DecodeIntOnly(payload)
	if err != nil {
		return false
	}
	a, ok := s.store.GetAccount(c.PlayerID)
	if !ok {
		return false
	}
	p := s.store.Meta.Player(c.PlayerID)
	code, model, amount := 0, 0, 0
	success := false
	if source == 1 {
		code = 2
	} else if source != 0 {
		code = 3
	} else {
		day := int(time.Now().Unix() / 86400)
		s.touchDaily(a, day)
		if p.LastSpin >= day {
			code = 1
		} else {
			roll, e := rand.Int(rand.Reader, big.NewInt(100))
			if e != nil {
				return false
			}
			switch n := roll.Int64(); {
			case n < 70:
				model, amount = 1, 50
			case n < 90:
				model, amount = 2, 20
			case n < 98:
				model, amount = 4, 10
			default:
				model, amount = 3, 5
			}
			multiplier := 1
			switch {
			case p.Streak >= 365:
				multiplier = 10
			case p.Streak >= 100:
				multiplier = 5
			case p.Streak >= 50:
				multiplier = 3
			case p.Streak >= 10:
				multiplier = 2
			}
			amount *= multiplier
			success = s.changeCurrency(a, model, amount, "daily_spin", fmt.Sprintf("daily-spin:%d", day))
			if success {
				p.LastSpin = day
			} else {
				code = 3
				amount = 0
			}
		}
	}
	w := protocol.NewWriter()
	w.WriteBool(success)
	w.WriteInt(int32(code))
	w.WriteInt(int32(model))
	w.WriteInt(int32(amount))
	w.WriteInt(int32(p.Streak))
	w.WriteInt(int32(p.Best))
	c.Send(protocol.SDailySpinResponse, w.Bytes())
	if success {
		return s.sendHome(c)
	}
	return true
}

func bondLevel(matches int) int {
	if matches < 5 {
		return 1
	}
	if matches < 20 {
		return 2
	}
	if matches < 50 {
		return 3
	}
	if matches < 100 {
		return 4
	}
	return 5
}
func (s *Server) onBondState(c *Conn, payload []byte) bool {
	only, err := protocol.DecodeLongOnly(payload)
	if err != nil {
		return false
	}
	list := []*store.Bond{}
	for _, b := range s.store.Meta.Bonds {
		if (b.Low == c.PlayerID || b.High == c.PlayerID) && (only <= 0 || only == b.Low || only == b.High) {
			list = append(list, b)
		}
	}
	sort.Slice(list, func(i, j int) bool {
		return store.BondKey(list[i].Low, list[i].High) < store.BondKey(list[j].Low, list[j].High)
	})
	w := protocol.NewWriter()
	w.WriteInt(int32(len(list)))
	for _, b := range list {
		other := b.Low
		if other == c.PlayerID {
			other = b.High
		}
		a, _ := s.store.GetAccount(other)
		name := ""
		if a != nil {
			name = a.Name
		}
		level := bondLevel(b.Matches)
		title := b.Title
		if title == "" {
			title = []string{"Знакомые", "Напарники", "Связка", "Соучастники", "Неразлучные"}[level-1]
		}
		if b.Requester > 0 {
			title = "Предложение связи"
		}
		w.WriteLong(other)
		w.WriteString(name)
		w.WriteInt(int32(b.Type))
		w.WriteInt(int32(level))
		w.WriteInt(int32(b.Matches))
		w.WriteInt(int32(b.Wins))
		w.WriteLong(b.CreatedAt)
		w.WriteString(title)
		w.WriteString(b.PinnedBattle)
		w.WriteBool(b.Requester > 0 && b.Requester != c.PlayerID)
		w.WriteBool(b.Requester == c.PlayerID)
	}
	return c.Send(protocol.SBondStateResponse, w.Bytes())
}
func bondOperation(c *Conn, other int64, success bool) bool {
	w := protocol.NewWriter()
	w.WriteBool(success)
	code := 1
	if success {
		code = 0
	}
	w.WriteInt(int32(code))
	w.WriteLong(other)
	return c.Send(protocol.SBondOperationResponse, w.Bytes())
}
func (s *Server) onBondOffer(c *Conn, payload []byte) bool {
	r := protocol.NewReader(payload)
	other, err := r.ReadLong()
	if err != nil {
		return false
	}
	kind, err := r.ReadInt()
	if err != nil || !r.Ended() {
		return false
	}
	key := store.BondKey(c.PlayerID, other)
	success := other != c.PlayerID && kind >= 0 && kind <= 3 && s.store.FriendStatus(c.PlayerID, other) == 3 && s.store.Meta.Bonds[key] == nil
	if success {
		b := &store.Bond{Low: min(c.PlayerID, other), High: max(c.PlayerID, other), Requester: c.PlayerID, CreatedAt: time.Now().Unix(), Type: int(kind)}
		s.store.Meta.Bonds[key] = b
		if friend, ok := s.ConnByPlayer(other); ok {
			a, _ := s.store.GetAccount(c.PlayerID)
			w := protocol.NewWriter()
			w.WriteLong(c.PlayerID)
			w.WriteString(a.Name)
			w.WriteInt(kind)
			friend.Send(protocol.SBondInvitation, w.Bytes())
		}
	}
	return bondOperation(c, other, success)
}
func (s *Server) onBondReply(c *Conn, payload []byte) bool {
	r := protocol.NewReader(payload)
	other, err := r.ReadLong()
	if err != nil {
		return false
	}
	accept, err := r.ReadBool()
	if err != nil || !r.Ended() {
		return false
	}
	key := store.BondKey(c.PlayerID, other)
	b := s.store.Meta.Bonds[key]
	success := b != nil && b.Requester == other && other != c.PlayerID
	if success {
		if accept && s.store.FriendStatus(c.PlayerID, other) != 3 {
			success = false
		} else if accept {
			b.Requester = 0
			b.CreatedAt = time.Now().Unix()
		} else {
			delete(s.store.Meta.Bonds, key)
		}
	}
	if success {
		if peer, ok := s.ConnByPlayer(other); ok {
			bondOperation(peer, c.PlayerID, true)
		}
	}
	return bondOperation(c, other, success)
}
func (s *Server) onBondUpdate(c *Conn, payload []byte) bool {
	r := protocol.NewReader(payload)
	other, err := r.ReadLong()
	if err != nil {
		return false
	}
	kind, err := r.ReadInt()
	if err != nil {
		return false
	}
	title, _, err := r.ReadString()
	if err != nil {
		return false
	}
	pinned, _, err := r.ReadString()
	if err != nil || !r.Ended() {
		return false
	}
	title, pinned = strings.TrimSpace(title), strings.TrimSpace(pinned)
	b := s.store.Meta.Bonds[store.BondKey(c.PlayerID, other)]
	success := b != nil && b.Requester == 0 && kind >= 0 && kind <= 3 && utf8.RuneCountInString(title) <= 32 && len(pinned) <= 100
	if success && pinned != "" {
		success = false
		for _, battle := range s.store.BattlesFor(c.PlayerID, 10000) {
			if battle.BattleID != pinned {
				continue
			}
			for _, pid := range battle.Participants {
				if pid == other {
					success = true
				}
			}
		}
	}
	if success {
		b.Type = int(kind)
		b.Title = title
		b.PinnedBattle = pinned
	}
	return bondOperation(c, other, success)
}

func reactorSeason() int { return int(time.Now().Unix() / 86400 / 14) }
func reactorStage(points int) int {
	stage := 0
	for _, v := range reactorThresholds {
		if points >= v {
			stage++
		}
	}
	return stage
}
func (s *Server) playerAlliance(pid int64) (*store.Alliance, bool) {
	a, ok := s.store.GetAccount(pid)
	if !ok || a.AllianceID <= 0 {
		return nil, false
	}
	al, ok := s.store.GetAlliance(a.AllianceID)
	if !ok {
		return nil, false
	}
	_, ok = al.Members[pid]
	return al, ok
}
func (s *Server) onReactorState(c *Conn) bool {
	season := reactorSeason()
	id := int64(0)
	points, personal, matches := 0, 0, 0
	claimed := false
	if al, ok := s.playerAlliance(c.PlayerID); ok {
		id = al.ID
		r := s.store.Meta.Reactor(id, season)
		m := r.Member(c.PlayerID)
		points, personal, matches, claimed = r.Points, m.Points, m.Matches, m.Claimed || s.store.Meta.Player(c.PlayerID).ReactorClaims[season] > 0
	}
	w := protocol.NewWriter()
	w.WriteLong(id)
	w.WriteInt(int32(season))
	w.WriteLong(int64(season+1) * 14 * 86400)
	w.WriteInt(int32(points))
	w.WriteInt(int32(reactorStage(points)))
	w.WriteInt(int32(personal))
	w.WriteInt(int32(matches))
	w.WriteBool(personal >= 100 || matches >= 5)
	w.WriteBool(claimed)
	w.WriteBool(points >= 8000)
	return c.Send(protocol.SAllianceReactorStateResponse, w.Bytes())
}
func (s *Server) addReactor(al int64, season int, pid int64, points, matches int) {
	r := s.store.Meta.Reactor(al, season)
	m := r.Member(pid)
	r.Points = min(2000000000, r.Points+points)
	m.Points = min(2000000000, m.Points+points)
	m.Matches += matches
	for i, threshold := range reactorThresholds {
		if r.Points >= threshold && r.Unlocks[i+1] == 0 {
			r.Unlocks[i+1] = time.Now().Unix()
		}
	}
}
func reactorOperation(c *Conn, code, gems, tokens int) bool {
	w := protocol.NewWriter()
	w.WriteBool(code == 0)
	w.WriteInt(int32(code))
	w.WriteInt(int32(gems))
	w.WriteInt(int32(tokens))
	return c.Send(protocol.SAllianceReactorOperationResponse, w.Bytes())
}
func (s *Server) onReactorContribute(c *Conn, payload []byte) bool {
	amount, err := protocol.DecodeIntOnly(payload)
	if err != nil {
		return false
	}
	al, ok := s.playerAlliance(c.PlayerID)
	code := 1
	if ok && amount >= 10 && amount <= 10000 && amount%10 == 0 {
		a, _ := s.store.GetAccount(c.PlayerID)
		season := reactorSeason()
		if s.changeCurrency(a, 1, -int(amount), "alliance_reactor", fmt.Sprintf("reactor:%d", season)) {
			s.addReactor(al.ID, season, c.PlayerID, int(amount)/10, 0)
			code = 0
		}
	}
	reactorOperation(c, code, 0, 0)
	if code == 0 {
		return s.sendHome(c)
	}
	return true
}
func (s *Server) onReactorClaim(c *Conn) bool {
	season := reactorSeason()
	al, ok := s.playerAlliance(c.PlayerID)
	code, gems, tokens := 1, 0, 0
	if ok {
		r := s.store.Meta.Reactor(al.ID, season)
		m := r.Member(c.PlayerID)
		p := s.store.Meta.Player(c.PlayerID)
		switch {
		case m.Claimed || p.ReactorClaims[season] > 0:
			code = 3
		case r.Points < 8000 || (m.Points < 100 && m.Matches < 5):
			code = 2
		default:
			a, _ := s.store.GetAccount(c.PlayerID)
			if a.Wallet.VoidGems <= 2147483617 && a.Wallet.UpgradeTokens <= 2147483547 {
				ref := fmt.Sprintf("reactor-final:%d", season)
				s.changeCurrency(a, 3, 30, "reactor_final", ref)
				s.changeCurrency(a, 4, 100, "reactor_final", ref)
				m.Claimed = true
				p.ReactorClaims[season] = time.Now().Unix()
				code, gems, tokens = 0, 30, 100
			}
		}
	}
	reactorOperation(c, code, gems, tokens)
	if code == 0 {
		return s.sendHome(c)
	}
	return true
}
