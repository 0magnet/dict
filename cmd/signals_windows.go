//go:build windows

package cmd

import (
	"os"
	"syscall"
)

// hangupSignals leave the console usable if the process is killed while the
// UI is active.
var hangupSignals = []os.Signal{syscall.SIGTERM, syscall.SIGHUP}

// Windows has no SIGWINCH, so a resize is not signaled.
var resizeSignals []os.Signal
