// Package dictdb pos.go
//
// Which parts of speech an entry claims for its headword.
//
// Nothing here parses grammar. It reads the label a lexicographer already
// wrote: GCIDE opens an entry with the headword and an abbreviation --
// "Deserter, n.", "Whisk, v. t.", "Stagy, a." -- and WordNet stamps every
// sense line with one -- "n 1: a fancy dock for small yachts". Both are
// conventions of the source, so recognizing them costs two regexps and is
// right about as often as the dictionary is.
//
// The two remaining databases are deliberately not read. FOLDOC and the
// acronym lists have no part-of-speech notation at all, and the wiktionary
// gloss file marks inflections ("v. Inflection of tarmac") rather than
// headwords, which is the opposite of what a caller wants: something asking
// for a verb wants "tarmac", not "tarmacked".
package dictdb

import (
	"regexp"
	"strings"
)

// Part is a part of speech, in the four flavors worth telling apart. The
// closed set is the point: a caller wants a word it can put in a slot, and
// prepositions and interjections are not slots anyone fills at random.
type Part string

// The parts an entry can be tagged with.
//
// Transitive and Intransitive refine Verb rather than replacing it: an entry
// marked "v. t." carries both, so anything that just wants a verb still finds
// one. The distinction earns its keep in a generated sentence, where an
// intransitive verb cannot take an object -- "the oink will elapse the
// abbacy" is what happens without it.
const (
	Noun         Part = "noun"
	Verb         Part = "verb"
	Transitive   Part = "v.t."
	Intransitive Part = "v.i."
	Adjective    Part = "adj"
	Adverb       Part = "adv"
)

// Parts is the set of parts of speech worth asking for, in the order they
// read most naturally in help text. Transitivity is not in it: it is a
// property of a verb, not a fifth thing a word can be.
var Parts = []Part{Noun, Verb, Adjective, Adverb}

// allParts is every tag, in reporting order.
var allParts = []Part{Noun, Verb, Transitive, Intransitive, Adjective, Adverb, Person, Place, Name}

var (
	// GCIDE's opening line: the headword, a comma, then the abbreviation.
	// What follows it varies -- an etymology in brackets, a plural, a field
	// label -- so the match stops at the abbreviation rather than the end of
	// the line. Clean has already removed the pronunciation that sat between
	// the headword and the comma.
	//
	// The trailing group collects the transitivity marks a verb carries:
	// "v. t.", "v. i." or "v. t. & i." for a verb that is both.
	reGcide = regexp.MustCompile(`^[^,\n]{1,48},\s*(n|v|a|adv)\.((?:\s*&?\s*[ti]\.)*)`)

	// WordNet's sense lines, which are indented and numbered: "n 1:", "v 2:".
	// "adj sat" marks a satellite adjective -- a synonym clustered under a
	// head adjective -- and is an adjective for every purpose here.
	reWordNet = regexp.MustCompile(`(?m)^[ \t]+(n|v|adj|adv|adj sat)[ \t]+\d+:`)
)

// gcideParts maps GCIDE's abbreviations. Its "a." is an adjective, which is
// the one that catches a reader out: in a 1913 dictionary "a." is not an
// article.
var gcideParts = map[string]Part{"n": Noun, "v": Verb, "a": Adjective, "adv": Adverb}

var wordnetParts = map[string]Part{"n": Noun, "v": Verb, "adj": Adjective, "adj sat": Adjective, "adv": Adverb}

// PartsOf reports the parts of speech an entry declares, deduplicated and in
// the order of Parts. An entry from a database that carries no such notation
// returns nothing, which is a fair answer: the part of speech is unknown, not
// absent.
func PartsOf(entry string) []Part {
	found := map[Part]bool{}
	if m := reGcide.FindStringSubmatch(entry); m != nil {
		if p, ok := gcideParts[m[1]]; ok {
			found[p] = true
			if p == Verb {
				if strings.Contains(m[2], "t.") {
					found[Transitive] = true
				}
				if strings.Contains(m[2], "i.") {
					found[Intransitive] = true
				}
			}
		}
	}
	for _, m := range reWordNet.FindAllStringSubmatch(entry, -1) {
		if p, ok := wordnetParts[m[1]]; ok {
			found[p] = true
		}
	}
	if len(found) == 0 {
		return nil
	}
	out := make([]Part, 0, len(found))
	for _, p := range allParts {
		if found[p] {
			out = append(out, p)
		}
	}
	return out
}

// Has reports whether a part of speech is in a list of them, as PartsOf and
// Tagged return.
func Has(parts []Part, p Part) bool {
	for _, got := range parts {
		if got == p {
			return true
		}
	}
	return false
}

// Is reports whether an entry declares a given part of speech.
func Is(entry string, p Part) bool { return Has(PartsOf(entry), p) }

// reWord is a headword worth handing back: letters only, long enough to be a
// word rather than an initial.
var reWord = regexp.MustCompile(`^[A-Za-z]{2,}$`)

