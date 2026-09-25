package handlers

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// HostAllowlistMiddleware refuses requests whose Host header names a host
// this server does not answer to. That stops DNS rebinding: a web page cannot
// point its own hostname at this machine and read its pages. IP addresses,
// localhost (and *.localhost) and the listed names (such as the machine's
// .local mDNS name) are accepted; names compare case-insensitively.
func HostAllowlistMiddleware(names []string, next http.Handler) http.Handler {
	allowed := make(map[string]bool, len(names))
	for _, n := range names {
		if n = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(n), ".")); n != "" {
			allowed[n] = true
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hostAllowed(r.Host, allowed) {
			http.Error(w, "Misdirected Request: this server does not answer to that hostname.", http.StatusMisdirectedRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func hostAllowed(hostport string, allowed map[string]bool) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.ToLower(strings.TrimSuffix(strings.Trim(host, "[]"), "."))
	if _, err := netip.ParseAddr(host); err == nil {
		return true
	}
	return host == "localhost" || strings.HasSuffix(host, ".localhost") || allowed[host]
}

// lanIPv4 is this machine's address on the local network: the source address
// of its default route. Dialing UDP sends nothing; it only picks the route.
func lanIPv4() string {
	conn, err := net.Dial("udp4", "192.0.2.1:9") // TEST-NET-1: never reached
	if err != nil {
		return ""
	}
	defer func() { _ = conn.Close() }()
	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || addr.IP.IsLoopback() || !addr.IP.IsPrivate() {
		return ""
	}
	return addr.IP.String()
}

// linkOrigin is the scheme and host for links other devices open: the
// address in use, except that localhost becomes this machine's network
// address, which TVs, phones and players (on this Mac too) can reach.
func (s *Server) linkOrigin(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	host, port, err := net.SplitHostPort(r.Host)
	if err != nil {
		host, port = r.Host, ""
	}
	name := strings.ToLower(strings.Trim(host, "[]"))
	ip, ipErr := netip.ParseAddr(name)
	loopback := name == "localhost" || strings.HasSuffix(name, ".localhost") || (ipErr == nil && ip.IsLoopback())
	if !loopback {
		return scheme + "://" + r.Host
	}
	lan := s.lanAddress
	if lan == nil {
		lan = lanIPv4
	}
	addr := lan()
	if addr == "" {
		return scheme + "://" + r.Host
	}
	if port != "" {
		return scheme + "://" + net.JoinHostPort(addr, port)
	}
	return scheme + "://" + addr
}
