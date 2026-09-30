package webtransport

import (
	"net"
	"net/http"
)

// ClientAddress accepts the proxy's replaced X-Real-IP only from explicitly
// trusted peers. Client-supplied forwarding headers cannot bypass per-IP limits.
func ClientAddress(r *http.Request, trusted []string) net.Addr {
	address, err := net.ResolveTCPAddr("tcp", r.RemoteAddr)
	if err != nil {
		return nil
	}
	for _, cidr := range trusted {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil || !network.Contains(address.IP) {
			continue
		}
		if ip := net.ParseIP(r.Header.Get("X-Real-IP")); ip != nil {
			return &net.TCPAddr{IP: ip, Port: address.Port}
		}
		break
	}
	return address
}

type RemoteConn struct {
	net.Conn
	Address net.Addr
}

func (c RemoteConn) RemoteAddr() net.Addr { return c.Address }
