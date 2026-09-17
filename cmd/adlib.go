// Package cmd adlib.go
//
// Sentences with the vocabulary chosen at random.
//
// It is the party game: a sentence with its nouns and verbs left blank, and
// words dropped into the blanks by someone who has not read it. What makes
// that work here rather than reading as word salad is that the blanks are
// typed -- a verb slot gets a verb, and a transitive slot gets a verb that
// can take an object -- and dict already knows which words are which, because
// every entry it carries says so.
//
// The sentence is built in layers. A frame is who is telling you about it --
// someone said this, somebody else denies it, someone is asking whether it is
// true. A clause is the thing that happened, and it is a subject and a
// predicate, each drawn separately: a subject from a dozen shapes, a
// predicate from four sets that differ in tense, voice and polarity, so that
// the same event can have happened, be happening, have been done to something
// else, or be refusing to happen at all.
//
// Nothing about that is enumerated. A list of whole sentences, however long,
// wears out faster than it looks: the vocabulary is 124,000 words deep and
// never repeats a word, so the only thing a reader can recognize twice on a
// page is the shape, and shapes written out by hand run to a few hundred.
// Composed instead -- every optional piece doubling the count, every choice
// multiplying it -- they stop being countable. Over 400 sentences, 399 have a
// distinct skeleton once the content words are blanked out, and no
// three-word opening accounts for more than one line in twenty.
//
// Nothing here conjugates a verb by guesswork either. dictdb.Tag carries the
// forms the entry lists, so a clause can say {vt:past} and get "rent" for
// "rend", which is what lets the sentences be told in the past tense instead
// of hiding every verb behind "will".
//
// The names come from the same place as everything else. A sixth of the word
// list is capitalized, and dict knows which of those are people and which are
// places, so a sentence can have a cast: Basil, Kowloon, that ocelot of
// Cheney's.
package cmd

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/0magnet/dict/dictdb"
)

// clauses are the events. They carry no terminal punctuation and no capital,
// because a frame decides how the clause is presented -- quoted, reported, or
// simply stated.
var clauses = []string{
	"{simple}{adjunct}",
	"{simple}{adjunct}",
	"{simple}{adjunct}",
	"{simple} (and|but|while|until|because|although|and yet|so) {simple}",
}

