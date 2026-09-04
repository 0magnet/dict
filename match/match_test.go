package match_test

import (
	"bufio"
	"os"
	"strings"
	"testing"

	"github.com/0magnet/dict/match"
)

func TestDistanceTransposition(t *testing.T) {
	// The case fuzzy matching cannot reach: one transposition, one edit.
	cases := []struct {
		a, b string
		want int
	}{
		{"recieve", "receive", 1},
		{"acheive", "achieve", 1},
		{"wierd", "weird", 1},
		{"", "", 0},
		{"a", "", 1},
		{"abc", "abc", 0},
		{"abc", "acb", 1},
		{"kitten", "sitting", 3},
	}
	for _, c := range cases {
		got := match.Distance([]rune(c.a), []rune(c.b), 8)
		if got != c.want {
			t.Errorf("Distance(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
		// Symmetry holds for optimal string alignment.
		if rev := match.Distance([]rune(c.b), []rune(c.a), 8); rev != got {
			t.Errorf("Distance(%q,%q) = %d but reversed = %d", c.a, c.b, got, rev)
		}
	}
}

func TestDistanceBudget(t *testing.T) {
	// Over budget must report max+1 rather than the true distance.
	if got := match.Distance([]rune("kitten"), []rune("sitting"), 1); got != 2 {
		t.Errorf("with max=1 got %d, want 2 (max+1)", got)
	}
	// A length gap alone exceeds the budget.
	if got := match.Distance([]rune("a"), []rune("abcdefgh"), 2); got != 3 {
		t.Errorf("length gap: got %d, want 3 (max+1)", got)
	}
}

func TestFuzzySubsequence(t *testing.T) {
	if _, ok := match.Fuzzy([]rune("accommodate"), []rune("accomo"), true, nil); !ok {
		t.Error("accomo should match accommodate as a subsequence")
	}
	// The premise of the whole package: this one cannot match.
	if _, ok := match.Fuzzy([]rune("receive"), []rune("recieve"), true, nil); ok {
		t.Error("recieve must NOT match receive fuzzily; if it does, the edit tier is unnecessary")
	}
}

func TestFuzzyPrefersEarlierAndTighter(t *testing.T) {
	// Same query, same letters present, but contiguous at the front versus
	// scattered with gaps. The tight match must score higher, which is what
	// puts real prefix matches above coincidental ones.
	tight, ok1 := match.Fuzzy([]rune("onomatopoeia"), []rune("onomat"), true, nil)
	loose, ok2 := match.Fuzzy([]rune("oznzozmzazt"), []rune("onomat"), true, nil)
	if !ok1 || !ok2 {
		t.Fatalf("both should match: %v %v", ok1, ok2)
	}
	if tight <= loose {
		t.Errorf("contiguous prefix match scored %d, scattered scored %d; want tight > loose", tight, loose)
	}
}

func TestSmartCase(t *testing.T) {
	ix := match.NewIndex([]string{"Polish", "polish"})
	// Lowercase query is case-insensitive, so both match.
	if got := ix.Search("polish", 0); len(got) != 2 {
		t.Errorf("lowercase query matched %d words, want 2", len(got))
	}
	// A capital makes it literal.
	res := ix.Search("Polish", 0)
	if len(res) == 0 || res[0].Word != "Polish" {
		t.Errorf("capitalised query should rank Polish first, got %v", res)
	}
}

func TestTierOrdering(t *testing.T) {
	ix := match.NewIndex([]string{"accommodate", "Como", "achoo"})
	res := ix.Search("accomo", 0)
	if len(res) == 0 || res[0].Word != "accommodate" {
		t.Fatalf("a clean fuzzy match must outrank distant edit matches, got %v", res)
	}
	if res[0].Tier != match.TierFuzzy {
		t.Errorf("accommodate should be a fuzzy match, got %s", res[0].Tier)
	}
}

func TestExactBeatsEverything(t *testing.T) {
	ix := match.NewIndex([]string{"cats", "cat", "cot", "catalog"})
	res := ix.Search("cat", 0)
	if res[0].Word != "cat" || res[0].Tier != match.TierExact {
		t.Errorf("exact match must come first, got %v", res[0])
	}
}

// loadSystemWords reads the real word list, skipping the test if absent.
func loadSystemWords(t testing.TB) []string {
	t.Helper()
	f, err := os.Open("/usr/share/dict/words")
	if err != nil {
		t.Skip("no /usr/share/dict/words on this machine")
	}
	defer f.Close()
	var words []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if w := strings.TrimSpace(sc.Text()); w != "" {
			words = append(words, w)
		}
	}
	return words
}

// TestCorpusAccuracy measures how often the intended word is ranked first and
// how often it appears at all, over the common-misspelling corpus. It is the
// evidence for the ranking rules; a change that lowers these numbers is a
// regression even if it looks better on one hand-picked example.
func TestCorpusAccuracy(t *testing.T) {
	ix := match.NewIndex(loadSystemWords(t))

	var top1, top3, top10, found int
	var misses []string
	for _, c := range commonMisspellings {
		res := ix.Search(c.wrong, 50)
		rank := -1
		for i, r := range res {
			if strings.EqualFold(r.Word, c.right) {
				rank = i
				break
			}
		}
		switch {
		case rank == 0:
			top1++
			top3++
			top10++
			found++
		case rank >= 0 && rank < 3:
			top3++
			top10++
			found++
		case rank >= 0 && rank < 10:
			top10++
			found++
		case rank >= 0:
			found++
		default:
			misses = append(misses, c.wrong+" -> "+c.right)
		}
		if rank != 0 && rank < 3 && rank > 0 {
			t.Logf("rank %d: %s -> %s (first was %s)", rank+1, c.wrong, c.right, res[0].Word)
		}
	}

	n := len(commonMisspellings)
	t.Logf("corpus %d: top1 %d (%.0f%%)  top3 %d (%.0f%%)  top10 %d (%.0f%%)  found %d (%.0f%%)",
		n, top1, pct(top1, n), top3, pct(top3, n), top10, pct(top10, n), found, pct(found, n))
	for _, m := range misses {
		t.Logf("MISS %s", m)
	}

	// Guard rails, set below the measured numbers so ordinary tuning does not
	// trip them but a real regression does.
	if pct(top1, n) < 75 {
		t.Errorf("top-1 accuracy %.0f%% fell below 75%%", pct(top1, n))
	}
	if pct(found, n) < 95 {
		t.Errorf("recall %.0f%% fell below 95%%", pct(found, n))
	}
}

func pct(a, b int) float64 { return float64(a) / float64(b) * 100 }

func BenchmarkSearch(b *testing.B) {
	ix := match.NewIndex(loadSystemWords(b))
	queries := []string{"a", "rec", "recieve", "definately", "antidisestab"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ix.Search(queries[i%len(queries)], 500)
	}
}

// TestEmptyQueryListsEverything pins the behavior of the shell function this
// replaces: `fzf -q ""` shows the whole list, so `dict` with no query must too.
func TestEmptyQueryListsEverything(t *testing.T) {
	words := []string{"alpha", "beta", "gamma"}
	ix := match.NewIndex(words)

	got := ix.Search("", 0)
	if len(got) != len(words) {
		t.Fatalf("empty query returned %d words, want all %d", len(got), len(words))
	}
	// Order must be the word list's own, not a ranking, since there is
	// nothing to rank against.
	for i, w := range words {
		if got[i].Word != w {
			t.Errorf("position %d: got %q, want %q", i, got[i].Word, w)
		}
		if got[i].Tier != match.TierAll {
			t.Errorf("%q: tier %s, want all", got[i].Word, got[i].Tier)
		}
	}

	// A limit still applies, so the caller can ask for a window.
	if got := ix.Search("", 2); len(got) != 2 {
		t.Errorf("limit 2 returned %d words, want 2", len(got))
	}
}

func BenchmarkEmptyQuery(b *testing.B) {
	ix := match.NewIndex(loadSystemWords(b))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ix.Search("", 0)
	}
}
