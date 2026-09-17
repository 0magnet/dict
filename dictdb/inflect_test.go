package dictdb

import "testing"

func TestRegularForms(t *testing.T) {
	for _, tc := range []struct{ word, past, gerund, third string }{
		{"rend", "rended", "rending", "rends"},
		{"size", "sized", "sizing", "sizes"},
		{"baby", "babied", "babying", "babies"},
		{"clot", "clotted", "clotting", "clots"},
		{"miss", "missed", "missing", "misses"},
		{"harlequin", "harlequined", "harlequining", "harlequins"},
		// Long words are left alone rather than doubled: "offering", not
		// "offerring".
		{"offer", "offered", "offering", "offers"},
		// A doubled vowel keeps its e.
		{"see", "seed", "seeing", "sees"},
	} {
		if got := Past(tc.word); got != tc.past {
			t.Errorf("Past(%q) = %q, want %q", tc.word, got, tc.past)
		}
		if got := Gerund(tc.word); got != tc.gerund {
			t.Errorf("Gerund(%q) = %q, want %q", tc.word, got, tc.gerund)
		}
		if got := ThirdPerson(tc.word); got != tc.third {
			t.Errorf("ThirdPerson(%q) = %q, want %q", tc.word, got, tc.third)
		}
	}
}

func TestPluralRule(t *testing.T) {
	for _, tc := range []struct{ word, want string }{
		{"anvil", "anvils"},
		{"abbacy", "abbacies"},
		{"ashtray", "ashtrays"},
		{"box", "boxes"},
		{"beech", "beeches"},
	} {
		if got := Plural(tc.word); got != tc.want {
			t.Errorf("Plural(%q) = %q, want %q", tc.word, got, tc.want)
		}
	}
}

// The forms GCIDE prints are the ones no rule would find. These entries are
// real, cleaned as Clean leaves them.
func TestFormsFromEntry(t *testing.T) {
	for _, tc := range []struct {
		entry, headword          string
		past, pp, gerund, plural string
	}{
		{
			entry:    "Rend, v. t. [imp. & p. p. Rent; p. pr. & vb. n. Rending.]\n   1. To tear apart.",
			headword: "rend", past: "rent", pp: "rent", gerund: "rending",
		},
		{
			entry:    "Sing, v. i. [imp. Sung or Sang; p. p. Sung; p. pr. & vb. n. Singing.]",
			headword: "sing", past: "sung", pp: "sung", gerund: "singing",
		},
		{
			entry:    "Whisk, v. t. [imp. & p. p. Whisked; p. pr. & vb. n. Whisking.]",
			headword: "whisk", past: "whisked", pp: "whisked", gerund: "whisking",
		},
		{
			entry:    "Abbacy, n.; pl. Abbacies. [L. abbatia.]",
			headword: "abbacy", plural: "abbacies",
		},
		// Nothing to read: every form comes back empty for the caller to
		// fill in from the rules.
		{entry: "marina\n    n 1: a fancy dock", headword: "marina"},
	} {
		past, pp, gerund, plural := formsFrom(tc.entry, tc.headword)
		if past != tc.past || pp != tc.pp || gerund != tc.gerund || plural != tc.plural {
			t.Errorf("%s: got %q/%q/%q/%q, want %q/%q/%q/%q",
				tc.headword, past, pp, gerund, plural, tc.past, tc.pp, tc.gerund, tc.plural)
		}
	}
}

// A form belonging to some other word is not this word's form: the notation
// is followed by etymology and cross-references, and "pl." can appear in
// either.
func TestFormsFromRejectsOtherWords(t *testing.T) {
	entry := "Toecap, n.; pl. Gloves. [See Glove.]"
	if _, _, _, plural := formsFrom(entry, "toecap"); plural != "" {
		t.Errorf("took %q as the plural of toecap", plural)
	}
}
