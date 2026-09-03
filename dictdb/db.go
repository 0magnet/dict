package dictdb

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"
)

// b64 is the alphabet dictd uses for the offsets and lengths in an .index
// file. It looks like base64 but encodes a plain big-endian number, not a byte
// stream, so it needs its own decoder.
const b64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

var b64val [256]int8

func init() {
	for i := range b64val {
		b64val[i] = -1
	}
	for i := 0; i < len(b64); i++ {
		b64val[b64[i]] = int8(i)
	}
}

func decodeB64(s string) (int64, error) {
	if s == "" {
		return 0, fmt.Errorf("empty number")
	}
	var n int64
	for i := 0; i < len(s); i++ {
		v := b64val[s[i]]
		if v < 0 {
			return 0, fmt.Errorf("invalid digit %q", s[i])
		}
		n = n<<6 | int64(v)
	}
	return n, nil
}

type entry struct {
	word   string // lower-cased headword, for matching
	offset int64
	length int64
}

// DB is one dictionary: an index of headwords over a dictzip body.
type DB struct {
	name    string // short id, e.g. "gcide"
	title   string // human-readable, from the 00databaseshort entry
	entries []entry
	z       *dictzip
}

// Name returns the short id of the dictionary.
func (d *DB) Name() string { return d.name }

// Title returns its human-readable name.
func (d *DB) Title() string { return d.title }

// ReaderAtSizer is what a dictionary body must provide: random access and a
// known size. Both an *os.File and a bytes.Reader over embedded data qualify.
type ReaderAtSizer interface {
	io.ReaderAt
}

// Open builds a DB from the contents of a .index file and a .dict.dz body.
func Open(name string, index io.Reader, body io.ReaderAt) (*DB, error) {
	z, err := newDictzip(body)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	db := &DB{name: name, z: z}

	sc := bufio.NewScanner(index)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		f := strings.Split(line, "\t")
		if len(f) < 3 {
			continue
		}
		off, err1 := decodeB64(f[1])
		length, err2 := decodeB64(f[2])
		if err1 != nil || err2 != nil {
			continue
		}
		// Entries beginning 00database are dictd's own metadata, not words.
		if strings.HasPrefix(f[0], "00database") {
			if f[0] == "00databaseshort" {
				if b, err := z.get(off, length); err == nil {
					db.title = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(b)), "00databaseshort"))
				}
			}
			continue
		}
		db.entries = append(db.entries, entry{strings.ToLower(f[0]), off, length})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%s index: %w", name, err)
	}
	if len(db.entries) == 0 {
		return nil, fmt.Errorf("%s: index holds no entries", name)
	}

	// dictd sorts its index with a collation this code does not reproduce, so
	// re-sort on the lower-cased headword to make binary search valid.
	sort.SliceStable(db.entries, func(i, j int) bool { return db.entries[i].word < db.entries[j].word })
	if db.title == "" {
		db.title = name
	}
	return db, nil
}

// Len reports how many headwords the dictionary holds.
func (d *DB) Len() int { return len(d.entries) }

// Lookup returns every definition filed under exactly this headword, matched
// without regard to case. A word can have several, so all are returned.
func (d *DB) Lookup(word string) ([]string, error) {
	w := strings.ToLower(word)
	i := sort.Search(len(d.entries), func(i int) bool { return d.entries[i].word >= w })

	var out []string
	for ; i < len(d.entries) && d.entries[i].word == w; i++ {
		b, err := d.z.get(d.entries[i].offset, d.entries[i].length)
		if err != nil {
			return nil, err
		}
		out = append(out, string(bytes.TrimRight(b, "\n")))
	}
	return out, nil
}

// Has reports whether the headword is present, without decompressing.
func (d *DB) Has(word string) bool {
	w := strings.ToLower(word)
	i := sort.Search(len(d.entries), func(i int) bool { return d.entries[i].word >= w })
	return i < len(d.entries) && d.entries[i].word == w
}
