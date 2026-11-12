//go:build windows

package server

import "os"

func reloadSignals() []os.Signal { return nil }
