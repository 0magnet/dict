package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/0magnet/dict/data"
	"github.com/0magnet/dict/dictdb"
	"github.com/0magnet/dict/unidata"
)

// runCmd executes the command tree the way a host that is not a process
// does, and returns what it wrote. It is how the tests below ask what the
// user would have seen.
func runCmd(t *testing.T, stdin string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errb bytes.Buffer
	h := &Host{Stdin: strings.NewReader(stdin), Width: 80}
	code = Run(context.Background(), h, args, &out, &errb)
	return out.String(), errb.String(), code
}

// TestCharArg fixes which arguments the root command answers as a character
// rather than searching the word list for. Getting this wrong in either
// direction is the bad outcome: a word swallowed by the character table, or
// a pasted character reported as no match.
func TestCharArg(t *testing.T) {
	cases := []struct {
		arg  string
		want rune
		ok   bool
	}{
		{"U+20AC", 0x20AC, true},
		{"u+00a0", 0x00A0, true},
		{"0x2014", 0x2014, true},
		{"&#8364;", 0x20AC, true},
		{"€", 0x20AC, true},
		{"中", 0x4E2D, true},
		{"’", 0x2019, true},
		// Words, every one of them, and several are also valid hex.
		{"beef", 0, false},
		{"cafe", 0, false},
		{"decade", 0, false},
		{"receive", 0, false},
		// A single ASCII character is a word ("a", "I") far more often than
		// it is a question about Unicode.
		{"a", 0, false},
		{"I", 0, false},
		{"?", 0, false},
		// More than one character is a phrase, not a character.
		{"€€", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		got, ok := charArg(c.arg)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("charArg(%q) = %X, %v; want %X, %v", c.arg, got, ok, c.want, c.ok)
		}
	}
}

// TestRootAnswersCharacter is the dispatch actually running: a character
// handed to dict gets named, and a word that looks like hex still gets
// defined.
func TestRootAnswersCharacter(t *testing.T) {
	out, _, code := runCmd(t, "", "€")
	if code != 0 {
		t.Fatalf("exit %d, stdout %q", code, out)
	}
	for _, want := range []string{"EURO SIGN", "U+20AC", "E2 82 AC", "Currency Symbols"} {
		if !strings.Contains(out, want) {
			t.Errorf("dict € did not mention %q; got:\n%s", want, out)
		}
	}

	// The regression this guards: "beef" is valid hex and is also a word.
	out, _, code = runCmd(t, "", "-f", "-n", "1", "beef")
	if code != 0 || strings.TrimSpace(out) != "beef" {
		t.Errorf("dict -f beef = %q (exit %d), want the word", out, code)
	}
}

// TestUnicodeArgOrder covers the order the readings are tried in, which is
// the part of :unicode most likely to surprise.
func TestUnicodeArgOrder(t *testing.T) {
	cases := []struct {
		arg  string
		want string
	}{
		{"U+2603", "SNOWMAN"},                    // a code point
		{"☃", "SNOWMAN"},                         // the character itself
		{"snowman", "SNOWMAN"},                   // its exact name
		{"MULTIPLCATION SIGN", "MULTIPLICATION"}, // misspelled: the matcher earns its keep
		{"1F680", "ROCKET"},                      // bare hex, once nothing is named that
		{"BELL", "U+1F514"},                      // the character that owns the name outright
	}
	for _, c := range cases {
		out, errs, code := runCmd(t, "", ":unicode", "-n", "5", c.arg)
		if code != 0 {
			t.Errorf(":unicode %q: exit %d, stderr %q", c.arg, code, errs)
			continue
		}
		if !strings.Contains(out, c.want) {
			t.Errorf(":unicode %q did not mention %q; got:\n%s", c.arg, c.want, out)
		}
	}
}

