// Package match ranks dictionary words against a possibly-misspelled query.
//
// The design point is that neither matching strategy alone is enough for
// spell checking. Fuzzy subsequence matching, which is what fzf does, is very
// good when you know how a word starts and not how it ends ("onomat" ->
// "onomatopoeia"), and structurally incapable of finding "receive" from
// "recieve", because the letters are out of order. Edit distance finds the
// transposition instantly but ranks poorly for the prefix case. So both run,
// and results are placed in tiers.
package match

import (
	"math/bits"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unicode"
)

// Tier orders the kinds of match against each other. A word found by more
// than one strategy is reported in its best tier.
//
// The split of edit matches either side of fuzzy is deliberate. A single edit
// is a strong signal: it is the whole transposition case, and nearly every
// common misspelling is exactly one edit from its target. Two or more edits on
// a short query is mostly noise -- "Como" is two edits from "accomo" -- and
// should not bury a genuine fuzzy hit like "accommodate". So distance 1 ranks
// above fuzzy and the rest ranks below it.
type Tier int

const (
	TierExact  Tier = iota // the query is already the word
	TierPrefix             // the word begins with the query
	TierNear               // exactly one edit away, transposition included
	TierFuzzy              // the query is a subsequence of the word
	TierEdit               // two or more edits away
	TierAll                // listed with no query at all, so unranked

	numTiers = iota
)

func (t Tier) String() string {
	switch t {
	case TierExact:
		return "exact"
	case TierPrefix:
		return "prefix"
	case TierNear:
		return "near"
	case TierFuzzy:
		return "fuzzy"
	case TierAll:
		return "all"
	default:
		return "edit"
	}
}

// Result is one ranked word.
type Result struct {
	Word       string
	Tier       Tier
	Distance   int   // edit distance, meaningful for TierEdit
	Score      int   // fuzzy score, meaningful for TierFuzzy
	Shared     int   // length of the common prefix with the query
	Transposed bool  // the single edit was an adjacent swap
	Positions  []int // rune indices matched, for highlighting
}

// Index holds a word list prepared for repeated queries.
type Index struct {
	Words []string
	runes [][]rune
	lower [][]rune
	masks []uint32 // set of distinct letters in each word, for a cheap reject
}

// NewIndex prepares words for searching. The folded forms are computed once
// here rather than per keystroke.
func NewIndex(words []string) *Index {
	ix := &Index{
		Words: words,
		runes: make([][]rune, len(words)),
		lower: make([][]rune, len(words)),
		masks: make([]uint32, len(words)),
	}
	for i, w := range words {
		r := []rune(w)
		ix.runes[i] = r
		lo := make([]rune, len(r))
		for j, c := range r {
			lo[j] = unicode.ToLower(c)
		}
		ix.lower[i] = lo
		ix.masks[i] = letterMask(lo)
	}
	return ix
}

// Search ranks the index against query and returns at most limit results.
// A limit of zero or less means no limit.
func (ix *Index) Search(query string, limit int) []Result {
	q := []rune(query)

	// An empty query lists the whole word list, in its own order. That is what
	// the shell function this replaces did -- `fzf -q ""` shows everything --
	// and it makes the tool usable as a plain browser of the dictionary.
	if len(q) == 0 {
		n := len(ix.Words)
		if limit > 0 && limit < n {
			n = limit
		}
		out := make([]Result, n)
		for i := 0; i < n; i++ {
			out[i] = Result{Word: ix.Words[i], Tier: TierAll}
		}
		return out
	}
	fold := !HasUpper(q)

	qlow := make([]rune, len(q))
	for i, c := range q {
		qlow[i] = unicode.ToLower(c)
	}
	qs := string(q)
	qlows := string(qlow)
	budget := MaxDistance(len(q))
	qmask := letterMask(qlow)

	// The edit pass is the expensive one, so split the word list across cores.
	workers := runtime.GOMAXPROCS(0)
	if workers > len(ix.Words) {
		workers = len(ix.Words)
	}
	if workers < 1 {
		workers = 1
	}
	// Results are bucketed by tier rather than gathered into one slice. Tiers
	// already dominate the ordering, so sorting each bucket and concatenating
	// them in tier order gives exactly the same answer as one global sort --
	// but lets the work stop as soon as the limit is filled. For a one-letter
	// query that means sorting a few thousand prefix matches instead of a
	// hundred thousand fuzzy ones.
	parts := make([][numTiers][]Result, workers)
	chunk := (len(ix.Words) + workers - 1) / workers

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		lo := w * chunk
		hi := lo + chunk
		if hi > len(ix.Words) {
			hi = len(ix.Words)
		}
		if lo >= hi {
			continue
		}
		wg.Add(1)
		go func(w, lo, hi int) {
			defer wg.Done()
			var buckets [numTiers][]Result
			for i := lo; i < hi; i++ {
				if r, ok := ix.classify(i, q, qlow, qs, qlows, fold, budget, qmask); ok {
					buckets[r.Tier] = append(buckets[r.Tier], r)
				}
			}
			parts[w] = buckets
		}(w, lo, hi)
	}
	wg.Wait()

	var all []Result
	for tier := Tier(0); tier < numTiers; tier++ {
		if limit > 0 && len(all) >= limit {
			break
		}
		var bucket []Result
		for w := range parts {
			bucket = append(bucket, parts[w][tier]...)
		}
		if len(bucket) == 0 {
			continue
		}
		sort.SliceStable(bucket, func(a, b int) bool { return less(bucket[a], bucket[b]) })
		take := len(bucket)
		if limit > 0 && len(all)+take > limit {
			take = limit - len(all)
		}
		all = append(all, bucket[:take]...)
	}

	ix.addPositions(all, q, fold)
	return all
}

