package rex

import "errors"

// ConPTY's consoles belong to the process that made them: a server of
// Windows does not upgrade.
const canUpgrade = false

var errNoUpgrade = errors.New("rex: the server does not upgrade on Windows")

func (s *Server) upgrade(string) error { return errNoUpgrade }

// Resume serves the sessions of an upgrading server; not on Windows.
func Resume(string) error { return errNoUpgrade }
