package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// The last words with nothing to show are ordinary modern vocabulary --
// paracetamol, colorway, telekinetic -- and brand and person names. Wiktionary
// has them, but as wikitext, which has to be turned back into a sentence.
//
// Unlike the name facts, these are somebody's writing, so this text is used
// under CC BY-SA 4.0 with attribution, which Creative Commons declares
// one-way compatible with the GPL-3 the binary already carries.

var (
	// The English section runs to the next language heading.
	reLangHeading = regexp.MustCompile(`(?m)^==[^=\n]+==\s*$`)
	rePOSHeading  = regexp.MustCompile(`(?m)^={3,4}\s*([^=\n]+?)\s*={3,4}\s*$`)

	// Inline markup, innermost first.
	reLabel = regexp.MustCompile(`\{\{(?:lb|label|lbl)\|[a-z-]+\|([^{}]*)\}\}`)
	reLink2 = regexp.MustCompile(`\{\{(?:l|m|mention|link)\|[a-z-]+\|([^{}|]*)(?:\|[^{}]*)?\}\}`)
	reQual  = regexp.MustCompile(`\{\{(?:q|qual|qualifier|gloss|gl)\|([^{}]*)\}\}`)
	// The "form of" family carries the whole meaning of an entry -- an
	// abbreviation page is often nothing but one of these -- so they are
	// expanded rather than dropped with the other templates.
	reFormOf = regexp.MustCompile(`\{\{([a-z]+(?: [a-z]+){0,3} of|short for|alt form|altform|alt sp|alt case)\|[a-z-]+\|([^{}|]*)(?:\|[^{}]*)?\}\}`)
	// English-specific inflection templates take no language parameter:
	// {{en-superlative of|def}}, {{en-past of|walk}}.
	reEnForm = regexp.MustCompile(`\{\{en-([a-z][a-z- ]*) of\|([^{}|]*)(?:\|[^{}]*)?\}\}`)
	// A unit built from a prefix and a base: {{SI-unit|en|mega|pascal|pressure}}.
	reSIUnit    = regexp.MustCompile(`\{\{SI-unit[a-z0-9-]*\|[a-z-]+\|([^{}|]*)\|([^{}|]*)\|([^{}|]*)(?:\|[^{}]*)?\}\}`)
	reNonGloss  = regexp.MustCompile(`\{\{(?:non-gloss definition|non-gloss|n-g|ngd|ng)\|([^{}]*)\}\}`)
	reTemplAny  = regexp.MustCompile(`\{\{[^{}]*\}\}`)
	reWikiPipe  = regexp.MustCompile(`\[\[[^\]|]*\|([^\]]*)\]\]`)
	reWikiPlain = regexp.MustCompile(`\[\[([^\]]*)\]\]`)
	reBold      = regexp.MustCompile(`'{2,5}`)
	reRef       = regexp.MustCompile(`(?s)<ref[^>]*>.*?</ref>|<ref[^>]*/>`)
	reTag       = regexp.MustCompile(`<[^>]+>`)
	reSpaces    = regexp.MustCompile(`\s{2,}`)
)

// formOfNames expands the shortcut spellings Wiktionary allows for the
// "form of" templates, so a page written with {{init of}} does not display as
// "Init of" rather than "Initialism of".
var formOfNames = map[string]string{
	"init of":                  "Initialism of",
	"alt case":                 "Alternative letter-case form of",
	"alternative case form of": "Alternative letter-case form of",
	"alt case form of":         "Alternative letter-case form of",
	"infl of":                  "Inflection of",
	"alt form of":              "Alternative form of",
	"alt sp of":                "Alternative spelling of",
	"abbr of":                  "Abbreviation of",
	"alt form":                 "Alternative form of",
	"altform":                  "Alternative form of",
	"alt sp":                   "Alternative spelling of",
	"clip of":                  "Clipping of",
	"syn of":                   "Synonym of",
	"ellipsis of":              "Ellipsis of",
	"short for":                "Short for",
	"acronym of":               "Acronym of",
	"apocopic of":              "Apocopic form of",
}

// posWanted are the headings whose definitions are worth showing. Sections
// like Pronunciation, Etymology and Anagrams carry no definition.
var posWanted = map[string]string{
	"noun": "n", "verb": "v", "adjective": "adj", "adverb": "adv",
	"proper noun": "prop n", "interjection": "interj", "preposition": "prep",
	"conjunction": "conj", "pronoun": "pron", "numeral": "num",
	"abbreviation": "abbr", "initialism": "abbr", "acronym": "abbr",
	"prefix": "prefix", "suffix": "suffix", "particle": "particle",
	"determiner": "det", "article": "art", "contraction": "contr",
	"phrase": "phrase", "proverb": "proverb",
}

// englishSection returns just the English part of a page.
func englishSection(text string) string {
	locs := reLangHeading.FindAllStringIndex(text, -1)
	for i, loc := range locs {
		if strings.TrimSpace(text[loc[0]:loc[1]]) != "==English==" {
			continue
		}
		end := len(text)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		return text[loc[1]:end]
	}
	return ""
}

