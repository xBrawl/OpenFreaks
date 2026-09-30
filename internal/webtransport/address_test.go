package webtransport

import (
	"net"
	"net/http/httptest"
	"testing"
)

func TestClientAddressTrustBoundary(t *testing.T) {
	r := httptest.NewRequest("GET", "http://example.test/ws", nil)
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("X-Real-IP", "198.51.100.9")
	if ClientAddress(r, nil).(*net.TCPAddr).IP.String() != "192.0.2.1" {
		t.Fatal("untrusted forwarding header accepted")
	}
	if ClientAddress(r, []string{"192.0.2.1/32"}).(*net.TCPAddr).IP.String() != "198.51.100.9" {
		t.Fatal("trusted proxy address not preserved")
	}
	r.Header.Set("X-Real-IP", "invalid,198.51.100.9")
	if ClientAddress(r, []string{"192.0.2.1/32"}).(*net.TCPAddr).IP.String() != "192.0.2.1" {
		t.Fatal("malformed forwarded address accepted")
	}
}