// Headword returns the word an entry is filed under, lower-cased, or "" when
// that is not a single ordinary word.
//
// This is not the word that was looked up, and the difference is the whole
// reason it exists. A word list carries inflections, and a lookup for
// "accumulated" lands on the entry "Accumulate, v. t. [imp. & p. p.
// Accumulated ...]" -- so reading the part of speech off that entry and
// pairing it with the word that was asked for yields "accumulated, a verb",
// which is true of the entry and useless in a sentence. Pairing it with the
// entry's own headword yields "accumulate, a verb", which can be spoken after
// "will". "Anally" lands on "Anal, a." the same way.
//
// Multiword headwords return "" rather than a first word: "ad hoc" is not an
// adjective called "ad".
func Headword(entry string) string { return strings.ToLower(headwordForm(entry)) }

// headwordForm is Headword without the lower-casing, for the one caller that
// needs the capital: a name is printed as the dictionary spells it.
func headwordForm(entry string) string {
	line := entry
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	// A comma means GCIDE, where everything after it is notation -- and so
	// does an opening parenthesis, which introduces a pronunciation. Clean
	// removes most of those, but only the ones whose escapes it recognizes:
	// "Rend (r[e^]nd), v. t." reaches here with the pronunciation intact, and
	// cutting at the comma alone would leave a headword that is not a word
	// and lose the entry -- the entry that carries the principal parts.
	if i := strings.IndexAny(line, ",("); i >= 0 {
		line = line[:i]
	}
	line = strings.TrimSpace(line)
	if !reWord.MatchString(line) {
		return ""
	}
	return line
}

// Tag is everything a lookup can say about a word beyond its meaning: the
// headword it is filed under, the parts of speech its entries claim, and the
// inflected forms to use when it is put in a sentence.
//
// The forms are always filled in. Where the entry lists them they are the
// dictionary's; where it does not they are the regular rules', which is why a
// caller never has to ask which it got.
type Tag struct {
	Word string
	// Display is the form to print when the word is a proper noun, capital
	// and all: "Gaborone", not "gaborone". It is empty for an ordinary word,
	// which is how a caller tells the two apart -- GCIDE capitalizes every
	// headword it prints, so the capital on Word would mean nothing.
	Display    string
	Parts      []Part
	Past       string // rend -> rent
	Participle string // rend -> rent, size -> sized
	Gerund     string // rend -> rending
	Third      string // rend -> rends
	Plural     string // abbacy -> abbacies
}

// Has reports whether the word is tagged with a part of speech.
func (t Tag) Has(p Part) bool { return Has(t.Parts, p) }

// Tagged looks a word up and reports what its entries say about it. The
// second result is false when the word has no entry, or none that carries the
// notation this reads.
//
// It reads all the entries a lookup returns, not just the first, because a
// dictionary files each part of speech separately: GCIDE has "Stanch, v. t."
// and "Stanch, a." as two entries, and a caller that read only the first
// would be told that "stanch" is a verb and never an adjective. For the same
// reason the forms are gathered across entries -- the plural is in the noun
// entry and the principal parts are in the verb entry. Entries filed under
// some other headword are ignored, which is the defensive case: one lookup's
// results ought to agree about what word they are for.
func (s *Set) Tagged(word string) (Tag, bool) {
	var t Tag
	found := map[Part]bool{}
	for _, r := range s.Define(word) {
		head := Headword(r.Text)
		if head == "" {
			continue
		}
		if t.Word == "" {
			t.Word = head
		} else if head != t.Word {
			continue
		}
		for _, p := range PartsOf(r.Text) {
			found[p] = true
		}
		// A name is a name because the word list spells it with a capital,
		// not because of anything in the entry; what the entry decides is
		// which kind of name it is.
		if IsProper(word) {
			found[Name] = true
			if kind := NameKind(r.Text); kind != Name {
				found[kind] = true
			}
			if t.Display == "" {
				t.Display = properForm(r.Text, word)
			}
		}
		past, particip, gerund, plural := formsFrom(r.Text, t.Word)
		keepFirst(&t.Past, past)
		keepFirst(&t.Participle, particip)
		keepFirst(&t.Gerund, gerund)
		keepFirst(&t.Plural, plural)
	}
	if len(found) == 0 {
		return Tag{}, false
	}
	for _, p := range allParts {
		if found[p] {
			t.Parts = append(t.Parts, p)
		}
	}

	// Whatever no entry supplied, the regular rules do.
	keepFirst(&t.Past, Past(t.Word))
	keepFirst(&t.Participle, t.Past)
	keepFirst(&t.Gerund, Gerund(t.Word))
	keepFirst(&t.Third, ThirdPerson(t.Word))
	keepFirst(&t.Plural, Plural(t.Word))
	return t, true
}

// keepFirst fills a form in only if it is still empty, so the first entry to
// name one wins and the regular rules apply last.
func keepFirst(dst *string, s string) {
	if *dst == "" {
		*dst = s
	}
}
