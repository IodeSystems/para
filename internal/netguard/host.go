package netguard

import (
	"net"
	"regexp"
	"strings"
)

// label is one DNS label: the form of a vault id.
var label = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

// Router maps a Host header to a vault id. It knows names only: the caller
// checks that the vault exists, so a known and an unknown name get one answer.
type Router struct {
	bases []string
	bare  map[string]string
}

// NewRouter routes <vault>.<base> for each base, and always <vault>.localhost.
// When bareVault is set, the bases and bareHosts themselves serve that vault.
func NewRouter(bases []string, bareVault string, bareHosts []string) *Router {
	rt := &Router{bases: []string{"localhost"}, bare: map[string]string{}}
	for _, b := range bases {
		if b = hostName(b); b != "" && b != "localhost" {
			rt.bases = append(rt.bases, b)
		}
	}
	if bareVault != "" {
		for _, h := range append(append([]string{}, rt.bases...), bareHosts...) {
			if h = hostName(h); h != "" {
				rt.bare[h] = bareVault
			}
		}
	}
	return rt
}

// Route returns the vault id named by host. The port is ignored: a proxy may
// publish another port than the one para binds.
func (rt *Router) Route(host string) (string, bool) {
	name := hostName(host)
	if name == "" {
		return "", false
	}
	if v, ok := rt.bare[name]; ok {
		return v, true
	}
	for _, b := range rt.bases {
		if v, ok := strings.CutSuffix(name, "."+b); ok && label.MatchString(v) {
			return v, true
		}
	}
	return "", false
}

// hostName lowers a host, removes the port, the IPv6 brackets, and one
// trailing dot. It returns "" for a value that is not a host.
func hostName(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	host = strings.TrimSuffix(host, ".")
	if strings.ContainsAny(host, " /\\@?#%\t\r\n") {
		return ""
	}
	return host
}
