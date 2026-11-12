package server

import (
	"net/http"
	"net/http/pprof"
	"strings"

	"github.com/iodesystems/para/internal/netguard"
)

// pprofPrefix is where the opt-in profiler lives. It is under /api so the
// gate, the no-store header, and the service worker denylist already cover it.
const pprofPrefix = "/api/debug/pprof/"

// pprofHandler serves the Go profiler. It is mounted only when the owner sets
// debug.pprof=true in server.properties. The gate has already checked the
// admin token (Policy returns ScopeAdmin for this prefix); this handler also
// refuses any client that is not on this machine's loopback.
func pprofHandler() http.Handler {
	mux := http.NewServeMux()
	// Registered by hand: importing net/http/pprof for its side effects would
	// put these on http.DefaultServeMux.
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	mux.HandleFunc("/debug/pprof/", pprof.Index) // index and named profiles, via pprof.Handler
	strip := http.StripPrefix("/api", mux)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, ok := netguard.From(r.Context())
		if !ok || !req.Principal.Admin || !req.Client.Local || !req.Client.Addr.IsLoopback() {
			netguard.WriteError(w, http.StatusForbidden, "pprof: loopback and the admin token only")
			return
		}
		strip.ServeHTTP(w, r)
	})
}

func isPprofPath(p string) bool { return strings.HasPrefix(p, pprofPrefix) }