// rules are the pieces a sentence is assembled from, one alternative chosen
// at random for each. A rule may name another, and the markup in an
// alternative -- [optional] and (a|b|c) -- is expanded the same way, so the
// combinations multiply rather than adding up.
//
// Every "np" alternative is singular so that "{np} {vt:s}" agrees without
// anything having to check; "nps" is the plural counterpart, for the
// predicates that want one. That is the only agreement rule in here, and it
// is enforced by keeping the two apart rather than by inspecting anything.
var rules = map[string][]string{
	// A simple clause is a subject and a predicate, and the predicate is
	// where tense, voice and polarity are chosen -- separately from the
	// clause, so that they multiply it instead of being written out with it.
	// The same subject can have happened, be happening, have been done to it
	// by something else, or be refusing to happen at all.
	"simple": {
		"{np} {vp}",
		"{np} {vp}",
		"{np} {vip}",
		"{np} {vip}",
		"{np} {vcp}",
		"{nps} {vplp}",
		"nothing {adj} can {vi} without {np}",
		"you cannot {vt} {np} with {np}",
		"one {noun} {vi:s} while another {vi:s}",
		"no {noun} should {vt} {np}[ {adv}]",
		"there was (always|never|only) a {adj} {noun} between {np} and {np}",
		"it took {np} (a whole|the better part of a|half a) {noun} to {vt} {np}",
		"every {noun} in {place} (knows|denies|forgets) that {np} {vp}",
	},

	// Transitive predicates, for a singular subject.
	"vp": {
		"{vt:past} {np}",
		"{vt:s} {np}",
		"will {vt} {np}",
		"had {vt:pp} {np}",
		"was {vt:pp} by {np}",
		"is {vt:ing} {np}",
		"(never|rarely|always) {vt:s} {np}",
		"(could not|would not|dared not) {vt} {np}",
		"has been {vt:ing} {np} since {when}",
		"used to {vt} {np}[, {adv}]",
		"would rather {vt} {np} than {vi}",
		"must {vt} {np} before {np} can {vi}",
		"{vt:past} {np} (twice|out of {noun}|for the sake of {np})",
	},

	// Intransitive predicates: no object, so the adverb does the work.
	"vip": {
		"{vi:past}[ {adv}]",
		"{vi:s}[ {adv}]",
		"will {vi}[ {adv}]",
		"began {vi:ing}",
		"kept {vi:ing} (all {noun}|until {np} {vt:past} {np})",
		"had {vi:pp} (already|twice|by then)",
		"(never|seldom|always) {vi:s}",
		"(could not|would not) {vi} at all",
		"went on {vi:ing} like a {adj} {noun}",
		"{vi:past} the way {np} {vi:s}",
	},

	// Copular predicates, where the adjective is the news.
	"vcp": {
		"was {adj}",
		"is (never|hardly|quite) {adj}",
		"seemed {adj} to {np}",
		"became {adj} after {np}",
		"was too {adj} to {vi}",
		"was {adj} enough to {vt} {np}",
		"is the only {noun} that can {vt} {np}",
		"looks {adj} for a {noun}",
		"remains {adj}, whatever {np} {vt:s}",
	},

	// Predicates for a plural subject, which is a different set only because
	// English marks agreement here and nowhere else worth the trouble.
	"vplp": {
		"were too {adj} to {vi}",
		"{vt:past} {np}",
		"{vi:past}[ {adv}]",
		"had {vt:pp} {np}",
		"(never|always) {vt} {np}",
		"are {adj}",
		"were {vt:pp} by {np}",
		"keep {vi:ing}",
	},

	"opener": {
		"even so",
		"for once",
		"in the end",
		"as usual",
		"by morning",
		"against all advice",
		"to be fair",
		"after everything",
		"{where}",
		"{when}",
		"for reasons no {noun} could {vt}",
	},

	"np": {
		"the {noun}",
		"the {adj} {noun}",
		"a {adj} {noun}",
		"another {noun}",
		"every {adj} {noun}",
		"{name}",
		"{name}",
		"{name}'s {noun}",
		"{person}, the {adj} {noun},",
		"the {noun} of {place}",
		"the {adj} {noun} from {place}",
		"that {noun} of {name}'s",
		"the {noun} that {vt:past} {name}",
		"whichever {noun} {vi:past} first",
	},
	"nps": {
		"the {noun:plural}",
		"the {adj} {noun:plural}",
		"all the {noun:plural} in {place}",
		"{name}'s {noun:plural}",
		"both {noun:plural}",
	},
	"where": {
		"in {place}",
		"near {place}",
		"outside {place}",
		"somewhere west of {place}",
		"on the road to {place}",
		"in the {noun} at {place}",
	},
	"when": {
		"one {adj} morning",
		"the year {name} came to {place}",
		"after the {noun} of {place}",
		"before anyone could {vi}",
		"the winter the {noun:plural} arrived",
		"since {person} was a {noun}",
	},
	// Most clauses get no adjunct at all: the empty alternatives are what
	// keep the sentences from all running to the same length. The comma
	// lives inside each alternative so that an empty one leaves no gap.
	"adjunct": {"", "", "", "", ", {where}", ", {when}", ", {adv}", ", as any {noun} would"},
}

func init() {
	// A clause is a rule like any other; it is kept in its own variable
	// because it is the one a frame always asks for.
	rules["clause"] = clauses

	// A clause standing on its own can open with an adverbial; one embedded
	// after "that" or "if" cannot, or a frame ends up saying "it is said
	// that in the end, the oink scooted". Only the frames that present a
	// clause as a whole sentence ask for this one.
	rules["topclause"] = []string{"[{opener}, ]{clause}"}
}

