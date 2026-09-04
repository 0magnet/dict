//go:build js && wasm

// Package fetch supplies the dictionaries the page does not carry.
//
// GCIDE and WordNet are 24.7 MB of the 28 MB corpus, which is the right
// trade for a binary installed once and the wrong one for a page someone
// is deciding whether to care about. But they are served beside the page,
// and a .dict.dz is deflated in independent chunks with an index of where
// each begins — so a definition costs one ranged GET of about 58 KB
// rather than the whole file. The page therefore answers the same words
// as the installed binary, having downloaded almost none of it.
//
// What is paid up front, on the first definition only, is the .index.gz:
// 1.6 MB for GCIDE, 1.4 MB for WordNet. Nothing is fetched at all until
// something is looked up, because Set opens its dictionaries lazily.
package fetch

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"syscall/js"

	"github.com/0magnet/dict/data"
	"github.com/0magnet/dict/dictdb"
)

// Dictionaries returns a data.Fetcher reading from base, which is a URL
// prefix the dictionary paths are joined to — "data/" when the site root
// is the repository, so that data/dictd/gcide.dict.dz is where it says.
func Dictionaries(base string) data.Fetcher {
	return func(name, index, body string) (dictdb.Dictionary, error) {
		gz, err := get(base+index, 0, 0)
		if err != nil {
			return nil, fmt.Errorf("%s index: %w", name, err)
		}
		zr, err := gzip.NewReader(bytes.NewReader(gz))
		if err != nil {
			return nil, fmt.Errorf("%s index: %w", name, err)
		}
		defer zr.Close()
		return dictdb.Open(name, zr, &ranged{url: base + body})
	}
}

// ranged reads a URL through ranged GETs, which is what lets dictzip's
// random access survive the trip to a static file server.
type ranged struct{ url string }

func (r *ranged) ReadAt(p []byte, off int64) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	b, err := get(r.url, off, int64(len(p)))
	if err != nil {
		return 0, err
	}
	n := copy(p, b)
	if n < len(p) {
		// Short is normal here: the header probe asks for more than the
		// file holds, and the last chunk runs to the end.
		return n, io.EOF
	}
	return n, nil
}

// get fetches url, optionally a byte range. length 0 means the whole file.
//
// This blocks the calling goroutine on the browser's event loop, which is
// safe only because it is never the main one: websh runs a command on its
// own goroutine, so the loop keeps turning and the promise can settle.
func get(url string, off, length int64) ([]byte, error) {
	type result struct {
		b   []byte
		err error
	}
	done := make(chan result, 1)

	opts := map[string]any{}
	if length > 0 {
		opts["headers"] = map[string]any{
			"Range": fmt.Sprintf("bytes=%d-%d", off, off+length-1),
		}
	}

	var onOK, onBuf, onErr js.Func
	release := func() {
		onOK.Release()
		onBuf.Release()
		onErr.Release()
	}

	onErr = js.FuncOf(func(_ js.Value, args []js.Value) any {
		msg := "fetch failed"
		if len(args) > 0 && args[0].Truthy() {
			msg = args[0].Call("toString").String()
		}
		done <- result{err: fmt.Errorf("%s: %s", url, msg)}
		return nil
	})

	onBuf = js.FuncOf(func(_ js.Value, args []js.Value) any {
		u8 := js.Global().Get("Uint8Array").New(args[0])
		b := make([]byte, u8.Get("length").Int())
		js.CopyBytesToGo(b, u8)
		done <- result{b: b}
		return nil
	})

	onOK = js.FuncOf(func(_ js.Value, args []js.Value) any {
		resp := args[0]
		// 206 for a served range, 200 when the server ignored it or none
		// was asked for. Anything else has no body worth reading.
		if st := resp.Get("status").Int(); st != 200 && st != 206 {
			done <- result{err: fmt.Errorf("%s: HTTP %d", url, st)}
			return nil
		}
		resp.Call("arrayBuffer").Call("then", onBuf).Call("catch", onErr)
		return nil
	})

	js.Global().Call("fetch", url, opts).Call("then", onOK).Call("catch", onErr)

	r := <-done
	release()
	if r.err != nil {
		return nil, r.err
	}
	// A server that ignores Range hands back the whole file; take the
	// slice that was asked for so the caller sees what it expected.
	if length > 0 && int64(len(r.b)) > length && off > 0 {
		if off >= int64(len(r.b)) {
			return nil, io.EOF
		}
		end := off + length
		if end > int64(len(r.b)) {
			end = int64(len(r.b))
		}
		return r.b[off:end], nil
	}
	return r.b, nil
}
