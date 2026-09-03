package dictdb

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Gloss is a dictionary of one-line entries, used for the names and places
// that a conventional dictionary does not define.
//
// It exists because the word list carries thousands of surnames and towns --
// "Hendricks", "Jaipur" -- that GCIDE and WordNet have no entry for. Confirming
// that such a spelling is a real surname, and roughly how common, is the useful
// answer for those; a full dictionary entry neither exists nor is wanted.
//
// Only words that nothing else defines are included, so the file stays at tens
// of kilobytes rather than the tens of megabytes of the sources it is built
// from.
type Gloss struct {
	name    string
	title   string
	words   []string
	entries []string
}

// Name returns the short id of this source.
func (g *Gloss) Name() string { return g.name }

// Title returns its human-readable name.
func (g *Gloss) Title() string { return g.title }

// ReadGloss parses the tab-separated, gzipped gloss file: one word and its
// text per line, sorted by word.
func ReadGloss(name, title string, r io.Reader) (*Gloss, error) {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	defer zr.Close()

	g := &Gloss{name: name, title: title}
	sc := bufio.NewScanner(zr)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		tab := strings.IndexByte(line, '\t')
		if tab <= 0 {
			continue
		}
		g.words = append(g.words, strings.ToLower(line[:tab]))
		g.entries = append(g.entries, line[tab+1:])
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if !sort.StringsAreSorted(g.words) {
		sort.Sort(&glossSorter{g})
	}
	return g, nil
}

type glossSorter struct{ g *Gloss }

func (s *glossSorter) Len() int { return len(s.g.words) }
func (s *glossSorter) Less(i, j int) bool {
	return s.g.words[i] < s.g.words[j]
}
func (s *glossSorter) Swap(i, j int) {
	s.g.words[i], s.g.words[j] = s.g.words[j], s.g.words[i]
	s.g.entries[i], s.g.entries[j] = s.g.entries[j], s.g.entries[i]
}

func (g *Gloss) find(word string) int {
	w := strings.ToLower(word)
	i := sort.SearchStrings(g.words, w)
	if i < len(g.words) && g.words[i] == w {
		return i
	}
	return -1
}

// Lookup returns the gloss for a word, if there is one. A word can carry more
// than one -- a surname that is also a town -- and they arrive as one entry
// with embedded newlines.
func (g *Gloss) Lookup(word string) ([]string, error) {
	i := g.find(word)
	if i < 0 {
		return nil, nil
	}
	return []string{strings.ReplaceAll(g.entries[i], "\\n", "\n")}, nil
}

// Has reports whether the word has a gloss.
func (g *Gloss) Has(word string) bool { return g.find(word) >= 0 }

// Len reports how many words are glossed.
func (g *Gloss) Len() int { return len(g.words) }
