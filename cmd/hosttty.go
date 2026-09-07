package cmd

import (
	"errors"
	"io"
	"sync"

	"github.com/gdamore/tcell/v3"
)

// hostTty presents Host.TTY to tcell as a terminal.
//
// tcell.NewScreen opens /dev/tty and asks termios for the size and for raw
// mode. In js/wasm there is no such file and no termios behind it, so what
// it falls back to is tcell's own browser screen — which wants a pair of
// JavaScript globals, tcellWrite and tcellWindowSize, that nothing in this
// project installs. The demo therefore failed outright:
//
//	dict: tcell wasm terminal host is not installed
//
// The picker had already answered this question, and this is the same
// answer: it draws on Host.TTY and toggles echo through Host.SetRaw rather
// than opening a terminal of its own. A screen built on this tty uses that
// same terminal, so the animation runs wherever the picker runs and there
// is one description of where dict's full-screen output goes, not two.
//
// Only Start and Stop do anything beyond delegating. Raw mode is the
// embedder's to grant, the size is whatever it reported, and there is no
// input to drain that is not already in the reader.
type hostTty struct {
	h *Host

	mu      sync.Mutex
	resize  chan<- bool
	started bool
}

// newHostTty returns a tty over h, or nil if h has no terminal to draw on —
// which is a process, where tcell can open one for itself.
func newHostTty(h *Host) *hostTty {
	if h == nil || h.TTY == nil {
		return nil
	}
	return &hostTty{h: h}
}

// Start puts the terminal into raw mode. It is idempotent, as the interface
// requires: tcell calls it again after a Stop when resuming.
func (t *hostTty) Start() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.started {
		return nil
	}
	if t.h.TTY == nil {
		return errors.New("dict: no terminal to draw on")
	}
	if t.h.SetRaw != nil {
		t.h.SetRaw(true)
	}
	t.started = true
	return nil
}

// Stop returns the terminal to the shell. tcell calls this from Fini, after
// it has written the sequences that restore the screen, so the echo has to
// stay off until it returns.
func (t *hostTty) Stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.started {
		return nil
	}
	if t.h.SetRaw != nil {
		t.h.SetRaw(false)
	}
	t.started = false
	return nil
}

// Drain is a no-op. It exists for /dev/tty implementations that must ask the
// driver to release a blocked reader; this reader is the shell's pipe and
// wakes on its own.
func (t *hostTty) Drain() error { return nil }

// NotifyResize records the channel tcell wants resize events on. Nothing
// posts to it: the embedder reports one size for the life of the invocation,
// so a resize mid-animation is not observable here.
func (t *hostTty) NotifyResize(ch chan<- bool) {
	t.mu.Lock()
	t.resize = ch
	t.mu.Unlock()
}

// WindowSize reports the size the embedder gave. tcell falls back to 80x24
// on a zero, and so does this, so a host that never learned its size still
// draws something rather than nothing.
func (t *hostTty) WindowSize() (tcell.WindowSize, error) {
	ws := tcell.WindowSize{Width: t.h.Width, Height: t.h.Height}
	if ws.Width <= 0 {
		ws.Width = 80
	}
	if ws.Height <= 0 {
		ws.Height = 24
	}
	return ws, nil
}

// Read serves tcell's input goroutine, and refuses once stopped.
//
// The refusal matters. tcell keeps a read outstanding at all times, and the
// terminal's stdin is an io.Pipe with no deadline, so a read already blocked
// when Stop arrives cannot be cancelled — it stays parked and swallows the
// next byte written to the pipe. That costs the first keystroke typed after
// leaving a full-screen view. Returning an error here does not rescue that
// one byte, but it does stop tcell issuing another read and eating a second.
//
// The picker does not have this problem because it reads only when it wants
// a key, so it is never parked in a read it has stopped caring about.
func (t *hostTty) Read(p []byte) (int, error) {
	t.mu.Lock()
	started := t.started
	t.mu.Unlock()
	if !started {
		return 0, io.EOF
	}
	r, ok := t.h.TTY.(io.Reader)
	if !ok {
		return 0, io.EOF
	}
	return r.Read(p)
}

func (t *hostTty) Write(p []byte) (int, error) { return t.h.TTY.Write(p) }

// Close stops the tty. The terminal itself belongs to the embedder and
// outlives the command, so there is nothing else to release.
func (t *hostTty) Close() error { return t.Stop() }

// newRainScreen builds the screen the animation draws on: the host's
// terminal when it has one, and otherwise the process terminal tcell finds
// for itself.
func newRainScreen(h *Host) (tcell.Screen, error) {
	if tty := newHostTty(h); tty != nil {
		return tcell.NewTerminfoScreenFromTty(tty)
	}
	return tcell.NewScreen()
}
