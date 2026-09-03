//go:build js && wasm

// Command term is the dict demo: a shell in the page with dict in it.
//
// The demo is a terminal rather than a search box because dict is a
// terminal program, and what is worth showing is the thing it is good
// at — that a misspelling still finds the word, and that the answer
// arrives without anything being installed or fetched. A search box
// would demonstrate a search box.
//
// This build carries the small half of the corpus (see the dictlite tag
// in package data). The full one is 34.8 MB, most of it GCIDE and
// WordNet, which is the right trade for a binary you install once and
// the wrong one for a page someone is deciding whether to care about.
package main

import (
	"strings"
	"syscall/js"

	"github.com/0magnet/afero"

	"github.com/0magnet/websh/shell"
	"github.com/0magnet/websh/shell/browser"
	"github.com/0magnet/websh/web"

	"github.com/0magnet/dict/web/app"
)

func main() {
	container := js.Global().Get("document").Call("getElementById", "terminal")

	vfs := afero.NewMemMapFs()
	if err := shell.Seed(vfs); err != nil {
		js.Global().Get("console").Call("error", "seed filesystem: "+err.Error())
	}

	browser.Register()
	app.Register()

	sess, err := web.NewSession(container, web.Options{
		FS:       vfs,
		Host:     "you@dict",
		Greeting: greeting(),
	})
	if err != nil {
		js.Global().Get("console").Call("error", "dict term: "+err.Error())
		return
	}

	// ?run= submits commands as though typed, so a link can show a
	// result rather than a prompt someone has to know what to do with.
	for _, c := range linked() {
		sess.Submit(c)
	}

	select {}
}

// greeting names the few things worth trying. Anything longer is
// scrolled away by the first command.
//
// The line endings are CRLF because this goes straight to the terminal
// with WriteString, not through a line discipline: a bare LF moves down a
// row without returning to column 0, so every line starts where the last
// one ended and the greeting walks off the right of the screen.
func greeting() string {
	return "" +
		"dict — look up how a word is spelled, and what it means.\r\n" +
		"\r\n" +
		"  dict recieve        a misspelling still finds the word\r\n" +
		"  dict -d yacc        what it means\r\n" +
		"  dict -R             a random word\r\n" +
		"  dict --help         everything else\r\n" +
		"\r\n" +
		"this page carries FOLDOC, Jargon and Elements plus the full word\r\n" +
		"list; the installed binary also has Webster's 1913 and WordNet.\r\n" +
		"github.com/0magnet/dict\r\n"
}

// linked reads ?run=, splitting on ';' so a link can run more than one.
func linked() []string {
	loc := js.Global().Get("location")
	if !loc.Truthy() {
		return nil
	}
	p := js.Global().Get("URLSearchParams").New(loc.Get("search"))
	if !p.Truthy() {
		return nil
	}
	v := p.Call("get", "run")
	if v.Type() != js.TypeString || v.String() == "" {
		return nil
	}
	var out []string
	for _, c := range strings.Split(v.String(), ";") {
		if c = strings.TrimSpace(c); c != "" {
			out = append(out, c)
		}
	}
	return out
}
