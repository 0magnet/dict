// Package unidata is the Unicode character database: what a character is
// called, what kind of thing it is, and which block it belongs to.
//
// It answers the question dict's dictionaries cannot. A dictionary is asked
// what a word means; this is asked what a character is -- which is the same
// question one step down, and the one that actually comes up when a character
// has been pasted from somewhere and is not what it appears to be. A
// no-break space and a space look identical and are not, and the only way to
// tell is to ask what they are called.
//
// The table is read from the compressed file built by data/gen/unicode. It
// holds a line per named character and a line per algorithmic range, because
// the ranges are most of Unicode by count -- the CJK ideographs alone are
// nearly 100,000 characters whose names are their code points spelled out --
// and writing them down would multiply the file by five for nothing.
package unidata

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// Char is one character: what it is called and what kind of thing it is.
type Char struct {
	Code     rune
	Name     string // "LATIN SMALL LETTER A"; never empty, see Table.Lookup
	Category string // the two-letter general category, e.g. "Ll"
	Block    string // "Basic Latin", or "" outside every assigned block
}

// String renders the character the way the command prints it.
func (c Char) String() string { return c.Name }

// Block is a named range of code points.
type Block struct {
	Lo, Hi rune
	Name   string
}

// rangeKind says how the names in an algorithmic range are formed.
type rangeKind int

const (
	rangeHex    rangeKind = iota // prefix + the code point in hex, e.g. CJK UNIFIED IDEOGRAPH-4E2D
	rangeHangul                  // composed from the jamo the syllable decomposes to
	rangeNone                    // no name at all: surrogates and private use
)

type nameRange struct {
	lo, hi   rune
	kind     rangeKind
	prefix   string
	category string

	// back is the name-to-code-point map for a composed range, built on
	// first use by reverse.
	back map[string]rune
}

// Table is the whole database, ready to be asked about a character.
type Table struct {
	version string

	// Characters with a name of their own, sorted by code point. Held as
	// parallel slices rather than a []Char because the names alone are what
	// a search over them wants, and a []string is what match.NewIndex takes.
	codes []rune
	names []string
	cats  []string

	ranges []nameRange
	blocks []Block

	// lowered names, built on first use, for exact lookup by name.
	byName map[string]rune
}

// Read parses the gzipped table written by data/gen/unicode.
func Read(r io.Reader) (*Table, error) {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("unicode table: %w", err)
	}
	defer zr.Close() //nolint:errcheck // read-only

	t := &Table{}
	sc := bufio.NewScanner(zr)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		// Metadata lines are marked with a leading @, which no code point
		// can begin with; everything else is a character.
		if !strings.HasPrefix(f[0], "@") {
			if len(f) < 3 {
				continue
			}
			code, err := parseCodePoint(f[0])
			if err != nil {
				continue
			}
			t.codes = append(t.codes, code)
			t.names = append(t.names, f[1])
			t.cats = append(t.cats, f[2])
			continue
		}
		if err := t.meta(f); err != nil {
			return nil, err
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("unicode table: %w", err)
	}
	if len(t.codes) == 0 {
		return nil, fmt.Errorf("unicode table: no characters")
	}
	if !sort.SliceIsSorted(t.codes, func(i, j int) bool { return t.codes[i] < t.codes[j] }) {
		sort.Sort(&byCode{t})
	}
	sort.Slice(t.ranges, func(i, j int) bool { return t.ranges[i].lo < t.ranges[j].lo })
	sort.Slice(t.blocks, func(i, j int) bool { return t.blocks[i].Lo < t.blocks[j].Lo })
	return t, nil
}

func (t *Table) meta(f []string) error {
	switch f[0] {
	case "@version":
		if len(f) > 1 {
			t.version = f[1]
		}
	case "@block":
		if len(f) < 4 {
			return fmt.Errorf("unicode table: short @block line")
		}
		lo, hi, err := parsePair(f[1], f[2])
		if err != nil {
			return err
		}
		t.blocks = append(t.blocks, Block{lo, hi, f[3]})
	case "@range":
		if len(f) < 5 {
			return fmt.Errorf("unicode table: short @range line")
		}
		lo, hi, err := parsePair(f[1], f[2])
		if err != nil {
			return err
		}
		nr := nameRange{lo: lo, hi: hi, category: f[3]}
		switch f[4] {
		case "hangul":
			nr.kind = rangeHangul
		case "none":
			nr.kind = rangeNone
		default:
			nr.kind = rangeHex
		}
		if len(f) > 5 {
			nr.prefix = f[5]
		}
		t.ranges = append(t.ranges, nr)
	}
	return nil
}

func parsePair(a, b string) (rune, rune, error) {
	lo, err1 := parseCodePoint(a)
	hi, err2 := parseCodePoint(b)
	if err1 != nil || err2 != nil {
		return 0, 0, fmt.Errorf("unicode table: bad code point range %s..%s", a, b)
	}
	return lo, hi, nil
}

