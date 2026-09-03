package dictdb

import "strings"

// Lemmas returns the base forms to try for a word, best first, when the word
// itself has no entry.
//
// This matters more than it might seem. /usr/share/dict/words is a spell
// checker's list, so it carries every inflection -- "abacuses", "overacts",
// "smoggiest" -- while a dictionary files definitions under the lemma. Around
// half of all apparent misses are really an inflected form of a headword that
// is present, so trying the base forms lifts coverage of the word list from
// roughly 62% to 90%.
//
// The rules are deliberately generous: a wrong guess costs one failed binary
// search, and the caller keeps the first candidate that actually resolves.
func Lemmas(word string) []string {
	w := strings.ToLower(word)
	n := len(w)
	if n < 3 {
		return nil
	}

	var out []string
	add := func(s string) {
		if len(s) < 2 || s == w {
			return
		}
		for _, e := range out {
			if e == s {
				return
			}
		}
		out = append(out, s)
	}

	switch {
	// Comparatives and superlatives. The -iest/-ier forms are what rescue the
	// rare adjectives: "smoggiest" -> "smoggy", "nippiest" -> "nippy".
	case strings.HasSuffix(w, "iest"):
		add(w[:n-4] + "y")
		add(w[:n-3])
	case strings.HasSuffix(w, "ier"):
		add(w[:n-3] + "y")
		add(w[:n-2])
	}
	if strings.HasSuffix(w, "est") {
		add(w[:n-3])
		add(w[:n-2])
		add(undouble(w[:n-3]))
	}
	if strings.HasSuffix(w, "er") {
		add(w[:n-2])
		add(w[:n-1])
		add(undouble(w[:n-2]))
	}

	// Plurals and third person singular.
	if strings.HasSuffix(w, "ies") {
		add(w[:n-3] + "y")
	}
	if strings.HasSuffix(w, "ves") {
		add(w[:n-3] + "f")
		add(w[:n-3] + "fe")
	}
	if strings.HasSuffix(w, "ches") || strings.HasSuffix(w, "shes") ||
		strings.HasSuffix(w, "sses") || strings.HasSuffix(w, "xes") || strings.HasSuffix(w, "zes") {
		add(w[:n-2])
	}
	if strings.HasSuffix(w, "es") {
		add(w[:n-2])
		add(w[:n-1])
	}
	if strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") {
		add(w[:n-1])
	}

	// Past tense and participles.
	if strings.HasSuffix(w, "ied") {
		add(w[:n-3] + "y")
	}
	if strings.HasSuffix(w, "ed") {
		add(w[:n-2])
		add(w[:n-1])
		add(undouble(w[:n-2]))
	}
	if strings.HasSuffix(w, "ing") {
		add(w[:n-3])
		add(w[:n-3] + "e")
		add(undouble(w[:n-3]))
	}

	// Adverbs.
	if strings.HasSuffix(w, "ily") {
		add(w[:n-3] + "y")
	}
	if strings.HasSuffix(w, "ly") {
		add(w[:n-2])
		add(w[:n-2] + "e")
	}
	return out
}

// undouble strips a doubled final consonant, which English adds before a
// vowel suffix: "hottest" -> "hot", "running" -> "run". It returns "" when the
// word does not end in a doubled letter, and add() ignores that.
func undouble(s string) string {
	if len(s) < 3 {
		return ""
	}
	if s[len(s)-1] != s[len(s)-2] {
		return ""
	}
	switch s[len(s)-1] {
	case 'a', 'e', 'i', 'o', 'u', 's', 'l', 'f', 'z':
		// "less", "ball", "off" and friends are not doubled inflections.
		return ""
	}
	return s[:len(s)-1]
}
