package gstinstall

import "os/exec"

// stopsWithItsChildren leaves cmd as it is: Windows has no install script
// (its TorrServer carries GStreamer), so nothing runs here.
func stopsWithItsChildren(*exec.Cmd) {}
