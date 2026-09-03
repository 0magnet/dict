// Package cmd rainglow.go
//
// Words found reading ACROSS the rain.
//
// The streams fall vertically and each spells a word downward. Now and
// then the letters that happen to sit side by side on one row spell a
// word too, by accident, and this finds those and holds them lit.
//
// Three rules, and each one is what makes it read as a discovery rather
// than as noise:
//
// A dark cell breaks a run. Only letters currently lit count, so a word
// cannot be completed by a letter that has already faded — the run has to
// be there, on the screen, at that moment.
//
// A found word stops fading. The trail behind a stream dims as it falls;
// a held word does not, so it stays legible while the rain moves on
// around it.
//
// A hold ends when new rain writes over it. That is the only thing that
// ends it: the word stays until a stream puts a different letter in one
// of its cells.
package cmd

import (
	"github.com/gdamore/tcell/v3"

	"github.com/0magnet/termanim/canvas"
	"github.com/0magnet/termanim/matrix"
)

// glowMin and glowMax bound what counts as a find.
//
// Three letters match constantly — "the", "and", "ear" fall out of any
// row of English letters — and the screen becomes a field of highlights
// that means nothing. Four is where a match starts being a surprise.
const (
	glowMin = 4
	glowMax = 16
)

// rainGlow is the rain with horizontal finds held lit.
//
// It drives the matrix and draws the screen itself rather than calling
// matrix.Frame, because holding a cell means overriding what the trail
// would otherwise have drawn there. Nothing about the simulation
// changes; only what is made of it.
type rainGlow struct {
	m     *matrix.Matrix
	words map[string]struct{}

	cols, rows int

	// holds are the words found, each kept whole. A word is one thing, so
	// new rain touching any part of it takes the whole highlight away
	// rather than eating it a letter at a time.
	holds []hold

	// glow is the color laid BEHIND a found word. The letters keep the
	// rain's own green-to-black fade: recoloring them would trade the
	// nicest thing the animation does for a label.
	glow tcell.Color
}

// hold is one found word, held until new rain disturbs it.
type hold struct {
	y, x0 int
	runes []rune
}

func newRainGlow(m *matrix.Matrix, words []string) *rainGlow {
	set := make(map[string]struct{}, len(words))
	for _, w := range words {
		if len(w) >= glowMin && len(w) <= glowMax {
			set[w] = struct{}{}
		}
	}
	return &rainGlow{m: m, words: set, glow: tcell.NewRGBColor(255, 196, 61)}
}

func (g *rainGlow) Resize(cols, rows int) {
	g.m.Resize(cols, rows)
	g.cols, g.rows = cols, rows
	g.holds = nil
}

// Frame advances the simulation, finds the words across it, and draws.
func (g *rainGlow) Frame(screen tcell.Screen, cols, rows int, dt float64) {
	if cols != g.cols || rows != g.rows {
		g.Resize(cols, rows)
	}
	g.m.AdvanceTime(dt)

	// This frame's lit cells. A zero rune is a dark cell, and a dark cell
	// is what breaks a run: a word cannot be finished by a letter that has
	// already faded.
	glyph := make([]rune, cols*rows)
	inten := make([]int, cols*rows)
	hot := make([]bool, cols*rows)
	g.m.Cells(func(x, y int, c matrix.Cell) {
		i := y*cols + x
		glyph[i], inten[i], hot[i] = c.Rune, c.Intensity, c.Hot
	})

	g.expireHolds(glyph)

	// Words are looked for in the lit cells AND the held ones. A held word
	// whose letters have faded is still on the screen — that is what holding
	// it means — so rain landing beside it can complete a longer word
	// through it. Scanning only the lit cells would make "rain" and a new
	// "b" beside it two separate things rather than "brain".
	g.findWords(g.scanGrid(glyph))
	g.absorbHolds()
	g.draw(screen, glyph, inten, hot)
}

// scanGrid is what the finder reads: the lit letters, with the held ones
// filled in where the rain has faded past them.
func (g *rainGlow) scanGrid(glyph []rune) []rune {
	out := make([]rune, len(glyph))
	copy(out, glyph)
	for _, h := range g.holds {
		for k, r := range h.runes {
			if i := h.y*g.cols + h.x0 + k; out[i] == 0 {
				out[i] = r
			}
		}
	}
	return out
}

// absorbHolds drops any word wholly inside another on the same row.
//
// When rain completes a longer word through a held one — "rain" becoming
// "brain" — both are found, and keeping both would mean the shorter one
// could dissolve on its own and leave a hole in the middle of the longer
// highlight. The longer word is the one on the screen.
func (g *rainGlow) absorbHolds() {
	kept := g.holds[:0]
	for i, h := range g.holds {
		inside := false
		for j, o := range g.holds {
			if i == j || h.y != o.y {
				continue
			}
			if o.x0 <= h.x0 && h.x0+len(h.runes) <= o.x0+len(o.runes) &&
				len(o.runes) > len(h.runes) {
				inside = true
				break
			}
		}
		if !inside {
			kept = append(kept, h)
		}
	}
	g.holds = kept
}

