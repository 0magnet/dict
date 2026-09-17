package cmd

import (
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/0magnet/dict/dictdb"
)

// tagged builds the Tag a lookup would have produced, so the template
// machinery can be tested without opening a dictionary.
func tagged(word string, parts ...dictdb.Part) dictdb.Tag {
	return dictdb.Tag{
		Word:       word,
		Parts:      parts,
		Past:       dictdb.Past(word),
		Participle: dictdb.Past(word),
		Gerund:     dictdb.Gerund(word),
		Third:      dictdb.ThirdPerson(word),
		Plural:     dictdb.Plural(word),
	}
}

// named builds the Tag of a proper noun, which carries the capitalized form
// to print.
func named(display string, parts ...dictdb.Part) dictdb.Tag {
	t := tagged(strings.ToLower(display), append(parts, dictdb.Name)...)
	t.Display = display
	return t
}

// stocked is a vocabulary whose pools are already full. Each pool is deep
// enough for a frame and its clause together, since a word is spent once used
// and a noun phrase can want several.
func stocked() *vocabulary {
	v := newVocabulary(nil, nil, rand.New(rand.NewPCG(1, 2)))
	add := func(p dictdb.Part, words []string, mk func(string) dictdb.Tag) {
		for _, w := range words {
			v.pools[p] = append(v.pools[p], mk(w))
		}
	}
	nouns := []string{"anvil", "bassoonist", "ocelot", "sandbank", "abbacy", "newt", "beech", "ashtray",
		"phlegm", "snorkel", "tureen", "gong", "abdomen", "puffball", "exigency", "hostel"}
	add(dictdb.Noun, nouns, func(w string) dictdb.Tag { return tagged(w, dictdb.Noun) })
	vts := []string{"excise", "gyrate", "scoot", "unsnarl", "clot", "harlequin", "size", "baby",
		"pillory", "smarten", "girdle", "rowel", "tackle", "shepherd", "throne", "spat"}
	add(dictdb.Transitive, vts, func(w string) dictdb.Tag { return tagged(w, dictdb.Verb, dictdb.Transitive) })
	add(dictdb.Verb, vts, func(w string) dictdb.Tag { return tagged(w, dictdb.Verb, dictdb.Transitive) })
	vis := []string{"roister", "wheeze", "moot", "fade", "echo", "quarrel", "dampen", "snow",
		"repent", "emerge", "growl", "struggle", "wave", "bob", "coexist", "reside"}
	add(dictdb.Intransitive, vis, func(w string) dictdb.Tag { return tagged(w, dictdb.Verb, dictdb.Intransitive) })
	adjs := []string{"owlish", "tubby", "mousy", "hornlike", "wealthy", "dank", "gouty", "sunlit",
		"leery", "woozy", "bold", "hasty", "wispy", "smutty", "lidless", "roan"}
	add(dictdb.Adjective, adjs, func(w string) dictdb.Tag { return tagged(w, dictdb.Adjective) })
	advs := []string{"slyly", "demurely", "muddily", "prolixly", "finely", "wheezily", "evil", "yearly",
		"barely", "sorely", "gladly", "meetly", "dourly", "archly", "coldly", "rudely"}
	add(dictdb.Adverb, advs, func(w string) dictdb.Tag { return tagged(w, dictdb.Adverb) })

	people := []string{"Abbott", "Basil", "Derrick", "Patti", "Nehru", "Zamenhof", "Alyson", "Jarrett",
		"Brynner", "Pyotr", "Rutledge", "Donizetti"}
	add(dictdb.Person, people, func(w string) dictdb.Tag { return named(w, dictdb.Person) })
	places := []string{"Gaborone", "Springdale", "Tabriz", "Kowloon", "Mariupol", "Toledo", "Trondheim",
		"Aguascalientes", "Essequibo", "Arizona", "Parkersburg", "Vilnius"}
	add(dictdb.Place, places, func(w string) dictdb.Tag { return named(w, dictdb.Place) })
	// The generic name pool is every proper noun, which is how the real one
	// is filled too.
	for _, w := range append(append([]string{}, people...), places...) {
		v.pools[dictdb.Name] = append(v.pools[dictdb.Name], named(w))
	}
	for _, w := range []string{"Etruscan", "Miltown", "Macedonian", "Alba", "Cheney", "Columbia"} {
		v.pools[dictdb.Name] = append(v.pools[dictdb.Name], named(w))
	}
	return v
}

// Every blank in every frame and every clause must name something the filler
// knows, which is the one way a template can be wrong that the compiler
// cannot catch.
func TestFramesAndClausesAreFillable(t *testing.T) {
	for _, frame := range frames {
		// Several times each: a template with [optional] and (a|b|c) in it
		// is many templates, and one run exercises one of them.
		for try := 0; try < 12; try++ {
			checkFillable(t, frame)
		}
	}
	for _, clause := range clauses {
		for try := 0; try < 12; try++ {
			got := checkFillable(t, clause)
			// A clause is punctuated by its frame, not by itself.
			if strings.HasSuffix(got, ".") || strings.HasSuffix(got, "?") {
				t.Errorf("%q: clause carries its own end punctuation: %s", clause, got)
			}
		}
	}
}

