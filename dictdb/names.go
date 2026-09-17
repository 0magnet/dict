// Package dictdb names.go
//
// Proper nouns: telling one from an ordinary word, and a person from a place.
//
// A sixth of the word list -- 21,567 of its 124,000 entries -- is names, and
// the list is where the evidence is: it writes "Zamenhof" and "Gaborone" with
// a capital and "zealot" without. The dictionaries cannot be asked instead,
// because GCIDE capitalizes every headword it prints, so its capital says
// nothing. That is why IsProper takes the word as the list spells it.
//
// Which kind of name it is comes from the gloss, where the wording is
// formulaic enough to read: the name list says "male given name or surname"
// and "city in California" outright, and WordNet's entries for people carry
// either a trade ("painter", "composer") or a pair of dates.
//
// The classification is a guess and is meant to be. Nothing here is load
// bearing: a place used as a person is a worse joke, not a wrong answer.
package dictdb

import (
	"regexp"
	"strings"
	"unicode"
)

// The kinds of proper noun, as Parts so that one vocabulary can hold them
// alongside the parts of speech. They are not parts of speech, and Parts --
// the list --pos offers -- does not include them for that reason; a caller
// that wants a name asks for one by name.
const (
	Person Part = "person"
	Place  Part = "place"
	// Name is any proper noun, including the ones that are neither a person
	// nor a place: "Etruscan", "Fox", "Miltown". Every proper noun carries
	// it, so a slot that just wants a capitalized word has the widest pool.
	Name Part = "name"
)

var (
	// Place first, because "capital of Saint Kitts" would otherwise be read
	// as a person on the strength of the saint.
	rePlaceGloss = regexp.MustCompile(`(?i)\b(city|town|capital|state|province|country|republic|island|river|mountain|village|port|county|region|peninsula|kingdom|nation)\b`)
	// A trade, a title, or a pair of dates in parentheses: "United States
	// painter (1856-1925)", "male given name or surname".
	rePersonGloss = regexp.MustCompile(`(?i)\b(given name|surname|born|painter|writer|philosopher|poet|president|general|composer|actor|actress|novelist|statesman|physicist|mathematician|inventor|king|queen|emperor|dramatist|theologian|singer|scientist|explorer|leader)\b|\(\d{3,4}`)
)

// IsProper reports whether a word is a name, judged by how the word list
// spells it: a capital, and not the run of capitals that marks an acronym.
// "Gaborone" is a name; "gazebo" is not, and neither is "DMZ".
func IsProper(word string) bool {
	if len([]rune(word)) < 3 {
		return false
	}
	r := []rune(word)
	if !unicode.IsUpper(r[0]) {
		return false
	}
	// TQM, KB, DMZ: initials, not names.
	return !unicode.IsUpper(r[1])
}

// NameKind reads an entry for what kind of name it describes. It answers Name
// when the entry says nothing either way, which is the common case for the
// proper nouns that are neither people nor places.
//
// Whichever the gloss mentions first wins, because a gloss opens with what
// the name mainly is and then qualifies it. Many names are both: "Abernathy,
// surname. 16,450 people in the 2010 US census. Abernathy, city in Texas" is
// a surname that a town was named after, and "Basseterre, the capital of
// Saint Kitts" is a capital with a saint in its country's name. Taking either
// kind as the more important one gets one of those two wrong; taking the
// earlier one gets both right.
func NameKind(entry string) Part {
	gloss := entry
	if len(gloss) > 300 {
		gloss = gloss[:300]
	}
	place := rePlaceGloss.FindStringIndex(gloss)
	person := rePersonGloss.FindStringIndex(gloss)
	switch {
	case place != nil && person != nil:
		if place[0] < person[0] {
			return Place
		}
		return Person
	case place != nil:
		return Place
	case person != nil:
		return Person
	}
	return Name
}

// properForm returns the name as it should be printed: the entry's own
// headword where that is a name in its own right, and otherwise the word as
// it was looked up.
//
// The entry's headword is preferred for the same reason it is everywhere else
// here -- a word list carries inflections, and "Toledos" is filed under
// "Toledo" -- but only when it still looks like a name, so that a lookup
// landing on an ordinary entry keeps the capital it came with.
func properForm(entry, word string) string {
	if head := headwordForm(entry); IsProper(head) {
		return head
	}
	return strings.TrimSpace(word)
}
