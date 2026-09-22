package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/clipperhouse/displaywidth"
	"github.com/spf13/cobra"

	"github.com/0magnet/dict/data"
	"github.com/0magnet/dict/dictdb"
	"github.com/0magnet/dict/match"
	"github.com/0magnet/dict/unidata"
)

// The character half of dict, which used to be a separate program.
//
// It is the same question one level down. A dictionary answers what a word
// means; this answers what a character is, and the reason to want that is
// almost always the same as the reason to want a spelling: something is not
// what it appears to be. A no-break space and a space are indistinguishable
// on screen and behave differently, an en dash and a hyphen likewise, and a
// Cyrillic er is a p that no search will find. None of that is visible. The
// name is the only handle on it.
//
// So the matcher dict already has is pointed at the character names, and the
// picker dict already has shows the answer, because a character is looked up
// the same way a word is: by half-remembering what it is called.

var uniOpts struct {
	file    string
	list    bool
	block   string
	blocks  bool
	limit   int
	all     bool
	noNames bool
	noANSI  bool
	reverse bool
}

var unicodeCmd = &cobra.Command{
	Use:     ":unicode [character|U+XXXX|name]...",
	Aliases: []string{":uni", ":char"},
	Short:   "name a character, or find one by name",
	Long: "Name a character, or find one by name.\n\n" +
		"With an argument, say what it is: a character, a code point written\n" +
		"U+XXXX, or part of a name to search for. With none, name every\n" +
		"character read from standard input, which is how to find out what is\n" +
		"actually in a line that does not look right; \"-\" asks for that\n" +
		"outright. On a terminal with nothing piped in, the interactive\n" +
		"picker searches the names.\n\n" +
		"The whole of Unicode is built into this binary, as it is into the\n" +
		"dictionaries: nothing is fetched and nothing needs to be installed.",
	Example: "  dict :unicode ’              what that apostrophe really is\n" +
		"  dict :unicode U+00A0         a code point by number\n" +
		"  dict :unicode snowman        find one by name\n" +
		"  pbpaste | dict :unicode      name everything in what was pasted\n" +
		"  dict :unicode -b Emoticons   list a block",
	Args:                  cobra.ArbitraryArgs,
	SilenceUsage:          true,
	DisableFlagsInUseLine: true,
	RunE:                  runUnicode,
}

func init() {
	f := unicodeCmd.Flags()
	f.StringVarP(&uniOpts.file, "file", "f", "", "name the characters in this file instead of standard input")
	f.BoolVarP(&uniOpts.list, "list", "l", false, "list every character in the database")
	f.StringVarP(&uniOpts.block, "block", "b", "", "only characters in this block; implies --list")
	f.BoolVar(&uniOpts.blocks, "blocks", false, "list the Unicode blocks")
	f.IntVarP(&uniOpts.limit, "num", "n", 0, "maximum characters to print (0 for all)")
	f.BoolVarP(&uniOpts.all, "all", "a", false, "print every occurrence, not just each distinct character")
	f.BoolVarP(&uniOpts.noNames, "no-names", "m", false, "print the characters alone, without their names")
	f.BoolVar(&uniOpts.noANSI, "no-ansi", false, "never wrap output in ANSI reset codes")
	f.BoolVar(&uniOpts.reverse, "reverse", false, "prompt at the top and list running down")
}

func runUnicode(cmd *cobra.Command, args []string) error {
	tab, err := data.Unicode()
	if err != nil {
		return err
	}
	switch {
	case uniOpts.blocks:
		printBlocks(tab)
		return nil
	case uniOpts.list || uniOpts.block != "":
		return listChars(tab)
	case uniOpts.file != "":
		f, err := os.Open(uniOpts.file)
		if err != nil {
			return err
		}
		defer f.Close() //nolint:errcheck // read-only
		return nameStream(tab, f)
	// "-" is standard input, said outright.
	//
	// With nothing piped in, a terminal host answers Interactive and gets
	// the picker; with something piped in it answers false, because stdin
	// is not a terminal, and the stream is read. That is the right answer
	// and it is free -- for a process. A host that is not a process cannot
	// tell the two apart: the shell in the demo page hands every command
	// the same kind of reader whether or not it is in a pipeline, and
	// neither it nor the interpreter under it exposes which. So there is a
	// way to say it, which is the usual one.
	case len(args) == 1 && args[0] == "-":
		return nameStream(tab, in())
	case len(args) > 0:
		return answerChars(tab, args)
	case host.Interactive:
		return pickChar(tab, "")
	default:
		return nameStream(tab, in())
	}
}