// parseDefinitions pulls up to max definitions out of a page, each tagged with
// its part of speech.
func parseDefinitions(text string, max int) []string {
	eng := englishSection(text)
	if eng == "" {
		return nil
	}

	// Walk the part-of-speech headings, taking the definition lines under each.
	heads := rePOSHeading.FindAllStringSubmatchIndex(eng, -1)
	var out []string
	for i, h := range heads {
		name := strings.ToLower(strings.TrimSpace(eng[h[2]:h[3]]))
		tag, ok := posWanted[name]
		if !ok {
			continue
		}
		end := len(eng)
		if i+1 < len(heads) {
			end = heads[i+1][0]
		}
		body := eng[h[1]:end]

		n := 0
		for _, line := range strings.Split(body, "\n") {
			// "# " is a definition; "#*" is a quotation and "#:" an example.
			if !strings.HasPrefix(line, "# ") {
				continue
			}
			def := cleanWikitext(line[2:])
			if def == "" || len(def) < 3 {
				continue
			}
			out = append(out, tag+". "+def)
			n++
			if n >= 2 || len(out) >= max {
				break
			}
		}
		if len(out) >= max {
			break
		}
	}
	return out
}

// cleanWikitext turns one definition line into plain prose.
func cleanWikitext(s string) string {
	s = reRef.ReplaceAllString(s, "")
	s = reNonGloss.ReplaceAllString(s, "$1")
	s = reSIUnit.ReplaceAllStringFunc(s, func(m string) string {
		p := reSIUnit.FindStringSubmatch(m)
		prefix, base, quantity := p[1], p[2], p[3]
		if quantity == "" || base == "" {
			return ""
		}
		return fmt.Sprintf("An SI unit of %s, one %s%s", quantity, prefix, base)
	})
	s = reEnForm.ReplaceAllStringFunc(s, func(m string) string {
		p := reEnForm.FindStringSubmatch(m)
		kind, target := p[1], strings.TrimSpace(p[2])
		if target == "" {
			return ""
		}
		return strings.ToUpper(kind[:1]) + kind[1:] + " of " + target
	})
	s = reFormOf.ReplaceAllStringFunc(s, func(m string) string {
		p := reFormOf.FindStringSubmatch(m)
		kind, target := p[1], strings.TrimSpace(p[2])
		if target == "" {
			return ""
		}
		if full, ok := formOfNames[kind]; ok {
			return full + " " + target
		}
		return strings.ToUpper(kind[:1]) + kind[1:] + " " + target
	})
	s = reLabel.ReplaceAllStringFunc(s, func(m string) string {
		inner := reLabel.FindStringSubmatch(m)[1]
		// Label parameters are separated by pipes and read as a list.
		parts := []string{}
		for _, p := range strings.Split(inner, "|") {
			p = strings.TrimSpace(p)
			if p != "" && !strings.Contains(p, "=") {
				parts = append(parts, p)
			}
		}
		if len(parts) == 0 {
			return ""
		}
		// Wiktionary uses "or" and "and" inside a label as connectors, so
		// "British|or|MLE" must read "British or MLE", not a three-item list.
		var b strings.Builder
		for i, p := range parts {
			switch {
			case i == 0:
			case p == "or" || p == "and":
				b.WriteString(" ")
			case i > 0 && (parts[i-1] == "or" || parts[i-1] == "and"):
				b.WriteString(" ")
			default:
				b.WriteString(", ")
			}
			b.WriteString(p)
		}
		return "(" + b.String() + ")"
	})
	s = reLink2.ReplaceAllString(s, "$1")
	s = reQual.ReplaceAllString(s, "($1)")

	// Whatever templates remain carry no prose worth keeping.
	for i := 0; i < 3 && strings.Contains(s, "{{"); i++ {
		s = reTemplAny.ReplaceAllString(s, "")
	}

	s = reWikiPipe.ReplaceAllString(s, "$1")
	s = reWikiPlain.ReplaceAllString(s, "$1")
	s = reBold.ReplaceAllString(s, "")
	s = reTag.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = reSpaces.ReplaceAllString(s, " ")
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, ", ")
	s = strings.TrimSpace(s)

	// An unbalanced template survives the removal pass, because that pattern
	// needs both braces. Such a line cannot be trusted, so it is dropped.
	if strings.Contains(s, "{{") || strings.Contains(s, "}}") || strings.Contains(s, "[[") {
		return ""
	}
	// A line reduced to punctuation, or to nothing but a label, carries no
	// definition: "(business)." says only where a word is used, not what it means.
	bare := regexp.MustCompile(`\([^)]*\)`).ReplaceAllString(s, "")
	if strings.Trim(bare, " .,;:()-") == "" {
		return ""
	}
	if !strings.HasSuffix(s, ".") && !strings.HasSuffix(s, "!") && !strings.HasSuffix(s, "?") {
		s += "."
	}
	return s
}

// fetchDefinitions asks Wiktionary for definitions of each word.
func fetchDefinitions(words []string, max int) map[string][]string {
	out := map[string][]string{}
	client := newClient()
	var totalBatches, failedBatches int

	for i := 0; i < len(words); i += titlesPerQuery {
		end := i + titlesPerQuery
		if end > len(words) {
			end = len(words)
		}
		totalBatches++
		pages, err := queryPages(client, words[i:end])
		if err != nil {
			failedBatches++
			fmt.Fprintf(os.Stderr, "  defs batch %d: %v\n", i/titlesPerQuery, err)
			continue
		}
		for title, content := range pages {
			if defs := parseDefinitions(content, max); len(defs) > 0 {
				out[strings.ToLower(title)] = defs
			}
		}
		if (i/titlesPerQuery)%20 == 0 {
			fmt.Fprintf(os.Stderr, "  definitions %d/%d words, %d defined so far\n", end, len(words), len(out))
		}
		sleepPolite()
	}
	checkBatchFailures(failedBatches, totalBatches)
	return out
}
