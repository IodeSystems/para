package netguard

import (
	"fmt"
	"net/http"
	"net/netip"
	"strings"
)

// Client is who sent a request, as far as the connection and the exact
// trusted proxies can prove.
type Client struct {
	// Peer is the direct TCP peer. It is invalid when RemoteAddr does not parse.
	Peer netip.Addr
	// Addr is the client address. It is invalid when the address is unknown,
	// and every IP rule denies an unknown address.
	Addr netip.Addr
	// Local means the request came straight from a loopback peer: that peer is
	// not a trusted proxy and the request carries no forwarding header. An
	// unlisted proxy on this machine sets those headers, so its traffic is not
	// local.
	Local bool
	// Proto is the scheme the browser used: "https" or "http".
	Proto string
}

// Known reports whether the client address is known.
func (c Client) Known() bool { return c.Addr.IsValid() }

// Proxies is the exact set of peers whose forwarding headers are read.
type Proxies map[netip.Addr]struct{}

// ParseProxies reads net.trusted_proxies. Each entry is one IP address. A
// range is refused: one host that joins a trusted range could forge any
// client address.
func ParseProxies(list []string) (Proxies, error) {
	out := Proxies{}
	for _, s := range list {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		a, err := netip.ParseAddr(s)
		if err != nil {
			return nil, fmt.Errorf("netguard: trusted proxy %q is not one IP address: %w", s, err)
		}
		out[norm(a)] = struct{}{}
	}
	return out, nil
}

func norm(a netip.Addr) netip.Addr { return a.Unmap().WithZone("") }

func (p Proxies) has(a netip.Addr) bool {
	_, ok := p[a]
	return ok
}

// Resolve finds the client of r. X-Forwarded-For is read only when the direct
// peer is a trusted proxy, and the result is the rightmost entry that is not a
// trusted proxy. A trusted peer with no usable header gives an unknown client.
// X-Forwarded-Host is never read.
func (p Proxies) Resolve(r *http.Request) Client {
	c := Client{Proto: "http"}
	if r.TLS != nil {
		c.Proto = "https"
	}
	ap, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return c
	}
	c.Peer = norm(ap.Addr())
	if !p.has(c.Peer) {
		c.Addr = c.Peer
		c.Local = c.Peer.IsLoopback() && !forwarded(r)
		return c
	}
	if vals := r.Header.Values("X-Forwarded-Proto"); len(vals) > 0 {
		last := vals[len(vals)-1]
		if i := strings.LastIndexByte(last, ','); i >= 0 {
			last = last[i+1:]
		}
		if strings.EqualFold(strings.TrimSpace(last), "https") {
			c.Proto = "https"
		}
	}
	c.Addr = p.rightmost(r.Header.Values("X-Forwarded-For"))
	return c
}

// rightmost walks the chain from the right. A malformed entry ends the walk:
// what is left of it cannot be trusted.
func (p Proxies) rightmost(headers []string) netip.Addr {
	var parts []string
	for _, h := range headers {
		parts = append(parts, strings.Split(h, ",")...)
	}
	for i := len(parts) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(parts[i]))
		if err != nil {
			return netip.Addr{}
		}
		a = norm(a)
		if !p.has(a) {
			return a
		}
	}
	return netip.Addr{}
}

func forwarded(r *http.Request) bool {
	for _, h := range []string{"X-Forwarded-For", "X-Real-Ip", "Forwarded", "X-Forwarded-Proto", "X-Forwarded-Host"} {
		if len(r.Header.Values(h)) > 0 {
			return true
		}
	}
	return false
}