// answerChars answers each argument, which may be a character, a code point
// or a name to search for.
//
// The order the readings are tried in is the whole of the design. A literal
// character comes first because a character is unambiguous and is what is
// usually pasted in. An exact name comes before a search, so asking for
// BELL gets the bell rather than the twelve things with BELL in the name.
// A bare hex number comes last of all, after the search has found nothing,
// because "beef" and "cafe" are hex and are also words -- see
// unidata.ParseCode.
func answerChars(tab *unidata.Table, args []string) error {
	for i, arg := range args {
		if i > 0 {
			println()
		}
		if r, ok := unidata.ParseCode(arg); ok {
			printChar(tab, r)
			continue
		}
		if r, n := utf8.DecodeRuneInString(arg); n == len(arg) && r != utf8.RuneError {
			printChar(tab, r)
			continue
		}
		if c, ok := tab.ByName(arg); ok {
			printChar(tab, c.Code)
			continue
		}
		if n := searchNames(tab, arg); n > 0 {
			continue
		}
		if r, ok := unidata.ParseHex(arg); ok {
			printChar(tab, r)
			continue
		}
		// Several characters, none of which is a name: name them one by one,
		// which is what someone who pasted a word of them wants.
		if utf8.RuneCountInString(arg) > 1 {
			if err := nameStream(tab, strings.NewReader(arg)); err != nil {
				return err
			}
			continue
		}
		return fmt.Errorf("nothing is named %q", arg)
	}
	return nil
}

// searchNames ranks the character names against the query and prints what it
// finds, returning how many. It is the same matcher the word list is
// searched with, so a half-remembered name behaves the way a half-remembered
// spelling does.
func searchNames(tab *unidata.Table, query string) int {
	ix := match.NewIndex(tab.Names())
	limit := uniOpts.limit
	if limit == 0 {
		limit = 20 // a screenful; -n 0 was asked for explicitly, see below
	}
	if uniOpts.limit < 0 {
		limit = 0
	}
	results := ix.Search(query, limit)
	for _, r := range results {
		c, ok := tab.ByName(r.Word)
		if !ok {
			continue
		}
		println(charLine(c))
	}
	return len(results)
}

// printChar writes the full account of one character: what it is called,
// what kind of thing it is, where it lives and what it is made of.
func printChar(tab *unidata.Table, r rune) {
	c, _ := tab.Lookup(r)
	printf("%s  %s\n\n", ansi(unidata.Glyph(c)), tab.Name(r))
	for _, l := range charFacts(c) {
		println(l)
	}
}

// charFacts is the body of a character's entry, the part that goes in the
// definition pane.
func charFacts(c unidata.Char) []string {
	facts := []string{
		fmt.Sprintf("%-7s %s", "code", unidata.Code(c.Code)),
	}
	if b := unidata.UTF8(c.Code); b != "" {
		facts = append(facts, fmt.Sprintf("%-7s %s", "utf-8", b))
	}
	if c.Category != "" {
		facts = append(facts, fmt.Sprintf("%-7s %s", "kind", unidata.CategoryName(c.Category)))
	}
	if c.Block != "" {
		facts = append(facts, fmt.Sprintf("%-7s %s", "block", c.Block))
	}
	return facts
}

// charLine is one character on one line, for a list or a set of matches.
//
// The glyph is padded to two columns rather than one because Unicode has
// characters two columns wide -- every CJK ideograph, most emoji -- and a
// column that assumed one would be ragged down its whole length exactly
// where the interesting characters are.
func charLine(c unidata.Char) string {
	if uniOpts.noNames {
		return ansi(unidata.Glyph(c))
	}
	return fmt.Sprintf("%s  %-8s %s", ansi(pad2(unidata.Glyph(c))), unidata.Code(c.Code), c.Name)
}

