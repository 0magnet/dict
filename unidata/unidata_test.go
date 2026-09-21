package unidata_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/0magnet/dict/data"
	"github.com/0magnet/dict/unidata"
)

func load(t *testing.T) *unidata.Table {
	t.Helper()
	tab, err := data.Unicode()
	if err != nil {
		t.Fatalf("load the table: %v", err)
	}
	return tab
}

// TestNames covers one character of each kind the table stores differently:
// a listed name, a control's Unicode 1.0 name, a code point in an
// append-the-hex range, and a Hangul syllable whose name is composed from
// the jamo it decomposes to.
func TestNames(t *testing.T) {
	tab := load(t)
	cases := []struct {
		r    rune
		name string
	}{
		{'a', "LATIN SMALL LETTER A"},
		{'€', "EURO SIGN"},
		{0x00A0, "NO-BREAK SPACE"},
		{0x0000, "NULL"},
		{0x000A, "LINE FEED (LF)"},
		{0x4E2D, "CJK UNIFIED IDEOGRAPH-4E2D"},
		{0x3400, "CJK UNIFIED IDEOGRAPH-3400"},
		{0xAC00, "HANGUL SYLLABLE GA"},
		{0xD7A3, "HANGUL SYLLABLE HIH"},
		{0xB108, "HANGUL SYLLABLE NEO"},
		{0x17000, "TANGUT IDEOGRAPH-17000"},
		{0x1F600, "GRINNING FACE"},
		{0x10FFFC, "<private-use>"},
	}
	for _, c := range cases {
		if got := tab.Name(c.r); got != c.name {
			t.Errorf("Name(%s) = %q, want %q", unidata.Code(c.r), got, c.name)
		}
	}
}

// TestUnnamed checks the code points that have no name. They are the reason
// Name never returns the empty string: a caller naming the characters in a
// file it did not write meets these, and "" would print as a blank line.
func TestUnnamed(t *testing.T) {
	tab := load(t)
	for _, c := range []struct {
		r     rune
		label string
	}{
		{0xD800, "<surrogate>"},
		{0xE000, "<private-use>"},
		{0x0378, "<unassigned>"}, // a hole in Greek and Coptic
	} {
		if _, ok := tab.Lookup(c.r); ok {
			t.Errorf("Lookup(%s) reports a name", unidata.Code(c.r))
		}
		if got := tab.Name(c.r); got != c.label {
			t.Errorf("Name(%s) = %q, want %q", unidata.Code(c.r), got, c.label)
		}
	}
}

func TestCategoryAndBlock(t *testing.T) {
	tab := load(t)
	cases := []struct {
		r     rune
		cat   string
		block string
		spelt string
	}{
		{'a', "Ll", "Basic Latin", "letter, lowercase"},
		{'€', "Sc", "Currency Symbols", "symbol, currency"},
		{0x0301, "Mn", "Combining Diacritical Marks", "mark, non-spacing"},
		{0x1F600, "So", "Emoticons", "symbol, other"},
		{0x4E2D, "Lo", "CJK Unified Ideographs", "letter, other"},
	}
	for _, c := range cases {
		got, ok := tab.Lookup(c.r)
		if !ok {
			t.Errorf("Lookup(%s) found nothing", unidata.Code(c.r))
			continue
		}
		if got.Category != c.cat {
			t.Errorf("%s category = %q, want %q", unidata.Code(c.r), got.Category, c.cat)
		}
		if got.Block != c.block {
			t.Errorf("%s block = %q, want %q", unidata.Code(c.r), got.Block, c.block)
		}
		if s := unidata.CategoryName(got.Category); s != c.spelt {
			t.Errorf("%s category spelt %q, want %q", unidata.Code(c.r), s, c.spelt)
		}
	}
}

// TestByName is the reverse direction, which is what a search lands on: the
// name found has to lead back to the character.
func TestByName(t *testing.T) {
	tab := load(t)
	for _, name := range []string{
		"EURO SIGN",
		"euro sign",
		"LATIN_SMALL_LETTER_A",
		"cjk unified ideograph-4e2d",
		"GRINNING FACE",
		// Names from the algorithmic ranges, which are generated on the way
		// out and so have to be unpicked on the way back in.
		"HANGUL SYLLABLE GA",
		"hangul syllable hih",
		"TANGUT IDEOGRAPH-17000",
		"SMALL SEAL CHARACTER-3D000",
	} {
		c, ok := tab.ByName(name)
		if !ok {
			t.Errorf("ByName(%q) found nothing", name)
			continue
		}
		if got := tab.Name(c.Code); !strings.EqualFold(strings.ReplaceAll(got, "_", " "), strings.ReplaceAll(name, "_", " ")) {
			t.Errorf("ByName(%q) gave %s, named %q", name, unidata.Code(c.Code), got)
		}
	}
	if _, ok := tab.ByName("NO SUCH CHARACTER AT ALL"); ok {
		t.Error("ByName invented a character")
	}
}

