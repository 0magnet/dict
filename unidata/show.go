package unidata

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ParseCode reads a code point written the way people write one, in any of
// the forms that say outright that a code point is what it is: U+20AC,
// u+20ac, 0x20AC, \u20ac, &#x20AC; or &#8364;.
//
// A bare hex number is not one of them, because it is not unambiguous: dict
// is a dictionary first, and "beef", "cafe", "faced" and "decade" are all
// valid hex and all more likely to be a word someone wants defined than a
// CJK compatibility ideograph. ParseHex reads those where the context has
// already established that a character is what is being asked about.
func ParseCode(s string) (rune, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	var digits string
	switch {
	case strings.HasPrefix(s, "U+"), strings.HasPrefix(s, "u+"),
		strings.HasPrefix(s, "0x"), strings.HasPrefix(s, "0X"),
		strings.HasPrefix(s, `\u`), strings.HasPrefix(s, `\U`):
		digits = s[2:]
	case strings.HasPrefix(s, "&#x") && strings.HasSuffix(s, ";"):
		digits = s[3 : len(s)-1]
	case strings.HasPrefix(s, "&#") && strings.HasSuffix(s, ";"):
		// The decimal character reference is the one place a decimal number
		// is unambiguous, so it is the one place decimal is read.
		n, err := strconv.ParseUint(s[2:len(s)-1], 10, 32)
		if err != nil || n > 0x10FFFF {
			return 0, false
		}
		return rune(n), true
	default:
		return 0, false
	}
	return parseHexDigits(digits, 1)
}

// ParseHex reads a bare hex code point, four to six digits. It is what a
// command that is already about characters reads an otherwise unrecognized
// argument as, once nothing turns out to be named that.
func ParseHex(s string) (rune, bool) {
	return parseHexDigits(strings.TrimSpace(s), 4)
}

func parseHexDigits(s string, min int) (rune, bool) {
	if len(s) < min || len(s) > 6 || !isHex(s) {
		return 0, false
	}
	n, err := strconv.ParseUint(s, 16, 32)
	if err != nil || n > 0x10FFFF {
		return 0, false
	}
	return rune(n), true
}

func isHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return len(s) > 0
}

// Code formats a code point the way Unicode writes one: at least four hex
// digits, more when it needs them.
func Code(r rune) string {
	if r > 0xFFFF {
		return fmt.Sprintf("U+%05X", r)
	}
	return fmt.Sprintf("U+%04X", r)
}

// UTF8 spells out the bytes a character is encoded as, which is the other
// half of the question when something has arrived mangled.
func UTF8(r rune) string {
	if r < 0 || r > 0x10FFFF || (r >= 0xD800 && r <= 0xDFFF) {
		return ""
	}
	var buf [4]byte
	n := utf8.EncodeRune(buf[:], r)
	parts := make([]string, n)
	for i, b := range buf[:n] {
		parts[i] = fmt.Sprintf("%02X", b)
	}
	return strings.Join(parts, " ")
}

// categories are the general category codes, spelled out. The two-letter
// code is what the data file carries and what someone who knows Unicode
// reads; the words are for everyone else.
var categories = map[string]string{
	"Lu": "letter, uppercase", "Ll": "letter, lowercase", "Lt": "letter, titlecase",
	"Lm": "letter, modifier", "Lo": "letter, other",
	"Mn": "mark, non-spacing", "Mc": "mark, spacing combining", "Me": "mark, enclosing",
	"Nd": "number, decimal digit", "Nl": "number, letter", "No": "number, other",
	"Pc": "punctuation, connector", "Pd": "punctuation, dash", "Ps": "punctuation, open",
	"Pe": "punctuation, close", "Pi": "punctuation, initial quote",
	"Pf": "punctuation, final quote", "Po": "punctuation, other",
	"Sm": "symbol, math", "Sc": "symbol, currency", "Sk": "symbol, modifier",
	"So": "symbol, other",
	"Zs": "separator, space", "Zl": "separator, line", "Zp": "separator, paragraph",
	"Cc": "other, control", "Cf": "other, format", "Cs": "other, surrogate",
	"Co": "other, private use", "Cn": "other, unassigned",
}

// CategoryName spells a general category out, or returns the code unchanged
// if it is not one.
func CategoryName(cat string) string {
	if s, ok := categories[cat]; ok {
		return s
	}
	return cat
}

// Glyph is the character, made safe to print on a terminal.
//
// Three kinds of character cannot simply be written out. A control character
// would be obeyed rather than shown -- printing the escape that arrived in a
// file is how a terminal ends up in a state its owner did not ask for -- so
// it is replaced by the Control Pictures glyph that stands for it. A
// combining mark has nothing to combine with and would land on whatever
// happens to precede it, so it is given a dotted circle, which is the base
// Unicode itself uses for the purpose. Anything unassigned has no glyph at
// all and is shown as a space.
func Glyph(c Char) string {
	r := c.Code
	switch {
	case r == 0x7F:
		return "\u2421" // SYMBOL FOR DELETE
	case r < 0x20:
		return string(0x2400 + r) // SYMBOL FOR NULL, and so on up
	case r >= 0x80 && r <= 0x9F, c.Category == "Cs", c.Category == "Cn", c.Name == "":
		// C1 controls, surrogates and the unassigned have no picture to use.
		return " "
	case c.Category == "Mn" || c.Category == "Mc" || c.Category == "Me":
		return "\u25CC" + string(r) // DOTTED CIRCLE, then the mark on it
	case c.Category == "Cf", c.Category == "Zl", c.Category == "Zp":
		// Format and line separators are invisible by definition; showing
		// them is showing nothing, and some of them reorder what follows.
		return " "
	default:
		return string(r)
	}
}
