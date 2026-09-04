package cmd

import (
	"io"
	"time"
	"unicode/utf8"
)

type keyKind int

const (
	keyRune keyKind = iota
	keyEnter
	keyEsc
	keyBackspace
	keyUp
	keyDown
	keyPageUp
	keyPageDown
	keyHome
	keyEnd
	keyTab
	keyDeleteWord // Ctrl-W
	keyToggleDefs // Ctrl-T
	keyClearLine  // Ctrl-U
	keyInterrupt  // Ctrl-C, Ctrl-D
	keyIgnore
)

type key struct {
	kind keyKind
	r    rune
}

// keyReader turns a byte stream from the terminal into keys. Bytes are pumped
// by a goroutine so that a lone Escape can be told apart from the start of an
// escape sequence by waiting briefly for a follow-up byte.
type keyReader struct {
	ch   chan byte
	errc chan error
}

func newKeyReader(f io.Reader) *keyReader {
	kr := &keyReader{ch: make(chan byte, 256), errc: make(chan error, 1)}
	go func() {
		buf := make([]byte, 256)
		for {
			n, err := f.Read(buf)
			for i := 0; i < n; i++ {
				kr.ch <- buf[i]
			}
			if err != nil {
				kr.errc <- err
				close(kr.ch)
				return
			}
		}
	}()
	return kr
}

// next blocks for the next byte.
func (kr *keyReader) next() (byte, bool) {
	b, ok := <-kr.ch
	return b, ok
}

// peek waits a short while for a byte that is part of the same sequence.
// The delay only ever applies to a bare Escape keypress.
func (kr *keyReader) peek(d time.Duration) (byte, bool) {
	select {
	case b, ok := <-kr.ch:
		return b, ok
	case <-time.After(d):
		return 0, false
	}
}

func (kr *keyReader) readKey() (key, bool) {
	b, ok := kr.next()
	if !ok {
		return key{kind: keyInterrupt}, false
	}

	switch b {
	case 0x0d, 0x0a:
		return key{kind: keyEnter}, true
	case 0x09:
		return key{kind: keyTab}, true
	case 0x7f, 0x08:
		return key{kind: keyBackspace}, true
	case 0x03, 0x04: // Ctrl-C, Ctrl-D
		return key{kind: keyInterrupt}, true
	case 0x17: // Ctrl-W
		return key{kind: keyDeleteWord}, true
	case 0x14: // Ctrl-T
		return key{kind: keyToggleDefs}, true
	case 0x15: // Ctrl-U
		return key{kind: keyClearLine}, true
	case 0x0e: // Ctrl-N
		return key{kind: keyDown}, true
	case 0x10: // Ctrl-P
		return key{kind: keyUp}, true
	case 0x1b:
		return kr.readEscape(), true
	}

	if b < 0x20 {
		return key{kind: keyIgnore}, true
	}

	// A leading byte >= 0x80 starts a multi-byte rune; gather the rest.
	if b < utf8.RuneSelf {
		return key{kind: keyRune, r: rune(b)}, true
	}
	buf := []byte{b}
	for len(buf) < utf8.UTFMax && !utf8.FullRune(buf) {
		nb, ok := kr.peek(50 * time.Millisecond)
		if !ok {
			break
		}
		buf = append(buf, nb)
	}
	r, _ := utf8.DecodeRune(buf)
	if r == utf8.RuneError {
		return key{kind: keyIgnore}, true
	}
	return key{kind: keyRune, r: r}, true
}

func (kr *keyReader) readEscape() key {
	b, ok := kr.peek(50 * time.Millisecond)
	if !ok {
		return key{kind: keyEsc} // a bare Escape
	}
	if b != '[' && b != 'O' {
		return key{kind: keyIgnore} // Alt-<key>, not bound
	}

	var params []byte
	for {
		c, ok := kr.peek(50 * time.Millisecond)
		if !ok {
			return key{kind: keyIgnore}
		}
		// A final byte ends the sequence; digits and ';' are parameters.
		if (c >= '0' && c <= '9') || c == ';' {
			params = append(params, c)
			continue
		}
		switch c {
		case 'A':
			return key{kind: keyUp}
		case 'B':
			return key{kind: keyDown}
		case 'H':
			return key{kind: keyHome}
		case 'F':
			return key{kind: keyEnd}
		case '~':
			switch string(params) {
			case "1", "7":
				return key{kind: keyHome}
			case "4", "8":
				return key{kind: keyEnd}
			case "5":
				return key{kind: keyPageUp}
			case "6":
				return key{kind: keyPageDown}
			}
			return key{kind: keyIgnore}
		default:
			return key{kind: keyIgnore}
		}
	}
}
