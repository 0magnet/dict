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

	"github.com/0magnet/sh/v3/interp"
	"github.com/0magnet/websh/shell"

	"github.com/0magnet/dict/cmd"
)

// Register adds the dict command to the shell.
func Register() {
	shell.RegisterApplet("dict", "look up how a word is spelled and what it means (try: dict recieve)",
		func(ctx context.Context, s *shell.Shell, hc *interp.HandlerContext, args []string) int {
			// websh strips the command name and cobra expects it gone too.
			return cmd.Run(ctx, hostFor(s), args, hc.Stdout, hc.Stderr)
		})
}

// hostFor answers, for one invocation, what the process host answers
// from isatty and an ioctl.
func hostFor(s *shell.Shell) *cmd.Host {
	cols := 0
	if s.Size != nil {
		cols, _ = s.Size()
	}
	return &cmd.Host{
		Width: cols,
		// The interactive picker takes over the screen and reads keys
		// directly. In a shell applet the terminal belongs to the shell,
		// so dict prints its matches instead, exactly as it does when its
		// output is piped.
		Interactive: false,
	}
}
