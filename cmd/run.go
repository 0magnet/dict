package cmd

import (
	"context"
	"fmt"
	"io"
	"runtime/debug"

	"github.com/spf13/pflag"
)

// Run executes the command tree with the given arguments and writers and
// returns an exit status. It is what a host that is not an operating
// system calls: a shell applet has arguments and a pair of pipes, not
// os.Args and a process to exit.
func Run(ctx context.Context, h *Host, args []string, stdout, stderr io.Writer) (code int) {
	// A host that is not an operating system has no process to lose, and
	// losing one command should not cost it the shell that ran it.
	// Unrecovered, a panic here takes down the goroutine the shell waits
	// on: no message, no status, a terminal that never prints another
	// prompt.
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(stderr, "dict: %v\n%s", r, debug.Stack()) //nolint:errcheck // reporting a panic; if stderr is gone there is nowhere to say so
			code = 2
		}
	}()

	if h == nil {
		h = &Host{}
	}
	h.Stdout, h.Stderr = stdout, stderr
	SetHost(h)
	defer SetHost(nil)

	reset()
	RootCmd.SetArgs(args)
	RootCmd.SetOut(stdout)
	RootCmd.SetErr(stderr)
	if err := RootCmd.ExecuteContext(ctx); err != nil {
		if _, ok := err.(exitNoSelection); ok {
			return 1
		}
		fmt.Fprintln(stderr, "dict:", err) //nolint:errcheck // as above
		return 1
	}
	return 0
}

// reset makes the tree ready to run again, which it is not by default.
// Cobra keeps the flag values and the resolved subcommand from the last
// run, so a second invocation in the same process would inherit both.
func reset() {
	opts = options{}
	RootCmd.Flags().VisitAll(func(f *pflag.Flag) {
		f.Changed = false
		_ = f.Value.Set(f.DefValue)
	})
	for _, c := range RootCmd.Commands() {
		c.Flags().VisitAll(func(f *pflag.Flag) {
			f.Changed = false
			_ = f.Value.Set(f.DefValue)
		})
	}
}