// expireHolds drops every word new rain has disturbed.
//
// A word is held whole, so ANY letter of it being overwritten dissolves
// the whole highlight rather than leaving a partial one behind.
//
// A dark cell is the trail having faded past and does NOT disturb
// anything: the highlight outliving the fade is the point of it.
func (g *rainGlow) expireHolds(glyph []rune) {
	kept := g.holds[:0]
	for _, h := range g.holds {
		if !h.disturbed(glyph, g.cols) {
			kept = append(kept, h)
		}
	}
	g.holds = kept
}

// disturbed reports whether new rain has written a different letter into
// any cell of the word.
func (h hold) disturbed(glyph []rune, cols int) bool {
	for k, r := range h.runes {
		if c := glyph[h.y*cols+h.x0+k]; c != 0 && c != r {
			return true
		}
	}
	return false
}

// findWords scans each row left to right for runs of lit letters and
// holds every dictionary word inside one.
//
// Overlaps are kept rather than resolved. Two words sharing a letter are
// both true, and picking one would mean explaining which.
func (g *rainGlow) findWords(glyph []rune) {
	for y := 0; y < g.rows; y++ {
		row := glyph[y*g.cols : (y+1)*g.cols]
		for start := 0; start < len(row); {
			if !isLetter(row[start]) {
				start++
				continue
			}
			end := start
			for end < len(row) && isLetter(row[end]) {
				end++
			}
			g.holdWordsIn(row[start:end], y, start)
			start = end
		}
	}
}

// holdWordsIn marks every dictionary word inside one run of letters.
func (g *rainGlow) holdWordsIn(run []rune, y, x0 int) {
	s := string(run)
	n := len(run)
	for i := 0; i+glyphMinRun <= n; i++ {
		max := glowMax
		if i+max > n {
			max = n - i
		}
		for l := max; l >= glowMin; l-- {
			if _, ok := g.words[s[i:i+l]]; !ok {
				continue
			}
			g.addHold(hold{y: y, x0: x0 + i, runes: append([]rune(nil), run[i:i+l]...)})
			break // the longest word at this start wins
		}
	}
}

// addHold records a word, unless the same span is already held. Without
// the check a word sitting still under the scan would be added every
// frame and the slice would grow without bound.
func (g *rainGlow) addHold(h hold) {
	for _, e := range g.holds {
		if e.y == h.y && e.x0 == h.x0 && len(e.runes) == len(h.runes) {
			return
		}
	}
	g.holds = append(g.holds, h)
}

// glyphMinRun is the shortest run worth scanning at all.
const glyphMinRun = glowMin

func isLetter(r rune) bool { return r >= 'a' && r <= 'z' }

// draw paints the frame.
//
// A held word gets the glow BEHIND it and nothing else: the letters keep
// the rain's own fade from bright green to black, which is the part
// worth looking at. Recoloring them would replace an animation with a
// label.
func (g *rainGlow) draw(screen tcell.Screen, glyph []rune, inten []int, hot []bool) {
	held := g.heldRunes()
	for y := 0; y < g.rows; y++ {
		for x := 0; x < g.cols; x++ {
			i := y*g.cols + x

			if held[i] != 0 {
				// A held cell keeps showing its letter for as long as the word
				// is held, whatever the rain is doing over it. While the trail
				// is still there the letter fades with it; once the trail has
				// gone the letter stays, in black on the glow, because a word
				// that has faded to nothing cannot be read and being able to
				// read it is the entire point of holding it.
				// Explicit RGB rather than tcell.ColorBlack: a named color can be
				// left to the terminal, and a held letter has to be drawn, not
				// left to whatever was on the cell before.
				fg := tcell.NewRGBColor(0, 0, 0)
				if glyph[i] != 0 {
					fg = g.m.Palette[inten[i]]
				}
				canvas.PutRune(screen, x, y, held[i], tcell.StyleDefault.Foreground(fg).Background(g.glow))
				continue
			}

			if glyph[i] == 0 {
				screen.Put(x, y, canvas.Blank, tcell.StyleDefault) //nolint:errcheck // one cell cannot fail
				continue
			}
			st := tcell.StyleDefault.Foreground(g.m.Palette[inten[i]])
			if hot[i] {
				st = st.Bold(true)
			}
			canvas.PutRune(screen, x, y, glyph[i], st)
		}
	}
}

// heldRunes gives the letter each held cell is holding, or zero where no
// word is held, so draw can ask per cell rather than searching the holds
// for each one.
//
// It returns the LETTER rather than a flag because a held cell has to go
// on showing its letter after the rain has faded past it. The word is
// what is being held; a highlight over blanks holds nothing.
func (g *rainGlow) heldRunes() []rune {
	out := make([]rune, g.cols*g.rows)
	for _, h := range g.holds {
		for k, r := range h.runes {
			out[h.y*g.cols+h.x0+k] = r
		}
	}
	return out
}