// frames are the telling. Each holds exactly one {clause}; a capitalized
// {Clause} is one that opens a sentence or a quotation and so needs a capital
// the clause itself does not carry.
//
// The plain frame appears three times because a sentence that is merely
// stated should still be the commonest kind. Attribution is funny until it is
// every line.
var frames = []string{
	"{Topclause}.",
	"{Topclause}.",
	"{Topclause}.",
	"{Topclause}{adjunct}.",
	"'{Clause},' (said|muttered|insisted|swore|{vt:past}) {np}[, {adv}].",
	"(according to|by the account of|in the opinion of|if you believe) {np}, {clause}.",
	"it is (written|said|rumored) that {clause}[, though no {noun} has ever {vt:pp} one].",
	"{np} (maintains|insists|denies|has proved|keeps saying) that {clause}.",
	"(nobody believes|no one will admit|few would accept) that {clause}, least of all {np}.",
	"they say that {clause}, but {np} knows better.",
	"before {np} could {vi}, {clause}.",
	"{Topclause}, and that, (said|decided) {np}, was that.",
	"ask any {noun} in {place}: {clause}.",
	"{name} left a note. it said: '{Clause}.'",
	"in the (year|summer|month) of the {adj} {noun}, {clause}.",
	"{Where}, {clause}.",
	"{person} never forgave {person} for the day {clause}.",
	"there is a (song|proverb|law) in {place}: {clause}.",

	// Questions. Every one of these puts the clause in a subordinate
	// position -- after "that", "if", "now that" -- where English keeps
	// declarative word order. A question that inverted the clause itself
	// would need to know that "the oink scooted" becomes "did the oink
	// scoot", and nothing here knows how to unmake a past tense.
	"is it true that {clause}?",
	"(who can deny|who would have guessed|does it matter) that {clause}?",
	"why should it (surprise|trouble) anyone that {clause}?",
	"what is {np} to do, now that {clause}?",
	"and if {clause}, what then?",
	"'{Clause}?' asked {np}[, {adv}].",
	"has {np} heard that {clause}?",
	"how many {noun:plural} must {vt} {np} before {clause}?",
}

// slots maps a blank's name onto the part of speech that fills it.
var slots = map[string]dictdb.Part{
	"noun":   dictdb.Noun,
	"verb":   dictdb.Verb,
	"vt":     dictdb.Transitive,
	"vi":     dictdb.Intransitive,
	"adj":    dictdb.Adjective,
	"adv":    dictdb.Adverb,
	"name":   dictdb.Name,
	"person": dictdb.Person,
	"place":  dictdb.Place,
}

// inflect returns the form of a word a blank asked for, as {noun:plural} or
// {vt:past}. An unnamed form is the word as the dictionary files it.
func inflect(t dictdb.Tag, form string) (string, error) {
	switch form {
	case "":
		// A name is printed as the dictionary spells it, capital and all.
		if t.Display != "" {
			return t.Display, nil
		}
		return t.Word, nil
	case "past":
		return t.Past, nil
	case "pp":
		return t.Participle, nil
	case "ing":
		return t.Gerund, nil
	case "s":
		return t.Third, nil
	case "plural":
		return t.Plural, nil
	}
	return "", fmt.Errorf("no such form as %q", form)
}

// vocabulary is a supply of words by part of speech, drawn from the word list
// and tagged from the dictionary.
//
// Drawing is rejection sampling -- pick a word, look it up, keep it if it is
// the part of speech wanted -- which sounds wasteful and is not: a lookup
// costs well under a millisecond, and no draw is thrown away, because a word
// that turns out to be a noun when a verb was wanted is put aside in the noun
// pool for later. Adverbs are the scarce ones, at roughly one word in forty,
// so a sentence with an adverb in it is what sets the pace.
type vocabulary struct {
	words []string
	defs  *dictdb.Set
	rng   *rand.Rand
	pools map[dictdb.Part][]dictdb.Tag
	seen  map[string]bool
	spent map[string]bool

	// lastFrame is the frame used for the previous sentence, or -1.
	lastFrame int
}

func newVocabulary(words []string, defs *dictdb.Set, rng *rand.Rand) *vocabulary {
	return &vocabulary{
		words: words,
		defs:  defs,
		rng:   rng,
		pools: map[dictdb.Part][]dictdb.Tag{},
		seen:  map[string]bool{},
		spent: map[string]bool{},

		lastFrame: -1,
	}
}

