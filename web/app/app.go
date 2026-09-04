//go:build js && wasm

// Package app is the dict command, as a websh applet.
//
// It runs the same cobra tree the terminal binary does. What the browser
// has to supply are the three things a command cannot discover without a
// process: where output goes, how wide it may be, and whether the
// full-screen picker can run. Everything else is shared, which is the
// point — a second browser-shaped copy of the command would drift from
// the first, and the drift is always discovered by a user.
package app

import (
	"context"
	"io"

	"github.com/0magnet/sh/v3/interp"
	"github.com/0magnet/websh/shell"

	"github.com/0magnet/dict/cmd"
	"github.com/0magnet/dict/web/fetch"
)

// Register adds the dict command to the shell.
func Register() {
	shell.RegisterApplet("dict", "look up how a word is spelled and what it means (try: dict recieve)",
		func(ctx context.Context, s *shell.Shell, hc *interp.HandlerContext, args []string) int {
			// websh strips the command name and cobra expects it gone too.
			return cmd.Run(ctx, hostFor(s, hc), args, hc.Stdout, hc.Stderr)
		})
}

// hostFor answers, for one invocation, what the process host answers
// from isatty and an ioctl.
//
// The interactive picker runs here too. It needs three things a process
// takes from /dev/tty — somewhere to draw, keys to read, and a way to
// stop the terminal echoing — and websh supplies all three: the applet's
// own stdio, and Shell.RawMode, which is what its own full-screen
// applets (less) use. What it must not do is ask termios, because in
// js/wasm there is nothing to ask.
func hostFor(s *shell.Shell, hc *interp.HandlerContext) *cmd.Host {
	cols, rows := 0, 0
	if s.Size != nil {
		cols, rows = s.Size()
	}
	h := &cmd.Host{
		Width:  cols,
		Height: rows,
		Stdout: hc.Stdout,
		Stderr: hc.Stderr,
		// GCIDE and WordNet are not in this binary; they are served
		// beside the page, and read a chunk at a time.
		Fetch: fetch.Dictionaries("data/"),
	}
	// Without a size the picker cannot lay out panes, and without raw
	// input every keystroke would also be echoed by the shell. Either
	// missing means printing the matches instead, which is what happens
	// when output is piped.
	if s.RawMode != nil && cols > 0 && rows > 0 {
		h.Interactive = true
		h.TTY = struct {
			io.Reader
			io.Writer
		}{hc.Stdin, hc.Stdout}
		h.SetRaw = s.RawMode
	}
	return h
}
