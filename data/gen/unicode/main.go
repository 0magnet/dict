// Command unicode builds data/unicode.tsv.gz: the name, general category and
// block of every Unicode character.
//
// The input is the Unicode Character Database, which is 2.1 MB of
// semicolon-separated text carrying fifteen fields per character. Three of
// them are wanted here, and the file is rewritten to hold those three, which
// is most of the difference between 2.1 MB and the quarter-megabyte that
// ships.
//
// The other saving is the ranges. UnicodeData.txt writes the CJK ideographs
// and the Hangul syllables as a First/Last pair rather than a line each,
// because their names are generated from their code points rather than
// assigned; that convention is kept, so 100,000 ideographs and 11,172
// syllables cost a dozen lines instead of 110,000. See unidata, which
// generates the names back.
//
// The patterns those names are generated from are read from the UCD as well,
// rather than written down here. They cannot be inferred from the range
// label: Unicode 18.0's <Seal Character> range is named SMALL SEAL
// CHARACTER-*, and a table maintained by hand would have had to be wrong
// once before anyone noticed.
//
// Run with: go run ./data/gen/unicode
//
// Source: https://www.unicode.org/Public/UCD/latest/ucd/ (Unicode license,
// see data/licenses/unicode.copyright)
package main

import (
	"bufio"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	ucdURL  = "https://www.unicode.org/Public/UCD/latest/ucd/"
	outPath = "data/unicode.tsv.gz"
)

func main() {
	local := flag.String("ucd", "", "directory holding the UCD files, instead of downloading them")
	out := flag.String("o", outPath, "where to write the table")
	flag.Parse()

	derived := open(*local, "extracted/DerivedName.txt")
	patterns := readPatterns(derived)
	derived.Close() //nolint:errcheck,gosec // read to the end already

	blocks := open(*local, "Blocks.txt")
	version, blockLines := readBlocks(blocks)
	blocks.Close() //nolint:errcheck,gosec // as above

	chars := open(*local, "UnicodeData.txt")
	defer chars.Close() //nolint:errcheck

	f, err := os.Create(*out)
	check(err)
	defer f.Close() //nolint:errcheck
	zw, err := gzip.NewWriterLevel(f, gzip.BestCompression)
	check(err)

	w := bufio.NewWriter(zw)
	fmt.Fprintf(w, "# built by data/gen/unicode from %s -- do not edit\n", ucdURL) //nolint:errcheck
	fmt.Fprintf(w, "@version\t%s\n", version)                                      //nolint:errcheck
	for _, l := range blockLines {
		fmt.Fprintln(w, l) //nolint:errcheck
	}
	named, ranges := readChars(chars, w, patterns)

	check(w.Flush())
	check(zw.Close())
	st, err := f.Stat()
	check(err)
	fmt.Fprintf(os.Stderr, "Unicode %s: %d named characters, %d ranges, %d blocks -> %s (%d bytes)\n", //nolint:errcheck
		version, named, ranges, len(blockLines), *out, st.Size())
}

// open returns a UCD file, from a local directory when one was given and
// from unicode.org otherwise.
func open(dir, name string) io.ReadCloser {
	if dir != "" {
		f, err := os.Open(filepath.Join(dir, filepath.FromSlash(name))) //nolint:gosec // a generator, reading the UCD directory it was pointed at
		check(err)
		return f
	}
	resp, err := http.Get(ucdURL + name) //nolint:noctx // a one-shot generator
	check(err)
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close() //nolint:errcheck,gosec
		check(fmt.Errorf("%s%s: %s", ucdURL, name, resp.Status))
	}
	return resp.Body
}

// pattern is one of the name-generation rules DerivedName.txt states, as a
// code point range and the prefix the code point is appended to.
type pattern struct {
	lo, hi rune
	prefix string
}

// patternLine matches the wildcard rows of DerivedName.txt, which are the
// only rows wanted here:
//
//	3400..4DBF    ; CJK UNIFIED IDEOGRAPH-*
//	18CFF         ; KHITAN SMALL SCRIPT CHARACTER-*
var patternLine = regexp.MustCompile(`^([0-9A-F]{4,6})(?:\.\.([0-9A-F]{4,6}))?\s*;\s*(.*)\*\s*$`)

// readPatterns collects the name-generation rules. Unicode marks them with a
// * standing in for the code point, which is what makes them findable
// without knowing any of the scripts involved.
func readPatterns(r io.Reader) []pattern {
	var out []pattern
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m := patternLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		lo, ok := codePoint(m[1])
		if !ok {
			check(fmt.Errorf("DerivedName.txt: bad code point %q", m[1]))
		}
		hi := lo
		if m[2] != "" {
			if hi, ok = codePoint(m[2]); !ok {
				check(fmt.Errorf("DerivedName.txt: bad code point %q", m[2]))
			}
		}
		out = append(out, pattern{lo, hi, m[3]})
	}
	check(sc.Err())
	if len(out) == 0 {
		check(fmt.Errorf("DerivedName.txt: no name patterns found"))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].lo < out[j].lo })
	return out
}

// find returns the pattern covering the whole of lo..hi. Covering all of it
// matters: a range that straddles two patterns would name half its
// characters wrongly, and there is no such range today, so the right
// response to one appearing is to stop.
func find(ps []pattern, lo, hi rune) (pattern, bool) {
	i := sort.Search(len(ps), func(i int) bool { return ps[i].hi >= lo })
	if i < len(ps) && lo >= ps[i].lo && hi <= ps[i].hi {
		return ps[i], true
	}
	return pattern{}, false
}