// drawLimit caps how long take() will look for one word. The built-in
// dictionaries answer long before this; a word list passed with -w that has
// no adverbs in it at all would otherwise spin forever.
const drawLimit = 20000

// take returns a word of the given part of speech, or the zero Tag if the
// word list cannot supply one.
//
// A word is spent once used, across the whole run and not just the sentence.
// It has to be tracked separately from the pools because a word that is both
// a noun and a verb sits in two of them, and "the torment torments the
// torment" is a poorer joke than three different words.
func (v *vocabulary) take(p dictdb.Part) dictdb.Tag {
	for tries := 0; tries < drawLimit; tries++ {
		if pool := v.pools[p]; len(pool) > 0 {
			i := v.rng.IntN(len(pool))
			t := pool[i]
			v.pools[p] = append(pool[:i], pool[i+1:]...)
			if v.spent[t.Word] {
				continue
			}
			v.spent[t.Word] = true
			return t
		}
		if len(v.words) == 0 {
			// Nothing to draw from: the pools are all there will ever be.
			break
		}
		v.draw()
	}
	return dictdb.Tag{}
}

// draw samples one word, tags it, and files it under every part of speech its
// entries claim -- including, for a verb, whether it takes an object.
func (v *vocabulary) draw() {
	w := v.words[v.rng.IntN(len(v.words))]
	// Possessives and multiword entries are not words to drop in a blank.
	if strings.ContainsAny(w, "' -") {
		return
	}
	// Neither are acronyms. The word list holds a thousand of them, and
	// "IED" lower-cased into a noun slot reads as a misspelling rather than
	// as a word: a capital that is not a name is not usable here.
	if w != strings.ToLower(w) && !dictdb.IsProper(w) {
		return
	}
	// The word filed is the entry's own headword, not the word drawn: see
	// dictdb.Tagged.
	tag, ok := v.defs.Tagged(w)
	// Two-letter headwords are abbreviations wearing a part of speech --
	// WordNet files "ie" as an adverb and "pm" as a noun -- and they land in
	// a sentence as a typo rather than as a joke.
	if !ok || len(tag.Word) < 3 || v.seen[tag.Word] {
		return
	}
	v.seen[tag.Word] = true
	for _, p := range tag.Parts {
		// A name goes to the name pools and nowhere else. WordNet files
		// "Toledo" and "Nauru" as nouns, and they are -- but lower-cased into
		// an ordinary noun slot they read as a typo, and the sentence loses
		// the one word in it that sounded like a character.
		if tag.Display != "" && p != dictdb.Name && p != dictdb.Person && p != dictdb.Place {
			continue
		}
		v.pools[p] = append(v.pools[p], tag)
	}
}

// sentence builds one: a frame, and a clause inside it.
//
// One redraw keeps a frame from following itself. Independent draws cluster,
// and three "they say that" in ten lines reads as a broken generator rather
// than as chance.
func (v *vocabulary) sentence() (string, error) {
	i := v.rng.IntN(len(frames))
	if i == v.lastFrame && len(frames) > 1 {
		i = v.rng.IntN(len(frames))
	}
	v.lastFrame = i

	s, err := v.expand(frames[i], 0)
	if err != nil {
		return "", err
	}
	return polish(s), nil
}

