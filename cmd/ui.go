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
	state, err := term.MakeRaw(int(tty.Fd()))
	if err != nil {
		tty.Close() //nolint:errcheck // the open succeeded; the failure to report is the raw one
		return nil, nil, nil, fmt.Errorf("cannot set raw mode: %w", err)
	}
	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			term.Restore(int(tty.Fd()), state) //nolint:errcheck // leaving anyway
			tty.Close()                        //nolint:errcheck // as above
		})
	}
	size := func() (int, int) {
		w, h, err := term.GetSize(int(tty.Fd()))
		if err != nil || w <= 0 || h <= 0 {
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

// run drives the interactive search and returns the chosen word, or "" if the
// user quit. For a process the interface is drawn on /dev/tty so that stdout
// carries only the result and stays usable in a pipeline; a host that is not a
// process supplies its own terminal through Host.TTY, because there is no
// /dev/tty in js/wasm and no termios behind it.
func runInteractive(ix *match.Index, query, source string, reverse bool, defs *dictdb.Set, showDefs bool) (string, error) {
	tty, size, cleanup, err := openTTY()
	if err != nil {
		return "", err
	}

	u := &ui{
		out: tty, size: size, ix: ix, sourceName: filepath.Base(source), total: len(ix.Words),
		query: []rune(query), reverse: reverse, defs: defs, showDefs: showDefs,
	}

	// Restore the terminal on the way out however we leave, including panics.
	restore := func() {
		fmt.Fprint(tty, cursorShow+altScreenOf)
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

	fmt.Fprint(tty, altScreenOn+cursorHide)

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
				u.query = u.query[:len(u.query)-1]
				u.search()
			}
		case keyDeleteWord:
			u.query = []rune(strings.TrimRight(string(u.query), " "))
			if i := strings.LastIndexByte(string(u.query), ' '); i >= 0 {
				u.query = []rune(string(u.query)[:i+1])
			} else {
				u.query = nil
			}
			u.search()
		case keyToggleDefs:
			u.showDefs = !u.showDefs
			u.defWord = "" // force a refetch at the new width
		case keyClearLine:
			u.query = nil
			u.search()
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
	listW = w / 3
	if listW < 16 {
		listW = 16
	}
	if listW > 34 {
		listW = 34
	}
	return listW, w - listW - 3, true
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

	u.out.Write(b.Bytes()) //nolint:errcheck // a closed terminal is the caller's business
}

// renderRow draws one result, highlighting the runes the query matched.
func (u *ui) renderRow(r match.Result, selected bool, w int) string {
	var b bytes.Buffer
	if selected {
		b.WriteString(sgrSelected)
		b.WriteString("> ")
	} else {
		b.WriteString("  ")
	}

	hit := make(map[int]bool, len(r.Positions))
	for _, p := range r.Positions {
		hit[p] = true
	}

	budget := w - 2
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
		_, size := utf8.DecodeRuneInString(s[i:])
		if size == 0 {
			break
		}
		i += size
		n++
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
	// Too long: cut on rune boundaries, counting only visible runes, and
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
		b.WriteRune(r)
		i += size
		count++
	}
	b.WriteString(sgrReset)
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
