package cmd

import (
	"testing"

	"github.com/0magnet/termanim/matrix"

	"github.com/0magnet/dict/data"
)

// scan drives the finder without a screen, so the rules can be tested
// apart from the drawing.
func (g *rainGlow) scan(rows []string) []rune {
	g.cols, g.rows = len(rows[0]), len(rows)
	g.holds = nil
	return g.rescan(rows)
}

// rescan feeds another frame to an existing glow, keeping its holds.
func (g *rainGlow) rescan(rows []string) []rune {
	glyph := make([]rune, g.cols*g.rows)
	for y, r := range rows {
		for x, c := range []rune(r) {
			if c != ' ' {
				glyph[y*g.cols+x] = c
			}
		}
	}
	g.expireHolds(glyph)
	g.findWords(g.scanGrid(glyph))
	g.absorbHolds()
	return glyph
}

func newTestGlow(words ...string) *rainGlow {
	return newRainGlow(matrix.New(1), words)
}

// heldWords reads back what is currently held.
func (g *rainGlow) heldWords() []string {
	out := make([]string, 0, len(g.holds))
	for _, h := range g.holds {
		out = append(out, string(h.runes))
	}
	return out
}

func TestGlowFindsWordAcross(t *testing.T) {
	g := newTestGlow("rain", "word")
	g.scan([]string{"xxrainxx"})
	got := g.heldWords()
	if len(got) != 1 || got[0] != "rain" {
		t.Errorf("held %v, want [rain]", got)
	}
}

// A word cannot be completed by a letter that has already faded.
func TestGlowRefusesToSpanADarkCell(t *testing.T) {
	g := newTestGlow("rain")
	g.scan([]string{"ra in"})
	if got := g.heldWords(); len(got) != 0 {
		t.Errorf("held %v across a dark cell, want none", got)
	}
}

// Three-letter words match constantly and would make the screen a field
// of highlights.
func TestGlowIgnoresShortMatches(t *testing.T) {
	g := newTestGlow("the")
	g.scan([]string{"xxthexx"})
	if got := g.heldWords(); len(got) != 0 {
		t.Errorf("held %v below the minimum length", got)
	}
}

// The highlight outliving the fade is the point of it.
func TestGlowSurvivesTheFade(t *testing.T) {
	g := newTestGlow("rain")
	g.scan([]string{"rain"})
	g.rescan([]string{"    "}) // every letter faded out
	if got := g.heldWords(); len(got) != 1 {
		t.Errorf("held %v after the letters faded, want [rain]", got)
	}
}

// New rain touching ANY letter takes the WHOLE word away, rather than
// eating the highlight one cell at a time.
func TestGlowDissolvesWholeWordOnOneHit(t *testing.T) {
	g := newTestGlow("rain")
	g.scan([]string{"rain"})
	if len(g.holds) != 1 {
		t.Fatalf("setup: held %v", g.heldWords())
	}
	g.rescan([]string{"rzin"}) // one letter overwritten
	if got := g.heldWords(); len(got) != 0 {
		t.Errorf("held %v after one letter was overwritten, want none", got)
	}
}

func TestGlowPrefersTheLongerWord(t *testing.T) {
	g := newTestGlow("rain", "rainy")
	g.scan([]string{"rainy"})
	got := g.heldWords()
	if len(got) != 1 || got[0] != "rainy" {
		t.Errorf("held %v, want [rainy]", got)
	}
}

// A word sitting still must not be recorded again every frame.
func TestGlowDoesNotAccumulateDuplicates(t *testing.T) {
	g := newTestGlow("rain")
	g.scan([]string{"rain"})
	for i := 0; i < 20; i++ {
		g.rescan([]string{"rain"})
	}
	if len(g.holds) != 1 {
		t.Errorf("%d holds after 20 identical frames, want 1", len(g.holds))
	}
}

// The interesting question is whether horizontal words occur in the
// vertical rain often enough to be worth showing.
func TestGlowFindsWordsInRealRain(t *testing.T) {
	all, err := data.Words()
	if err != nil {
		t.Skip("no word list")
	}
	m := matrix.New(5)
	m.Words = rainVocabulary(5)
	m.FreshWords = true
	g := newRainGlow(m, all)
	g.Resize(100, 30)

	found := map[string]bool{}
	for step := 0; step < 400; step++ {
		m.Advance(1)
		glyph := make([]rune, g.cols*g.rows)
		m.Cells(func(x, y int, c matrix.Cell) { glyph[y*g.cols+x] = c.Rune })
		g.expireHolds(glyph)
		g.findWords(glyph)
		for _, w := range g.heldWords() {
			found[w] = true
		}
	}
	if len(found) == 0 {
		t.Fatal("400 steps of rain and not one word across it")
	}
	t.Logf("found %d distinct words across the rain", len(found))
	n := 0
	for w := range found {
		if n++; n > 10 {
			break
		}
		t.Logf("  %s", w)
	}
}

// TestGlowKeepsTheLetterAfterTheFade: a held word has to stay READABLE.
// Drawing a blank on the highlight leaves a colored band with nothing in
// it, which is the one thing the highlight exists to prevent.
func TestGlowKeepsTheLetterAfterTheFade(t *testing.T) {
	g := newTestGlow("rain")
	g.scan([]string{"rain"})
	// Every letter has faded out of the trail.
	g.rescan([]string{"    "})

	held := g.heldRunes()
	want := []rune("rain")
	for i, w := range want {
		if held[i] != w {
			t.Errorf("cell %d holds %q after the fade, want %q", i, held[i], w)
		}
	}
}

// TestGlowExtendsThroughAHeldWord: rain landing beside a held word that
// completes a longer one must expand the highlight, not sit next to it.
func TestGlowExtendsThroughAHeldWord(t *testing.T) {
	g := newTestGlow("rain", "brain")
	g.scan([]string{" rain"})
	if got := g.heldWords(); len(got) != 1 || got[0] != "rain" {
		t.Fatalf("setup: held %v, want [rain]", got)
	}

	// The letters fade, and a stream drops a "b" into the cell before it.
	g.rescan([]string{"b    "})
	got := g.heldWords()
	if len(got) != 1 || got[0] != "brain" {
		t.Errorf("held %v, want [brain] — the highlight should have grown", got)
	}
}

// TestGlowAbsorbsTheShorterWord: the shorter word must not survive
// alongside the longer one, or it could dissolve on its own and leave a
// hole in the middle of the highlight.
func TestGlowAbsorbsTheShorterWord(t *testing.T) {
	g := newTestGlow("rain", "brain")
	g.scan([]string{"brain"})
	got := g.heldWords()
	if len(got) != 1 || got[0] != "brain" {
		t.Errorf("held %v, want only [brain]", got)
	}
}

// TestGlowExtensionStillDissolves: the grown highlight is one word, so
// disturbing any of it — including the part that was held first — takes
// all of it.
func TestGlowExtensionStillDissolves(t *testing.T) {
	g := newTestGlow("rain", "brain")
	g.scan([]string{" rain"})
	g.rescan([]string{"b    "})
	if got := g.heldWords(); len(got) != 1 || got[0] != "brain" {
		t.Fatalf("setup: held %v", got)
	}
	// New rain writes a different letter into the middle.
	g.rescan([]string{"bzain"})
	if got := g.heldWords(); len(got) != 0 {
		t.Errorf("held %v after the word was disturbed, want none", got)
	}
}
