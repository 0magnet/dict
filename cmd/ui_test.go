package cmd

import (
	"strings"
	"testing"

	"github.com/0magnet/dict/match"
)

// newTestUI builds a ui without a terminal. move() and up() only touch the
// result list and the row count, so they can be exercised directly.
func newTestUI(n int, reverse bool) *ui {
	results := make([]match.Result, n)
	for i := range results {
		results[i] = match.Result{Word: string(rune('a' + i))}
	}
	return &ui{results: results, rows: 12, reverse: reverse}
}

// TestArrowsFollowTheScreen pins the direction the arrow keys move in each
// layout. The arrows follow the screen, not the list index: in the default
// layout the list is drawn upward from the prompt, so pressing Up moves later
// into the results. Getting this backwards makes Up appear to do nothing,
// since the selection starts on the first result.
func TestArrowsFollowTheScreen(t *testing.T) {
	t.Run("default layout: up moves later into the list", func(t *testing.T) {
		u := newTestUI(10, false)
		if u.up() != 1 {
			t.Fatalf("up() = %d, want 1", u.up())
		}
		u.move(u.up())
		if u.sel != 1 {
			t.Errorf("after Up, sel = %d, want 1", u.sel)
		}
		u.move(u.up())
		u.move(u.up())
		if u.sel != 3 {
			t.Errorf("after three Ups, sel = %d, want 3", u.sel)
		}
		u.move(-u.up())
		if u.sel != 2 {
			t.Errorf("after a Down, sel = %d, want 2", u.sel)
		}
		// Down at the first result must not wrap or go negative.
		u.sel = 0
		u.move(-u.up())
		if u.sel != 0 {
			t.Errorf("Down at the best match moved to %d, want 0", u.sel)
		}
	})

	t.Run("reverse layout: up moves earlier, as usual", func(t *testing.T) {
		u := newTestUI(10, true)
		if u.up() != -1 {
			t.Fatalf("up() = %d, want -1", u.up())
		}
		u.move(u.up())
		if u.sel != 0 {
			t.Errorf("Up at the top moved to %d, want 0", u.sel)
		}
		u.move(-u.up())
		u.move(-u.up())
		if u.sel != 2 {
			t.Errorf("after two Downs, sel = %d, want 2", u.sel)
		}
	})
}

func TestMoveClampsAndScrolls(t *testing.T) {
	u := newTestUI(100, false)
	u.move(1000)
	if u.sel != 99 {
		t.Errorf("moving past the end left sel = %d, want 99", u.sel)
	}
	// The window must have followed the selection.
	if u.sel < u.off || u.sel >= u.off+u.listRows() {
		t.Errorf("selection %d outside the visible window [%d,%d)", u.sel, u.off, u.off+u.listRows())
	}
	u.move(-1000)
	if u.sel != 0 || u.off != 0 {
		t.Errorf("moving back gave sel=%d off=%d, want 0 and 0", u.sel, u.off)
	}
}

func TestMoveOnEmptyResults(t *testing.T) {
	u := newTestUI(0, false)
	u.move(u.up()) // must not panic or leave sel out of range
	if u.sel != 0 {
		t.Errorf("sel = %d on an empty list, want 0", u.sel)
	}
}

// newSearchUI builds a ui over a real index, so that search() and
// searchKeeping() run the matcher rather than a stand-in for it.
func newSearchUI(words []string) *ui {
	ix := match.NewIndex(words)
	u := &ui{ix: ix, rows: 12, total: len(words), marginW: digits(len(words))}
	u.search()
	return u
}

// TestDeletingKeepsTheWord is the difference between the query being a filter
// and the query being a way of getting somewhere: typing letters to reach a
// word and then erasing them must leave the selection on that word, ready for
// the arrow keys, rather than back at the top of the whole list.
func TestDeletingKeepsTheWord(t *testing.T) {
	words := []string{"aardvark", "abacus", "yarn", "zygote", "zymurgy"}
	u := newSearchUI(words)

	// Typing goes to the best match, which is what it did before.
	for _, r := range "zygote" {
		u.query = append(u.query, r)
		u.search()
	}
	if u.word() != "zygote" {
		t.Fatalf("after typing zygote, selection is %q", u.word())
	}

	// Erasing it leaves the selection there rather than at "aardvark".
	for len(u.query) > 0 {
		here := u.word()
		u.query = u.query[:len(u.query)-1]
		u.searchKeeping(here)
	}
	if len(u.results) != len(words) {
		t.Fatalf("an empty query gave %d results, want the whole list", len(u.results))
	}
	if u.word() != "zygote" {
		t.Errorf("after erasing the query, selection is %q, want zygote", u.word())
	}

	// And the arrow keys go on from there rather than from the top.
	u.move(-1)
	if u.word() != "yarn" {
		t.Errorf("Down from zygote gave %q, want yarn", u.word())
	}
}

