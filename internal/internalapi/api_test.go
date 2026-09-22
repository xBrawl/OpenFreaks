package internalapi

import (
	"github.com/freaks/openfreaks/internal/battleserver"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMainResultAcknowledgesOnlyCommitAndLimitsInput(t *testing.T) {
	commits := 0
	commitOK := false
	ready := false
	h := MainHandler("secret", func() bool { return ready }, func(*battleserver.Result) bool { commits++; return commitOK })
	for _, tc := range []struct {
		method, path, secret, body string
		status                     int
	}{
		{"GET", "/health", "", "", 503}, {"GET", "/result", "secret", "{}", 405},
		{"POST", "/result", "wrong", "{}", 403}, {"POST", "/result", "secret", "{}{}", 400},
		{"POST", "/result", "secret", `{"BattleID":"` + strings.Repeat("x", 1<<20) + `"}`, 400},
		{"POST", "/result", "secret", "{}", 503},
	} {
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		r.Header.Set(SecretHeader, tc.secret)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s %s: %d expected %d", tc.method, tc.path, w.Code, tc.status)
		}
	}
	if commits != 1 {
		t.Fatal("malformed/unauthenticated input reached commit")
	}
	commitOK = true
	ready = true
	r := httptest.NewRequest("POST", "/result", strings.NewReader("{}"))
	r.Header.Set(SecretHeader, "secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("committed result not acknowledged")
	}
}
