// Package cmd host.go
//
// Where the command's output goes, and how wide it may be.
//
// dict was written as a program with a process: it printed to os.Stdout,
// asked the tty how wide it was, and exited. A shell applet in a browser
// has none of those. It has a pair of pipes, a size the terminal
// reports, and a caller that must survive the command failing.
//
// The three answers live here so the commands can stay one
// implementation rather than growing a second, browser-shaped copy that
// drifts from the first.
package cmd

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/term"
)

// Host supplies what the command cannot discover for itself.
type Host struct {
	Stdout io.Writer
	Stderr io.Writer
	// Width is the wrapping width. Zero means ask the terminal, which is
	// what a process does and what a browser cannot.
	Width int

	// Height is the rows a full-screen still fills. Zero asks the terminal.
	Height int
	// Interactive reports whether the full-screen picker can run. A
	// process decides this from isatty; a host that is not a process has
	// to say.
	Interactive bool
}

// host is the current host. The default is the process one, so the
// terminal binary behaves exactly as it did before any of this existed.
var host = processHost()

func processHost() *Host {
	return &Host{
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
		Interactive: term.IsTerminal(int(os.Stdout.Fd())) && term.IsTerminal(int(os.Stdin.Fd())),
	}
}

// SetHost installs a host for the next run. Passing nil restores the
// process host.
func SetHost(h *Host) {
	if h == nil {
		host = processHost()
		return
	}
	if h.Stdout == nil {
		h.Stdout = os.Stdout
	}
	if h.Stderr == nil {
		h.Stderr = os.Stderr
	}
	host = h
}

// out and errOut are what the commands print to.
func out() io.Writer    { return host.Stdout }
func errOut() io.Writer { return host.Stderr }

// printf and println replace the fmt.Print* calls, which wrote to the
// process's stdout and so were invisible to any other host.
func printf(format string, a ...any) {
	fmt.Fprintf(out(), format, a...) //nolint:errcheck // a closed pipe is the caller's business
}

func println(a ...any) {
	fmt.Fprintln(out(), a...) //nolint:errcheck // as above
}
