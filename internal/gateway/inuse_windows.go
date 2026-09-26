package gateway

import (
	"errors"

	"golang.org/x/sys/windows"
)

// inUse reports whether err says another program listens on the port.
func inUse(err error) bool { return errors.Is(err, windows.WSAEADDRINUSE) }
