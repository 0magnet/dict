// Command gen builds data/names.tsv.gz: one-line glosses for the words in the
// built-in list that no dictionary defines.
//
// The inputs are large -- the US census surname file is 9MB, the GeoNames city
// dump 40MB -- but only the rows matching an otherwise-undefined word are
// kept, so the output is tens of kilobytes. Because the word list is embedded
// and fixed, that gap is known ahead of time and this can run once, with the
// result committed.
//
// Run with: go run ./data/gen
//
// Sources:
//
//	US Census 2010 surnames  public domain (a work of the US government)
//	GeoNames cities1000      CC-BY 4.0
//	en.wiktionary.org        name origins, taken as structured template facts
package main

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/0magnet/dict/data"
	"github.com/0magnet/dict/dictdb"
)

const (
	surnameURL = "https://www2.census.gov/topics/genealogy/2010surnames/names.zip"
	placesURL  = "https://download.geonames.org/export/dump/cities1000.zip"
	countryURL = "https://download.geonames.org/export/dump/countryInfo.txt"
	admin1URL  = "https://download.geonames.org/export/dump/admin1CodesASCII.txt"
	outPath    = "data/names.tsv.gz"
	wiktPath   = "data/wikt.tsv.gz"
	wikiPath   = "data/wiki.tsv.gz"
)

// info gathers everything known about one word before any of it is worded.
// Collecting first and rendering once keeps a name that is both a surname and
// a town from being described twice over.
type info struct {
	surname      bool
	surnameCount int
	surnameRank  int
	given        bool
	gender       string
	origins      []string
	place        string
	numeral      string
}

