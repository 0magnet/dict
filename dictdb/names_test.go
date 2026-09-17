package dictdb

import "testing"

// A name is a name because the word list spells it with a capital. An acronym
// is not, and neither is an ordinary word.
func TestIsProper(t *testing.T) {
	for _, tc := range []struct {
		word string
		want bool
	}{
		{"Gaborone", true},
		{"Zamenhof", true},
		{"Abbott", true},
		{"gazebo", false},
		{"DMZ", false},
		{"TQM", false},
		{"IE", false},
		{"Be", false},
		{"", false},
	} {
		if got := IsProper(tc.word); got != tc.want {
			t.Errorf("IsProper(%q) = %v, want %v", tc.word, got, tc.want)
		}
	}
}

// The glosses are formulaic enough to read. These are real ones.
func TestNameKind(t *testing.T) {
	for _, tc := range []struct {
		entry string
		want  Part
	}{
		{"Abbott, male given name or surname, from occupations. 52,739 people in the 2010 US census.", Person},
		{"Abelson, surname. 801 people in the 2010 US census, 29,473rd most common.", Person},
		{"Aaliyah, female given name, from Arabic.", Person},
		{"Zurich\n    n 1: the largest city in Switzerland; located in the northern part", Place},
		{"Abernathy, surname. 16,450 people in the census.\nAbernathy, city in Texas, United States.", Person},
		{"Gaborone\n    n 1: the capital of Botswana", Place},
		// A saint in a country's name must not make the country a person.
		{"Basseterre\n    n 1: the capital of Saint Kitts and Nevis", Place},
		// Neither, and that is an answer too.
		{"Etruscan\n    n 1: the language of the Etruscans", Name},
	} {
		if got := NameKind(tc.entry); got != tc.want {
			t.Errorf("NameKind(%.40q) = %v, want %v", tc.entry, got, tc.want)
		}
	}
}

// The printed form is the entry's headword where that is itself a name, so a
// lookup for the inflection "Toledos" prints "Toledo".
func TestProperForm(t *testing.T) {
	for _, tc := range []struct{ entry, word, want string }{
		{"Toledo\n    n 1: a city in northwest Ohio", "Toledos", "Toledo"},
		{"Abbott, male given name or surname.", "Abbott", "Abbott"},
		// An entry filed under an ordinary word keeps the capital the word
		// list gave it.
		{"marina\n    n 1: a fancy dock", "Marina", "Marina"},
	} {
		if got := properForm(tc.entry, tc.word); got != tc.want {
			t.Errorf("properForm(%.20q, %q) = %q, want %q", tc.entry, tc.word, got, tc.want)
		}
	}
}