// pad2 widens a glyph to exactly two columns. It deliberately adds no escape
// codes of its own: the picker draws it inside a reverse-video row, where a
// reset would end the highlight early. Callers writing to a terminal wrap the
// result in ansi themselves.
func pad2(glyph string) string {
	if w := displaywidth.String(glyph); w < 2 {
		return glyph + strings.Repeat(" ", 2-w)
	}
	return glyph
}

// ansi wraps a glyph in reset codes, which is what keeps this safe to point
// at a file nobody has read.
//
// unidata.Glyph already refuses to emit a control character, so nothing here
// can start an escape sequence. The reset is the second line of defense, and
// it is cheap: if a character ever turns out to change the terminal's state
// in a way the category did not predict, the reset after it puts the
// terminal back. It is left out when output is not a terminal, because a
// pipe has no state to protect and escape codes in a pipe are just noise.
func ansi(glyph string) string {
	if uniOpts.noANSI || !host.Interactive {
		return glyph
	}
	return sgrReset + glyph + sgrReset
}

// nameStream names the characters in r.
//
// Each distinct character is named once by default. The question being asked
// is almost always "what is in this", and a file has thousands of characters
// and a few dozen distinct ones; printing every occurrence answers a
// different question, which --all asks.
func nameStream(tab *unidata.Table, r io.Reader) error {
	if r == nil {
		return nil
	}
	seen := make(map[rune]bool)
	printed := 0
	br := bufio.NewReader(r)
	for {
		c, _, err := br.ReadRune()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		// A newline is in every line of every file and is never the answer
		// to what is wrong with one.
		if c == '\n' || c == '\r' {
			continue
		}
		if !uniOpts.all {
			if seen[c] {
				continue
			}
			seen[c] = true
		}
		ch, _ := tab.Lookup(c)
		if ch.Name == "" {
			ch.Name = tab.Name(c)
		}
		println(charLine(ch))
		printed++
		if uniOpts.limit > 0 && printed >= uniOpts.limit {
			return nil
		}
	}
}

// listChars prints the database, or one block of it.
func listChars(tab *unidata.Table) error {
	lo, hi := rune(0), rune(0x10FFFF)
	if uniOpts.block != "" {
		b, ok := findBlock(tab, uniOpts.block)
		if !ok {
			return fmt.Errorf("no block named %q; dict :unicode --blocks lists them", uniOpts.block)
		}
		lo, hi = b.Lo, b.Hi
	}
	printed := 0
	for i := 0; i < tab.Len(); i++ {
		c := tab.At(i)
		if c.Code < lo {
			continue
		}
		if c.Code > hi {
			break
		}
		println(charLine(c))
		printed++
		if uniOpts.limit > 0 && printed >= uniOpts.limit {
			break
		}
	}
	// The ranges are not stored character by character -- 100,000 CJK
	// ideographs would be most of the file -- so a block that is one of them
	// is generated here rather than skipped, which is the difference between
	// listing Unicode and listing the part of it that is cheap to list.
	if uniOpts.block != "" && printed == 0 {
		for r := lo; r <= hi; r++ {
			c, ok := tab.Lookup(r)
			if !ok {
				continue
			}
			println(charLine(c))
			printed++
			if uniOpts.limit > 0 && printed >= uniOpts.limit {
				break
			}
		}
	}
	if printed == 0 {
		return fmt.Errorf("nothing is assigned in that block")
	}
	return nil
}

// findBlock matches a block by name, ignoring case, spaces and hyphens, so
// "basic latin", "Basic_Latin" and "BasicLatin" all find it.
func findBlock(tab *unidata.Table, name string) (unidata.Block, bool) {
	want := squash(name)
	for _, b := range tab.Blocks() {
		if squash(b.Name) == want {
			return b, true
		}
	}
	return unidata.Block{}, false
}

func squash(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == ' ' || r == '_' || r == '-':
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + 32)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func printBlocks(tab *unidata.Table) {
	for _, b := range tab.Blocks() {
		printf("%-9s %-9s %s\n", unidata.Code(b.Lo), unidata.Code(b.Hi), b.Name)
	}
}

// index is the list dict searches: the words, and then the Unicode character
// names after them.
//
// The names are a tail, which is what keeps this from changing any answer
// dict gives about a word. NewIndexWithTail ranks every name below every
// word however well it matches, so a transposed "receive" still finds it and not
// RECYCLING SYMBOL; the characters are simply what is there once the words
// run out. With no query that is literally what you see -- the list runs on
// past Zzz into U+0000.
//
// Without the table the words are still the program, so a failure to read it
// is not a failure to run.
func index(words []string) *match.Index {
	tab, err := data.Unicode()
	if err != nil {
		return match.NewIndex(words)
	}
	return match.NewIndexWithTail(words, tab.Names())
}