// addPositions fills in highlight positions for the results that will actually
// be shown, which is why classify does not bother computing them.
func (ix *Index) addPositions(results []Result, q []rune, fold bool) {
	for i := range results {
		r := &results[i]
		switch r.Tier {
		case TierExact, TierPrefix:
			n := len([]rune(r.Word))
			if r.Tier == TierPrefix {
				n = len(q)
			}
			pos := make([]int, n)
			for j := range pos {
				pos[j] = j
			}
			r.Positions = pos
		case TierFuzzy:
			var pos []int
			Fuzzy([]rune(r.Word), q, fold, &pos)
			r.Positions = pos
		}
	}
}

// classify decides the best tier for word i, if it matches at all.
//
// Edit and fuzzy are both evaluated rather than short-circuiting on the first
// hit, because a word can qualify under both and the better tier should win:
// "accommodate" is two edits from "accomo" but a clean fuzzy match, and it is
// the fuzzy reading that makes it the right answer.
func (ix *Index) classify(i int, q, qlow []rune, qs, qlows string, fold bool, budget int, qmask uint32) (Result, bool) {
	word := ix.Words[i]
	wr, wl := ix.runes[i], ix.lower[i]

	cmp, cmpq := word, qs
	if fold {
		cmp, cmpq = string(wl), qlows
	}

	if cmp == cmpq {
		return Result{Word: word, Tier: TierExact}, true
	}
	if strings.HasPrefix(cmp, cmpq) {
		return Result{Word: word, Tier: TierPrefix}, true
	}

	// Every distinct letter the query has and the word lacks costs at least one
	// edit, so counting them is a valid lower bound on the distance -- and one
	// AND-NOT plus a popcount is far cheaper than the dynamic program. It also
	// settles fuzzy matching: a subsequence needs every query letter present,
	// so any missing letter at all rules that out too.
	missing := bits.OnesCount32(qmask &^ ix.masks[i])
	if missing > budget {
		return Result{}, false
	}

	// Edit distance, compared folded so that case never costs an edit.
	a, b := wr, q
	if fold {
		a, b = wl, qlow
	}
	dist := Distance(a, b, budget)
	if dist == 1 {
		return Result{Word: word, Tier: TierNear, Distance: 1, Shared: sharedPrefix(a, b), Transposed: isTransposition(a, b)}, true
	}

	// Positions are deliberately not collected here. Highlighting is only
	// needed for the few rows that get drawn, and allocating a slice for every
	// one of a hundred thousand matches was the single largest cost of a
	// one-letter query.
	if missing == 0 {
		if score, ok := Fuzzy(wr, q, fold, nil); ok {
			return Result{Word: word, Tier: TierFuzzy, Score: score}, true
		}
	}
	if dist <= budget {
		return Result{Word: word, Tier: TierEdit, Distance: dist, Shared: sharedPrefix(a, b)}, true
	}
	return Result{}, false
}

// less orders two results. Within a tier the comparisons differ, but every
// tier finally breaks ties by length then alphabetically, so that the ordering
// is total and stable rather than dependent on word list order.
func less(a, b Result) bool {
	if a.Tier != b.Tier {
		return a.Tier < b.Tier
	}
	if a.Tier == TierNear && a.Transposed != b.Transposed {
		return a.Transposed
	}
	switch a.Tier {
	case TierEdit:
		if a.Distance != b.Distance {
			return a.Distance < b.Distance
		}
	case TierFuzzy:
		if a.Score != b.Score {
			return a.Score > b.Score
		}
	}
	// For spelling, agreeing on more of the start of the word is a stronger
	// signal than being closer in length: "occurance" is two edits from both
	// "occupancy" and "occurrence", but shares five leading letters with the
	// latter and four with the former.
	if a.Shared != b.Shared {
		return a.Shared > b.Shared
	}
	if len(a.Word) != len(b.Word) {
		return len(a.Word) < len(b.Word)
	}
	return a.Word < b.Word
}

// sharedPrefix returns how many leading runes a and b have in common.
func sharedPrefix(a, b []rune) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

// isTransposition reports whether a and b differ by exactly one swap of
// adjacent runes. Transposition is the most common single typo, so among
// candidates that are all one edit away it is the likeliest intent.
func isTransposition(a, b []rune) bool {
	if len(a) != len(b) {
		return false
	}
	i := 0
	for i < len(a) && a[i] == b[i] {
		i++
	}
	if i >= len(a)-1 {
		return false
	}
	if a[i] != b[i+1] || a[i+1] != b[i] {
		return false
	}
	for j := i + 2; j < len(a); j++ {
		if a[j] != b[j] {
			return false
		}
	}
	return true
}

// letterMask records which distinct letters appear in s: one bit per a-z, and
// a shared bit for everything else, so accented and punctuated entries are
// merely conservative rather than wrong.
func letterMask(s []rune) uint32 {
	var m uint32
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			m |= 1 << uint(r-'a')
		} else {
			m |= 1 << 26
		}
	}
	return m
}
