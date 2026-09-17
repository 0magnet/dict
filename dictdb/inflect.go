// Package dictdb inflect.go
//
// The inflected forms of a headword: past tense, participles, plural.
//
// The rules at the bottom of this file are the regular ones every English
// primer gives, and they are the fallback, not the method. The method is to
// read the forms the entry already lists, because GCIDE prints them:
//
//	Whisk, v. t. [imp. & p. p. Whisked; p. pr. & vb. n. Whisking.]
//	Rend, v. t. [imp. & p. p. Rent; p. pr. & vb. n. Rending.]
//	Sing, v. i. [imp. Sung or Sang; p. p. Sung; p. pr. & vb. n. Singing.]
//	Abbacy, n.; pl. Abbacies.
//
// Six thousand verbs carry their principal parts that way and three thousand
// nouns carry their plural, irregulars included -- which is the whole point,
// since no rule turns "rend" into "rent". Where the entry says nothing the
// regular rules answer, and they are right about the regular words, which is
// what they are for.
//
// This is the inverse of Lemmas, which strips inflections to find a headword
// to look up. The two disagree in the corners, and that is fine: Lemmas may
// guess generously because a wrong guess costs one failed lookup, while a
// wrong guess here is printed.
package dictdb

import (
	"regexp"
	"strings"
)

// The notation GCIDE uses for principal parts. Clean has already removed the
// braces around each form, so what is left is the abbreviation and the word.
//
// "imp." is the past tense and "p. p." the past participle; an entry may give
// them together ("imp. & p. p. Whisked") or apart ("imp. Sung or Sang; p. p.
// Sung"). Matching "p. p." first and "imp." separately gets both readings,
// since the combined form contains the text of each.
var (
	rePast     = regexp.MustCompile(`imp\.(?: & p\. p\.)?\s+([A-Za-z]+)`)
	reParticip = regexp.MustCompile(`p\. p\.\s+([A-Za-z]+)`)
	reGerund   = regexp.MustCompile(`p\. pr\.(?: & vb\. n\.)?\s+([A-Za-z]+)`)
	rePlural   = regexp.MustCompile(`pl\.\s+([A-Za-z]+)`)
)

// formsFrom reads whichever principal parts an entry lists, lower-cased. A
// form it does not list comes back empty, for the caller to fill in.
//
// Only the entry's opening matters -- the notation sits in the head, before
// the senses -- and reading further invites a quotation's stray "pl." into
// the answer.
func formsFrom(entry string, headword string) (past, particip, gerund, plural string) {
	head := entry
	if len(head) > 240 {
		head = head[:240]
	}
	take := func(re *regexp.Regexp) string {
		m := re.FindStringSubmatch(head)
		if m == nil {
			return ""
		}
		form := strings.ToLower(m[1])
		// A form of some other word is not a form of this one. The shared
		// first letter is a weak test and a sufficient one: it rejects the
		// abbreviations and cross-references that follow the notation, and
		// no English inflection changes the initial.
		if form == "" || headword == "" || form[0] != headword[0] {
			return ""
		}
		return form
	}
	return take(rePast), take(reParticip), take(reGerund), take(rePlural)
}

// vowel reports whether a byte is one, for the doubling and -es rules. "y" is
// deliberately not one: it is the consonant in "try" that makes "tried".
func vowel(c byte) bool { return strings.IndexByte("aeiou", c) >= 0 }

// doubles reports whether a word doubles its final consonant before a vowel
// suffix: "run" -> "running", "clot" -> "clotted".
//
// The real rule needs the stress -- "prefer" doubles, "offer" does not -- and
// stress is not in reach here. So this doubles only a word of one syllable,
// which is the case that is never in doubt. Longer words are left undoubled,
// which is wrong for "prefer" but still leaves a word a reader can say --
// where doubling nothing at all would mangle the short words instead, and
// those are the common ones.
func doubles(w string) bool {
	n := len(w)
	if n < 3 {
		return false
	}
	if vowel(w[n-1]) || w[n-1] == 'y' || w[n-1] == 'w' || w[n-1] == 'x' {
		return false
	}
	if !vowel(w[n-2]) || vowel(w[n-3]) {
		return false
	}
	return syllables(w) == 1
}

// syllables counts runs of vowels, which is the cheap approximation: "clot"
// has one, "offer" two. A final "e" does not count, being usually silent.
func syllables(w string) int {
	w = strings.TrimSuffix(w, "e")
	n, in := 0, false
	for i := 0; i < len(w); i++ {
		if vowel(w[i]) {
			if !in {
				n++
			}
			in = true
			continue
		}
		in = false
	}
	return n
}

// Past is the regular past tense: "clot" -> "clotted", "try" -> "tried",
// "size" -> "sized".
func Past(w string) string {
	switch {
	case w == "":
		return ""
	case strings.HasSuffix(w, "e"):
		return w + "d"
	case len(w) > 2 && strings.HasSuffix(w, "y") && !vowel(w[len(w)-2]):
		return w[:len(w)-1] + "ied"
	case doubles(w):
		return w + string(w[len(w)-1]) + "ed"
	}
	return w + "ed"
}

// Gerund is the regular -ing form: "rend" -> "rending", "size" -> "sizing",
// "clot" -> "clotting".
func Gerund(w string) string {
	switch {
	case w == "":
		return ""
	// The silent e goes, except where it is doing work: "see" -> "seeing".
	case strings.HasSuffix(w, "e") && !strings.HasSuffix(w, "ee") && len(w) > 2:
		return w[:len(w)-1] + "ing"
	case doubles(w):
		return w + string(w[len(w)-1]) + "ing"
	}
	return w + "ing"
}

// ThirdPerson is the -s form of a verb, which is also the regular plural of a
// noun: "miss" -> "misses", "try" -> "tries", "oink" -> "oinks".
func ThirdPerson(w string) string {
	switch {
	case w == "":
		return ""
	case strings.HasSuffix(w, "s"), strings.HasSuffix(w, "x"), strings.HasSuffix(w, "z"),
		strings.HasSuffix(w, "ch"), strings.HasSuffix(w, "sh"):
		return w + "es"
	case len(w) > 2 && strings.HasSuffix(w, "y") && !vowel(w[len(w)-2]):
		return w[:len(w)-1] + "ies"
	}
	return w + "s"
}

// Plural is the regular plural of a noun, which follows the same rule.
func Plural(w string) string { return ThirdPerson(w) }