// blockHeader is the first line of Blocks.txt, which is where the UCD says
// which version it is: "# Blocks-16.0.0.txt".
var blockHeader = regexp.MustCompile(`^# Blocks-([0-9.]+)\.txt`)

// readBlocks turns "0000..007F; Basic Latin" into an @block line, and picks
// the Unicode version out of the header while it is there.
func readBlocks(r io.Reader) (version string, lines []string) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "#") {
			if m := blockHeader.FindStringSubmatch(line); m != nil {
				version = strings.TrimSuffix(m[1], ".0")
			}
			continue
		}
		semi := strings.IndexByte(line, ';')
		if semi < 0 {
			continue
		}
		lo, hi, ok := strings.Cut(strings.TrimSpace(line[:semi]), "..")
		if !ok {
			continue
		}
		name := strings.TrimSpace(line[semi+1:])
		// "No_Block" is the filler between blocks and names nothing.
		if name == "No_Block" {
			continue
		}
		lines = append(lines, fmt.Sprintf("@block\t%s\t%s\t%s", lo, hi, name))
	}
	check(sc.Err())
	if version == "" {
		check(fmt.Errorf("Blocks.txt: no version in the header"))
	}
	return version, lines
}

// readChars writes a line per named character and a line per range.
//
// A range with no rule to name it by is fatal rather than guessed at. A
// guess would produce a plausible wrong name for every character in the
// range -- tens of thousands of them -- and nothing downstream could tell it
// was wrong.
func readChars(r io.Reader, w io.Writer, patterns []pattern) (named, ranges int) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var pending struct {
		code  rune
		label string
		cat   string
		open  bool
	}
	for sc.Scan() {
		f := strings.Split(sc.Text(), ";")
		if len(f) < 11 {
			continue
		}
		name, cat := f[1], f[2]
		code, ok := codePoint(f[0])
		if !ok {
			continue
		}

		// A range is two lines: <Label, First> and <Label, Last>.
		if label, ok := rangeLabel(name, "First"); ok {
			pending.code, pending.cat, pending.label, pending.open = code, cat, label, true
			continue
		}
		if label, ok := rangeLabel(name, "Last"); ok {
			if !pending.open || pending.label != label {
				check(fmt.Errorf("UnicodeData.txt: %04X closes range %q with no matching First", code, label))
			}
			kind, prefix := ruleFor(patterns, pending.code, code, pending.cat, label)
			fmt.Fprintf(w, "@range\t%04X\t%04X\t%s\t%s\t%s\n", pending.code, code, pending.cat, kind, prefix) //nolint:errcheck
			pending.open = false
			ranges++
			continue
		}

		// A control character has no name of its own -- UnicodeData.txt
		// writes <control> for all sixty-five of them -- but most carry the
		// Unicode 1.0 name in field 10, and NULL or LINE FEED is the answer
		// someone naming the bytes in a file is after.
		if name == "<control>" {
			if old := strings.TrimSpace(f[10]); old != "" {
				name = old
			}
		}
		if strings.HasPrefix(name, "<") {
			continue // any other angle-bracket label names nothing
		}
		fmt.Fprintf(w, "%04X\t%s\t%s\n", code, name, cat) //nolint:errcheck
		named++
	}
	check(sc.Err())
	if pending.open {
		check(fmt.Errorf("UnicodeData.txt: range %q opened at %04X and never closed", pending.label, pending.code))
	}
	return named, ranges
}

// rangeLabel recognizes "<CJK Ideograph, First>" and returns "CJK Ideograph".
func rangeLabel(name, half string) (string, bool) {
	s, ok := strings.CutPrefix(name, "<")
	if !ok {
		return "", false
	}
	return strings.CutSuffix(s, ", "+half+">")
}

// ruleFor says how the names in one range are formed.
//
// Three kinds exist. Surrogates and private use have no names at all, and
// are recognized by their category rather than their label so that a new
// private use plane needs no change here. The Hangul syllables are the one
// range whose names are composed rather than appended, and DerivedName.txt
// lists all 11,172 of them individually rather than as a pattern, so that
// one is named here. Everything else appends its code point to a prefix the
// UCD states.
func ruleFor(patterns []pattern, lo, hi rune, cat, label string) (kind, prefix string) {
	if cat == "Cs" || cat == "Co" {
		return "none", ""
	}
	if label == "Hangul Syllable" {
		return "hangul", "HANGUL SYLLABLE "
	}
	p, ok := find(patterns, lo, hi)
	if !ok {
		check(fmt.Errorf("UnicodeData.txt: range %q (%04X..%04X) has no name pattern in DerivedName.txt", label, lo, hi))
	}
	return "hex", p.prefix
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "gen/unicode:", err) //nolint:errcheck
		os.Exit(1)
	}
}

// codePoint reads a hex code point, rejecting anything past the last one so
// that a malformed line cannot become a negative rune.
func codePoint(s string) (rune, bool) {
	n, err := strconv.ParseUint(s, 16, 32)
	if err != nil || n > 0x10FFFF {
		return 0, false
	}
	return rune(n), true
}