// parseCodePoint reads one hex code point from the table.
//
// The upper bound is not decoration. Without it a corrupt or truncated line
// claiming FFFFFFFF would convert to a negative rune, sort ahead of
// everything, and quietly poison every binary search over the table.
func parseCodePoint(s string) (rune, error) {
	n, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0, err
	}
	if n > 0x10FFFF {
		return 0, fmt.Errorf("code point %s is past U+10FFFF", s)
	}
	return rune(n), nil
}

type byCode struct{ t *Table }

func (s *byCode) Len() int           { return len(s.t.codes) }
func (s *byCode) Less(i, j int) bool { return s.t.codes[i] < s.t.codes[j] }
func (s *byCode) Swap(i, j int) {
	t := s.t
	t.codes[i], t.codes[j] = t.codes[j], t.codes[i]
	t.names[i], t.names[j] = t.names[j], t.names[i]
	t.cats[i], t.cats[j] = t.cats[j], t.cats[i]
}

// Version is the Unicode version the table was built from, e.g. "16.0".
func (t *Table) Version() string { return t.version }

// Len reports how many characters carry a name of their own. It is not the
// number of assigned code points, which is three times larger: the ranges
// are not counted here because they are not stored one by one.
func (t *Table) Len() int { return len(t.codes) }

// Assigned reports how many code points are assigned, ranges included.
func (t *Table) Assigned() int {
	n := len(t.codes)
	for _, r := range t.ranges {
		n += int(r.hi-r.lo) + 1
	}
	return n
}

// Lookup returns what is known about a code point.
//
// A code point in an algorithmic range gets the name Unicode derives for it
// rather than no name, so a CJK ideograph answers CJK UNIFIED IDEOGRAPH-4E2D
// and a Hangul syllable answers HANGUL SYLLABLE GA. Unassigned code points,
// surrogates and private use have no name at all; those report ok false, and
// Name gives the angle-bracket label instead.
func (t *Table) Lookup(r rune) (Char, bool) {
	i := sort.Search(len(t.codes), func(i int) bool { return t.codes[i] >= r })
	if i < len(t.codes) && t.codes[i] == r {
		return Char{Code: r, Name: t.names[i], Category: t.cats[i], Block: t.BlockOf(r)}, true
	}
	for i := range t.ranges {
		nr := &t.ranges[i]
		if r < nr.lo || r > nr.hi {
			continue
		}
		name := nr.name(r)
		if name == "" {
			return Char{Code: r, Category: nr.category, Block: t.BlockOf(r)}, false
		}
		return Char{Code: r, Name: name, Category: nr.category, Block: t.BlockOf(r)}, true
	}
	return Char{Code: r, Block: t.BlockOf(r)}, false
}

func (nr *nameRange) name(r rune) string {
	switch nr.kind {
	case rangeHangul:
		return nr.prefix + hangulName(r)
	case rangeNone:
		return ""
	default:
		return fmt.Sprintf("%s%04X", nr.prefix, r)
	}
}

// Name is the character's name, or an angle-bracket label for a code point
// that has none: <control>, <surrogate>, <private-use>, <unassigned>.
//
// Every code point gets an answer, because the caller asking is naming
// characters out of a file it did not write and one it cannot name is
// exactly the interesting case.
func (t *Table) Name(r rune) string {
	c, ok := t.Lookup(r)
	if ok {
		return c.Name
	}
	switch {
	case r >= 0xD800 && r <= 0xDFFF:
		return "<surrogate>"
	case c.Category == "Co":
		return "<private-use>"
	case r > 0x10FFFF:
		return "<not a character>"
	default:
		return "<unassigned>"
	}
}

// At returns the i'th named character in code point order, for walking the
// table without copying it.
func (t *Table) At(i int) Char {
	return Char{Code: t.codes[i], Name: t.names[i], Category: t.cats[i], Block: t.BlockOf(t.codes[i])}
}

// Names returns the name of every character that has one, in code point
// order and sharing the table's storage. It is what a search index is built
// over; At turns a position in it back into a character.
func (t *Table) Names() []string { return t.names }

