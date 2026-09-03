package cmd

import (
	"os"
	"testing"
	"time"
)

// keyTest drives the decoder over an os.Pipe rather than a terminal, so the
// exact bytes and their timing are under the test's control.
func keyTest(t *testing.T, in []byte, want []keyKind) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	kr := newKeyReader(r)
	go func() {
		w.Write(in)
		// Left open: closing here would race the 50ms escape timeout and
		// turn a bare Escape into an end-of-input instead.
	}()

	done := make(chan []keyKind, 1)
	go func() {
		var got []keyKind
		for range want {
			k, ok := kr.readKey()
			if !ok {
				break
			}
			got = append(got, k.kind)
		}
		done <- got
	}()

	select {
	case got := <-done:
		if len(got) != len(want) {
			t.Fatalf("got %v keys, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("key %d: got %v, want %v", i, got[i], want[i])
			}
		}
	case <-time.After(3 * time.Second):
		t.Fatal("decoder blocked; a key was never produced")
	}
	w.Close()
}

func TestDecodeBareEscape(t *testing.T) {
	// The awkward one: Escape is both a key and the start of every arrow
	// sequence, told apart only by whether a byte follows promptly.
	keyTest(t, []byte{0x1b}, []keyKind{keyEsc})
}

func TestDecodeArrows(t *testing.T) {
	keyTest(t, []byte("\x1b[A\x1b[B"), []keyKind{keyUp, keyDown})
	keyTest(t, []byte("\x1bOA"), []keyKind{keyUp}) // application cursor mode
}

func TestDecodePageKeys(t *testing.T) {
	keyTest(t, []byte("\x1b[5~\x1b[6~"), []keyKind{keyPageUp, keyPageDown})
}

func TestDecodeControls(t *testing.T) {
	keyTest(t, []byte{0x03}, []keyKind{keyInterrupt})
	keyTest(t, []byte{0x7f}, []keyKind{keyBackspace})
	keyTest(t, []byte{0x17}, []keyKind{keyDeleteWord})
	keyTest(t, []byte{0x15}, []keyKind{keyClearLine})
	keyTest(t, []byte{0x0d}, []keyKind{keyEnter})
	keyTest(t, []byte{0x09}, []keyKind{keyTab})
	keyTest(t, []byte{0x0e}, []keyKind{keyDown}) // Ctrl-N
	keyTest(t, []byte{0x10}, []keyKind{keyUp})   // Ctrl-P
}

func TestDecodeUTF8(t *testing.T) {
	r, w, _ := os.Pipe()
	defer r.Close()
	kr := newKeyReader(r)
	go w.Write([]byte("café"))

	var got []rune
	for i := 0; i < 4; i++ {
		k, ok := kr.readKey()
		if !ok {
			t.Fatal("stream ended early")
		}
		if k.kind != keyRune {
			t.Fatalf("key %d: kind %v, want a rune", i, k.kind)
		}
		got = append(got, k.r)
	}
	if string(got) != "café" {
		t.Errorf("decoded %q, want %q", string(got), "café")
	}
	w.Close()
}

func TestDecodeEndOfInput(t *testing.T) {
	// A closed input must surface as an interrupt so the caller can exit,
	// rather than blocking forever.
	r, w, _ := os.Pipe()
	kr := newKeyReader(r)
	w.Close()

	done := make(chan struct{})
	go func() {
		k, ok := kr.readKey()
		if ok || k.kind != keyInterrupt {
			t.Errorf("on EOF got kind=%v ok=%v, want keyInterrupt/false", k.kind, ok)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("decoder blocked on end of input")
	}
	r.Close()
}