// TestEveryNameResolves is the invariant that makes the search usable: every
// name in the index has to lead back to a character actually called that, or
// picking a search result would hand back the wrong character.
func TestEveryNameResolves(t *testing.T) {
	tab := load(t)
	names := tab.Names()
	if len(names) < 40000 {
		t.Fatalf("table holds %d names, expected the whole of Unicode", len(names))
	}
	var collisions []string
	for i, name := range names {
		want := tab.At(i)
		got, ok := tab.ByName(name)
		if !ok {
			t.Fatalf("%s %q does not resolve by name", unidata.Code(want.Code), name)
		}
		if got.Code != want.Code {
			// The one exception is documented on ByName: a control
			// character carries its Unicode 1.0 name, and BELL is also
			// the real name of U+1F514.
			if want.Category == "Cc" && tab.Name(got.Code) == name {
				collisions = append(collisions, name)
				continue
			}
			t.Fatalf("%q resolves to %s, want %s", name, unidata.Code(got.Code), unidata.Code(want.Code))
		}
	}
	// Any new collision is a data change worth noticing rather than
	// absorbing: it means a name promoted from field 10 has landed on a
	// name Unicode assigned to something else.
	if want := []string{"BELL"}; !slices.Equal(collisions, want) {
		t.Errorf("names shared with a control = %q, want %q", collisions, want)
	}
}

func TestParseCode(t *testing.T) {
	cases := []struct {
		in   string
		want rune
		ok   bool
	}{
		{"U+20AC", 0x20AC, true},
		{"u+20ac", 0x20AC, true},
		{"0x20AC", 0x20AC, true},
		{"\\u20ac", 0x20AC, true},
		{"&#x20AC;", 0x20AC, true},
		{"&#8364;", 0x20AC, true},
		{"U+1F600", 0x1F600, true},
		{"U+0", 0, true},
		{"", 0, false},
		{"U+110000", 0, false}, // past the last code point
		{"hello", 0, false},
		{"U+ZZZZ", 0, false},
		// A bare hex number is not a code point here. This is the whole
		// reason ParseCode and ParseHex are separate: "beef" reaching the
		// root command has to stay a word, or `dict beef` stops being a
		// dictionary lookup.
		{"20AC", 0, false},
		{"beef", 0, false},
		{"cafe", 0, false},
	}
	for _, c := range cases {
		got, ok := unidata.ParseCode(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("ParseCode(%q) = %X, %v; want %X, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

// TestParseHex is the looser reading, used only where the command is already
// about characters and nothing turned out to be named that.
func TestParseHex(t *testing.T) {
	cases := []struct {
		in   string
		want rune
		ok   bool
	}{
		{"20AC", 0x20AC, true},
		{"1F600", 0x1F600, true},
		{"beef", 0xBEEF, true},
		{"10FFFF", 0x10FFFF, true},
		// Under four digits stays a word even here: three hex digits name
		// nothing anyone is looking for, and "ace" and "dad" are words.
		{"ace", 0, false},
		{"dad", 0, false},
		{"0", 0, false},
		{"110000", 0, false},
		{"hello", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		got, ok := unidata.ParseHex(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("ParseHex(%q) = %X, %v; want %X, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

// TestGlyph covers the characters that cannot simply be printed: a control
// would be obeyed by the terminal rather than shown, and a combining mark
// would land on whatever happened to precede it.
func TestGlyph(t *testing.T) {
	tab := load(t)
	cases := []struct {
		r    rune
		want string
	}{
		{'a', "a"},
		{'€', "€"},
		{0x0000, "␀"},  // SYMBOL FOR NULL
		{0x001B, "␛"},  // SYMBOL FOR ESCAPE
		{0x007F, "␡"},  // SYMBOL FOR DELETE
		{0x0301, "◌́"}, // COMBINING ACUTE on a dotted circle
		{0x0085, " "},  // a C1 control, which has no picture
		{0x200E, " "},  // LEFT-TO-RIGHT MARK, invisible by definition
	}
	for _, c := range cases {
		ch, _ := tab.Lookup(c.r)
		if got := unidata.Glyph(ch); got != c.want {
			t.Errorf("Glyph(%s) = %q, want %q", unidata.Code(c.r), got, c.want)
		}
	}
}

// TestNoEscapesEscape is the safety property behind Glyph: naming the
// characters in a hostile file must not hand the terminal an escape
// sequence to obey.
func TestNoEscapesEscape(t *testing.T) {
	tab := load(t)
	for r := rune(0); r < 0x300; r++ {
		ch, _ := tab.Lookup(r)
		g := unidata.Glyph(ch)
		if strings.ContainsAny(g, "\x1b\r\n\a\b") {
			t.Errorf("Glyph(%s) = %q carries a control character", unidata.Code(r), g)
		}
	}
}

func TestUTF8(t *testing.T) {
	for _, c := range []struct {
		r    rune
		want string
	}{
		{'a', "61"},
		{0x00A0, "C2 A0"},
		{'€', "E2 82 AC"},
		{0x1F600, "F0 9F 98 80"},
		{0xD800, ""}, // a surrogate is not encodable
	} {
		if got := unidata.UTF8(c.r); got != c.want {
			t.Errorf("UTF8(%s) = %q, want %q", unidata.Code(c.r), got, c.want)
		}
	}
}

func TestCode(t *testing.T) {
	for _, c := range []struct {
		r    rune
		want string
	}{
		{'a', "U+0061"},
		{0x20AC, "U+20AC"},
		{0x1F600, "U+1F600"},
		{0x10FFFF, "U+10FFFF"},
	} {
		if got := unidata.Code(c.r); got != c.want {
			t.Errorf("Code(%X) = %q, want %q", c.r, got, c.want)
		}
	}
}

func TestAssigned(t *testing.T) {
	tab := load(t)
	// The ranges are most of Unicode by count, which is why they are stored
	// as ranges; if this ever drops to the named count they stopped being
	// read.
	if tab.Assigned() < 2*tab.Len() {
		t.Errorf("Assigned() = %d with %d named; the ranges are not being counted", tab.Assigned(), tab.Len())
	}
	if tab.Version() == "" {
		t.Error("the table carries no Unicode version")
	}
}
