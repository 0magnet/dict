//go:build js || wasip1

package cmd

import "os"

// A wasm build has no controlling terminal, so there is no raw mode to
// restore and no window to resize. Empty here rather than absent so the
// UI code stays one implementation; see the guard in interactive().
var hangupSignals []os.Signal

var resizeSignals []os.Signal