// checkFillable expands a template once and reports what came out, having
// first checked that it came out at all.
func checkFillable(t *testing.T, template string) string {
	t.Helper()
	got, err := stocked().expand(template, 0)
	if err != nil {
		t.Errorf("%q: %v", template, err)
		return ""
	}
	if strings.ContainsAny(got, "{}[]|") {
		t.Errorf("%q: markup left unresolved: %s", template, got)
	}
	return got
}

// Every frame holds exactly one clause: no frame without one, none with two.
func TestEveryFrameHoldsOneClause(t *testing.T) {
	for _, frame := range frames {
		n := strings.Count(strings.ToLower(frame), "{clause}") + strings.Count(strings.ToLower(frame), "{topclause}")
		if n != 1 {
			t.Errorf("%q holds %d clauses, want 1", frame, n)
		}
	}
}

func TestSentenceIsPunctuatedAndCapitalized(t *testing.T) {
	v := stocked()
	// Deep pools, since each sentence spends its words for good.
	for i := 0; i < 3; i++ {
		s, err := v.sentence()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(s, ".") && !strings.HasSuffix(s, "?") && !strings.HasSuffix(s, "'") {
			t.Errorf("no end punctuation: %s", s)
		}
		if strings.IndexFunc(s, func(r rune) bool { return r >= 'A' && r <= 'Z' }) < 0 {
			t.Errorf("nothing capitalized: %s", s)
		}
		v = stocked()
	}
}

// A word is spent once used, so it cannot come back later in the run.
func TestWordsAreNotReused(t *testing.T) {
	v := stocked()
	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		s, err := v.expand("the {noun} and the {noun} and the {noun}", 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range strings.Fields(s) {
			if w == "the" || w == "and" {
				continue
			}
			if seen[w] {
				t.Errorf("%q used twice across the run: %s", w, s)
			}
			seen[w] = true
		}
	}
}

