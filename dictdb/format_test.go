package dictdb

import "testing"

func TestCleanGCIDEMarkup(t *testing.T) {
	raw := `Weird \Weird\ (w[=e]rd), n. [OE. wirde, AS. wyrd fate,
   fr. weor[eth]an to be. [root]143. See {Worth} to become.]
   [1913 Webster]
   1. Fate; destiny. [Obs. or Scot.]
      [1913 Webster]`
	got := Clean(raw)

	for _, bad := range []string{`\Weird\`, "[1913 Webster]", "{Worth}", "[=e]", "[eth]", "[root]"} {
		if contains(got, bad) {
			t.Errorf("markup %q survived:\n%s", bad, got)
		}
	}
	for _, want := range []string{"Weird", "weorðan", "√143", "Worth", "Fate; destiny"} {
		if !contains(got, want) {
			t.Errorf("expected %q in output:\n%s", want, got)
		}
	}
	// The headword must appear once, not twice.
	if n := count(got, "Weird"); n != 1 {
		t.Errorf("headword appears %d times, want 1:\n%s", n, got)
	}
	// No gap left where the pronunciation was.
	if contains(got, " ,") {
		t.Errorf("space before comma left behind:\n%s", got)
	}
}

func TestCleanLeavesPlainTextAlone(t *testing.T) {
	// WordNet and FOLDOC use none of GCIDE's conventions.
	raw := "multinational\n    adj 1: involving several nations [syn: transnational]"
	if got := Clean(raw); got != raw {
		t.Errorf("plain entry was altered:\ngot  %q\nwant %q", got, raw)
	}
}

func TestWrapKeepsIndent(t *testing.T) {
	lines := Wrap("   1. a fairly long numbered sense that needs to be broken across lines", 30)
	if len(lines) < 2 {
		t.Fatalf("expected wrapping, got %v", lines)
	}
	for i, l := range lines {
		if len([]rune(l)) > 30 {
			t.Errorf("line %d is %d wide, over the limit: %q", i, len([]rune(l)), l)
		}
	}
	if lines[1][:3] != "   " {
		t.Errorf("continuation lost its indent: %q", lines[1])
	}
}

func TestLemmas(t *testing.T) {
	has := func(list []string, want string) bool {
		for _, s := range list {
			if s == want {
				return true
			}
		}
		return false
	}
	cases := []struct{ word, want string }{
		{"smoggiest", "smoggy"}, // the rare-adjective superlative
		{"nippiest", "nippy"},
		{"hottest", "hot"}, // doubled consonant
		{"running", "run"},
		{"abolishes", "abolish"},
		{"knives", "knife"},
		{"tried", "try"},
		{"happily", "happy"},
		{"boxes", "box"},
	}
	for _, c := range cases {
		if got := Lemmas(c.word); !has(got, c.want) {
			t.Errorf("Lemmas(%q) = %v, missing %q", c.word, got, c.want)
		}
	}
	// A doubled letter that is not an inflection must not be stripped.
	if got := Lemmas("less"); has(got, "le") {
		t.Errorf("Lemmas(%q) wrongly undoubled: %v", "less", got)
	}
}

func TestDecodeB64(t *testing.T) {
	// dictd's index numbers are base-64 digits, not encoded bytes.
	for _, c := range []struct {
		in   string
		want int64
	}{{"A", 0}, {"B", 1}, {"/", 63}, {"BA", 64}, {"c", 28}} {
		got, err := decodeB64(c.in)
		if err != nil || got != c.want {
			t.Errorf("decodeB64(%q) = %d, %v; want %d", c.in, got, err, c.want)
		}
	}
	if _, err := decodeB64("!"); err == nil {
		t.Error("expected an error for an invalid digit")
	}
}

func contains(s, sub string) bool { return len(sub) == 0 || indexOf(s, sub) >= 0 }
func count(s, sub string) int {
	n, i := 0, 0
	for {
		j := indexOf(s[i:], sub)
		if j < 0 {
			return n
		}
		n++
		i += j + len(sub)
	}
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// TestCleanRemovesEditionStampsButKeepsUsageLabels draws the line between the
// two kinds of bracketed note GCIDE uses. An edition stamp records which
// revision contributed a sense and means nothing to a reader; a usage label
// says the word is obsolete or colloquial, which is exactly what someone
// checking a spelling wants to know.
func TestCleanRemovesEditionStampsButKeepsUsageLabels(t *testing.T) {
	raw := `Tangelo, n. A hybrid between the tangerine and the pomelo.
   [Webster 1913 Suppl.]
   2. Another sense. [Obs.]
   [Webster 1913 Suppl. +PJC]
   3. A third. [Colloq.] [Prov. Eng.] [U.S.]`
	got := Clean(raw)

	for _, stamp := range []string{"Webster 1913 Suppl.", "+PJC", "1913 Webster"} {
		if contains(got, stamp) {
			t.Errorf("edition stamp %q survived:\n%s", stamp, got)
		}
	}
	for _, label := range []string{"[Obs.]", "[Colloq.]", "[Prov. Eng.]", "[U.S.]"} {
		if !contains(got, label) {
			t.Errorf("usage label %q was removed, but it is part of the entry:\n%s", label, got)
		}
	}
}
