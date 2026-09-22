package webtransport

import (
	"net/http/httptest"
	"testing"
)

func TestCapacityIsReleasedExactlyOnce(t *testing.T) {
	a := &Acceptor{MaxConnections: 2, MaxPerAddress: 1}
	request := httptest.NewRequest("GET", "http://localhost/ws", nil)
	request.RemoteAddr = "127.0.0.1:1"
	release, ok := a.Acquire(httptest.NewRecorder(), request)
	if !ok {
		t.Fatal("first rejected")
	}
	if _, ok = a.Acquire(httptest.NewRecorder(), request); ok {
		t.Fatal("per-address cap not applied")
	}
	release()
	release()
	release, ok = a.Acquire(httptest.NewRecorder(), request)
	if !ok {
		t.Fatal("capacity not released")
	}
	release()
	if a.active != 0 || len(a.byAddress) != 0 {
		t.Fatal("capacity leak")
	}
}
