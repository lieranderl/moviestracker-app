package engine

import "strconv"

// Options are TorrServer settings that exist only as command-line flags, so
// changing them takes an engine restart. Empty fields are left out.
type Options struct {
	ProxyURL    string // --proxyurl: http, socks4, socks5 or socks5h URL
	ProxyMode   string // --proxymode: tracker, peers or full
	PublicIPv4  string // --pubipv4
	PublicIPv6  string // --pubipv6
	MaxSize     int64  // --maxsize: largest file TorrServer streams, in bytes (0: no limit)
	TorrentsDir string // --torrentsdir: folder whose .torrent files are added automatically
	HTTPS       bool   // --ssl: also serve HTTPS (port, certificate and key come from TorrServer's settings)
	// Reachable listens on every interface instead of loopback, so other
	// devices reach TorrServer (behind its login). It applies with HTTPS only.
	Reachable bool
}

// loopbackOnly reports whether the engine listens on 127.0.0.1 only.
func (o Options) loopbackOnly() bool {
	return !o.HTTPS || !o.Reachable
}

// args turns the options into TorrServer flags.
func (o Options) args() []string {
	var args []string
	add := func(flag, value string) {
		if value != "" {
			args = append(args, flag, value)
		}
	}
	add("--proxyurl", o.ProxyURL)
	if o.ProxyURL != "" {
		add("--proxymode", o.ProxyMode)
	}
	add("--pubipv4", o.PublicIPv4)
	add("--pubipv6", o.PublicIPv6)
	if o.MaxSize > 0 {
		add("--maxsize", strconv.FormatInt(o.MaxSize, 10))
	}
	add("--torrentsdir", o.TorrentsDir)
	if o.HTTPS {
		args = append(args, "--ssl")
	}
	return args
}

// SetOptions replaces the startup options used by the next (re)start and
// reports whether they changed.
func (s *Supervisor) SetOptions(o Options) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg.Options == o {
		return false
	}
	s.cfg.Options = o
	return true
}

// Options returns the startup options in use.
func (s *Supervisor) Options() Options {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.Options
}