// tailName is what the status line calls the tail, and nothing when the
// table could not be read and there is no tail to name.
func tailName(ix *match.Index, words []string) string {
	if ix.Primary < len(ix.Words) && ix.Primary == len(words) {
		return unicodeName
	}
	return ""
}

// exactName resolves a list entry, which is a character name spelled exactly
// as Unicode spells it and nothing else.
//
// ByName is deliberately forgiving, because it is what reads what a person
// typed. An entry is not something a person typed; it is a row taken from the
// list. Requiring the exact name is what keeps the word "bell" from turning
// into a character it merely sounds like.
func exactName(tab *unidata.Table, name string) (unidata.Char, bool) {
	c, ok := tab.ByName(name)
	if !ok || c.Name != name {
		return unidata.Char{}, false
	}
	return c, true
}

// charDisplay draws an entry that names a character as that character, and
// leaves an ordinary word as it is. This is what makes the character the
// headword of its own row; its name becomes the definition.
func charDisplay() func(string) string {
	tab, err := data.Unicode()
	if err != nil {
		return nil
	}
	return func(name string) string {
		c, ok := exactName(tab, name)
		if !ok {
			return ""
		}
		return unidata.Glyph(c)
	}
}

// charCode is the code point a character row shows where a word row shows
// its place in the list, and the width of the widest one the table holds.
//
// A place in the list is worth knowing about a word, which can be read. Two
// characters drawn as themselves can be the same smudge at the size a
// terminal draws a glyph -- a dozen of the SIGNWRITING characters are, and
// so are most pairs of emoji at 16 pixels -- and then the code point is the
// only thing that tells them apart, and the thing to type or paste into a
// program that wants one.
func charCode() (func(string) string, int) {
	tab, err := data.Unicode()
	if err != nil {
		return nil, 0
	}
	// The table is in code point order, so the last entry is the longest
	// this can return. Measuring it beats assuming a width that a later
	// Unicode could outgrow.
	width := 0
	if n := tab.Len(); n > 0 {
		width = len(unidata.Code(tab.At(n - 1).Code))
	}
	return func(name string) string {
		c, ok := exactName(tab, name)
		if !ok {
			return ""
		}
		return unidata.Code(c.Code)
	}, width
}

// charValue is what choosing a character row yields: the character, not the
// drawing of it.
//
// Glyph is the safe form -- a picture for a control character, a dotted
// circle under a combining mark, a space where there is nothing to draw --
// which is what a terminal needs and is not what anyone wants pasted. What
// comes out of the picker is going somewhere else, so it is the real thing.
func charValue() func(string) string {
	tab, err := data.Unicode()
	if err != nil {
		return nil
	}
	return func(name string) string {
		c, ok := exactName(tab, name)
		if !ok {
			return ""
		}
		return string(c.Code)
	}
}

// unicodeDict loads the character table as a dictionary, when one is asked
// for. Nothing reads the table until a character is actually looked at.
func unicodeDict() (dictdb.Dictionary, error) {
	tab, err := data.Unicode()
	if err != nil {
		return nil, err
	}
	return unicodeSource{tab}, nil
}

// characterSet is the character table on its own, which is what the rows in
// the word list's tail are looked up in. See ui.definition.
func characterSet() *dictdb.Set {
	set := &dictdb.Set{}
	set.Add(unicodeName, unicodeDict)
	return set
}

// pickChar runs the interactive picker over the character names.
//
// It is the same picker the word list uses, given a different list and a
// different source of entries, which is the reason the character table was
// made to look like a dictionary at all: the alternative was a second
// full-screen interface that would have drifted from the first.
//
// What it prints on the way out is the character, not its name -- see
// pick.value. Finding out that the character wanted is called MULTIPLICATION
// SIGN is rarely the end of the errand; having × is.
func pickChar(tab *unidata.Table, query string) error {
	ix := match.NewIndex(tab.Names())
	code, codeW := charCode()

	picked, err := runInteractive(pick{
		ix: ix, query: query, source: "unicode " + tab.Version(),
		reverse: uniOpts.reverse, defs: characterSet(), showDefs: true,
		display: charDisplay(), value: charValue(),
		// Every row here is a character, so the ordinal never shows: this
		// list is indexed by code point and always was.
		code: code, codeW: codeW,
	})
	if err != nil {
		return err
	}
	if picked == "" {
		return exitNoSelection{}
	}
	println(picked)
	return nil
}

