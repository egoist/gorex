//go:build !darwin && !linux && !windows

package rex

import "errors"

var errUnsupported = errors.New("rex: the session server runs on macOS, Linux and Windows")

// Serve runs the server; not on this platform.
func Serve() error { return errUnsupported }

// Spawn starts a server; not on this platform.
func Spawn() error { return errUnsupported }

func processAlive(int) bool { return false }
