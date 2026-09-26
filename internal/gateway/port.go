package gateway

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// Port is the gateway's listening port: open while apps may connect, shut
// otherwise, so nothing listens until an admin turns the gateway on.
type Port struct {
	addr    string
	handler http.Handler

	mu     sync.Mutex
	server *http.Server
	ln     net.Listener
}

// NewPort returns the shut port addr (host:port) that serves handler once
// opened.
func NewPort(addr string, handler http.Handler) *Port {
	return &Port{addr: addr, handler: handler}
}

// Open starts listening; an open port stays open.
func (p *Port) Open() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.server != nil {
		return nil
	}
	ln, err := net.Listen("tcp", p.addr)
	if err != nil {
		if inUse(err) {
			return fmt.Errorf("another program already uses port %s (a TorrServer of its own?): stop it, or set MT_TORRSERVER_LISTEN", portOf(p.addr))
		}
		return fmt.Errorf("open port %s: %w", portOf(p.addr), err)
	}
	// No write timeout: streams last as long as the film.
	server := &http.Server{Handler: p.handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 120 * time.Second, MaxHeaderBytes: 1 << 20}
	p.server, p.ln = server, ln
	go func() { _ = server.Serve(ln) }()
	return nil
}

// Shut stops listening and ends open connections, streams included.
func (p *Port) Shut() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.server == nil {
		return nil
	}
	err := p.server.Close()
	p.server, p.ln = nil, nil
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Close shuts the port for good, at shutdown: requests in flight get a
// moment to finish.
func (p *Port) Close() error {
	p.mu.Lock()
	server := p.server
	p.mu.Unlock()
	if server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}
	return p.Shut()
}

// Addr is the address the port listens on, or "" while it is shut.
func (p *Port) Addr() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ln == nil {
		return ""
	}
	return p.ln.Addr().String()
}

func portOf(addr string) string {
	if _, port, err := net.SplitHostPort(addr); err == nil {
		return port
	}
	return addr
}
