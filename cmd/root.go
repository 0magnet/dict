// Package cmd holds the command line, built on cobra.
package cmd

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/0magnet/calvin"
	cc "github.com/0magnet/coloredcobra"
	"github.com/0magnet/dict/data"
	"github.com/0magnet/dict/dictdb"
	"github.com/0magnet/dict/match"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// Version is overridden at build time with -ldflags -X ...cmd.Version=...
var Version = "dev"

type options struct {
	wordFile  string
	lang      string
	filter    bool
	limit     int
	showTier  bool
	define    bool
	defOnly   bool
	noDefs    bool
	reverse   bool
	random    bool
	randomDef bool
}

var opts options

// exitNoSelection is returned when the user quits the interactive view without
// choosing, so main can exit 1 the way fzf does without printing an error.
type exitNoSelection struct{}

func (exitNoSelection) Error() string { return "no selection" }

// RootCmd is the root command
var RootCmd = &cobra.Command{
	Use:   "dict [query]",
	Short: "look up how a word is spelled, and what it means",
	Long: calvin.AsciiFont("dict") + "\n" +
		"look up how a word is spelled, and what it means.\n" +
		"the word list and all eight dictionaries are built into this binary;\n" +
		"nothing needs to be installed.",
	Example: "  dict recieve          search interactively, starting from a misspelling\n" +
		"  dict -d receive       print the definition and exit\n" +
		"  dict -R               a random word and what it means\n" +
		"  dict -f -n 5 acheive  print the five best matches",
	Args:                  cobra.ArbitraryArgs,
	SilenceErrors:         true,
	SilenceUsage:          true,
	DisableSuggestions:    true,
	DisableFlagsInUseLine: true,
	RunE:                  run,
	Version:               Version,
}

// Execute executes the root cli command
func Execute() error {
	cc.Init(&cc.Config{
		RootCmd:         RootCmd,
		Headings:        cc.HiBlue + cc.Bold,
		Commands:        cc.HiBlue + cc.Bold,
		CmdShortDescr:   cc.HiBlue,
		Example:         cc.HiBlue + cc.Italic,
		ExecName:        cc.HiBlue + cc.Bold,
		Flags:           cc.HiBlue + cc.Bold,
		FlagsDescr:      cc.HiBlue,
		NoExtraNewlines: true,
		NoBottomNewline: true,
	})
	err := RootCmd.Execute()
	if _, ok := err.(exitNoSelection); ok {
		os.Exit(1)
	}
	return err
}

func init() {
	f := RootCmd.Flags()
	f.StringVarP(&opts.wordFile, "words", "w", "", "word list to search (default: the built-in list)")
	f.StringVarP(&opts.lang, "lang", "l", "", "language word list under /usr/share/dict, e.g. french")
	f.BoolVarP(&opts.filter, "filter", "f", false, "print matches and exit, without the interactive view")
	f.IntVarP(&opts.limit, "num", "n", 0, "maximum matches to print (0 for all)")
	f.BoolVarP(&opts.showTier, "tier", "t", false, "label each printed match with why it matched")
	f.BoolVarP(&opts.define, "define", "d", false, "print the definition of the best match and exit")
	f.BoolVarP(&opts.defOnly, "definition-only", "D", false, "print only the definition text, with no headword or source")
	f.BoolVarP(&opts.random, "random", "r", false, "print a random word and exit")
	f.BoolVarP(&opts.randomDef, "random-define", "R", false, "print a random word with its definition and exit")
	f.BoolVar(&opts.noDefs, "no-defs", false, "hide the definition pane in the interactive view")
	f.BoolVar(&opts.reverse, "reverse", false, "prompt at the top and list running down, as with fzf --layout=reverse")

	RootCmd.AddCommand(languagesCmd, licensesCmd, sourcesCmd, rainCmd)
	RootCmd.SetVersionTemplate("dict {{.Version}}\n")
	RootCmd.CompletionOptions.DisableDefaultCmd = true

	var helpflag bool
	RootCmd.SetUsageTemplate(help)
	RootCmd.PersistentFlags().BoolVarP(&helpflag, "help", "h", false, "help for "+RootCmd.Use)
	RootCmd.SetHelpCommand(&cobra.Command{Hidden: true})
	RootCmd.PersistentFlags().MarkHidden("help") //nolint
}