// TestUnicodeStream is the behavior inherited from the program this came
// from: name the characters arriving on standard input.
func TestUnicodeStream(t *testing.T) {
	// A no-break space and an em dash hiding among ordinary letters is the
	// case the whole command exists for.
	in := "caf\u00e9\u00a0x\u2014x\n"

	out, _, code := runCmd(t, in, ":unicode")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{"NO-BREAK SPACE", "EM DASH", "LATIN SMALL LETTER E WITH ACUTE"} {
		if !strings.Contains(out, want) {
			t.Errorf("stream did not name %q; got:\n%s", want, out)
		}
	}
	// Each distinct character once: "x" appears twice in the input and
	// should be named once.
	if n := strings.Count(out, "LATIN SMALL LETTER X"); n != 1 {
		t.Errorf("x named %d times, want 1 (distinct characters only)", n)
	}
	// A newline is in every file and is never the answer.
	if strings.Contains(out, "LINE FEED") {
		t.Error("the trailing newline was named")
	}

	// --all is the other question: every occurrence, in order.
	out, _, code = runCmd(t, in, ":unicode", "--all")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if n := strings.Count(out, "LATIN SMALL LETTER X"); n != 2 {
		t.Errorf("--all named x %d times, want 2", n)
	}
}

// TestStreamIsSafeOnHostileInput is the property that makes it safe to point
// this at a file nobody has read. Naming the characters in a file must not
// hand the terminal the escape sequences that file contains.
func TestStreamIsSafeOnHostileInput(t *testing.T) {
	// A color change, a cursor move, an alternate-screen switch and a bell.
	hostile := "\x1b[31mred\x1b[2J\x1b[?1049h\a\x07"

	out, _, code := runCmd(t, hostile, ":unicode")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if strings.ContainsAny(out, "\x1b\a\b") {
		t.Errorf("output carries a control character from the input:\n%q", out)
	}
	// It has to have actually said what was in there, or it is safe by
	// virtue of being useless.
	for _, want := range []string{"ESCAPE", "U+001B"} {
		if !strings.Contains(out, want) {
			t.Errorf("did not report the escape; got:\n%s", out)
		}
	}
}

// TestUnicodeBlocks covers the two kinds of block: one stored character by
// character, and one whose names are generated because storing 11,172 of
// them would be most of the file.
func TestUnicodeBlocks(t *testing.T) {
	out, _, code := runCmd(t, "", ":unicode", "-b", "Emoticons", "-n", "3")
	if code != 0 || !strings.Contains(out, "GRINNING FACE") {
		t.Errorf("Emoticons block: exit %d, got:\n%s", code, out)
	}
	if n := len(strings.Split(strings.TrimSpace(out), "\n")); n != 3 {
		t.Errorf("-n 3 printed %d lines", n)
	}

	// Hangul is a range: every one of these names is composed at lookup.
	out, _, code = runCmd(t, "", ":unicode", "-b", "Hangul Syllables", "-n", "2")
	if code != 0 || !strings.Contains(out, "HANGUL SYLLABLE GA") {
		t.Errorf("Hangul block: exit %d, got:\n%s", code, out)
	}

	// The name is matched loosely, since nobody remembers the underscores.
	for _, name := range []string{"basic latin", "Basic_Latin", "BASICLATIN"} {
		if _, _, code := runCmd(t, "", ":unicode", "-b", name, "-n", "1"); code != 0 {
			t.Errorf("-b %q was not recognized", name)
		}
	}
	if _, _, code := runCmd(t, "", ":unicode", "-b", "No Such Block"); code == 0 {
		t.Error("an unknown block succeeded")
	}
}

// TestCleanLeavesCharacterFacts is the assumption the picker rests on, made
// into a test.
//
// Everything a dictdb.Set returns goes through dictdb.Clean, which is built
// for GCIDE's 1913 typesetting: it strips braces, rewrites bracketed accent
// escapes and drops parenthesized pronunciations. Character facts use none
// of those conventions, so Clean is a no-op on them -- but "is a no-op" is a
// claim about two pieces of code that do not know about each other, and it
// would fail silently and invisibly if either changed.
//
// The entry includes the character itself, which is the part with something
// to lose: the sample covers the brackets and braces Clean rewrites, and a
// character that is one of them is a character someone will look up.
func TestCleanLeavesCharacterFacts(t *testing.T) {
	tab, err := data.Unicode()
	if err != nil {
		t.Fatal(err)
	}
	// Every hundredth character, plus the ones most likely to carry
	// something Clean reacts to.
	var sample []rune
	for i := 0; i < tab.Len(); i += 100 {
		sample = append(sample, tab.At(i).Code)
	}
	sample = append(sample, '€', 0x00A0, 0x0301, 0x1F600, 0x4E2D, 0xAC00, 0x0007, 0x007B, 0x007D, 0x005C, 0x0028)

	for _, r := range sample {
		c, _ := tab.Lookup(r)
		text := charEntry(c)
		if got := dictdb.Clean(text); got != text {
			t.Errorf("Clean changed the facts for %s:\n%q\nbecame\n%q", unidata.Code(r), text, got)
		}
	}
}

