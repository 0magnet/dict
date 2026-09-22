package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"

	"unicode/utf8"

	"github.com/clipperhouse/displaywidth"

	"github.com/0magnet/dict/dictdb"
	"github.com/0magnet/dict/match"
	"golang.org/x/term"
)

const (
	maxResults  = 500 // more than anyone scrolls, and keeps sorting cheap
	esc         = "\x1b"
	altScreenOn = esc + "[?1049h"
	altScreenOf = esc + "[?1049l"
	cursorHide  = esc + "[?25l"
	cursorShow  = esc + "[?25h"
	sgrReset    = esc + "[0m"
	sgrSelected = esc + "[7m"
	sgrMatch    = esc + "[1;36m"
	sgrDim      = esc + "[2m"
)

type ui struct {
	out        io.Writer
	size       func() (cols, rows int)
	ix         *match.Index
	sourceName string
	total      int
	query      []rune
	results    []match.Result
	sel        int
	off        int
	rows       int
	cols       int
	reverse    bool // prompt on top, list downward, as with fzf --layout=reverse

	// what a row draws to the left of the word
	ordW   int // columns the ordinal takes; see pick.glyph for the rest
	glyph  func(word string) string
	glyphW int

	// definition pane
	defs     *dictdb.Set
	showDefs bool
	defWord  string   // word the cached definition belongs to
	defWidth int      // width it was wrapped for
	defLines []string // wrapped, ready to draw
	defTitle string   // which dictionary answered
}

// openTTY returns the terminal the picker draws on, a way to ask its size, and
// a cleanup that undoes whatever raw mode was entered. The cleanup is
// idempotent: it runs from a defer and again from the signal handler, and
// restoring a terminal twice must not be an error.
//
// Two hosts arrive here. A process has none of this supplied and opens
// /dev/tty itself, so the terminal binary behaves exactly as it did before any
// of this existed. A host that is not a process — the browser applet — hands
// over a terminal it already owns, and says how to make it raw; asking termios
// about it would fail, because in js/wasm there is nothing to ask.
func openTTY() (io.ReadWriter, func() (int, int), func(), error) {
	if host.TTY != nil {
		if host.SetRaw != nil {
			host.SetRaw(true)
		}
		var once sync.Once
		cleanup := func() {
			once.Do(func() {
				if host.SetRaw != nil {
					host.SetRaw(false)
				}
			})
		}
		return host.TTY, hostSize, cleanup, nil
	}

	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("cannot open terminal: %w", err)
	}
	state, err := term.MakeRaw(int(tty.Fd())) //nolint:staticcheck // SA4023 only under js/wasm, where termios is not there to ask
	if err != nil {                           //nolint:staticcheck // as above
		tty.Close() //nolint:errcheck,gosec // the open succeeded; the failure to report is the raw one
		return nil, nil, nil, fmt.Errorf("cannot set raw mode: %w", err)
	}
	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			term.Restore(int(tty.Fd()), state) //nolint:errcheck,gosec // leaving anyway
			tty.Close()                        //nolint:errcheck,gosec // as above
		})
	}
	size := func() (int, int) {
		w, h, err := term.GetSize(int(tty.Fd())) //nolint:staticcheck // as above
		if err != nil || w <= 0 || h <= 0 {      //nolint:staticcheck // as above
			return 80, 24
		}
		return w, h
	}
	return tty, size, cleanup, nil
}

// hostSize reports the size a non-process host declared. Zero means it did not
// know, and 80x24 is the answer a terminal gives when nothing else does.
func hostSize() (int, int) {
	w, h := host.Width, host.Height
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}
	return w, h
}