// TestSearchKeepingFallsBackToTheBestMatch covers the word that did not
// survive the new query, which has to behave like an ordinary search.
func TestSearchKeepingFallsBackToTheBestMatch(t *testing.T) {
	u := newSearchUI([]string{"alpha", "beta", "gamma"})
	u.query = []rune("beta")
	u.searchKeeping("nothing is called this")
	if u.sel != 0 || u.off != 0 {
		t.Errorf("sel=%d off=%d, want the best match at the top", u.sel, u.off)
	}
	if u.word() != "beta" {
		t.Errorf("selection is %q, want beta", u.word())
	}
}

// TestCenterKeepsTheSelectionOnScreen: a selection restored by
// searchKeeping is not reached by move(), so it has to bring the window
// with it.
func TestCenterKeepsTheSelectionOnScreen(t *testing.T) {
	u := newTestUI(100, false)
	for _, i := range []int{0, 5, 50, 99} {
		u.center(i)
		if u.sel != i {
			t.Fatalf("center(%d) selected %d", i, u.sel)
		}
		if u.sel < u.off || u.sel >= u.off+u.listRows() {
			t.Errorf("center(%d) left it outside the window [%d,%d)", i, u.off, u.off+u.listRows())
		}
		if u.off < 0 {
			t.Errorf("center(%d) scrolled past the start: off=%d", i, u.off)
		}
	}
}

// TestRowShowsTheOrdinal pins the number to the left of the marker: it is the
// word's place in the word list counted from one, and it does not change when
// the search reorders the results.
func TestRowShowsTheOrdinal(t *testing.T) {
	words := []string{"aardvark", "abacus", "yarn", "zygote"}
	u := newSearchUI(words)
	u.query = []rune("zygote")
	u.search()

	row := u.renderRow(u.results[0], true, 40)
	if got := visibleLen(strings.TrimRight(stripSGR(row), " ")); got == 0 {
		t.Fatal("empty row")
	}
	// zygote is the fourth word, so the first row reads "4 > zygote"
	// however well it ranked.
	if want := "4 > zygote"; stripSGR(row) != want {
		t.Errorf("row = %q, want %q", stripSGR(row), want)
	}
}

// TestRowDrawsWhatTheEntryStandsFor: an entry that names a character is drawn
// as that character, and an ordinary word is drawn as itself. A two-column
// character must still leave the row the width it claims, or the divider
// between the panes zigzags.
func TestRowDrawsWhatTheEntryStandsFor(t *testing.T) {
	stands := map[string]string{"NARROW": "x", "WIDE": "中"}
	u := &ui{
		ix:   match.NewIndex([]string{"NARROW", "WIDE", "word"}),
		rows: 12, total: 3, marginW: 1,
		display: func(w string) string { return stands[w] },
	}
	u.search()
	for i, want := range []string{"1 > x", "2   中", "3   word"} {
		row := u.renderRow(u.results[i], i == 0, 40)
		if got := stripSGR(row); got != want {
			t.Errorf("row %d = %q, want %q", i, got, want)
		}
		if got := visibleLen(padVisible(row, 20)); got != 20 {
			t.Errorf("row %d padded to %d columns, want 20", i, got)
		}
	}
}

// TestSubstitutedRowIsNotHighlighted: the match positions are rune indices
// into the name that was searched, and on a row drawing the character in its
// place they would color the wrong thing -- or, past the end of a one-rune
// row, nothing at all while still emitting the escapes.
func TestSubstitutedRowIsNotHighlighted(t *testing.T) {
	u := &ui{
		ix:   match.NewIndex([]string{"SNOWMAN"}),
		rows: 12, total: 1, marginW: 1,
		display: func(string) string { return "☃" },
	}
	u.query = []rune("snowman")
	u.search()
	if len(u.results) != 1 || len(u.results[0].Positions) == 0 {
		t.Fatal("expected an exact match carrying highlight positions")
	}
	if row := u.renderRow(u.results[0], false, 40); strings.Contains(row, sgrMatch) {
		t.Errorf("the character row carries match highlighting: %q", row)
	}
}

// stripSGR removes the color escapes, leaving what is actually on screen.
func stripSGR(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && !(s[j] >= 'A' && s[j] <= 'Z' || s[j] >= 'a' && s[j] <= 'z') {
				j++
			}
			i = j + 1
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// TestMarginShowsTheCodeForACharacter pins what the dim column holds. A word
// gets its place in the list; a character gets its code point, which is the
// only thing that tells two glyphs apart when a terminal draws both as the
// same smudge. Both are right-aligned in one column wide enough for either.
func TestMarginShowsTheCodeForACharacter(t *testing.T) {
	codes := map[string]string{"SNOWMAN": "U+2603"}
	u := &ui{
		ix:   match.NewIndex([]string{"word", "SNOWMAN"}),
		rows: 12, total: 2, marginW: 6,
		display: func(w string) string {
			if w == "SNOWMAN" {
				return "☃"
			}
			return ""
		},
		code: func(w string) string { return codes[w] },
	}
	u.search()
	for i, want := range []string{"     1   word", "U+2603   ☃"} {
		if got := stripSGR(u.renderRow(u.results[i], false, 40)); got != want {
			t.Errorf("row %d = %q, want %q", i, got, want)
		}
	}
}
