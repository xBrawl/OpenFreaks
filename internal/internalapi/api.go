package internalapi

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/freaks/openfreaks/internal/battleserver"
)

const SecretHeader = "X-Internal-Secret"

func HealthHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}

func ServeHealth(bind string) error {
	return http.ListenAndServe(bind, HealthHandler())
}

type BattleHTTP struct {
	bs     *battleserver.Server
	secret string
}

func NewBattleHTTP(bs *battleserver.Server, secret string) *BattleHTTP {
	return &BattleHTTP{bs: bs, secret: secret}
}

func (b *BattleHTTP) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if !b.bs.Healthy() {
			http.Error(w, "not ready", 503)
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/practice", func(w http.ResponseWriter, r *http.Request) {
		if !Authorize(w, r, b.secret) {
			return
		}
		var req battleserver.PracticeRequest
		if err := DecodeBody(w, r, 8192, &req); err != nil {
			http.Error(w, "bad request", 400)
			return
		}
		grant, err := b.bs.JoinPractice(&req)
		if err != nil {
			http.Error(w, "practice unavailable", 409)
			return
		}
		_ = json.NewEncoder(w).Encode(grant)
	})
	mux.HandleFunc("/allocate", func(w http.ResponseWriter, r *http.Request) {
		if !Authorize(w, r, b.secret) {
			return
		}
		var req battleserver.AllocateRequest
		if err := DecodeBody(w, r, 64<<10, &req); err != nil {
			http.Error(w, "bad request", 400)
			return
		}
		sess, host, port, allocErr := b.bs.Allocate(&req)
		if allocErr != "" {
			http.Error(w, allocErr, 400)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"sessions": sess, "host": host, "port": port})
	})
	mux.HandleFunc("/replay", func(w http.ResponseWriter, r *http.Request) {
		if !Authorize(w, r, b.secret) {
			return
		}
		var request struct {
			PlayerID int64
			BattleID string
		}
		if err := DecodeBody(w, r, 4096, &request); err != nil {
			http.Error(w, "bad request", 400)
			return
		}
		grant, err := b.bs.OpenReplay(request.PlayerID, request.BattleID)
		if err != nil {
			http.Error(w, "replay unavailable", 404)
			return
		}

		copy := *grant.Replay
		copy.Frames = []battleserver.ReplayFrame{{Tick: grant.Replay.Frames[len(grant.Replay.Frames)-1].Tick}}
		response := *grant
		response.Replay = &copy
		_ = json.NewEncoder(w).Encode(response)
	})
	for _, route := range []string{"/resume", "/leave"} {
		mux.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) {
			if !Authorize(w, r, b.secret) {
				return
			}
			var request struct {
				BattleID string
				PlayerID int64
			}
			if err := DecodeBody(w, r, 4096, &request); err != nil || request.PlayerID <= 0 || len(request.BattleID) > 128 {
				http.Error(w, "bad request", 400)
				return
			}
			if r.URL.Path == "/leave" {
				if !b.bs.Leave(request.BattleID, request.PlayerID) {
					http.Error(w, "participant unavailable", 404)
					return
				}
				_, _ = w.Write([]byte("{}"))
				return
			}
			grant, err := b.bs.Resume(request.BattleID, request.PlayerID)
			if err != nil {
				http.Error(w, "battle unavailable", 404)
				return
			}
			_ = json.NewEncoder(w).Encode(grant)
		})
	}
	mux.HandleFunc("/rooms", func(w http.ResponseWriter, r *http.Request) {
		if !Authorize(w, r, b.secret) {
			return
		}
		var ids []string
		if err := DecodeBody(w, r, 8192, &ids); err != nil || len(ids) > 64 {
			http.Error(w, "bad request", 400)
			return
		}
		status, err := b.bs.RoomStatus(ids)
		if err != nil {
			http.Error(w, "unavailable", 503)
			return
		}
		_ = json.NewEncoder(w).Encode(status)
	})
	return mux
}

func PostResult(baseURL, secret string, r *battleserver.Result, timeout time.Duration, retries int, delay time.Duration) error {
	body, _ := json.Marshal(r)
	var last error
	for i := 0; i <= retries; i++ {
		req, err := http.NewRequest("POST", baseURL+"/result", bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set(SecretHeader, secret)
		req.Header.Set("Content-Type", "application/json")
		client := &http.Client{Timeout: timeout}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode < 300 {
				return nil
			}
			last = fmt.Errorf("result post status %d", resp.StatusCode)
		} else {
			last = err
		}
		if i < retries {
			time.Sleep(delay)
		}
	}
	return last
}

func Authorize(w http.ResponseWriter, r *http.Request, secret string) bool {
	if r.Method != "POST" {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", 405)
		return false
	}
	if secret == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get(SecretHeader)), []byte(secret)) != 1 {
		http.Error(w, "forbidden", 403)
		return false
	}
	return true
}
func DecodeBody(w http.ResponseWriter, r *http.Request, limit int64, out any) error {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	if err := d.Decode(out); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return fmt.Errorf("expected one JSON value")
	}
	return nil
}

func MainHandler(secret string, healthy func() bool, commit func(*battleserver.Result) bool) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if !healthy() {
			http.Error(w, "not ready", 503)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/result", func(w http.ResponseWriter, r *http.Request) {
		if !Authorize(w, r, secret) {
			return
		}
		var result battleserver.Result
		if err := DecodeBody(w, r, 1<<20, &result); err != nil {
			http.Error(w, "bad request", 400)
			return
		}
		if !commit(&result) {
			http.Error(w, "result not committed", 503)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}