// pick is everything a caller has to say to run the picker: what list to
// search, where it came from, and how a row is drawn.
//
// It is a struct rather than a parameter list because the two callers differ
// in one field each, and six positional arguments of which four are bare
// strings and bools is the kind of call that gets one of them wrong.
type pick struct {
	ix       *match.Index
	query    string
	source   string
	reverse  bool
	defs     *dictdb.Set
	showDefs bool

	// glyph, when set, is drawn between the selection marker and the word:
	// for the character table it is the character itself, without which the
	// list is a list of descriptions of things you cannot see.
	//
	// It must come back exactly glyphW columns wide and must contain no
	// escape sequence -- a selected row is drawn in reverse video, and a
	// reset in the middle of it would end the highlight early.
	glyph  func(word string) string
	glyphW int
}

// runInteractive drives the interactive search and returns the chosen word,
// or "" if the user quit. For a process the interface is drawn on /dev/tty so
// that stdout carries only the result and stays usable in a pipeline; a host
// that is not a process supplies its own terminal through Host.TTY, because
// there is no /dev/tty in js/wasm and no termios behind it.
func runInteractive(p pick) (string, error) {
	tty, size, cleanup, err := openTTY()
	if err != nil {
		return "", err
	}

	u := &ui{
		out: tty, size: size, ix: p.ix, sourceName: filepath.Base(p.source), total: len(p.ix.Words),
		query: []rune(p.query), reverse: p.reverse, defs: p.defs, showDefs: p.showDefs,
		ordW: digits(len(p.ix.Words)), glyph: p.glyph, glyphW: p.glyphW,
	}

	// Restore the terminal on the way out however we leave, including panics.
	restore := func() {
		fmt.Fprint(tty, cursorShow+altScreenOf) //nolint:errcheck // leaving anyway
		cleanup()
	}
	defer restore()

	// SIGTERM still arrives in raw mode; leave the terminal usable if it does.
	sigs := make(chan os.Signal, 1)
	// signal.Notify with no signals means EVERY signal, so an empty list
	// has to skip the call rather than pass it through.
	if len(hangupSignals) > 0 {
		signal.Notify(sigs, hangupSignals...)
		defer signal.Stop(sigs)
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-sigs:
			restore()
			os.Exit(130)
		case <-done:
		}
	}()

	fmt.Fprint(tty, altScreenOn+cursorHide) //nolint:errcheck // as above

	winch := make(chan os.Signal, 1)
	if len(resizeSignals) > 0 {
		signal.Notify(winch, resizeSignals...)
		defer signal.Stop(winch)
	}

	u.search()
	u.draw()

	kr := newKeyReader(tty)
	for {
		// Redraw promptly on a resize rather than waiting for a keypress.
		select {
		case <-winch:
			u.draw()
			continue
		default:
		}

		k, ok := kr.readKey()
		if !ok {
			return "", nil
		}
		switch k.kind {
		case keyInterrupt, keyEsc:
			return "", nil
		case keyEnter:
			if u.sel < len(u.results) {
				return u.results[u.sel].Word, nil
			}
			return "", nil
		case keyTab:
			// Complete the query to the highlighted word, so you can keep
			// going from a suggestion instead of retyping it.
			if u.sel < len(u.results) {
				u.query = []rune(u.results[u.sel].Word)
				u.search()
			}
		case keyRune:
			u.query = append(u.query, k.r)
			u.search()
		case keyBackspace:
			if len(u.query) > 0 {
				here := u.word()
				u.query = u.query[:len(u.query)-1]
				u.searchKeeping(here)
			}
		case keyDeleteWord:
			here := u.word()
			u.query = []rune(strings.TrimRight(string(u.query), " "))
			if i := strings.LastIndexByte(string(u.query), ' '); i >= 0 {
				u.query = []rune(string(u.query)[:i+1])
			} else {
				u.query = nil
			}
			u.searchKeeping(here)
		case keyToggleDefs:
			u.showDefs = !u.showDefs
			u.defWord = "" // force a refetch at the new width
		case keyClearLine:
			here := u.word()
			u.query = nil
			u.searchKeeping(here)
		case keyUp:
			u.move(u.up())
		case keyDown:
			u.move(-u.up())
		case keyPageUp:
			u.move(u.up() * u.listRows())
		case keyPageDown:
			u.move(-u.up() * u.listRows())
		case keyHome:
			u.sel, u.off = 0, 0
		case keyEnd:
			u.move(len(u.results))
		}
		u.draw()
	}
}

