package cmd

import (
	"strings"
	"testing"

	"github.com/0magnet/dict/data"
	"github.com/0magnet/dict/match"
)

// TestDefineResolvesMisspelling covers the point of the -d flag: it answers
// "what does this mean" for a word the user could not spell, by going through
// the same matcher as the interactive view rather than requiring an exact hit.
func TestDefineResolvesMisspelling(t *testing.T) {
	words, _, err := loadWordList("", "")
	if err != nil {
		t.Fatal(err)
	}
	ix := match.NewIndex(words)
	defs := data.OpenSet(nil)

	for _, tc := range []struct{ query, want string }{
		{"recieve", "receive"},
		{"seperate", "separate"},
		{"acheive", "achieve"},
	} {
		res := ix.Search(tc.query, 1)
		if len(res) == 0 {
			t.Errorf("%s: no match", tc.query)
			continue
		}
		if !strings.EqualFold(res[0].Word, tc.want) {
			t.Errorf("%s matched %q, want %q", tc.query, res[0].Word, tc.want)
			continue
		}
		if d := defs.Define(res[0].Word); len(d) == 0 {
			t.Errorf("%s -> %s: no definition", tc.query, res[0].Word)
		}
	}
}

func TestTerminalWidthIsSane(t *testing.T) {
	// Not a terminal under test, so this exercises the fallback.
	if w := terminalWidth(); w < 40 || w > 100 {
		t.Errorf("terminalWidth() = %d, want something readable", w)
	}
}

func TestRandomPool(t *testing.T) {
	ix := match.NewIndex([]string{"cat", "cat's", "cats", "dog", "dog's"})

	// Possessives are poor things to be handed at random, so they are left
	// out whether or not a query narrowed the pool.
	pool := randomPool(ix, "")
	for _, w := range pool {
		if strings.Contains(w, "'") {
			t.Errorf("possessive %q in the unqueried pool", w)
		}
	}
	if len(pool) != 3 {
		t.Errorf("unqueried pool holds %d words, want 3", len(pool))
	}

	pool = randomPool(ix, "dog")
	for _, w := range pool {
		if strings.Contains(w, "'") {
			t.Errorf("possessive %q in the queried pool", w)
		}
		if !strings.Contains(w, "dog") {
			t.Errorf("%q does not match the query", w)
		}
	}

	// When everything matching is a possessive, those are better than nothing.
	only := match.NewIndex([]string{"cat's"})
	if got := randomPool(only, ""); len(got) != 1 {
		t.Errorf("a pool of only possessives came back empty: %v", got)
	}
}

func TestPickUnseenExhausts(t *testing.T) {
	seen := map[int]bool{}
	for i := 0; i < 5; i++ {
		if got := pickUnseen(5, seen); got < 0 {
			t.Fatalf("ran out after %d draws of 5", i)
		}
	}
	if got := pickUnseen(5, seen); got != -1 {
		t.Errorf("expected exhaustion, got %d", got)
	}
	if len(seen) != 5 {
		t.Errorf("drew %d distinct values, want 5", len(seen))
	}
}

// TestWorksWithNothingInstalled is the guarantee the embedded data exists for:
// the word list and every dictionary are in the binary, so a machine with no
// words package and no dictd databases still gets a full answer.
//
// data.OpenSet(nil) is exactly what the command falls back to when nothing is
// found on disk, and data.Words() is the embedded list.
func TestWorksWithNothingInstalled(t *testing.T) {
	words, err := data.Words()
	if err != nil {
		t.Fatalf("embedded word list unreadable: %v", err)
	}
	if len(words) < 100000 {
		t.Errorf("embedded word list holds %d words, expected ~124k", len(words))
	}

	defs := data.OpenSet(nil) // nil dirs: embedded only, nothing from disk
	for _, w := range []string{"weird", "receive", "paracetamol", "Jaipur"} {
		if got := defs.Define(w); len(got) == 0 {
			t.Errorf("%s: no definition from the embedded data alone", w)
		}
	}

	// And the matcher must work over the embedded list.
	ix := match.NewIndex(words)
	res := ix.Search("recieve", 1)
	if len(res) == 0 || !strings.EqualFold(res[0].Word, "receive") {
		t.Errorf("recieve did not resolve to receive using the embedded list: %v", res)
	}
}
