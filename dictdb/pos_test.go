package dictdb

import "testing"

// The entries here are real, cleaned as Clean leaves them: that is the shape
// PartsOf is asked about in practice.
func TestPartsOfGCIDE(t *testing.T) {
	for _, tc := range []struct {
		entry string
		want  Part
	}{
		{"Deserter, n.\n   One who forsakes a duty, a cause or a party.", Noun},
		{"Whisk, v. t. [imp. & p. p. Whisked; p. pr. & vb. n.\n   Whisking.]", Verb},
		{"Stagy, a. [Written also stagey.]", Adjective},
		{"Insincerely, adv.\n   Without sincerity.", Adverb},
		// An etymology or a plural follows the abbreviation as often as not.
		{"Verbosity, n.; pl. Verbosities. [L. verbositas:", Noun},
		{"Vitality (?; 277), n. [L. vitalitas: cf. F.", Noun},
	} {
		if !Is(tc.entry, tc.want) {
			t.Errorf("%q: got %v, want %v", firstLine(tc.entry), PartsOf(tc.entry), tc.want)
		}
	}
}

func TestPartsOfWordNet(t *testing.T) {
	entry := "marina\n    n 1: a fancy dock for small yachts and cabin cruisers"
	if !Is(entry, Noun) {
		t.Errorf("wordnet noun not seen: %v", PartsOf(entry))
	}
	// A satellite adjective is an adjective.
	sat := "multinational\n    adj 1: involving several nations\n    adj sat 2: of a company"
	if !Is(sat, Adjective) {
		t.Errorf("wordnet adjective not seen: %v", PartsOf(sat))
	}
	// An entry can be more than one part of speech, and all of them count.
	both := "level\n    n 1: a position on a scale\n    v 2: make level or straight\n    adj 3: not showing abrupt variations"
	got := PartsOf(both)
	if len(got) != 3 {
		t.Errorf("got %v, want noun, verb and adjective", got)
	}
}

// A database with no part-of-speech notation must report nothing rather than
// guess, so that a caller asking for a verb is never handed an acronym.
func TestPartsOfUntagged(t *testing.T) {
	for _, entry := range []string{
		"py\n\n   <networking> The country code for Paraguay.\n\n   (1999-01-27)",
		"Terkel, surname.",
		"SOS",
	} {
		if got := PartsOf(entry); len(got) != 0 {
			t.Errorf("%q: got %v, want nothing", firstLine(entry), got)
		}
	}
}

// Headword is what makes a tagged word usable in a sentence: the entry a
// lookup lands on is filed under the base form, and that is the form to take.
func TestHeadword(t *testing.T) {
	for _, tc := range []struct{ entry, want string }{
		{"Accumulate, v. t. [imp. & p. p. Accumulated; p. pr.", "accumulate"},
		{"Anal, a.\n   Of or pertaining to the anus.", "anal"},
		{"marina\n    n 1: a fancy dock", "marina"},
		// Not a single ordinary word, so no headword to offer.
		{"Ad hoc, a.\n   For this purpose.", ""},
		{"", ""},
	} {
		if got := Headword(tc.entry); got != tc.want {
			t.Errorf("%q: got %q, want %q", firstLine(tc.entry), got, tc.want)
		}
	}
}

func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}

// A pronunciation Clean did not recognize must not cost the entry its
// headword: "Rend (r[e^]nd), v. t." is the entry with the principal parts in
// it, and losing it means losing "rent".
func TestHeadwordSurvivesAPronunciation(t *testing.T) {
	entry := "Rend (r[e^]nd), v. t. [imp. & p. p. Rent (r[e^]nt); p. pr. & vb. n. Rending.]"
	if got := Headword(entry); got != "rend" {
		t.Errorf("Headword = %q, want %q", got, "rend")
	}
	if !Is(entry, Transitive) {
		t.Errorf("transitivity not seen: %v", PartsOf(entry))
	}
}
