//go:build !windows

package server

import (
	"os"
	"syscall"
)

func reloadSignals() []os.Signal { return []os.Signal{syscall.SIGHUP} }