// up reports which way along the result list the Up key moves. The arrow keys
// follow the screen rather than the list index, and in the default layout the
// list is drawn upward from the prompt -- so moving up the screen means moving
// later into the results, not earlier.
func (u *ui) up() int {
	if u.reverse {
		return -1
	}
	return 1
}

func (u *ui) listRows() int {
	n := u.rows - 2 // one header line, one status line
	if n < 1 {
		n = 1
	}
	return n
}

// search re-ranks the list for the current query and puts the selection on
// the best match, which is what typing another letter should do.
func (u *ui) search() {
	// With no query there is nothing to rank, so the cap that keeps sorting
	// cheap serves no purpose -- drop it and let the whole list be scrolled.
	limit := maxResults
	if len(u.query) == 0 {
		limit = 0
	}
	u.results = u.ix.Search(string(u.query), limit)
	u.sel, u.off = 0, 0
}

// searchKeeping re-ranks the list but stays on word if it is still in it.
//
// This is what deleting does, and it is the difference between the query
// being a filter and the query being a way of getting somewhere. Typing
// narrows towards a word, so the best match is where the selection belongs;
// deleting widens back out, and sending the selection to the top of a hundred
// thousand words would throw away the place the letters were typed to reach.
// Keeping it means you can type "zyg", land near zygote, erase it, and go on
// from there with the arrow keys.
//
// A word can fail to survive -- past the first letter the list is capped at
// maxResults, and a word ranked below that is not in it -- and then this is
// an ordinary search, which is the same thing it used to do.
func (u *ui) searchKeeping(word string) {
	u.search()
	if word == "" {
		return
	}
	for i := range u.results {
		if u.results[i].Word == word {
			u.center(i)
			return
		}
	}
}

// word reports the highlighted word, or "" when nothing is highlighted.
func (u *ui) word() string {
	if u.sel < 0 || u.sel >= len(u.results) {
		return ""
	}
	return u.results[u.sel].Word
}

// center puts result i in the middle of the window, which is where the eye
// expects to find a selection that no keypress moved.
func (u *ui) center(i int) {
	u.sel = i
	n := u.listRows()
	u.off = i - n/2
	if hi := len(u.results) - n; u.off > hi {
		u.off = hi
	}
	if u.off < 0 {
		u.off = 0
	}
}

func (u *ui) move(d int) {
	if len(u.results) == 0 {
		return
	}
	u.sel += d
	if u.sel < 0 {
		u.sel = 0
	}
	if u.sel >= len(u.results) {
		u.sel = len(u.results) - 1
	}
	// Keep the selection inside the visible window.
	if u.sel < u.off {
		u.off = u.sel
	}
	if n := u.listRows(); u.sel >= u.off+n {
		u.off = u.sel - n + 1
	}
}

// statusLine reports where the words came from and what matched.
func (u *ui) statusLine() string {
	s := fmt.Sprintf("%s  %d/%d", u.sourceName, len(u.results), u.total)
	// With no query nothing was matched against, so naming a tier would
	// describe a comparison that never happened.
	if u.sel < len(u.results) && u.results[u.sel].Tier != match.TierAll {
		r := u.results[u.sel]
		switch r.Tier {
		case match.TierEdit:
			s += fmt.Sprintf("  %s %d", r.Tier, r.Distance)
		default:
			s += "  " + r.Tier.String()
		}
	}
	if u.showDefs && u.defTitle != "" {
		s += "  ·  " + u.defTitle
	}
	return s
}

