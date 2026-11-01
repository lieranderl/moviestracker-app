package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strconv"
)

// peerAddr is the BitTorrent address (--torrentaddr) for the next start: a
// port TCP and UDP can both listen on, or "" when the user set a peer port
// (PeersListenPort), which TorrServer then uses, unless Options.OwnPeerPort.
// Left to itself TorrServer takes a random TCP port and then wants UDP on the
// same number, which Windows refuses when the number is in a reserved range.
func (s *Supervisor) peerAddr() (string, error) {
	if set, known := userPeerPort(s.cfg.Dir); (set || !known) && !s.Options().OwnPeerPort {
		return "", nil
	}
	for range 20 {
		if port, ok := tcpUDPPort(); ok {
			return ":" + strconv.Itoa(port), nil
		}
	}
	return "", errors.New("find a peer port TCP and UDP can use")
}

// userPeerPort reports whether TorrServer's settings in dir have a peer port
// set, and whether that is known: settings TorrServer keeps in its config.db
// instead of settings.json are not read.
func userPeerPort(dir string) (set, known bool) {
	raw, err := os.ReadFile(filepath.Join(dir, "settings.json")) // #nosec G304 -- the engine's own data directory
	if err != nil {
		// A fresh engine has neither file, and TorrServer's defaults.
		_, dbErr := os.Stat(filepath.Join(dir, "config.db"))
		return false, errors.Is(err, fs.ErrNotExist) && errors.Is(dbErr, fs.ErrNotExist)
	}
	var stored struct {
		BitTorr *struct{ PeersListenPort int }
	}
	if err := json.Unmarshal(raw, &stored); err != nil || stored.BitTorr == nil {
		return false, false
	}
	return stored.BitTorr.PeersListenPort > 0, true
}

// tcpUDPPort returns a port free for TCP on which UDP may listen too.
func tcpUDPPort() (int, bool) {
	tcp, err := net.Listen("tcp4", ":0") // #nosec G102 -- probing the port TorrServer will listen on for peers
	if err != nil {
		return 0, false
	}
	defer func() { _ = tcp.Close() }()
	port := tcp.Addr().(*net.TCPAddr).Port
	udp, err := net.ListenPacket("udp4", fmt.Sprintf(":%d", port)) // #nosec G102 -- as above
	if err != nil {
		return 0, false
	}
	_ = udp.Close()
	return port, true
}