// TestUnicodeSourceIsADictionary checks the adapter the picker's definition
// pane goes through, by the route the pane actually takes.
func TestUnicodeSourceIsADictionary(t *testing.T) {
	tab, err := data.Unicode()
	if err != nil {
		t.Fatal(err)
	}
	set := &dictdb.Set{}
	set.Add(unicodeName, func() (dictdb.Dictionary, error) { return unicodeSource{tab}, nil })

	res := set.Define("EURO SIGN")
	if len(res) == 0 {
		t.Fatal("the picker would show no entry for EURO SIGN")
	}
	if res[0].DB != unicodeName {
		t.Errorf("entry came from %q", res[0].DB)
	}
	for _, want := range []string{"U+20AC", "symbol, currency", "Currency Symbols"} {
		if !strings.Contains(res[0].Text, want) {
			t.Errorf("the pane would not show %q; got:\n%s", want, res[0].Text)
		}
	}
	if !strings.Contains(unicodeSource{tab}.Title(), tab.Version()) {
		t.Error("the source does not say which Unicode it is")
	}
	if set.Define("NOT A CHARACTER NAME") != nil {
		t.Error("the set invented an entry")
	}
}

// TestPickedNameGivesBackACharacter is what the picker does on the way out:
// the name it returns has to turn back into the character, or choosing one
// hands back nothing usable.
func TestPickedNameGivesBackACharacter(t *testing.T) {
	tab, err := data.Unicode()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"EURO SIGN", "SNOWMAN", "MULTIPLICATION SIGN", "HANGUL SYLLABLE GA"} {
		c, ok := tab.ByName(name)
		if !ok {
			t.Errorf("picking %q would yield nothing", name)
			continue
		}
		if string(c.Code) == "" {
			t.Errorf("picking %q would print an empty string", name)
		}
	}
}

// TestSourcesMentionsUnicode: :sources is how you find out what a build
// carries, and it now carries this.
func TestSourcesMentionsUnicode(t *testing.T) {
	out, _, code := runCmd(t, "", ":sources")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out, "unicode") {
		t.Errorf(":sources does not mention the character table:\n%s", out)
	}
}

// TestLicensesCoverUnicode: the terms travel with the data, which is the
// rule the rest of the corpus already follows.
func TestLicensesCoverUnicode(t *testing.T) {
	lics, err := data.Licenses()
	if err != nil {
		t.Fatal(err)
	}
	text, ok := lics["unicode"]
	if !ok {
		t.Fatal("no license for the character table")
	}
	if !strings.Contains(text, "Unicode") {
		t.Errorf("the unicode license does not look like one:\n%s", text)
	}
}

// TestUnicodeDashIsStdin covers the explicit spelling of "read standard
// input", which exists for a host that cannot tell a pipe from a terminal --
// the shell in the demo page, where without it `echo x | dict :unicode`
// opens the picker instead of answering.
func TestUnicodeDashIsStdin(t *testing.T) {
	out, _, code := runCmd(t, " ", ":unicode", "-")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out, "NO-BREAK SPACE") {
		t.Errorf(`:unicode - did not read stdin; got:\n%s`, out)
	}
	// It must not be mistaken for a character or a name to search for.
	if strings.Contains(out, "HYPHEN-MINUS") {
		t.Error(`"-" was read as the character rather than as stdin`)
	}
}
