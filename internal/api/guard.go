package api

import (
	"net"
	"net/http"
	"strings"
)

// guard rejects requests that a browser on another site could have caused.
//
// anchord listens on loopback and has no authentication, which is safe only
// while nothing else can reach it. Two things can:
//
//   - A page the user is visiting can post a cross-origin form to
//     http://127.0.0.1:7740 without any preflight. Shaping the body with
//     enctype="text/plain" makes it parse as JSON, so a plain visit to a hostile
//     page could register repositories, rewrite anchors, or delete a repo and
//     everything anchored in it.
//   - DNS rebinding points an attacker-controlled hostname at 127.0.0.1, which
//     turns those requests same-origin and reaches the methods forms cannot.
//
// Requiring a loopback Host closes the rebinding path, since the browser sends
// the attacker's hostname. Requiring a same-origin or absent Origin closes the
// form path. Requiring a JSON content type on writes closes the simple-request
// loophole that lets forms bypass preflight in the first place.
type guard struct {
	next http.Handler
	// allowedHosts holds extra hostnames to accept, for a server deliberately
	// bound to a non-loopback address.
	allowedHosts map[string]bool
}

func newGuard(next http.Handler, listenAddr string) http.Handler {
	allowed := map[string]bool{}
	if host, _, err := net.SplitHostPort(listenAddr); err == nil {
		host = strings.TrimSpace(host)
		// An operator who binds a routable address has chosen to expose the
		// server, so that address is legitimate for it to be reached at. The
		// wildcards say nothing about which name was used, so they add nothing.
		if host != "" && host != "0.0.0.0" && host != "::" && !isLoopbackHost(host) {
			allowed[strings.ToLower(host)] = true
		}
	}
	return &guard{next: next, allowedHosts: allowed}
}

func isLoopbackHost(host string) bool {
	host = strings.ToLower(strings.Trim(host, "[]"))
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

func (g *guard) hostAllowed(hostHeader string) bool {
	host := hostHeader
	if h, _, err := net.SplitHostPort(hostHeader); err == nil {
		host = h
	}
	if isLoopbackHost(host) {
		return true
	}
	return g.allowedHosts[strings.ToLower(strings.Trim(host, "[]"))]
}

func (g *guard) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !g.hostAllowed(r.Host) {
		writeError(w, http.StatusForbidden,
			"request rejected: unexpected Host header. AnchorDB only answers on loopback, "+
				"which stops a hostile page from reaching it by pointing a name at 127.0.0.1")
		return
	}

	// A cross-origin form post carries an Origin; a same-origin fetch or an
	// ordinary address-bar navigation either matches or omits it.
	if origin := r.Header.Get("Origin"); origin != "" && !g.originAllowed(origin) {
		writeError(w, http.StatusForbidden, "request rejected: cross-origin request to a local service")
		return
	}

	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		// Forms can only send three content types, none of them JSON, so
		// requiring JSON here is what makes a cross-origin form unable to reach
		// these routes at all.
		if !hasJSONContentType(r) && r.ContentLength != 0 {
			writeError(w, http.StatusUnsupportedMediaType,
				"request rejected: writes require Content-Type: application/json")
			return
		}
	}

	g.next.ServeHTTP(w, r)
}

func (g *guard) originAllowed(origin string) bool {
	// Origin is a serialized origin, "scheme://host[:port]", not a URL to walk.
	rest := origin
	if idx := strings.Index(rest, "://"); idx >= 0 {
		rest = rest[idx+3:]
	}
	return g.hostAllowed(rest)
}

func hasJSONContentType(r *http.Request) bool {
	contentType := r.Header.Get("Content-Type")
	if idx := strings.Index(contentType, ";"); idx >= 0 {
		contentType = contentType[:idx]
	}
	return strings.EqualFold(strings.TrimSpace(contentType), "application/json")
}