func main() {
	words, err := data.Words()
	check(err)

	// Everything the dictionaries already answer is excluded, so the gloss
	// file only carries what would otherwise show nothing.
	set := data.OpenSetWithout(data.GlossName)

	// Wiktionary is case-sensitive: "superglue" and "Superglue" are different
	// pages, and only the first defines the glue. The word list holds both, so
	// every spelling of an undefined word is asked about, not just whichever
	// came first.
	gap := map[string]bool{}
	spellings := map[string][]string{}
	for _, w := range words {
		if strings.Contains(w, "'") {
			continue
		}
		lw := strings.ToLower(w)
		if _, _, ok := set.Covers(lw); ok {
			continue
		}
		gap[lw] = true
		if !slices.Contains(spellings[lw], w) {
			spellings[lw] = append(spellings[lw], w)
		}
		// The all-lower form is worth asking for even when the list only
		// carries the capitalised one, since that is where a common noun
		// sense lives.
		if w != lw && !slices.Contains(spellings[lw], lw) {
			spellings[lw] = append(spellings[lw], lw)
		}
	}
	var gapList []string
	for _, forms := range spellings {
		gapList = append(gapList, forms...)
	}
	sort.Strings(gapList)
	fmt.Fprintf(os.Stderr, "%d words undefined by the dictionaries\n", len(gap))

	all := map[string]*info{}
	at := func(k string) *info {
		if all[k] == nil {
			all[k] = &info{}
		}
		return all[k]
	}

	addSurnames(gap, at)
	addPlaces(gap, at)
	addRomanNumerals(gap, at)

	// Wiktionary is asked only about the words still unexplained or lacking an
	// origin, which is most of them but not the ones already fully described.
	var ask []string
	for _, w := range gapList {
		i := all[strings.ToLower(w)]
		if i == nil || len(i.origins) == 0 {
			ask = append(ask, w)
		}
	}
	fmt.Fprintf(os.Stderr, "asking wiktionary about %d words\n", len(ask))
	for w, f := range fetchNameFacts(ask) {
		i := at(w)
		if f.isSurname {
			i.surname = true
		}
		if f.isGiven {
			i.given = true
			if i.gender == "" {
				i.gender = f.givenGen
			}
		}
		i.origins = append(i.origins, f.origins...)
	}

	glosses := map[string][]string{}
	for w, i := range all {
		if lines := render(w, i); len(lines) > 0 {
			glosses[w] = lines
		}
	}

	keys := make([]string, 0, len(glosses))
	for k := range glosses {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	for _, k := range keys {
		fmt.Fprintf(zw, "%s\t%s\n", k, strings.Join(glosses[k], "\\n"))
	}
	check(zw.Close())
	check(os.WriteFile(outPath, buf.Bytes(), 0o644))
	fmt.Fprintf(os.Stderr, "wrote %s: %d words, %d bytes\n", outPath, len(keys), buf.Len())

	// Whatever the names file still cannot answer is ordinary vocabulary, so
	// ask Wiktionary for an actual definition. These two files are disjoint by
	// construction: only words with no gloss reach this pass.
	var rest []string
	for _, w := range gapList {
		if len(glosses[strings.ToLower(w)]) == 0 {
			rest = append(rest, w)
		}
	}
	fmt.Fprintf(os.Stderr, "asking wiktionary for definitions of %d words\n", len(rest))
	defs := fetchDefinitions(rest, 3)
	writeGloss(wiktPath, defs)

	// Whatever Wiktionary has no page for is largely brands, companies and
	// people, which is what an encyclopedia is for.
	var last []string
	for _, w := range rest {
		if len(defs[strings.ToLower(w)]) == 0 {
			last = append(last, w)
		}
	}
	fmt.Fprintf(os.Stderr, "asking wikipedia about %d words\n", len(last))
	summaries := map[string][]string{}
	for w, text := range fetchWikipedia(last) {
		summaries[w] = []string{text}
	}
	writeGloss(wikiPath, summaries)
}

// writeGloss writes the sorted, gzipped word-and-text file the runtime reads.
func writeGloss(path string, entries map[string][]string) {
	keys := make([]string, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	for _, k := range keys {
		fmt.Fprintf(zw, "%s\t%s\n", k, strings.Join(entries[k], "\\n"))
	}
	check(zw.Close())
	check(os.WriteFile(path, buf.Bytes(), 0o644))
	fmt.Fprintf(os.Stderr, "wrote %s: %d words, %d bytes\n", path, len(keys), buf.Len())
}

// render turns the gathered facts into the lines shown in the pane: what kind
// of name it is and where it came from, then where the place is.
func render(word string, i *info) []string {
	var out []string

	var kinds []string
	if i.given {
		switch i.gender {
		case "female", "male":
			kinds = append(kinds, i.gender+" given name")
		default:
			kinds = append(kinds, "given name")
		}
	}
	if i.surname {
		kinds = append(kinds, "surname")
	}
	if len(kinds) > 0 {
		s := title(word) + ", " + joinWords(kinds)
		if o := dedupe(i.origins); len(o) > 0 {
			if len(o) > 3 {
				o = o[:3]
			}
			s += ", from " + joinWords(o)
		}
		s += "."
		if i.surnameCount > 0 {
			s += fmt.Sprintf(" %s people in the 2010 US census", commas(i.surnameCount))
			if i.surnameRank > 0 {
				s += fmt.Sprintf(", %s most common", ordinal(i.surnameRank))
			}
			s += "."
		}
		out = append(out, s)
	}
	if i.place != "" {
		out = append(out, i.place)
	}
	if i.numeral != "" {
		out = append(out, i.numeral)
	}
	return out
}

func addSurnames(gap map[string]bool, at func(string) *info) {
	zr := fetchZip(surnameURL)
	for _, f := range zr.File {
		if !strings.HasSuffix(strings.ToLower(f.Name), ".csv") {
			continue
		}
		rc, err := f.Open()
		check(err)
		cr := csv.NewReader(rc)
		cr.FieldsPerRecord = -1
		rows, err := cr.ReadAll()
		rc.Close()
		check(err)
		n := 0
		for k, rec := range rows {
			if k == 0 || len(rec) < 3 {
				continue
			}
			name := strings.ToLower(rec[0])
			if !gap[name] {
				continue
			}
			count, err := strconv.Atoi(rec[2])
			if err != nil {
				continue
			}
			i := at(name)
			i.surname = true
			i.surnameCount = count
			i.surnameRank, _ = strconv.Atoi(rec[1])
			n++
		}
		fmt.Fprintf(os.Stderr, "surnames: %d\n", n)
	}
}

func addPlaces(gap map[string]bool, at func(string) *info) {
	countries := fetchCountries()
	regions := fetchAdmin1()
	zr := fetchZip(placesURL)

	// GeoNames columns: 1 name, 2 asciiname, 7 feature code, 8 country,
	// 10 first-order division, 14 population.
	const (
		colName    = 1
		colASCII   = 2
		colFeature = 7
		colCountry = 8
		colAdmin1  = 10
		colPop     = 14
	)
	bestPop := map[string]int{}
	n := 0
	for _, f := range zr.File {
		if !strings.HasSuffix(f.Name, ".txt") {
			continue
		}
		rc, err := f.Open()
		check(err)
		raw, err := io.ReadAll(rc)
		rc.Close()
		check(err)

		for _, line := range strings.Split(string(raw), "\n") {
			rec := strings.Split(line, "\t")
			if len(rec) <= colPop {
				continue
			}
			pop, _ := strconv.Atoi(rec[colPop])
			for _, nm := range []string{rec[colName], rec[colASCII]} {
				key := strings.ToLower(nm)
				if key == "" || !gap[key] {
					continue
				}
				// Where a name belongs to several places, describe the largest.
				if p, seen := bestPop[key]; seen && p >= pop {
					continue
				}
				bestPop[key] = pop

				country := countries[rec[colCountry]]
				if country == "" {
					country = rec[colCountry]
				}
				region := regions[rec[colCountry]+"."+rec[colAdmin1]]

				var desc string
				switch rec[colFeature] {
				case "PPLC":
					desc = fmt.Sprintf("%s, capital of %s", nm, country)
				case "PPLA":
					if region != "" {
						desc = fmt.Sprintf("%s, capital of %s, %s", nm, region, country)
					} else {
						desc = fmt.Sprintf("%s, city in %s", nm, country)
					}
				default:
					if region != "" {
						desc = fmt.Sprintf("%s, city in %s, %s", nm, region, country)
					} else {
						desc = fmt.Sprintf("%s, city in %s", nm, country)
					}
				}
				if pop > 0 {
					desc += fmt.Sprintf(". Population %s.", commas(pop))
				} else {
					desc += "."
				}
				if at(key).place == "" {
					n++
				}
				at(key).place = desc
			}
		}
	}
	fmt.Fprintf(os.Stderr, "places: %d\n", n)
}

func fetchCountries() map[string]string { return fetchTable(countryURL, 0, 4) }
func fetchAdmin1() map[string]string    { return fetchTable(admin1URL, 0, 1) }

// fetchTable reads a tab-separated GeoNames table into a key/value map.
func fetchTable(url string, keyCol, valCol int) map[string]string {
	fmt.Fprintf(os.Stderr, "fetching %s\n", url)
	resp, err := http.Get(url)
	check(err)
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	check(err)

	out := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) > valCol && f[keyCol] != "" {
			out[f[keyCol]] = f[valCol]
		}
	}
	return out
}

func fetchZip(url string) *zip.Reader {
	fmt.Fprintf(os.Stderr, "fetching %s\n", url)
	resp, err := http.Get(url)
	check(err)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		check(fmt.Errorf("%s: http %d", url, resp.StatusCode))
	}
	b, err := io.ReadAll(resp.Body)
	check(err)
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	check(err)
	return zr
}

func dedupe(s []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range s {
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

func title(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func commas(n int) string {
	s := strconv.Itoa(n)
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}

// ordinal renders 1 as "1st", 1143 as "1,143rd".
func ordinal(n int) string {
	suffix := "th"
	if n%100 < 11 || n%100 > 13 {
		switch n % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return commas(n) + suffix
}

var _ = dictdb.Lemmas

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "gen:", err)
		os.Exit(1)
	}
}
