package mainserver

import (
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/freaks/openfreaks/internal/battleserver"
	"github.com/freaks/openfreaks/internal/config"
	"github.com/freaks/openfreaks/internal/content"
	"github.com/freaks/openfreaks/internal/protocol"
	"github.com/freaks/openfreaks/internal/store"
)

func sendFrame(t *testing.T, c net.Conn, id int16, payload []byte) {
	t.Helper()
	_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Write(protocol.EncodeTCPFrame(id, payload)); err != nil {
		t.Fatalf("write %d: %v", id, err)
	}
}

func readFrame(t *testing.T, c net.Conn) (int16, []byte) {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(30 * time.Second))
	h := make([]byte, 6)
	off := 0
	for off < 6 {
		n, err := c.Read(h[off:])
		if err != nil {
			t.Fatalf("read header: %v", err)
		}
		off += n
	}
	id, n, err := protocol.DecodeTCPHeader(h)
	if err != nil {
		t.Fatalf("decode header: %v", err)
	}
	p := make([]byte, n)
	off = 0
	for off < len(p) {
		m, err := c.Read(p[off:])
		if err != nil {
			t.Fatalf("read payload: %v", err)
		}
		off += m
	}
	return id, p
}

func emptyPayload() []byte {
	w := protocol.NewWriter()
	w.WriteByte(0)
	return w.Bytes()
}

var smokeSequence atomic.Int64

func TestSmokeFullFlow(t *testing.T) {
	cfg := config.Default()
	cfg.Main.Network.TCPBind = "127.0.0.1:19609"
	cfg.Main.Network.InternalHTTPBind = "127.0.0.1:19709"
	cfg.Main.Authentication.EnableTestRegistration = true
	cfg.Main.Matchmaking.MinPlayers = 1
	cfg.Main.Testing.EnableBots = true
	cfg.Main.Matchmaking.BotFillSeconds = 1
	cfg.Battle.Network.UDPBind = "127.0.0.1:19610"
	cfg.Battle.Network.UDPAdvertisedHost = "127.0.0.1"
	cfg.Battle.Network.UDPAdvertisedPort = 19610

	endpoint := os.Getenv("OPENFREAKS_SMOKE_MAIN")
	external := endpoint != ""
	if !external {
		dir, _ := os.MkdirTemp("", "openfreaks-smoke")
		defer os.RemoveAll(dir)
		st := store.New(dir)
		cat := content.Load("content-that-does-not-exist")
		catData, _ := content.Parse("content-that-does-not-exist")

		var mainSrv *Server
		bcfg := battleserver.Config{
			UDPBind: "127.0.0.1:19610", AdvertisedHost: "127.0.0.1", AdvertisedPort: 19610,
			MaxPeers: 64, MaxPerAddress: 8, HandshakeTimeout: 10 * time.Second,
			PeerTimeout: 8 * time.Second, TickMs: 50, IntroTicks: 5,
			ReadinessTimeout: 5 * time.Second, MaxMatches: 16,
		}
		bs := battleserver.NewServerWithCatalog(bcfg, nil, func(r *battleserver.Result) {
			if mainSrv != nil {
				mainSrv.HandleBattleResult(r)
			}
		}, catData)
		mainSrv = NewServer(cfg.Main, st, cat, catData, bs, "127.0.0.1", 19610, nil, nil)
		go func() { _ = bs.ListenAndServe() }()
		go func() { _ = mainSrv.ListenAndServe() }()
		t.Cleanup(mainSrv.Close)
		t.Cleanup(bs.Close)
		time.Sleep(300 * time.Millisecond)

		endpoint = "127.0.0.1:19609"
	}
	c, err := net.Dial("tcp", endpoint)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	w := protocol.NewWriter()
	w.WriteInt(123)
	sendFrame(t, c, protocol.CClientHello, w.Bytes())
	if id, _ := readFrame(t, c); id != protocol.SServerHello {
		t.Fatalf("hello id=%d", id)
	}

	sendFrame(t, c, protocol.CFingerprintRequest, emptyPayload())
	if id, _ := readFrame(t, c); id != protocol.SFingerprintResponse {
		t.Fatalf("fp id=%d", id)
	}
	sendFrame(t, c, protocol.CModelsRequest, emptyPayload())
	if id, _ := readFrame(t, c); id != protocol.SModelsResponse {
		t.Fatalf("models id=%d", id)
	}

	tok := "test-login-v1:" + base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf(`{"nickname":"smoke%d_%d","password":"pass1234","register":true,"invitationCode":"VERITYINFREAKS"}`, time.Now().UnixMilli(), smokeSequence.Add(1))))
	w = protocol.NewWriter()
	w.WriteLong(-1)
	w.WriteString(tok)
	w.WriteInt(1)
	sendFrame(t, c, protocol.CLogin, w.Bytes())
	id, p := readFrame(t, c)
	if id != protocol.SLoginOk {
		t.Fatalf("login id=%d payload=%x", id, p)
	}
	lr := protocol.NewReader(p)
	pid, _ := lr.ReadLong()
	if pid <= 0 {
		t.Fatalf("bad pid %d", pid)
	}
	if id, _ := readFrame(t, c); id != protocol.SHomeData {
		t.Fatalf("home id=%d", id)
	}

	w = protocol.NewWriter()
	w.WriteInt(1)
	w.WriteBool(false)
	w.WriteBool(false)
	sendFrame(t, c, protocol.CStartBattle, w.Bytes())
	var session int64
	var host string
	var port int32
	deadline := time.Now().Add(45 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatalf("no BattleStarted")
		}
		id, p := readFrame(t, c)
		if id == protocol.SBattleStarted {
			r := protocol.NewReader(p)
			pt, _ := r.ReadInt()
			_ = pt
			loc, _, _ := r.ReadString()
			_ = loc
			oi, _ := r.ReadInt()
			ot, _ := r.ReadInt()
			_ = oi
			_ = ot
			n, _ := r.ReadCollectionCount()
			for i := 0; i < n; i++ {

				_, _, _ = r.ReadString()
				for j := 0; j < 8; j++ {
					_, _ = r.ReadInt()
				}
				cn, _ := r.ReadCollectionCount()
				for j := 0; j < cn; j++ {
					_, _ = r.ReadInt()
				}
			}
			host, _, _ = r.ReadString()
			port, _ = r.ReadInt()
			session, _ = r.ReadLong()
			break
		}
		if id != protocol.SMatchmaking {
			t.Fatalf("unexpected id=%d", id)
		}
	}
	if host == "" || port <= 0 || session == 0 || (!external && port != 19610) {
		t.Fatalf("bad battle endpoint %s:%d sess=%d", host, port, session)
	}

	assertNativeBattleSnapshot(t, host, int(port), session)
	sendFrame(t, c, protocol.CExitBattle, emptyPayload())

	fmt.Println("smoke OK")
}

func TestExternal20ConcurrentPlayers(t *testing.T) {
	if os.Getenv("OPENFREAKS_LOAD20") != "1" || os.Getenv("OPENFREAKS_SMOKE_MAIN") == "" {
		t.Skip("explicit external load test only")
	}
	for i := 0; i < 20; i++ {
		t.Run(fmt.Sprint(i), func(t *testing.T) { t.Parallel(); TestSmokeFullFlow(t) })
	}
}