// unicodeName is what the character table calls itself where a dictionary
// name is expected.
const unicodeName = "unicode"

// unicodeSource presents the character table as a dictionary, so that the
// picker can show what a character is in the pane it shows what a word
// means in.
//
// The entry text is deliberately plain: dictdb.Clean runs over everything a
// Set returns, and it is built for GCIDE's 1913 typesetting conventions --
// braces, bracketed accent escapes, parenthesized pronunciations. Unicode
// names use capitals, digits, spaces and hyphens and none of those
// conventions, so Clean leaves them alone. TestCleanLeavesCharacterFacts
// holds that true rather than assuming it.
type unicodeSource struct{ tab *unidata.Table }

func (u unicodeSource) Name() string { return unicodeName }

func (u unicodeSource) Title() string {
	return "Unicode " + u.tab.Version() + " character database"
}

func (u unicodeSource) Lookup(word string) ([]string, error) {
	c, ok := u.exact(word)
	if !ok {
		return nil, nil
	}
	return []string{charEntry(c)}, nil
}

// exact resolves a headword, which is a character name spelled exactly as
// Unicode spells it and nothing else.
//
// This is the whole of what keeps the character table out of the way of the
// dictionaries now that it is in the same lookup chain. The word list has
// "bell" and "Bell" in it and Unicode has BELL, and only the third of those
// is a character; matching them loosely would append a character to the
// definition of an ordinary word every time the two happened to agree.
func (u unicodeSource) exact(word string) (unidata.Char, bool) {
	return exactName(u.tab, word)
}

// charEntry is a character's whole entry as the definition pane shows it: the
// character first, because the pane is where it is looked at, then the facts.
//
// The character goes on a labeled line rather than alone on the first one so
// that the entry can never begin with a bracket. Clean reads a leading
// parenthesis as a pronunciation and drops it, and LEFT PARENTHESIS is a
// character someone will look up.
// A character with nothing to draw gets no line at all. Glyph gives back a
// space for a format character, a surrogate or an unassigned code point, and
// "char" followed by a space says less than nothing -- as well as leaving
// trailing whitespace, which is the form the discovery took.
func charEntry(c unidata.Char) string {
	facts := charFacts(c)
	if g := unidata.Glyph(c); strings.TrimSpace(g) != "" {
		facts = append([]string{fmt.Sprintf("%-7s %s", "char", g)}, facts...)
	}
	return strings.Join(facts, "\n")
}

func (u unicodeSource) Has(word string) bool {
	_, ok := u.exact(word)
	return ok
}

// charArg reports whether a root-command argument is asking about a
// character rather than a word.
//
// Two forms count. A code point written out -- U+00A0, 0x00A0 -- says so
// outright and cannot be anything else. A single character that is not ASCII
// says so too: it is not in the word list, no amount of fuzzy matching will
// make it one, and the only useful answer dict has for it is what it is.
//
// A single ASCII character is left alone. "a" and "I" are words, and dict
// answering LATIN SMALL LETTER A to `dict a` would be answering a question
// nobody asked. They are still reachable as `dict :unicode a`.
func charArg(arg string) (rune, bool) {
	if r, ok := unidata.ParseCode(arg); ok {
		return r, true
	}
	if r, n := utf8.DecodeRuneInString(arg); n == len(arg) && r != utf8.RuneError && r > 0x7F {
		return r, true
	}
	return 0, false
}

// answerCharArg prints the entry for a character reached through the root
// command. -D asks for the body without the headword, as it does for a word.
func answerCharArg(r rune) error {
	tab, err := data.Unicode()
	if err != nil {
		return err
	}
	if opts.defOnly {
		c, _ := tab.Lookup(r)
		for _, l := range charFacts(c) {
			println(l)
		}
		return nil
	}
	printChar(tab, r)
	return nil
}
