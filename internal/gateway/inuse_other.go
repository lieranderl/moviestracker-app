//go:build !windows

package gateway

import (
	"errors"
	"syscall"
)

// inUse reports whether err says another program listens on the port.
func inUse(err error) bool { return errors.Is(err, syscall.EADDRINUSE) }