// expand walks a template and resolves its markup, of which there are three
// kinds:
//
//	{blank}    a word of that part of speech, or another rule
//	[part]     included half the time, dropped the other half
//	(a|b|c)    one of these
//
// The last two are what keep a template from being a shape. A line written
// "[{opener}, ]{np} (never|rarely|always) {vp}" is eight sentences before a
// single word is drawn, and they nest, so the shapes multiply rather than
// adding up. That matters more than it sounds: the vocabulary is 124,000
// words deep and never repeats, so anything the ear can recognize twice in a
// page is the structure, and enumerating structures by hand never keeps up.
func (v *vocabulary) expand(template string, depth int) (string, error) {
	// A frame holds a clause, which holds a predicate, which holds a noun
	// phrase, which holds a place: five or six deep in the ordinary way of
	// things. The cap turns a rule that referred to itself into an error
	// rather than a hang.
	if depth > 12 {
		return "", fmt.Errorf("rules nest too deeply")
	}
	var b strings.Builder
	for i := 0; i < len(template); {
		switch template[i] {
		case '{':
			end := strings.IndexByte(template[i:], '}')
			if end < 0 {
				b.WriteByte(template[i])
				i++
				continue
			}
			end += i
			filled, err := v.blank(template[i+1:end], depth)
			if err != nil {
				return "", fmt.Errorf("template %q: %w", template, err)
			}
			b.WriteString(filled)
			i = end + 1

		case '[':
			end := matchBracket(template, i, '[', ']')
			if end < 0 {
				b.WriteByte(template[i])
				i++
				continue
			}
			if v.rng.IntN(2) == 0 {
				s, err := v.expand(template[i+1:end], depth+1)
				if err != nil {
					return "", err
				}
				b.WriteString(s)
			}
			i = end + 1

		case '(':
			end := matchBracket(template, i, '(', ')')
			if end < 0 {
				b.WriteByte(template[i])
				i++
				continue
			}
			alts := splitTop(template[i+1 : end])
			s, err := v.expand(alts[v.rng.IntN(len(alts))], depth+1)
			if err != nil {
				return "", err
			}
			b.WriteString(s)
			i = end + 1

		default:
			b.WriteByte(template[i])
			i++
		}
	}
	return b.String(), nil
}

// matchBracket returns the index of the bracket closing the one at i, or -1
// when the template is unbalanced. Only brackets of the same kind are
// counted: the others cannot straddle this one in a well-formed template.
func matchBracket(s string, i int, open, close byte) int {
	depth := 0
	for ; i < len(s); i++ {
		switch s[i] {
		case open:
			depth++
		case close:
			if depth--; depth == 0 {
				return i
			}
		}
	}
	return -1
}

// splitTop cuts a choice into its alternatives at the bars that are not
// inside a nested group, so that "(a|(b|c) d)" is two alternatives and not
// three.
func splitTop(s string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case '|':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

// blank fills one, from its name: a part of speech with an optional form
// after a colon, or "clause". A name written with a capital is filled and
// then capitalized, which is how a quotation gets its opening letter.
func (v *vocabulary) blank(name string, depth int) (string, error) {
	capital := name != "" && name[0] >= 'A' && name[0] <= 'Z'
	if capital {
		name = strings.ToLower(name[:1]) + name[1:]
	}

	form := ""
	if i := strings.IndexByte(name, ':'); i >= 0 {
		name, form = name[:i], name[i+1:]
	}

	var out string
	if alts, ok := rules[name]; ok {
		s, err := v.expand(alts[v.rng.IntN(len(alts))], depth+1)
		if err != nil {
			return "", err
		}
		out = s
	} else {
		part, ok := slots[name]
		if !ok {
			return "", fmt.Errorf("no such blank as {%s}", name)
		}
		tag := v.take(part)
		if tag.Word == "" {
			return "", fmt.Errorf("the word list has no %s in it", part)
		}
		word, err := inflect(tag, form)
		if err != nil {
			return "", err
		}
		out = word
	}

	if capital && out != "" {
		out = strings.ToUpper(out[:1]) + out[1:]
	}
	return out, nil
}

// polish fixes up what filling a blank cannot know in advance: the article in
// front of a word chosen afterwards, and the capital the first word wants.
//
// The article rule is the schoolroom one, on the letter rather than the
// sound, so "an hour" and "a university" come out wrong. Both are rare enough
// among random words to be worth less than the machinery for pronouncing a
// word to find out how it starts.
func polish(s string) string {
	// Assembling a sentence from pieces leaves seams. An apposition carries
	// its own closing comma, so "said {np}." can end ",."; an empty adjunct
	// can leave a double space behind. Both are cheaper to sweep up here
	// than to prevent in every rule that might cause them.
	for _, seam := range [][2]string{
		{"  ", " "}, {" ,", ","}, {",,", ","}, {", .", "."}, {",.", "."},
		{",?", "?"}, {" .", "."},
	} {
		for strings.Contains(s, seam[0]) {
			s = strings.ReplaceAll(s, seam[0], seam[1])
		}
	}

	// Templates are written in lower case, so the article to correct is
	// always a bare "a": at the start, after a space, or after a quote.
	for i := 0; ; {
		j := strings.Index(s[i:], "a ")
		if j < 0 {
			break
		}
		at := i + j
		next := at + 2
		standalone := at == 0 || s[at-1] == ' ' || s[at-1] == '\''
		if standalone && next < len(s) && strings.ContainsRune("aeiou", rune(s[next])) {
			s = s[:at+1] + "n" + s[at+1:]
		}
		i = at + 1
	}
	// A capital opens the line and follows every full stop. The capital is
	// not always on the first character: a line may open with a quotation
	// mark, and a stop may be followed by one.
	b := []byte(s)
	start := true
	for i := 0; i < len(b); i++ {
		switch {
		case start && b[i] >= 'a' && b[i] <= 'z':
			b[i] -= 'a' - 'A'
			start = false
		case b[i] == '.' || b[i] == '!' || b[i] == '?':
			// A stop inside a quotation does not end the sentence that
			// carries it: what follows a closing quote is the speech tag,
			// and "'Scoot?' asked the newt" keeps its lower case.
			start = i+1 >= len(b) || b[i+1] != '\''
		case b[i] == ' ' || b[i] == '\'':
			// Neither ends a sentence nor begins a word, so whatever was
			// pending stays pending.
		default:
			start = false
		}
	}
	return string(b)
}

// parsePart resolves the value of --pos. The long spellings are accepted
// because "adj" is what the dictionaries print and "adjective" is what a
// person types.
func parsePart(s string) (dictdb.Part, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return "", nil
	case "n", "noun":
		return dictdb.Noun, nil
	case "v", "verb":
		return dictdb.Verb, nil
	case "name", "proper":
		return dictdb.Name, nil
	case "person", "people":
		return dictdb.Person, nil
	case "place", "places":
		return dictdb.Place, nil
	case "vt", "v.t.", "transitive":
		return dictdb.Transitive, nil
	case "vi", "v.i.", "intransitive":
		return dictdb.Intransitive, nil
	case "a", "adj", "adjective":
		return dictdb.Adjective, nil
	case "adv", "adverb":
		return dictdb.Adverb, nil
	}
	return "", fmt.Errorf("no such part of speech as %q (noun, verb, vt, vi, adj, adv, name, person, place)", s)
}