// ByName looks a character up by its exact name, ignoring case and reading
// underscores as spaces, so "latin small letter a" and "LATIN_SMALL_LETTER_A"
// both find it. A hyphen is not a space: Unicode uses the difference, and
// TIBETAN LETTER -A is U+0F60 while TIBETAN LETTER A is U+0F68. Approximate
// input is what the name search is for; this is the exact match under it.
//
// One name in Unicode is not unique here, and it is a consequence of the
// choice made in data/gen/unicode: control characters have no name of their
// own, so their Unicode 1.0 name is carried instead -- NULL, LINE FEED -- and
// one of those, BELL, is also the real name of U+1F514. The character that
// owns a name outright wins, so BELL finds the bell and U+0007 is reached by
// its code point, which is how a control is written anyway.
func (t *Table) ByName(name string) (Char, bool) {
	if t.byName == nil {
		t.byName = make(map[string]rune, len(t.names))
		for i, n := range t.names {
			key := foldName(n)
			if prev, dup := t.byName[key]; dup && t.categoryOf(prev) != "Cc" {
				continue
			}
			t.byName[key] = t.codes[i]
		}
	}
	key := foldName(name)
	if r, ok := t.byName[key]; ok {
		return t.Lookup(r)
	}
	// Then the ranges, whose names are generated rather than stored and so
	// are not in the map at all. Each one is asked to run its rule backwards.
	for i := range t.ranges {
		if r, ok := t.ranges[i].code(key); ok {
			return t.Lookup(r)
		}
	}
	return Char{}, false
}

// code runs the range's naming rule backwards: given a folded name, the code
// point it belongs to.
//
// This is the half that makes a search result usable. Forwards, a range
// turns U+AC00 into HANGUL SYLLABLE GA; a picker hands back the name it
// showed, and without the way back there is nothing to hand the user.
func (nr *nameRange) code(folded string) (rune, bool) {
	rest, ok := strings.CutPrefix(folded, foldName(nr.prefix)+" ")
	if !ok {
		// A hex range's prefix ends in a hyphen rather than a space, and
		// foldName leaves hyphens alone.
		if rest, ok = strings.CutPrefix(folded, foldName(nr.prefix)); !ok {
			return 0, false
		}
	}
	switch nr.kind {
	case rangeHangul:
		nr.reverse()
		r, ok := nr.back[rest]
		return r, ok
	case rangeNone:
		return 0, false
	default:
		v, err := parseCodePoint(rest)
		if err != nil || v < nr.lo || v > nr.hi {
			return 0, false
		}
		return v, true
	}
}

// reverse builds the name-to-code-point map for a composed range, on first
// use. Only the Hangul syllables need one -- 11,172 entries -- because
// theirs is the one rule that is not simply a code point in hex, and
// unpicking a run of jamo short names by hand is ambiguous in a way that
// generating all of them is not.
func (nr *nameRange) reverse() {
	if nr.back != nil {
		return
	}
	nr.back = make(map[string]rune, int(nr.hi-nr.lo)+1)
	for r := nr.lo; r <= nr.hi; r++ {
		if n := hangulName(r); n != "" {
			nr.back[foldName(n)] = r
		}
	}
}

func foldName(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '_' || r == ' ':
			b.WriteByte(' ')
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + 32)
		default:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// BlockOf names the block a code point falls in, or "" if it falls outside
// every one of them.
func (t *Table) BlockOf(r rune) string {
	i := sort.Search(len(t.blocks), func(i int) bool { return t.blocks[i].Hi >= r })
	if i < len(t.blocks) && r >= t.blocks[i].Lo {
		return t.blocks[i].Name
	}
	return ""
}

// Blocks lists every block, in code point order.
func (t *Table) Blocks() []Block { return t.blocks }

// Hangul syllable names are composed rather than listed: the syllable
// decomposes into an initial, a medial and an optional final jamo, and the
// name is their short names run together. See Unicode 3.12, "Hangul Syllable
// Name Generation".
const (
	hangulBase   = 0xAC00
	hangulVCount = 21
	hangulTCount = 28
	hangulNCount = hangulVCount * hangulTCount
	hangulCount  = 19 * hangulNCount
)

var (
	jamoL = [...]string{"G", "GG", "N", "D", "DD", "R", "M", "B", "BB", "S", "SS", "", "J", "JJ", "C", "K", "T", "P", "H"}
	jamoV = [...]string{"A", "AE", "YA", "YAE", "EO", "E", "YEO", "YE", "O", "WA", "WAE", "OE", "YO", "U", "WEO", "WE", "WI", "YU", "EU", "YI", "I"}
	jamoT = [...]string{"", "G", "GG", "GS", "N", "NJ", "NH", "D", "L", "LG", "LM", "LB", "LS", "LT", "LP", "LH", "M", "B", "BS", "S", "SS", "NG", "J", "C", "K", "T", "P", "H"}
)

func hangulName(r rune) string {
	i := int(r) - hangulBase
	if i < 0 || i >= hangulCount {
		return ""
	}
	return jamoL[i/hangulNCount] + jamoV[(i%hangulNCount)/hangulTCount] + jamoT[i%hangulTCount]
}

// categoryOf is the general category of a listed character, or "" if the
// code point is not one. It reads the stored column rather than going
// through Lookup, which would also resolve the block.
func (t *Table) categoryOf(r rune) string {
	i := sort.Search(len(t.codes), func(i int) bool { return t.codes[i] >= r })
	if i < len(t.codes) && t.codes[i] == r {
		return t.cats[i]
	}
	return ""
}
