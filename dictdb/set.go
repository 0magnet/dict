package dictdb

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Result is one definition found for a word.
type Result struct {
	DB    string // which dictionary, e.g. "gcide"
	Title string // its human-readable name
	Word  string // the headword actually matched, which may be a lemma
	Text  string // the definition, cleaned for display
}

// Set is an ordered group of dictionaries, consulted in turn.
//
// Order is the whole point: GCIDE is asked first because its 1913 prose is
// what makes the tool pleasant, and WordNet follows because GCIDE predates
// most modern vocabulary. The specialist databases come last and cover what
// neither general dictionary has -- acronyms, computing terms.
//
// Dictionaries are opened lazily. Parsing a 4MB index costs real time, so a
// dictionary that is never consulted is never read.
type Set struct {
	mu      sync.Mutex
	sources []source
}

// Dictionary is one source of definitions. Both a dictd database and the
// generated gloss file satisfy it.
type Dictionary interface {
	Name() string
	Title() string
	Lookup(word string) ([]string, error)
	Has(word string) bool
}

type source struct {
	name string
	open func() (Dictionary, error)
	db   Dictionary
	err  error
	once sync.Once
}

// Add registers a dictionary under a name, to be opened on first use.
func (s *Set) Add(name string, open func() (Dictionary, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sources = append(s.sources, source{name: name, open: open})
}

// Names lists the registered dictionaries, in consultation order.
func (s *Set) Names() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.sources))
	for i := range s.sources {
		out[i] = s.sources[i].name
	}
	return out
}

func (s *Set) get(i int) (Dictionary, error) {
	src := &s.sources[i]
	src.once.Do(func() { src.db, src.err = src.open() })
	return src.db, src.err
}

// Define looks a word up across the set.
//
// Every dictionary is asked for the exact word first. Only if none has it are
// the base forms tried, so a word that is genuinely present is never shadowed
// by another dictionary's entry for its stem.
func (s *Set) Define(word string) []Result {
	if r := s.lookupExact(word); len(r) > 0 {
		return r
	}
	for _, lemma := range Lemmas(word) {
		if r := s.lookupExact(lemma); len(r) > 0 {
			return r
		}
	}
	return nil
}

// lookupExact returns the entries from the first dictionary that has the word,
// not from all of them. Beyond matching the intended reading of "GCIDE first,
// WordNet as fallback", this is what keeps lookups cheap: dictionaries open
// lazily, so a word GCIDE knows means the other four indexes are never parsed.
func (s *Set) lookupExact(word string) []Result {
	for i := range s.sources {
		db, err := s.get(i)
		if err != nil || db == nil {
			continue
		}
		defs, err := db.Lookup(word)
		if err != nil || len(defs) == 0 {
			continue
		}
		out := make([]Result, 0, len(defs))
		for _, d := range defs {
			out = append(out, Result{DB: db.Name(), Title: db.Title(), Word: word, Text: Clean(d)})
		}
		return out
	}
	return nil
}

// AddFS registers a dictionary held in an fs.FS, which is how the embedded
// copies are reached. The whole body is read into memory because an fs.File
// offers no random access; the index is streamed.
func (s *Set) AddFS(fsys fs.FS, name, indexPath, bodyPath string) {
	s.Add(name, func() (Dictionary, error) {
		idx, err := fsys.Open(indexPath)
		if err != nil {
			return nil, err
		}
		defer idx.Close()

		// Index files are plain text and compress about four to one, so the
		// embedded copies are gzipped; the bodies already carry their own
		// compression and are used as they are.
		var r io.Reader = idx
		if strings.HasSuffix(indexPath, ".gz") {
			zr, err := gzip.NewReader(idx)
			if err != nil {
				return nil, fmt.Errorf("%s index: %w", name, err)
			}
			defer zr.Close()
			r = zr
		}
		raw, err := fs.ReadFile(fsys, bodyPath)
		if err != nil {
			return nil, err
		}
		return Open(name, r, bytes.NewReader(raw))
	})
}

// AddDir registers a dictionary from a directory of dictd files, so an
// installed dict-gcide or dict-wn is used in preference to the embedded copy.
func (s *Set) AddDir(dir, name string) error {
	index := filepath.Join(dir, name+".index")
	body := filepath.Join(dir, name+".dict.dz")
	if _, err := os.Stat(index); err != nil {
		return err
	}
	if _, err := os.Stat(body); err != nil {
		// Some installations ship the body uncompressed.
		body = filepath.Join(dir, name+".dict")
		if _, err := os.Stat(body); err != nil {
			return err
		}
	}
	s.Add(name, func() (Dictionary, error) {
		idx, err := os.Open(index)
		if err != nil {
			return nil, err
		}
		defer idx.Close()
		f, err := os.Open(body)
		if err != nil {
			return nil, err
		}
		st, err := f.Stat()
		if err != nil {
			f.Close()
			return nil, err
		}
		if st.Size() == 0 {
			f.Close()
			return nil, fmt.Errorf("%s: empty", body)
		}
		return Open(name, idx, f)
	})
	return nil
}

var _ io.Reader = (io.Reader)(nil)

// Covers reports whether the word can be defined, and under which headword,
// without decompressing anything. It exists for measuring coverage over a
// whole word list, where inflating every entry would be wasteful.
func (s *Set) Covers(word string) (db, headword string, ok bool) {
	try := func(w string) (string, bool) {
		for i := range s.sources {
			d, err := s.get(i)
			if err != nil || d == nil {
				continue
			}
			if d.Has(w) {
				return d.Name(), true
			}
		}
		return "", false
	}
	if name, found := try(word); found {
		return name, word, true
	}
	for _, l := range Lemmas(word) {
		if name, found := try(l); found {
			return name, l, true
		}
	}
	return "", "", false
}
