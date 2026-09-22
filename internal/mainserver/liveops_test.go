package mainserver

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"github.com/freaks/openfreaks/internal/content"
	"github.com/freaks/openfreaks/internal/protocol"
	"github.com/freaks/openfreaks/internal/store"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
)

func metaServer(t *testing.T) (*Server, *Conn, *store.Account) {
	t.Helper()
	cat, err := content.Parse(`../../deploy/content-real`)
	if err != nil {
		t.Fatal(err)
	}
	st := store.New("")
	a := st.CreateAccount("MetaAudit", []byte("token"))
	st.ConfigureHeroes([]int32{int32(cat.Heroes()[0].Id)})
	s := &Server{store: st, catData: cat, log: slog.New(slog.NewTextHandler(io.Discard, nil)), conns: map[int64]*Conn{}, byPlayer: map[int64]*Conn{}}
	c := &Conn{ID: 1, Server: s, PlayerID: a.PlayerID, SendCh: make(chan []byte, 4096)}
	c.Authed.Store(true)
	s.conns[1] = c
	s.byPlayer[a.PlayerID] = c
	return s, c, a
}
func intPayload(value int32) []byte  { w := protocol.NewWriter(); w.WriteInt(value); return w.Bytes() }
func longPayload(value int64) []byte { w := protocol.NewWriter(); w.WriteLong(value); return w.Bytes() }

func TestConcurrentDailySpinAwardsExactlyOnce(t *testing.T) {
	s, c, a := metaServer(t)
	before := a.Wallet
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !s.dispatch(c, protocol.CDailySpinRequest, intPayload(0)) {
				t.Error("spin dispatch failed")
			}
		}()
	}
	wg.Wait()
	successes := 0
	for len(c.SendCh) > 0 {
		frame := <-c.SendCh
		if int16(binary.BigEndian.Uint16(frame[:2])) == protocol.SDailySpinResponse && frame[6] != 0 {
			successes++
		}
	}
	if successes != 1 || len(s.store.Meta.Ledger) != 1 || a.Wallet == before {
		t.Fatalf("successes=%d ledger=%d wallet=%+v", successes, len(s.store.Meta.Ledger), a.Wallet)
	}
}
func TestStorageFailureRollsBackAndSendsNoSuccess(t *testing.T) {
	s, c, a := metaServer(t)
	before := a.Wallet
	s.store.SetCommitter(func(*store.Dump) error { return errors.New("injected storage outage") })
	if s.dispatch(c, protocol.CDailySpinRequest, intPayload(0)) {
		t.Fatal("acknowledged failed commit")
	}
	restored, _ := s.store.GetAccount(a.PlayerID)
	if restored.Wallet != before || len(c.SendCh) != 0 || s.Healthy() {
		t.Fatal("failed transaction escaped rollback")
	}
}
func TestDialogueUnlockAndBondConsent(t *testing.T) {
	s, c, a := metaServer(t)
	id := a.SelectedChar
	ch := s.catData.CharByID[int(id)]
	ds := content.DialoguesFor(ch.Name)
	w := protocol.NewWriter()
	w.WriteInt(id)
	w.WriteString(ds[1].ID)
	s.dispatch(c, protocol.CCompleteCharacterDialogueRequest, w.Bytes())
	if s.store.Meta.Player(a.PlayerID).Completed[ds[1].ID] > 0 {
		t.Fatal("locked dialogue completed")
	}
	b := s.store.CreateAccount("Friend", []byte("token2"))
	s.store.AddFriendReq(a.PlayerID, b.PlayerID)
	s.store.AddFriendReq(b.PlayerID, a.PlayerID)
	w = protocol.NewWriter()
	w.WriteLong(b.PlayerID)
	w.WriteInt(0)
	s.dispatch(c, protocol.CBondOfferRequest, w.Bytes())
	key := store.BondKey(a.PlayerID, b.PlayerID)
	if s.store.Meta.Bonds[key] == nil || s.store.Meta.Bonds[key].Requester != a.PlayerID {
		t.Fatal("proposal missing")
	}
	w = protocol.NewWriter()
	w.WriteLong(b.PlayerID)
	w.WriteBool(true)
	s.dispatch(c, protocol.CBondReplyRequest, w.Bytes())
	if s.store.Meta.Bonds[key].Requester == 0 {
		t.Fatal("requester accepted own invitation")
	}
	peer := &Conn{Server: s, PlayerID: b.PlayerID, SendCh: make(chan []byte, 16)}
	peer.Authed.Store(true)
	w = protocol.NewWriter()
	w.WriteLong(a.PlayerID)
	w.WriteBool(true)
	s.dispatch(peer, protocol.CBondReplyRequest, w.Bytes())
	if s.store.Meta.Bonds[key].Requester != 0 {
		t.Fatal("recipient could not accept")
	}
}
func TestReactorCannotClaimAgainAfterClanSwitch(t *testing.T) {
	s, c, a := metaServer(t)
	al := s.store.CreateAlliance("Audit", "", 0, a.PlayerID)
	season := reactorSeason()
	r := s.store.Meta.Reactor(al.ID, season)
	r.Points = 8000
	r.Member(a.PlayerID).Points = 100
	before := a.Wallet.VoidGems
	s.dispatch(c, protocol.CAllianceReactorClaimRequest, nil)
	if a.Wallet.VoidGems-before != 30 {
		t.Fatal("qualified claim failed")
	}
	second := s.store.CreateAlliance("Second", "", 0, a.PlayerID)
	r = s.store.Meta.Reactor(second.ID, season)
	r.Points = 8000
	r.Member(a.PlayerID).Points = 100
	s.dispatch(c, protocol.CAllianceReactorClaimRequest, nil)
	if a.Wallet.VoidGems-before != 30 {
		t.Fatal("clan hopping duplicated seasonal reward")
	}
}
func TestNativeMainProtocolFixtures(t *testing.T) {
	path := os.Getenv("OPENFREAKS_PROTOCOL_FIXTURES")
	if path == "" {
		t.Skip("fixture export not requested")
	}
	s, c, a := metaServer(t)
	s.dispatch(c, protocol.CLiveOpsStateRequest, nil)
	s.dispatch(c, protocol.CDailySpinRequest, intPayload(0))
	b := s.store.CreateAccount("Other", []byte("token"))
	s.store.Meta.Bonds[store.BondKey(a.PlayerID, b.PlayerID)] = &store.Bond{Low: a.PlayerID, High: b.PlayerID, Type: 1, CreatedAt: 1700000000, Matches: 17, Wins: 9}
	s.dispatch(c, protocol.CBondStateRequest, longPayload(b.PlayerID))
	al := s.store.CreateAlliance("Audit", "", 0, a.PlayerID)
	r := s.store.Meta.Reactor(al.ID, reactorSeason())
	r.Points = 3200
	r.Member(a.PlayerID).Points = 170
	s.dispatch(c, protocol.CAllianceReactorStateRequest, nil)
	type fixture struct {
		ID      int16
		Payload []byte
	}
	out := []fixture{}
	for len(c.SendCh) > 0 {
		f := <-c.SendCh
		out = append(out, fixture{int16(binary.BigEndian.Uint16(f[:2])), f[6:]})
	}
	raw, _ := json.Marshal(out)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}
