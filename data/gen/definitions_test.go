package main

import "testing"

// Each case is wikitext taken verbatim from the page named, so a change to the
// parser is checked against what Wiktionary actually writes rather than an
// idealised version of it.
func TestParseDefinitions(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string // must appear in the first definition
	}{
		{
			name: "superglue: plain definition with a section-anchored link",
			text: "==English==\n\n===Noun===\n{{en-noun|-}}\n\n# A very [[strong]] and [[instant#Adjective|instant]] [[glue]], generally [[cyanoacrylate]].\n",
			want: "A very strong and instant glue",
		},
		{
			name: "deffest: superlative template with no language parameter",
			text: "==English==\n\n===Adjective===\n{{head|en|superlative}}\n\n# {{en-superlative of|def}}\n",
			want: "Superlative of def",
		},
		{
			name: "megapascal: a unit built entirely from a template",
			text: "==English==\n\n===Noun===\n{{en-noun}}\n\n# {{SI-unit|en|mega|pascal|pressure}}\n",
			want: "pascal",
		},
		{
			name: "afb: an initialism, where the meaning is the template",
			text: "==English==\n\n===Noun===\n{{en-noun}}\n\n# {{lb|en|military|aviation}} {{initialism of|en|air force base}}\n",
			want: "Initialism of air force base",
		},
		{
			name: "only the English section is read",
			text: "==English==\n\n===Noun===\n\n# An English sense.\n\n==Portuguese==\n\n===Noun===\n\n# Um sentido português.\n",
			want: "An English sense",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseDefinitions(c.text, 3)
			if len(got) == 0 {
				t.Fatalf("no definitions parsed")
			}
			if !contains(got[0], c.want) {
				t.Errorf("first definition = %q, want it to contain %q", got[0], c.want)
			}
		})
	}
}

func TestCleanRejectsEmptyAndBroken(t *testing.T) {
	for _, s := range []string{
		"{{lb|en|business}}",       // a label with no definition
		"{{lb|en|British|or|MLE|.", // an unbalanced template
		"   ",                      // nothing at all
	} {
		if got := cleanWikitext(s); got != "" {
			t.Errorf("cleanWikitext(%q) = %q, want it rejected", s, got)
		}
	}
}

func TestCleanLabelConnectors(t *testing.T) {
	// "or" and "and" join the terms either side rather than being list items.
	got := cleanWikitext("{{lb|en|dialectal|or|colloquial}} Some sense.")
	if !contains(got, "(dialectal or colloquial)") {
		t.Errorf("got %q, want the label to read \"(dialectal or colloquial)\"", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestParseShortcutsAndLongNames covers the template spellings that each cost
// a word its definition until they were handled: the non-gloss shortcuts, and
// "form of" names longer than two words.
func TestParseShortcutsAndLongNames(t *testing.T) {
	cases := []struct{ name, text, want string }{
		{"yikes: {{ng|...}}", "==English==\n\n===Interjection===\n\n# {{ng|Expression of [[shock]] and [[alarm]].}}\n", "Expression of shock and alarm"},
		{"blimey: {{non-gloss|...}}", "==English==\n\n===Interjection===\n\n# {{lb|en|UK}} {{non-gloss|Used to express [[anger]].}}\n", "Used to express anger"},
		{"dadaist: {{alt case|...}}", "==English==\n\n===Noun===\n\n# {{alt case|en|Dadaist}}.\n", "Alternative letter-case form of Dadaist"},
		{"anglophone: a four-word form-of name", "==English==\n\n===Adjective===\n\n# {{alternative case form of|en|Anglophone}}.\n", "Alternative letter-case form of Anglophone"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseDefinitions(c.text, 3)
			if len(got) == 0 {
				t.Fatal("no definitions parsed")
			}
			if !contains(got[0], c.want) {
				t.Errorf("got %q, want it to contain %q", got[0], c.want)
			}
		})
	}
}