// paneWidths splits the screen between the word list and the definition. The
// pane is dropped entirely on a narrow terminal, where a definition column
// would leave too little of either to read.
func (u *ui) paneWidths(w int) (listW, defW int, show bool) {
	if !u.showDefs || w < 60 {
		return w, 0, false
	}
	// The 16..34 bounds are about the column the words are in. Whatever a
	// row draws to the left of them -- the ordinal, and in the character
	// table the character itself -- is added on top, so that turning it on
	// takes its columns from the definition rather than from the words.
	lead := u.lead()
	listW = w/3 + lead
	if listW < 16+lead {
		listW = 16 + lead
	}
	if listW > 34+lead {
		listW = 34 + lead
	}
	return listW, w - listW - 3, true
}

// lead is how many columns a row spends before the selection marker and the
// word: the ordinal, and the character where there is one.
func (u *ui) lead() int {
	n := 0
	if u.ordW > 0 {
		n += u.ordW + 1
	}
	if u.glyph != nil {
		n += u.glyphW + 1
	}
	return n
}

// digits is how many columns the largest ordinal needs.
func digits(n int) int {
	d := 1
	for n >= 10 {
		n /= 10
		d++
	}
	return d
}

// definition returns the wrapped definition of the highlighted word, fetching
// it if the selection has moved. Lookups are cached because the selection is
// reset on every keystroke and would otherwise be refetched constantly.
func (u *ui) definition(width int) []string {
	if u.defs == nil || u.sel >= len(u.results) {
		u.defTitle = ""
		return nil
	}
	word := u.results[u.sel].Word
	if word == u.defWord && width == u.defWidth {
		return u.defLines
	}
	u.defWord, u.defWidth = word, width

	res := u.defs.Define(word)
	if len(res) == 0 {
		u.defTitle = ""
		u.defLines = []string{sgrDim + "no definition" + sgrReset}
		return u.defLines
	}
	u.defTitle = res[0].DB
	if !strings.EqualFold(res[0].Word, word) {
		// Say so when the entry is really for the base form, so a definition
		// of "abolish" under "abolishes" is not mistaken for an exact match.
		u.defTitle += " · " + res[0].Word
	}

	var lines []string
	for i, r := range res {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, dictdb.Wrap(r.Text, width)...)
	}
	u.defLines = lines
	return lines
}

func (u *ui) draw() {
	w, h := u.size()
	u.cols, u.rows = w, h

	listRows := u.listRows()
	n := len(u.results) - u.off
	if n > listRows {
		n = listRows
	}
	if n < 0 {
		n = 0
	}

	listW, defW, pane := u.paneWidths(w)

	// Render the visible matches once, in rank order.
	rendered := make([]string, 0, listRows)
	if n == 0 && len(u.query) > 0 {
		rendered = append(rendered, sgrDim+"  no matches"+sgrReset)
	}
	for i := 0; i < n; i++ {
		idx := u.off + i
		rendered = append(rendered, u.renderRow(u.results[idx], idx == u.sel, listW))
	}

	// Place them on screen rows. The default layout stacks upward from the
	// prompt, so the best match lands on the last row rather than the first.
	left := make([]string, listRows)
	for i, ln := range rendered {
		r := i
		if !u.reverse {
			r = listRows - 1 - i
		}
		if r >= 0 && r < listRows {
			left[r] = ln
		}
	}

	var right []string
	if pane {
		right = u.definition(defW)
	}

	prompt := "> " + string(u.query) + sgrDim + "▏" + sgrReset
	status := sgrDim + truncate(u.statusLine(), w) + sgrReset

	var b bytes.Buffer
	b.WriteString(esc + "[H" + esc + "[2J")

	writeRows := func() {
		for i := 0; i < listRows; i++ {
			if pane {
				b.WriteString(padVisible(left[i], listW))
				b.WriteString(sgrDim + " │ " + sgrReset)
				if i < len(right) {
					b.WriteString(right[i])
				}
			} else {
				b.WriteString(left[i])
			}
			b.WriteString("\r\n")
		}
	}

	if u.reverse {
		// fzf's --layout=reverse: prompt on top, list running downward.
		b.WriteString(prompt + "\r\n")
		b.WriteString(status + "\r\n")
		writeRows()
	} else {
		// fzf's default layout, which is what the shell function this
		// replaces shows: the prompt sits on the bottom line and matches
		// stack upward from just above it.
		writeRows()
		b.WriteString(status + "\r\n")
		b.WriteString(prompt)
	}

	u.out.Write(b.Bytes()) //nolint:errcheck,gosec // a closed terminal is the caller's business
}

