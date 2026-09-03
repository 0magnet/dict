// Package data holds the dictionaries and word list built into the binary.
//
// Embedding means dict works on a machine with nothing installed, which is
// most of the point: the word list alone tells you a spelling exists, and the
// dictionaries tell you what it means. Installed copies still win when they
// are present, so a newer GCIDE on the system is used in preference to this
// one.
package data

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"strings"

	"github.com/0magnet/dict/dictdb"
)

// Paths returns the embedded index and body paths for a dictionary.
func Paths(name string) (index, body string) {
	return "dictd/" + name + ".index.gz", "dictd/" + name + ".dict.dz"
}

// Words returns the embedded word list.
func Words() ([]string, error) {
	f, err := FS.Open("words.gz")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("embedded word list: %w", err)
	}
	defer zr.Close()

	var words []string
	sc := bufio.NewScanner(zr)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		if w := strings.TrimSpace(sc.Text()); w != "" && !strings.HasPrefix(w, "#") {
			words = append(words, w)
		}
	}
	return words, sc.Err()
}

// Licenses returns the copyright notice for each embedded dictionary, so the
// terms travel with the binary that contains the data.
func Licenses() (map[string]string, error) {
	entries, err := FS.ReadDir("licenses")
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(entries))
	for _, e := range entries {
		b, err := FS.ReadFile("licenses/" + e.Name())
		if err != nil {
			return nil, err
		}
		out[strings.TrimSuffix(e.Name(), ".copyright")] = string(b)
	}
	return out, nil
}

var _ io.Reader = (io.Reader)(nil)

// GlossName is the id of the generated names-and-places source. It is
// consulted after every real dictionary, so a genuine definition always wins
// over "a surname".
const GlossName = "names"

// GlossTitle is what the status line shows for it.
const GlossTitle = "Names and Places (US Census 2010, GeoNames)"

// GlossPath is the embedded gloss file, built by data/gen.
const GlossPath = "names.tsv.gz"

// The Wiktionary definitions, for the ordinary vocabulary none of the
// dictionaries carry. Consulted last, after the name glosses.
const (
	WiktName  = "wikt"
	WiktTitle = "English Wiktionary (CC BY-SA 4.0)"
	WiktPath  = "wikt.tsv.gz"
)

// Wikipedia summaries, for the brands, companies and people no dictionary
// carries. Consulted last of all.
const (
	WikiName  = "wiki"
	WikiTitle = "English Wikipedia (CC BY-SA 4.0)"
	WikiPath  = "wiki.tsv.gz"
)

// DictdDirs are searched for installed dictionaries, which take precedence
// over the embedded copies so a newer or fuller database wins.
var DictdDirs = []string{"/usr/share/dictd", "/usr/lib/dict", "/usr/local/share/dictd"}

// OpenSet builds the lookup chain: each dictionary in Order, preferring an
// installed copy in one of dirs over the embedded one, then the generated
// names-and-places glosses last so a real definition always wins.
//
// Both the command and the tests use this, so what is measured is what runs.
// Pass nil for dirs to use only the embedded data.
func OpenSet(dirs []string) *dictdb.Set { return openSet(dirs, "") }

// OpenSetWithout is OpenSet with one source left out. The generator uses it to
// ask what the dictionaries cannot answer without consulting the gloss file it
// is in the middle of rebuilding.
func OpenSetWithout(exclude string) *dictdb.Set { return openSet(nil, exclude) }

func openSet(dirs []string, exclude string) *dictdb.Set {
	s := &dictdb.Set{}
	for _, name := range Order {
		if name == exclude {
			continue
		}
		installed := false
		for _, dir := range dirs {
			if err := s.AddDir(dir, name); err == nil {
				installed = true
				break
			}
		}
		if !installed {
			idx, body := Paths(name)
			s.AddFS(FS, name, idx, body)
		}
	}
	if exclude == GlossName {
		return s
	}
	addGloss(s, GlossName, GlossTitle, GlossPath)
	addGloss(s, WiktName, WiktTitle, WiktPath)
	addGloss(s, WikiName, WikiTitle, WikiPath)
	return s
}

func addGloss(s *dictdb.Set, name, title, path string) {
	s.Add(name, func() (dictdb.Dictionary, error) {
		f, err := FS.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return dictdb.ReadGloss(name, title, f)
	})
}
