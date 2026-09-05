package dictdb

import (
	"regexp"
	"strings"
)

// GCIDE carries the typesetting conventions of a printed 1913 dictionary.
// Accents are written as bracketed escapes because the source predates
// Unicode, every sense is stamped with the edition that contributed it, and
// cross-references are wrapped in braces. Shown raw it reads like markup, so
// these turn it back into prose.
var (
	// Source stamps: [1913 Webster], [PJC], [WordNet 1.5], [Century Dict. 1906]
	reSourceTag = regexp.MustCompile(`\[(1913 Webster[^\]]*|Webster 1913[^\]]*|PJC|AS|RH|WordNet[^\]]*|Century[^\]]*|Moby[^\]]*)\]`)
	// Cross-references and emphasis: {Worth} -> Worth
	reBrace = regexp.MustCompile(`\{([^{}]*)\}`)
	// The headword restatement that opens each entry: \Weird\ (w[=e]rd)
	reHeadword         = regexp.MustCompile(`\\([^\\]*)\\`)
	reBlank            = regexp.MustCompile(`\n{3,}`)
	reSpaceBeforePunct = regexp.MustCompile(`[ \t]+([,;.:)])`)
	reTrailWS          = regexp.MustCompile(`[ \t]+\n`)
	// Any accent escape the table above missed: keep the letter, drop the
	// brackets and the diacritic marker, so "r[-e]" reads as "re".
	// A parenthesised pronunciation, recognized by the syllable dot or a
	// diacritic: "(re*sev\")". This is a spelling tool, so they are dropped.
	rePronounce = regexp.MustCompile("\\s*\\([^()]*[*\u0101\u0113\u012b\u014d\u016b\u0103\u0115\u012d\u014f\u016d][^()]*\\)")
	reAnyAccent = regexp.MustCompile("\\[[-=^\"'`~,.]?([a-zA-Z])\\]")
)

// accents maps GCIDE's bracketed letter escapes onto real characters. The
// list covers what actually appears in etymologies and pronunciations; an
// unrecognized escape falls back to its bare letter rather than being shown
// as brackets.
var accents = strings.NewReplacer(
	"[=a]", "ā", "[=e]", "ē", "[=i]", "ī", "[=o]", "ō", "[=u]", "ū", "[=y]", "ȳ",
	"[^a]", "â", "[^e]", "ê", "[^i]", "î", "[^o]", "ô", "[^u]", "û",
	`["a]`, "ä", `["e]`, "ë", `["i]`, "ï", `["o]`, "ö", `["u]`, "ü",
	"[`a]", "à", "[`e]", "è", "[`i]", "ì", "[`o]", "ò", "[`u]", "ù",
	"['a]", "á", "['e]", "é", "['i]", "í", "['o]", "ó", "['u]", "ú", "['y]", "ý",
	"[~a]", "ã", "[~n]", "ñ", "[~o]", "õ",
	"[,c]", "ç", "[vs]", "š", "[vz]", "ž", "[vc]", "č",
	"[ae]", "æ", "[AE]", "Æ", "[oe]", "œ", "[OE]", "Œ",
	"[eth]", "ð", "[th]", "þ", "[=E]", "Ē", "[=A]", "Ā",
	"[breve]", "˘", "[root]", "√", "[deg]", "°", "[Emac]", "Ē",
	"[imac]", "ī", "[amac]", "ā", "[emac]", "ē", "[omac]", "ō", "[umac]", "ū",
	"[ecr]", "ĕ", "[acr]", "ă", "[icr]", "ĭ", "[ocr]", "ŏ", "[ucr]", "ŭ",
)

// Clean turns one raw dictionary entry into plain text fit for a terminal.
// It is safe to run on entries from any of the databases: WordNet and FOLDOC
// use none of these conventions, so for them it is very nearly a no-op.
func Clean(entry string) string {
	s := accents.Replace(entry)
	s = reSourceTag.ReplaceAllString(s, "")
	s = dropLeadingPronunciation(s)
	s = rePronounce.ReplaceAllString(s, "")
	s = reHeadword.ReplaceAllString(s, "$1")
	s = reBrace.ReplaceAllString(s, "$1")

	// Any accent escape the table missed keeps its letter and loses the
	// brackets, which reads far better than "w[=e]rd".
	s = reAnyAccent.ReplaceAllString(s, "$1")

	s = dedupeHeadword(s)
	// Dropping a pronunciation can leave a gap before the punctuation that
	// followed it, as in "Weird , a."
	s = reSpaceBeforePunct.ReplaceAllString(s, "$1")
	s = reTrailWS.ReplaceAllString(s, "\n")
	s = reBlank.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// Wrap breaks text to the given width, keeping the indentation of each line
// so that numbered senses and quotations stay visually distinct.
func Wrap(s string, width int) []string {
	if width < 20 {
		width = 20
	}
	var out []string
	for _, line := range strings.Split(s, "\n") {
		indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		if len(indent) > width/2 {
			indent = indent[:width/2]
		}
		words := strings.Fields(line)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		cur := indent
		for _, w := range words {
			switch {
			case cur == indent:
				cur += w
			case len([]rune(cur))+1+len([]rune(w)) <= width:
				cur += " " + w
			default:
				out = append(out, cur)
				cur = indent + w
			}
		}
		if cur != indent {
			out = append(out, cur)
		}
	}
	return out
}

// dedupeHeadword removes the repetition left by unwrapping a GCIDE entry.
// Entries open with the headword in plain text and again in the bold form,
// as "Weird \Weird\ (w[=e]rd)", so stripping the markers yields it twice.
// A backreference would express this in one pattern, but RE2 has none, so the
// duplicate is found by comparing the first two words.
func dedupeHeadword(s string) string {
	nl := strings.IndexByte(s, '\n')
	head := s
	rest := ""
	if nl >= 0 {
		head, rest = s[:nl], s[nl:]
	}
	f := strings.Fields(head)
	if len(f) >= 2 && strings.EqualFold(f[0], f[1]) {
		// Cut the first word, keeping the original spacing of what follows.
		trimmed := strings.TrimLeft(head, " \t")
		if i := strings.IndexAny(trimmed, " \t"); i >= 0 {
			return strings.TrimLeft(trimmed[i:], " \t") + rest
		}
	}
	return s
}

// dropLeadingPronunciation removes the decorated repeat of the headword that
// opens a GCIDE entry. An entry begins "Receive \Re*ceive"\ (r[-e]*sev"), v.t."
// -- the plain headword, then the same word again carrying syllable dots and
// stress marks. Unwrapping that second copy leaves the word twice, once with
// typesetting debris, so it is cut instead. Only the first is treated this
// way; later \...\ marks are ordinary emphasis and are unwrapped normally.
func dropLeadingPronunciation(s string) string {
	loc := reHeadword.FindStringIndex(s)
	if loc == nil || loc[0] > 48 {
		return s
	}
	head := strings.TrimSpace(s[:loc[0]])
	if head == "" || strings.ContainsAny(head, "\n") {
		return s
	}
	inner := s[loc[0]+1 : loc[1]-1]
	if !sameWord(head, inner) {
		return s
	}
	return s[:loc[0]] + s[loc[1]:]
}

// sameWord compares two spellings ignoring the syllable dots, stress marks
// and accents GCIDE adds to a pronunciation.
func sameWord(a, b string) bool {
	strip := func(s string) string {
		var out []rune
		for _, r := range strings.ToLower(s) {
			if r >= 'a' && r <= 'z' {
				out = append(out, r)
			}
		}
		return string(out)
	}
	sa, sb := strip(a), strip(b)
	return sa != "" && sa == sb
}