func run(cmd *cobra.Command, args []string) error {
	words, source, err := loadWordList(opts.wordFile, opts.lang)
	if err != nil {
		return err
	}
	if len(words) == 0 {
		return fmt.Errorf("%s is empty", source)
	}

	query := strings.Join(args, " ")
	ix := match.NewIndex(words)
	defs := dictionaries()

	if opts.random || opts.randomDef {
		return printRandom(ix, defs, query)
	}

	// -d is the direct answer to "what does this mean", so it needs no
	// terminal and no interaction.
	if opts.define || opts.defOnly {
		return printDefinition(ix, defs, query)
	}

	// Filter mode when asked for, and also whenever output is redirected, so
	// that `dict recieve | head` behaves the way a pipeline should.
	interactive := !opts.filter && host.Interactive
	if !interactive {
		printMatches(ix, query)
		return nil
	}

	picked, err := runInteractive(ix, query, source, opts.reverse, defs, !opts.noDefs)
	if err != nil {
		return err
	}
	if picked == "" {
		return exitNoSelection{}
	}
	println(picked)
	return nil
}

// printDefinition writes the definition of the best match for the query. The
// word itself is resolved through the same matcher as everything else, so a
// misspelling still finds its definition.
func printDefinition(ix *match.Index, defs *dictdb.Set, query string) error {
	if query == "" {
		return fmt.Errorf("a word is required with --define")
	}
	results := ix.Search(query, 1)
	if len(results) == 0 {
		return fmt.Errorf("no word matches %q", query)
	}
	word := results[0].Word

	found := defs.Define(word)
	if len(found) == 0 {
		return fmt.Errorf("%s: no definition", word)
	}

	width := terminalWidth()
	if !opts.defOnly {
		label := found[0].DB
		if !strings.EqualFold(found[0].Word, word) {
			label += " · " + found[0].Word
		}
		printf("%s  [%s]\n\n", word, label)
	}
	for i, r := range found {
		if i > 0 {
			println()
		}
		for _, line := range dictdb.Wrap(r.Text, width) {
			println(line)
		}
	}
	return nil
}

func printMatches(ix *match.Index, query string) {
	for _, r := range ix.Search(query, opts.limit) {
		if opts.showTier {
			printf("%-24s %s", r.Word, r.Tier)
			if r.Tier == match.TierEdit {
				printf(" %d", r.Distance)
			}
			println()
		} else {
			println(r.Word)
		}
	}
}

// terminalWidth returns a sensible wrapping width, falling back to 80 when
// output is not a terminal.
func terminalWidth() int {
	if host.Width > 20 {
		w := host.Width
		if w > 100 {
			return 100
		}
		return w - 1
	}
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 20 {
		if w > 100 {
			return 100
		}
		return w - 1
	}
	return 78
}

var languagesCmd = &cobra.Command{
	Use:     ":languages",
	Aliases: []string{":langs"},
	Short:   "list other-language word lists installed on this system",
	Long: "List the word lists installed under " + dictDir + ", for use with -l.\n\n" +
		"These are optional and separate from the built-in data: dict carries its\n" +
		"own English word list and dictionaries, and needs nothing installed to\n" +
		"work. Searching another language is the one thing that does, since those\n" +
		"word lists come from the system.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		names, err := listLanguages()
		if err != nil || len(names) == 0 {
			// Saying nothing here reads as though the command failed, when in
			// fact nothing is wrong: this is an optional extra.
			fmt.Fprintf(errOut(),
				"no word lists in %s; install a words package to search other languages\n"+
					"(the built-in English list and dictionaries need nothing installed)\n", dictDir)
			return nil
		}
		for _, n := range names {
			println(n)
		}
		return nil
	},
}

var sourcesCmd = &cobra.Command{
	Use:   ":sources",
	Short: "show the word list and dictionaries in use",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Naming the word list matters: everything is built in, but an
		// installed copy is preferred when there is one, and this is how to
		// tell which is being read.
		_, source, err := loadWordList(opts.wordFile, opts.lang)
		if err != nil {
			return err
		}
		printf("words       %s\n", source)
		fetched := map[string]bool{}
		if host.Fetch != nil {
			for _, n := range data.Absent {
				fetched[n] = true
			}
		}
		for _, n := range dictionaries().Names() {
			where := "built in"
			if fetched[n] {
				// Not in this binary at all: read over HTTP from the
				// site that served the page, a chunk per lookup.
				idx, _ := data.Paths(n)
				where = "fetched from " + idx
			}
			for _, dir := range data.DictdDirs {
				if _, err := os.Stat(filepath.Join(dir, n+".index")); err == nil {
					where = filepath.Join(dir, n)
					break
				}
			}
			printf("%-11s %s\n", n, where)
		}
		return nil
	},
}

