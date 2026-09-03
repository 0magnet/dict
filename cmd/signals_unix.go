//go:build !js && !wasip1

package cmd

import (
	"os"
	"syscall"
)

// hangupSignals leave the terminal usable if the process is killed while
// the alternate screen and raw mode are active.
var hangupSignals = []os.Signal{syscall.SIGTERM, syscall.SIGHUP}

// resizeSignals ask the UI to redraw at the new size.
var resizeSignals = []os.Signal{syscall.SIGWINCH}
