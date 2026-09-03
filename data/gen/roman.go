package main

import (
	"fmt"
	"os"
	"strings"
)

// The word list carries Roman numerals -- clii, lix, xcix, xxxix -- which no
// dictionary files as headwords and Wiktionary covers only patchily. They are
// arithmetic rather than vocabulary, so they are computed here instead of
// being looked up anywhere.

var romanValues = map[byte]int{'i': 1, 'v': 5, 'x': 10, 'l': 50, 'c': 100, 'd': 500, 'm': 1000}

// parseRoman returns the value of a Roman numeral, and whether the spelling is
// the canonical one for that value. Requiring canonical form rejects strings
// that merely happen to be made of those letters -- "mild", "did", "civil" --
// which would otherwise be glossed as numbers.
func parseRoman(s string) (int, bool) {
	s = strings.ToLower(s)
	if s == "" {
		return 0, false
	}
	total := 0
	for i := 0; i < len(s); i++ {
		v, ok := romanValues[s[i]]
		if !ok {
			return 0, false
		}
		// A smaller value before a larger one is subtracted: "ix" is nine.
		if i+1 < len(s) {
			if next, ok := romanValues[s[i+1]]; ok && next > v {
				total -= v
				continue
			}
		}
		total += v
	}
	if total <= 0 || total > 3999 {
		return 0, false
	}
	return total, formatRoman(total) == s
}

func formatRoman(n int) string {
	steps := []struct {
		v int
		s string
	}{
		{1000, "m"}, {900, "cm"}, {500, "d"}, {400, "cd"},
		{100, "c"}, {90, "xc"}, {50, "l"}, {40, "xl"},
		{10, "x"}, {9, "ix"}, {5, "v"}, {4, "iv"}, {1, "i"},
	}
	var b strings.Builder
	for _, st := range steps {
		for n >= st.v {
			b.WriteString(st.s)
			n -= st.v
		}
	}
	return b.String()
}

// addRomanNumerals glosses any gap word that is a well-formed Roman numeral.
func addRomanNumerals(gap map[string]bool, at func(string) *info) int {
	n := 0
	for w := range gap {
		v, canonical := parseRoman(w)
		if !canonical {
			continue
		}
		i := at(w)
		if i.numeral == "" {
			i.numeral = fmt.Sprintf("%s, the Roman numeral %s.", strings.ToUpper(w), commas(v))
			n++
		}
	}
	fmt.Fprintf(os.Stderr, "roman numerals: %d\n", n)
	return n
}