var licensesCmd = &cobra.Command{
	Use:   ":licenses",
	Short: "print the licenses of the built-in dictionaries",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		lics, err := data.Licenses()
		if err != nil {
			return err
		}
		names := make([]string, 0, len(lics))
		for n := range lics {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			printf("======== %s ========\n%s\n", n, lics[n])
		}
		return nil
	},
}

// printRandom prints one or more words chosen at random, with their
// definitions under -R.
//
// A query narrows the pool to what matches it, so "dict -r rec" picks among
// the words a search for "rec" would offer. Without one the pool is the whole
// list.
func printRandom(ix *match.Index, defs *dictdb.Set, query string) error {
	pool := randomPool(ix, query)
	if len(pool) == 0 {
		return fmt.Errorf("no word matches %q", query)
	}

	count := opts.limit
	if count < 1 {
		count = 1
	}
	if count > len(pool) {
		count = len(pool)
	}

	width := terminalWidth()
	seen := make(map[int]bool, count)
	for printed := 0; printed < count; {
		i := pickUnseen(len(pool), seen)
		if i < 0 {
			break
		}
		word := pool[i]

		if !opts.randomDef {
			println(word)
			printed++
			continue
		}

		// Under -R a word with no entry is a dud, so another is drawn. At
		// better than 99% coverage this almost never repeats, and the loop
		// gives up rather than spinning if a pool has nothing defined.
		found := defs.Define(word)
		if len(found) == 0 {
			if len(seen) >= len(pool) {
				return fmt.Errorf("nothing in that set has a definition")
			}
			continue
		}
		if printed > 0 {
			println()
		}
		label := found[0].DB
		if !strings.EqualFold(found[0].Word, word) {
			label += " · " + found[0].Word
		}
		printf("%s  [%s]\n\n", word, label)
		for _, line := range dictdb.Wrap(found[0].Text, width) {
			println(line)
		}
		printed++
	}
	return nil
}

// randomPool is the set to draw from: the matches for a query, or the whole
// word list.
//
// Possessive forms are left out either way. A third of the word list is
// entries like "aardvark's", which are inflections of a word already present
// and make a poor thing to be handed at random.
func randomPool(ix *match.Index, query string) []string {
	var candidates []string
	if query != "" {
		for _, r := range ix.Search(query, 0) {
			candidates = append(candidates, r.Word)
		}
	} else {
		candidates = ix.Words
	}

	out := make([]string, 0, len(candidates))
	for _, w := range candidates {
		if !strings.Contains(w, "'") {
			out = append(out, w)
		}
	}
	// A pool of nothing but possessives is better answered with them than
	// with nothing at all.
	if len(out) == 0 {
		return candidates
	}
	return out
}

// pickUnseen draws an index not drawn before, or -1 once they are exhausted.
func pickUnseen(n int, seen map[int]bool) int {
	if len(seen) >= n {
		return -1
	}
	for tries := 0; tries < 64; tries++ {
		i := rand.IntN(n)
		if !seen[i] {
			seen[i] = true
			return i
		}
	}
	// Dense pools defeat random probing, so fall back to a scan.
	for i := 0; i < n; i++ {
		if !seen[i] {
			seen[i] = true
			return i
		}
	}
	return -1
}

const help = "{{if .HasAvailableSubCommands}}{{end}} {{if gt (len .Aliases) 0}}\r\n\r\n" +
	"{{.NameAndAliases}}{{end}}{{if .HasAvailableSubCommands}}" +
	"Available Commands:{{range .Commands}}  {{if and (ne .Name \"completion\") .IsAvailableCommand}}\r\n  " +
	"{{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}\r\n\r\n" +
	"Flags:\r\n" +
	"{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}\r\n\r\n" +
	"Global Flags:\r\n" +
	"{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}\r\n\r\n"
