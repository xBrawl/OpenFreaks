package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/freaks/openfreaks/internal/battleserver"
	"github.com/freaks/openfreaks/internal/config"
	"github.com/freaks/openfreaks/internal/content"
	"github.com/freaks/openfreaks/internal/internalapi"
	"github.com/freaks/openfreaks/internal/mainserver"
	"github.com/freaks/openfreaks/internal/pgstore"
	"github.com/freaks/openfreaks/internal/resultoutbox"
	"github.com/freaks/openfreaks/internal/store"
)

type HTTPBattleAllocator struct {
	baseURL string
	secret  string
	timeout time.Duration
}

func (h *HTTPBattleAllocator) Allocate(req *battleserver.AllocateRequest) (map[int]int64, string, int, string) {
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequest("POST", h.baseURL+"/allocate", bytes.NewReader(body))
	if err != nil {
		return nil, "", 0, err.Error()
	}
	httpReq.Header.Set(internalapi.SecretHeader, h.secret)
	httpReq.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: h.timeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, "", 0, err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, "", 0, fmt.Sprintf("battle http status %d", resp.StatusCode)
	}
	var out struct {
		Sessions map[int]int64 `json:"sessions"`
		Host     string        `json:"host"`
		Port     int           `json:"port"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, "", 0, err.Error()
	}
	return out.Sessions, out.Host, out.Port, ""
}
func (h *HTTPBattleAllocator) OpenReplay(playerID int64, battleID string) (*battleserver.ReplayGrant, error) {
	body, err := json.Marshal(map[string]any{"PlayerID": playerID, "BattleID": battleID})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", h.baseURL+"/replay", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set(internalapi.SecretHeader, h.secret)
	response, err := (&http.Client{Timeout: h.timeout}).Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, fmt.Errorf("replay status %d", response.StatusCode)
	}
	var grant battleserver.ReplayGrant
	err = json.NewDecoder(response.Body).Decode(&grant)
	return &grant, err
}

func (h *HTTPBattleAllocator) lifecycle(path, battleID string, playerID int64, out any) error {
	body, err := json.Marshal(map[string]any{"BattleID": battleID, "PlayerID": playerID})
	if err != nil {
		return err
	}
	request, err := http.NewRequest("POST", h.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set(internalapi.SecretHeader, h.secret)
	response, err := (&http.Client{Timeout: h.timeout}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("battle lifecycle status %d", response.StatusCode)
	}
	return json.NewDecoder(response.Body).Decode(out)
}
func (h *HTTPBattleAllocator) Resume(id string, pid int64) (*battleserver.BattleGrant, error) {
	var grant battleserver.BattleGrant
	err := h.lifecycle("/resume", id, pid, &grant)
	return &grant, err
}
func (h *HTTPBattleAllocator) Leave(id string, pid int64) bool {
	var result map[string]any
	return h.lifecycle("/leave", id, pid, &result) == nil
}

func (h *HTTPBattleAllocator) RoomStatus(ids []string) (map[string]bool, error) {
	body, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequest("POST", h.baseURL+"/rooms", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set(internalapi.SecretHeader, h.secret)
	response, err := (&http.Client{Timeout: 2 * time.Second}).Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, fmt.Errorf("room status %d", response.StatusCode)
	}
	var result map[string]bool
	err = json.NewDecoder(response.Body).Decode(&result)
	return result, err
}

const shutdownGrace = 20 * time.Second

func main() {
	var (
		cfgPath = flag.String("config", "deploy/config.local.json", "path to unified JSON config")
		mode    = flag.String("mode", "all", "all|main|battle")
	)
	flag.Parse()
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Error("config load failed", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, cfg, *mode, log); err != nil {
		log.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

func battleConfig(cfg config.Root) battleserver.Config {
	b := cfg.Battle
	return battleserver.Config{
		UDPBind: b.Network.UDPBind, AdvertisedHost: b.Network.UDPAdvertisedHost,
		ReplayDir:      filepath.Join(b.Persistence.ResultOutboxPath, "replays"),
		AdvertisedPort: b.Network.UDPAdvertisedPort,
		MaxPeers:       max1(b.Network.MaxPeers, 64), MaxPerAddress: max1(b.Network.MaxPeersPerAddress, 8),
		HandshakeTimeout: time.Duration(max1(b.Network.HandshakeTimeoutSec, 10)) * time.Second,
		PeerTimeout:      time.Duration(max1(b.Network.PeerTimeoutSec, 8)) * time.Second,
		TickMs:           max1(b.Simulation.TickMilliseconds, 50),
		IntroTicks:       max1(b.Simulation.IntroTicks, 20),
		ReadinessTimeout: time.Duration(max1(b.ReadinessTimeoutSeconds, 30)) * time.Second,
		MaxMatches:       max1(b.Simulation.MaxMatches, 16),
		MaxCatchUpTicks:  b.Simulation.MaxCatchUpTicks,
	}
}

func max1(v, d int) int {
	if v <= 0 {
		return d
	}
	return v
}

// openPostgres restores state before serving clients; all accepted mutations
// commit synchronously. The advisory lease rejects duplicate Main writers.
func openPostgres(cfg config.Root, st *store.Store, log *slog.Logger) (*pgxpool.Pool, func(), error) {
	env := cfg.Main.Persistence.ConnectionStringEnv
	if env == "" {
		env = "XBRAWL_DATABASE_URL"
	}
	url := os.Getenv(env)
	if url == "" {
		return nil, nil, fmt.Errorf("PostgreSQL connection required in %s", env)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgstore.PoolWithSchema(ctx, url, cfg.Main.Persistence.DatabaseSchema)
	if err != nil {
		return nil, nil, err
	}
	if pool.Config().MaxConns < 2 {
		pool.Close()
		return nil, nil, fmt.Errorf("PostgreSQL pool needs at least two connections")
	}
	if err := pgstore.Migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, nil, err
	}
	lease, err := pgstore.AcquireWriter(ctx, pool)
	if err != nil {
		pool.Close()
		return nil, nil, err
	}
	cleanup := func() { lease.Close(); pool.Close() }
	dump, err := pgstore.Load(ctx, pool)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	st.RestoreState(dump)
	st.SetCommitter(func(d *store.Dump) error {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return pgstore.PersistOwned(ctx, pool, d, lease.Token)
	})
	log.Info("postgres backend active", "accounts", len(dump.Accounts))
	return pool, cleanup, nil
}

func run(ctx context.Context, cfg config.Root, mode string, log *slog.Logger) error {
	if mode != "all" && mode != "main" && mode != "battle" {
		return fmt.Errorf("unknown mode %q", mode)
	}

	if cfg.Battle.Simulation.TickMilliseconds != 50 {
		return fmt.Errorf("tickMilliseconds must be 50")
	}
	if mode == "main" && !cfg.Main.BattleServer.Enabled {
		mode = "all"
	}
	root := cfg.Main.Content.Root
	if mode == "battle" {
		root = cfg.Battle.Content.Root
	}
	cat, err := content.Parse(root)
	if err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var mainSrv *mainserver.Server
	var battleSrv *battleserver.Server
	var st *store.Store
	if mode != "main" {
		battleSrv = battleserver.NewServerWithCatalog(battleConfig(cfg), log, nil, cat)
		defer battleSrv.Close()
	}
	if mode != "battle" {
		st = store.New("")
		_, cleanup, err := openPostgres(cfg, st, log)
		if err != nil {
			return err
		}
		defer cleanup()
		var allocator mainserver.BattleAllocator = battleSrv
		if mode == "main" {
			secret := cfg.Main.BattleServer.SharedSecret
			if secret == "" {
				secret = cfg.Main.InternalSecret()
			}
			allocator = &HTTPBattleAllocator{baseURL: cfg.Main.BattleServer.BaseURL, secret: secret, timeout: 5 * time.Second}
		}
		mainSrv = mainserver.NewServer(cfg.Main, st, content.Load(root), cat, allocator, cfg.Battle.Network.UDPAdvertisedHost, cfg.Battle.Network.UDPAdvertisedPort, log, nil)
		defer mainSrv.Close()
	}
	if battleSrv != nil {
		deliver := func(result *battleserver.Result) error {
			if mainSrv != nil {
				if mainSrv.HandleBattleResult(result) {
					return nil
				}
				return fmt.Errorf("main did not commit result")
			}
			target := cfg.Battle.MainServer
			secret := target.SharedSecret
			if secret == "" {
				secret = cfg.Battle.InternalSecret()
			}
			if target.BaseURL == "" {
				return fmt.Errorf("main endpoint missing")
			}
			return internalapi.PostResult(target.BaseURL, secret, result, 5*time.Second, 0, 0)
		}
		box, err := resultoutbox.New(filepath.Join(cfg.Battle.Persistence.ResultOutboxPath, "results"), deliver, log)
		if err != nil {
			return err
		}
		go box.Run()
		defer box.Close()
		battleSrv.SetOnResult(func(result *battleserver.Result) {

			for {
				if err := box.Submit(result); err == nil {
					return
				} else {
					battleSrv.Drain()
					log.Error("result outbox write failed", "battle", result.BattleID, "err", err)
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Second):
				}
			}
		})
	}
	exits := make(chan error, 8)
	launch := func(name string, serve func() error) {
		go func() {
			err := serve()
			if err != nil {
				err = fmt.Errorf("%s: %w", name, err)
			}
			exits <- err
		}()
	}
	var webServers []*http.Server
	defer func() {
		for _, server := range webServers {
			_ = server.Close()
		}
	}()
	serveHTTP := func(name, addr string, handler http.Handler) {
		server := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
		webServers = append(webServers, server)
		launch(name, server.ListenAndServe)
	}
	if battleSrv != nil {
		launch("battle UDP", battleSrv.ListenAndServe)
		if cfg.Battle.Network.WebSocketBind != "" {
			mux := http.NewServeMux()
			mux.Handle("/ws/battle", battleSrv.WebSocketHandler(ctx, cfg.Battle.Network.WebSocketOrigins))
			serveHTTP("battle WebSocket", cfg.Battle.Network.WebSocketBind, mux)
		}
		if cfg.Battle.Network.InternalHTTPEnabled {
			serveHTTP("battle HTTP", cfg.Battle.Network.InternalHTTPBind, internalapi.NewBattleHTTP(battleSrv, cfg.Battle.InternalSecret()).Handler())
		}
	}
	if mainSrv != nil {
		launch("main TCP", mainSrv.ListenAndServe)
		if cfg.Main.Network.WebSocketBind != "" {
			mux := http.NewServeMux()
			mux.Handle("/ws/main", mainSrv.WebSocketHandler(ctx, cfg.Main.Network.WebSocketOrigins))
			serveHTTP("main WebSocket", cfg.Main.Network.WebSocketBind, mux)
		}
		if cfg.Main.Network.InternalHTTPEnabled {
			serveHTTP("main HTTP", cfg.Main.Network.InternalHTTPBind, internalapi.MainHandler(cfg.Main.InternalSecret(), mainSrv.Healthy, mainSrv.HandleBattleResult))
		}
	}
	log.Info("OpenFreaks started", "mode", mode)
	defer func() {
		cancel()
		if battleSrv != nil {

			battleSrv.Shutdown(shutdownGrace)
			battleSrv.Wait()
		}
		if mainSrv != nil {
			mainSrv.Close()
		}
	}()
	select {
	case <-ctx.Done():
		log.Info("shutdown requested")
		return nil
	case err := <-exits:
		cancel()
		if err == nil {
			if mainSrv != nil && !mainSrv.Healthy() {
				return fmt.Errorf("main stopped accepting connections")
			}
			return fmt.Errorf("listener stopped unexpectedly")
		}
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func (h *HTTPBattleAllocator) JoinPractice(req *battleserver.PracticeRequest) (*battleserver.BattleGrant, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequest("POST", h.baseURL+"/practice", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set(internalapi.SecretHeader, h.secret)
	response, err := (&http.Client{Timeout: h.timeout}).Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, fmt.Errorf("practice status %d", response.StatusCode)
	}
	var out battleserver.BattleGrant
	err = json.NewDecoder(response.Body).Decode(&out)
	return &out, err
}
