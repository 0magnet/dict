package dictdb_test

import (
	"strings"
	"testing"

	"github.com/0magnet/dict/data"
	"github.com/0magnet/dict/dictdb"
)

// embedded builds exactly the chain the command builds, minus any installed
// dictionaries, so the coverage figure measured here is the one users get.
func embedded(t testing.TB) *dictdb.Set {
	t.Helper()
	return data.OpenSet(nil)
}

func TestDefineFromEmbedded(t *testing.T) {
	s := embedded(t)
	res := s.Define("weird")
	if len(res) == 0 {
		t.Fatal("no definition for weird")
	}
	if res[0].DB != "gcide" {
		t.Errorf("first result came from %q, want gcide -- GCIDE must lead", res[0].DB)
	}
	if !strings.Contains(res[0].Text, "Fate") {
		t.Errorf("gcide entry for weird looks wrong: %.120q", res[0].Text)
	}
	// The 1913 markup must be gone.
	for _, bad := range []string{"[1913 Webster]", `\Weird\`, "{Worth}", "[=e]"} {
		if strings.Contains(res[0].Text, bad) {
			t.Errorf("raw markup %q survived cleaning", bad)
		}
	}
}

func TestWordNetFallsBackForModernWords(t *testing.T) {
	s := embedded(t)
	// GCIDE is from 1913 and has no entry; WordNet must supply one.
	res := s.Define("multinational")
	if len(res) == 0 {
		t.Fatal("no definition for multinational")
	}
	if res[0].DB == "gcide" {
		t.Errorf("unexpectedly found in gcide; the fallback is untested by this word")
	}
}

func TestLemmaFallback(t *testing.T) {
	s := embedded(t)
	// These are inflections that no dictionary lists as a headword, so they
	// can only be reached through a base form. Many inflections -- "running",
	// "hottest" -- are headwords in GCIDE in their own right and so never
	// exercise this path.
	for _, tc := range []struct{ word, want string }{
		{"abolishes", "abolish"},
		{"aborting", "abort"},
		{"abseiling", "abseil"},
		{"smoggiest", "smoggy"},
	} {
		res := s.Define(tc.word)
		if len(res) == 0 {
			t.Errorf("%s: no definition found via any lemma", tc.word)
			continue
		}
		if res[0].Word != tc.want {
			t.Errorf("%s resolved via %q, want %q", tc.word, res[0].Word, tc.want)
		}
	}
}

func TestExactBeatsLemma(t *testing.T) {
	s := embedded(t)
	// "saw" is a word in its own right; it must not resolve to "see".
	res := s.Define("saw")
	if len(res) == 0 {
		t.Fatal("no definition for saw")
	}
	if res[0].Word != "saw" {
		t.Errorf("saw resolved via %q; an exact entry must win over a lemma", res[0].Word)
	}
}

func TestEmbeddedWordList(t *testing.T) {
	w, err := data.Words()
	if err != nil {
		t.Fatal(err)
	}
	if len(w) < 100000 {
		t.Errorf("embedded word list holds %d words, expected ~124k", len(w))
	}
}

func BenchmarkFirstLookup(b *testing.B) {
	for i := 0; i < b.N; i++ {
		s := &dictdb.Set{}
		idx, body := data.Paths("gcide")
		s.AddFS(data.FS, "gcide", idx, body)
		if r := s.Define("weird"); len(r) == 0 {
			b.Fatal("nothing found")
		}
	}
}

func BenchmarkWarmLookup(b *testing.B) {
	s := embedded(b)
	s.Define("weird") // pay the index cost once
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Define("receive")
	}
}

// TestCoverageOfWordList measures how much of the built-in word list can be
// defined. It is the evidence for the figure quoted in the README, and guards
// against a change to the lemmatiser or the dictionary order quietly losing
// ground.
func TestCoverageOfWordList(t *testing.T) {
	if testing.Short() {
		t.Skip("measures the whole word list")
	}
	words, err := data.Words()
	if err != nil {
		t.Fatal(err)
	}
	s := embedded(t)

	var total, covered int
	byDB := map[string]int{}
	for _, w := range words {
		// Possessive forms are inflections of a word already counted, and a
		// dictionary never files them separately.
		if strings.Contains(w, "'") {
			continue
		}
		total++
		if db, _, ok := s.Covers(strings.ToLower(w)); ok {
			covered++
			byDB[db]++
		}
	}

	pct := float64(covered) / float64(total) * 100
	t.Logf("word list %d (excluding possessives): %d defined = %.1f%%", total, covered, pct)
	for _, name := range append(append([]string{}, data.Order...), data.GlossName, data.WiktName, data.WikiName) {
		if n := byDB[name]; n > 0 {
			t.Logf("  %-9s %6d (%.1f%%)", name, n, float64(n)/float64(total)*100)
		}
	}
	if pct < 99.5 {
		t.Errorf("coverage %.1f%% fell below 99.5%%", pct)
	}
}

func TestGlossesAreLastResort(t *testing.T) {
	s := data.OpenSet(nil)
	// A surname that is also an ordinary word must get the real definition.
	res := s.Define("baker")
	if len(res) == 0 {
		t.Fatal("no definition for baker")
	}
	if res[0].DB == data.GlossName {
		t.Errorf("baker answered by the gloss file; a real dictionary entry must win")
	}
	// A name nothing defines falls through to the gloss.
	res = s.Define("Pacheco")
	if len(res) == 0 {
		t.Fatal("no gloss for Pacheco")
	}
	if res[0].DB != data.GlossName {
		t.Errorf("Pacheco answered by %q, expected the gloss file", res[0].DB)
	}
	if !strings.Contains(res[0].Text, "surname") {
		t.Errorf("gloss looks wrong: %q", res[0].Text)
	}
}

// TestGlossesCarryOriginAndPlace checks the detail that makes a name gloss
// worth showing: what kind of name it is, where it came from, and for a place,
// where it is.
func TestGlossesCarryOriginAndPlace(t *testing.T) {
	s := data.OpenSet(nil)
	cases := []struct {
		word string
		want []string
	}{
		{"Moriarty", []string{"surname", "from Irish", "New Mexico"}},
		{"Pacheco", []string{"surname", "Spanish", "California"}},
		{"Anastasia", []string{"given name", "Ancient Greek"}},
		{"Ilene", []string{"given name"}},
		{"Jaipur", []string{"Rajasthan", "India"}},
	}
	for _, c := range cases {
		res := s.Define(c.word)
		if len(res) == 0 {
			t.Errorf("%s: no gloss", c.word)
			continue
		}
		text := res[0].Text
		for _, want := range c.want {
			if !strings.Contains(text, want) {
				t.Errorf("%s: gloss lacks %q:\n%s", c.word, want, text)
			}
		}
	}
}

// TestWiktionaryFillsOrdinaryVocabulary covers the last source: modern words
// that none of the dictionaries carry, with the markup turned back into prose.
func TestWiktionaryFillsOrdinaryVocabulary(t *testing.T) {
	s := data.OpenSet(nil)
	for _, word := range []string{"paracetamol", "colorway", "feta"} {
		res := s.Define(word)
		if len(res) == 0 {
			t.Errorf("%s: no definition", word)
			continue
		}
		if res[0].DB != data.WiktName {
			t.Errorf("%s answered by %q, expected %q", word, res[0].DB, data.WiktName)
		}
		// No wikitext may survive into what is displayed.
		for _, bad := range []string{"{{", "}}", "[[", "]]", "'''"} {
			if strings.Contains(res[0].Text, bad) {
				t.Errorf("%s: raw wikitext %q in output: %s", word, bad, res[0].Text)
			}
		}
	}
}

// TestNoWikitextAnywhere sweeps the whole generated file, because a template
// this parser does not know would otherwise show up as braces in the pane.
func TestNoWikitextAnywhere(t *testing.T) {
	if testing.Short() {
		t.Skip("reads every generated entry")
	}
	f, err := data.FS.Open(data.WiktPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	g, err := dictdb.ReadGloss(data.WiktName, data.WiktTitle, f)
	if err != nil {
		t.Fatal(err)
	}
	words, err := data.Words()
	if err != nil {
		t.Fatal(err)
	}
	bad := 0
	for _, w := range words {
		defs, _ := g.Lookup(strings.ToLower(w))
		for _, d := range defs {
			if strings.Contains(d, "{{") || strings.Contains(d, "[[") || strings.Contains(d, "'''") {
				if bad < 5 {
					t.Errorf("%s: wikitext survived: %s", w, d)
				}
				bad++
			}
		}
	}
	if bad > 0 {
		t.Errorf("%d entries carry unparsed wikitext", bad)
	}
	t.Logf("%d entries checked", g.Len())
}