var adlibOpts struct {
	count int
	seed  int64
}

var adlibCmd = &cobra.Command{
	Use:     ":adlib",
	Short:   "a sentence built from random words of the right parts of speech",
	Aliases: []string{":madlib"},
	Long: "Print a sentence with its vocabulary drawn at random.\n\n" +
		"The structure comes from a template and the words come from the word list,\n" +
		"each one checked against the dictionaries so that a verb slot gets a verb --\n" +
		"and an object only goes to a verb that can take one. Tense and plural come\n" +
		"from the entry too, where it lists them. The grammar holds; the meaning does\n" +
		"not.",
	Example: "  dict :adlib           one sentence\n" +
		"  dict :adlib -n 10     ten of them\n" +
		"  dict :adlib --seed 7  the same sentence every time",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		words, source, err := loadWordList(opts.wordFile, opts.lang)
		if err != nil {
			return err
		}
		if len(words) == 0 {
			return fmt.Errorf("%s is empty", source)
		}
		seed := adlibOpts.seed
		if seed == 0 {
			seed = time.Now().UnixNano()
		}
		rng := rand.New(rand.NewPCG(uint64(seed), uint64(seed>>32))) //nolint:gosec // a joke, not a key
		vocab := newVocabulary(words, dictionaries(), rng)

		count := adlibOpts.count
		if count < 1 {
			count = 1
		}
		for i := 0; i < count; i++ {
			s, err := vocab.sentence()
			if err != nil {
				return err
			}
			println(s)
		}
		return nil
	},
}

func init() {
	f := adlibCmd.Flags()
	f.IntVarP(&adlibOpts.count, "num", "n", 1, "how many sentences to print")
	f.Int64Var(&adlibOpts.seed, "seed", 0, "seed the word draw, to repeat a run")
}