// renderRow draws one result, highlighting the runes the query matched.
func (u *ui) renderRow(r match.Result, selected bool, w int) string {
	var b bytes.Buffer
	// The ordinal is where the word is in the list, counted from one -- the
	// number the word has whatever the search did to the order. It sits
	// outside the selection highlight because it is a fact about the list
	// rather than part of the word, and reads better as a dim margin than
	// as the left end of a reversed bar.
	if u.ordW > 0 {
		fmt.Fprintf(&b, "%s%*d %s", sgrDim, u.ordW, r.At+1, sgrReset)
	}
	if selected {
		b.WriteString(sgrSelected)
		b.WriteString("> ")
	} else {
		b.WriteString("  ")
	}
	if u.glyph != nil {
		b.WriteString(u.glyph(r.Word))
		b.WriteString(" ")
	}

	hit := make(map[int]bool, len(r.Positions))
	for _, p := range r.Positions {
		hit[p] = true
	}

	budget := w - u.lead() - 2
	for i, c := range []rune(r.Word) {
		if i >= budget {
			break
		}
		// Recoloring inside a reverse-video row would fight with it, so
		// matched runes are only highlighted on unselected rows.
		if hit[i] && !selected {
			b.WriteString(sgrMatch)
			b.WriteRune(c)
			b.WriteString(sgrReset)
		} else {
			b.WriteRune(c)
		}
	}
	if selected {
		b.WriteString(sgrReset)
	}
	return b.String()
}

// visibleLen measures a string as the terminal will draw it, skipping the SGR
// escapes that carry no width.
//
// Columns, not runes. Most of what is drawn here is one column a rune, but
// the character table draws Unicode itself: an ideograph takes two columns
// and a combining mark takes none, and counting either as one leaves the
// divider between the panes zigzagging down the screen.
func visibleLen(s string) int {
	n := 0
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			j := i + 1
			if j < len(s) && s[j] == '[' {
				for j < len(s) && !(s[j] >= 'A' && s[j] <= 'Z' || s[j] >= 'a' && s[j] <= 'z') {
					j++
				}
				i = j + 1
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if size == 0 {
			break
		}
		i += size
		n += displaywidth.Rune(r)
	}
	return n
}

// padVisible pads or truncates to an exact drawn width, leaving escapes intact.
func padVisible(s string, w int) string {
	n := visibleLen(s)
	if n == w {
		return s
	}
	if n < w {
		return s + strings.Repeat(" ", w-n)
	}
	// Too long: cut on rune boundaries, counting only visible columns, and
	// close any color left open by the cut.
	var b strings.Builder
	count := 0
	for i := 0; i < len(s) && count < w; {
		if s[i] == 0x1b {
			j := i + 1
			if j < len(s) && s[j] == '[' {
				for j < len(s) && !(s[j] >= 'A' && s[j] <= 'Z' || s[j] >= 'a' && s[j] <= 'z') {
					j++
				}
				b.WriteString(s[i : j+1])
				i = j + 1
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if size == 0 {
			break
		}
		// A two-column character straddling the cut has to be dropped
		// whole, which can leave the row a column short of the width.
		rw := displaywidth.Rune(r)
		if count+rw > w {
			break
		}
		b.WriteRune(r)
		i += size
		count += rw
	}
	b.WriteString(sgrReset)
	if count < w {
		b.WriteString(strings.Repeat(" ", w-count))
	}
	return b.String()
}

func truncate(s string, w int) string {
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	if w <= 1 {
		return ""
	}
	return string(r[:w-1])
}