// The forms a blank asks for are the ones it gets.
func TestInflectedBlanks(t *testing.T) {
	v := newVocabulary(nil, nil, rand.New(rand.NewPCG(3, 4)))
	v.pools[dictdb.Noun] = []dictdb.Tag{tagged("abbacy", dictdb.Noun)}
	v.pools[dictdb.Transitive] = []dictdb.Tag{{Word: "rend", Past: "rent", Participle: "rent", Gerund: "rending", Third: "rends"}}

	got, err := v.expand("{noun:plural} {vt:past}", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got != "abbacies rent" {
		t.Errorf("got %q, want %q", got, "abbacies rent")
	}
	if _, err := v.expand("{noun:pluperfect}", 0); err == nil {
		t.Error("an unknown form was accepted")
	}
}

func TestPolish(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"a ocelot can scoot.", "An ocelot can scoot."},
		{"a newt told a anvil to buck.", "A newt told an anvil to buck."},
		{"never buck a treasury.", "Never buck a treasury."},
		// A quotation mark before the first letter, and a second sentence.
		{"'The newt scooted,' said the anvil.", "'The newt scooted,' said the anvil."},
		{"the newt left a note. it said: 'Scoot.'", "The newt left a note. It said: 'Scoot.'"},
		// A speech tag after a quoted question stays in lower case.
		{"'the newt scooted?' asked the anvil, slyly.", "'The newt scooted?' asked the anvil, slyly."},
		// An article at the start of a quotation.
		{"'a ocelot scooted,' said the newt.", "'An ocelot scooted,' said the newt."},
	} {
		if got := polish(tc.in); got != tc.want {
			t.Errorf("polish(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParsePart(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want dictdb.Part
		bad  bool
	}{
		{in: "noun", want: dictdb.Noun},
		{in: "V", want: dictdb.Verb},
		{in: "vt", want: dictdb.Transitive},
		{in: "intransitive", want: dictdb.Intransitive},
		{in: "adjective", want: dictdb.Adjective},
		{in: "adv", want: dictdb.Adverb},
		{in: "", want: ""},
		{in: "gerund", bad: true},
	} {
		got, err := parsePart(tc.in)
		if tc.bad {
			if err == nil {
				t.Errorf("parsePart(%q) accepted it", tc.in)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("parsePart(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
}

// The real draw, over the built-in word list and dictionaries: the words that
// come back have to be the part of speech the slot asked for.
func TestVocabularyDrawsTheRightPartOfSpeech(t *testing.T) {
	words, _, err := loadWordList("", "")
	if err != nil {
		t.Fatal(err)
	}
	defs := dictionaries()
	v := newVocabulary(words, defs, rand.New(rand.NewPCG(7, 7)))
	for _, p := range []dictdb.Part{dictdb.Noun, dictdb.Verb, dictdb.Transitive, dictdb.Intransitive, dictdb.Adjective, dictdb.Adverb} {
		for i := 0; i < 3; i++ {
			tag := v.take(p)
			if tag.Word == "" {
				t.Fatalf("no %s found in the word list", p)
			}
			fresh, ok := defs.Tagged(tag.Word)
			if !ok {
				t.Errorf("%s %q came back with no part of speech", p, tag.Word)
				continue
			}
			if !fresh.Has(p) {
				t.Errorf("%q was drawn as a %s but its entries say %v", tag.Word, p, fresh.Parts)
			}
		}
	}
}

// The principal parts GCIDE prints are what a sentence should be using, in
// preference to anything a rule would produce.
func TestFormsComeFromTheDictionary(t *testing.T) {
	defs := dictionaries()
	for _, tc := range []struct{ word, past, plural string }{
		{word: "rend", past: "rent"},
		{word: "abbacy", plural: "abbacies"},
	} {
		tag, ok := defs.Tagged(tc.word)
		if !ok {
			t.Errorf("%s: not tagged", tc.word)
			continue
		}
		if tc.past != "" && tag.Past != tc.past {
			t.Errorf("%s: past is %q, want %q", tc.word, tag.Past, tc.past)
		}
		if tc.plural != "" && tag.Plural != tc.plural {
			t.Errorf("%s: plural is %q, want %q", tc.word, tag.Plural, tc.plural)
		}
	}
}

// The same seed has to give the same sentence, which is what --seed promises.
func TestSeedRepeats(t *testing.T) {
	words, _, err := loadWordList("", "")
	if err != nil {
		t.Fatal(err)
	}
	run := func() string {
		v := newVocabulary(words, dictionaries(), rand.New(rand.NewPCG(42, 0)))
		s, err := v.sentence()
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	if a, b := run(), run(); a != b {
		t.Errorf("same seed gave two sentences:\n%s\n%s", a, b)
	}
}

// The generator must ask as well as state. An earlier version of this file
// had questions in it, lost them in a rewrite, and nothing noticed -- the
// output is random enough that an absence reads as a run of bad luck.
func TestSomeFramesAsk(t *testing.T) {
	asking := 0
	for _, frame := range frames {
		if strings.HasSuffix(frame, "?") || strings.Contains(frame, "?'") {
			asking++
		}
	}
	if asking == 0 {
		t.Fatal("no interrogative frames: the generator only ever states")
	}
	if share := float64(asking) / float64(len(frames)); share < 0.1 || share > 0.4 {
		t.Errorf("%d of %d frames ask a question (%.0f%%), want between 10%% and 40%%",
			asking, len(frames), 100*share)
	}
}

// Every alternative of every rule must be fillable, including the ones that
// only turn up when a die lands a certain way -- which, left to the generator,
// might be a thousand sentences from now.
func TestEveryRuleAlternativeIsFillable(t *testing.T) {
	for name, alts := range rules {
		for _, alt := range alts {
			for try := 0; try < 12; try++ {
				if got := checkFillable(t, alt); strings.ContainsAny(got, "{}") {
					t.Errorf("{%s} %q left a blank: %s", name, alt, got)
				}
			}
		}
	}
}

// Assembling a sentence from pieces leaves seams: an apposition ends in a
// comma, an empty adjunct leaves a gap. polish sweeps them up, and this is
// what says it still does.
func TestNoSeamsInOutput(t *testing.T) {
	for i := 0; i < 200; i++ {
		v := stocked()
		s, err := v.sentence()
		if err != nil {
			t.Fatal(err)
		}
		for _, seam := range []string{"{", "}", "  ", " ,", ",,", ",.", " .", ",?"} {
			if strings.Contains(s, seam) {
				t.Errorf("seam %q in: %s", seam, s)
			}
		}
	}
}

// A name is printed as a name: with its capital, and never lower-cased into
// an ordinary slot.
func TestNamesKeepTheirCapital(t *testing.T) {
	for i := 0; i < 10; i++ {
		v := stocked()
		got, err := v.expand("{name} {person} {place}", 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range strings.Fields(got) {
			if w[0] < 'A' || w[0] > 'Z' {
				t.Errorf("name printed without its capital: %q in %q", w, got)
			}
		}
	}
}

// An ordinary noun slot must not be handed a name, which is the failure that
// reads as a typo: "the strengthening vilnius".
func TestNamesStayOutOfCommonSlots(t *testing.T) {
	words, _, err := loadWordList("", "")
	if err != nil {
		t.Fatal(err)
	}
	v := newVocabulary(words, dictionaries(), rand.New(rand.NewPCG(9, 9)))
	for _, p := range []dictdb.Part{dictdb.Noun, dictdb.Verb, dictdb.Adjective, dictdb.Adverb} {
		for i := 0; i < 20; i++ {
			tag := v.take(p)
			if tag.Word == "" {
				t.Fatalf("no %s in the word list", p)
			}
			if tag.Display != "" {
				t.Errorf("%s slot was handed the name %q", p, tag.Display)
			}
		}
	}
}

// The draw has to find people and places in the real word list, not just in a
// stocked pool: they are the scarcest things it looks for.
func TestDrawFindsPeopleAndPlaces(t *testing.T) {
	words, _, err := loadWordList("", "")
	if err != nil {
		t.Fatal(err)
	}
	v := newVocabulary(words, dictionaries(), rand.New(rand.NewPCG(4, 4)))
	for _, p := range []dictdb.Part{dictdb.Person, dictdb.Place, dictdb.Name} {
		for i := 0; i < 3; i++ {
			tag := v.take(p)
			if tag.Word == "" {
				t.Fatalf("no %s found in the word list", p)
			}
			if tag.Display == "" {
				t.Errorf("%s %q came back with no printable form", p, tag.Word)
			}
			if !dictdb.IsProper(tag.Display) {
				t.Errorf("%s %q is not a name", p, tag.Display)
			}
		}
	}
}

// The markup: [optional] parts appear about half the time, (a|b|c) picks one,
// and both nest.
func TestOptionalAndChoiceMarkup(t *testing.T) {
	v := stocked()
	// A choice always yields one of its alternatives, never the bars.
	seen := map[string]bool{}
	for i := 0; i < 60; i++ {
		got, err := v.expand("(alpha|beta|gamma)", 0)
		if err != nil {
			t.Fatal(err)
		}
		if got != "alpha" && got != "beta" && got != "gamma" {
			t.Fatalf("choice gave %q", got)
		}
		seen[got] = true
	}
	if len(seen) != 3 {
		t.Errorf("60 draws found only %v", seen)
	}

	// An optional part is sometimes there and sometimes not.
	with, without := 0, 0
	for i := 0; i < 60; i++ {
		got, err := v.expand("a[ b]", 0)
		if err != nil {
			t.Fatal(err)
		}
		switch got {
		case "a b":
			with++
		case "a":
			without++
		default:
			t.Fatalf("optional gave %q", got)
		}
	}
	if with == 0 || without == 0 {
		t.Errorf("optional part was not optional: %d with, %d without", with, without)
	}

	// Nesting: a choice inside an optional, and an optional inside a choice.
	for i := 0; i < 40; i++ {
		got, err := v.expand("[(a|b)][(c|[d])]", 0)
		if err != nil {
			t.Fatal(err)
		}
		if strings.ContainsAny(got, "[]()|") {
			t.Errorf("nested markup survived: %q", got)
		}
	}
}

func TestMatchBracketAndSplitTop(t *testing.T) {
	if got := matchBracket("[a[b]c]d", 0, '[', ']'); got != 6 {
		t.Errorf("matchBracket = %d, want 6", got)
	}
	if got := matchBracket("[unclosed", 0, '[', ']'); got != -1 {
		t.Errorf("matchBracket = %d, want -1 for unbalanced", got)
	}
	// A bar inside a nested group does not split the outer one.
	got := splitTop("a|(b|c)|d[e|f]")
	want := []string{"a", "(b|c)", "d[e|f]"}
	if len(got) != len(want) {
		t.Fatalf("splitTop gave %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("splitTop[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// Structure has to vary as freely as vocabulary does. The word list is deep
// enough that a word never repeats; if the shapes repeat instead, that is
// what a reader notices. This is a floor, not a target: a fixed seed keeps it
// from flickering.
func TestStructureVaries(t *testing.T) {
	words, _, err := loadWordList("", "")
	if err != nil {
		t.Fatal(err)
	}
	v := newVocabulary(words, dictionaries(), rand.New(rand.NewPCG(31, 41)))

	const runs = 200
	openings := map[string]int{}
	lengths := map[int]bool{}
	for i := 0; i < runs; i++ {
		s, err := v.sentence()
		if err != nil {
			t.Fatal(err)
		}
		f := strings.Fields(s)
		lengths[len(f)] = true
		if len(f) >= 3 {
			openings[strings.Join(f[:3], " ")]++
		}
	}
	if len(openings) < runs/2 {
		t.Errorf("%d sentences had only %d distinct three-word openings", runs, len(openings))
	}
	for opening, n := range openings {
		if n > runs/20 {
			t.Errorf("%q opens %d of %d sentences", opening, n, runs)
		}
	}
	if len(lengths) < 15 {
		t.Errorf("only %d distinct sentence lengths in %d sentences", len(lengths), runs)
	}
}
