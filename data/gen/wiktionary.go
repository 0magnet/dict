package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Wiktionary supplies what the census and gazetteer cannot: where a name comes
// from, and whether it is a given name at all. Both are recorded in templates
// rather than prose --
//
//	# {{surname|en|from=Irish}}.
//	# {{given name|en|female|from=Ancient Greek}}.
//
// so what is taken from here are the structured facts in those templates, not
// anyone's writing.
const (
	wiktAPI = "https://en.wiktionary.org/w/api.php"
	// Wikimedia asks that a tool identify itself and offer a route to its
	// author. The repository serves as that route; no personal address of the
	// person running the generator is sent.
	userAgent = "dict-gloss-builder/0.1 (+https://github.com/0magnet/dict)"
	// 50 titles per query is the ceiling for an ordinary client.
	titlesPerQuery = 50
	politeDelay    = 250 * time.Millisecond
	maxRetries     = 6
	// A run that loses more than this fraction of its batches would write a
	// file worse than the one it replaces, so it stops instead.
	maxFailedFrac = 0.02
)

var (
	reSurname   = regexp.MustCompile(`\{\{surname\|([a-z-]+)((?:\|[^{}]*)?)\}\}`)
	reGivenName = regexp.MustCompile(`\{\{given name\|([a-z-]+)\|([a-z]+)((?:\|[^{}]*)?)\}\}`)
	reFrom      = regexp.MustCompile(`\bfrom=([^|}]+)`)
)

// langNames covers the language codes that actually turn up on name pages.
// An unknown code is skipped rather than guessed at.
var langNames = map[string]string{
	"ar": "Arabic", "hy": "Armenian", "eu": "Basque", "bn": "Bengali",
	"ca": "Catalan", "zh": "Chinese", "hr": "Croatian", "cs": "Czech",
	"da": "Danish", "nl": "Dutch", "et": "Estonian", "fi": "Finnish",
	"fr": "French", "de": "German", "el": "Greek", "he": "Hebrew",
	"hi": "Hindi", "hu": "Hungarian", "is": "Icelandic", "ga": "Irish",
	"it": "Italian", "ja": "Japanese", "ko": "Korean", "la": "Latin",
	"lv": "Latvian", "lt": "Lithuanian", "mk": "Macedonian", "no": "Norwegian",
	"fa": "Persian", "pl": "Polish", "pt": "Portuguese", "ro": "Romanian",
	"ru": "Russian", "gd": "Scottish Gaelic", "sr": "Serbian", "sk": "Slovak",
	"sl": "Slovene", "es": "Spanish", "sv": "Swedish", "tr": "Turkish",
	"uk": "Ukrainian", "vi": "Vietnamese", "cy": "Welsh", "yi": "Yiddish",
}

// nameFacts is what one Wiktionary page yields about a name.
type nameFacts struct {
	isSurname bool
	givenGen  string // "female", "male", or "" when unspecified
	isGiven   bool
	origins   []string
}

type wiktResponse struct {
	Query struct {
		Pages []struct {
			Title     string `json:"title"`
			Missing   bool   `json:"missing"`
			Revisions []struct {
				Slots struct {
					Main struct {
						Content string `json:"content"`
					} `json:"main"`
				} `json:"slots"`
			} `json:"revisions"`
		} `json:"pages"`
	} `json:"query"`
}

// fetchNameFacts asks Wiktionary about each word, in batches, and returns what
// it knows keyed by lower-cased word.
func fetchNameFacts(words []string) map[string]*nameFacts {
	out := map[string]*nameFacts{}
	client := newClient()
	var totalBatches, failedBatches int

	for i := 0; i < len(words); i += titlesPerQuery {
		end := i + titlesPerQuery
		if end > len(words) {
			end = len(words)
		}
		batch := words[i:end]

		totalBatches++
		facts, err := queryBatch(client, batch)
		if err != nil {
			failedBatches++
			fmt.Fprintf(os.Stderr, "  batch %d: %v\n", i/titlesPerQuery, err)
			continue
		}
		for k, v := range facts {
			out[k] = v
		}
		if (i/titlesPerQuery)%20 == 0 {
			fmt.Fprintf(os.Stderr, "  wiktionary %d/%d words, %d named so far\n", end, len(words), len(out))
		}
		sleepPolite()
	}
	checkBatchFailures(failedBatches, totalBatches)
	return out
}

// newClient returns the HTTP client both Wiktionary passes use.
func newClient() *http.Client { return &http.Client{Timeout: 60 * time.Second} }

// sleepPolite spaces out requests to a public API.
func sleepPolite() { time.Sleep(politeDelay) }

// queryPages fetches the wikitext of up to titlesPerQuery pages, keyed by the
// title the wiki reports, which may differ in case from what was asked.
func queryPages(client *http.Client, titles []string) (map[string]string, error) {
	v := url.Values{}
	v.Set("action", "query")
	v.Set("prop", "revisions")
	v.Set("rvprop", "content")
	v.Set("rvslots", "main")
	v.Set("format", "json")
	v.Set("formatversion", "2")
	v.Set("titles", strings.Join(titles, "|"))

	req, err := http.NewRequest("GET", wiktAPI+"?"+v.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	body, err := doWithRetry(client, req)
	if err != nil {
		return nil, err
	}

	var r wiktResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}

	out := map[string]string{}
	for _, p := range r.Query.Pages {
		if p.Missing || len(p.Revisions) == 0 {
			continue
		}
		out[p.Title] = p.Revisions[0].Slots.Main.Content
	}
	return out, nil
}

func queryBatch(client *http.Client, titles []string) (map[string]*nameFacts, error) {
	pages, err := queryPages(client, titles)
	if err != nil {
		return nil, err
	}
	out := map[string]*nameFacts{}
	for title, content := range pages {
		if f := parseNamePage(content); f != nil {
			out[strings.ToLower(title)] = f
		}
	}
	return out, nil
}

// parseNamePage pulls the surname and given-name templates out of a page.
func parseNamePage(text string) *nameFacts {
	f := &nameFacts{}
	seen := map[string]bool{}
	addOrigin := func(s string) {
		s = strings.TrimSpace(s)
		// "English < Hebrew" records a route; the ultimate source is wanted.
		if i := strings.LastIndex(s, "<"); i >= 0 {
			s = strings.TrimSpace(s[i+1:])
		}
		// "surnames" and "patronymics" describe formation, not a language.
		if s == "" || s == "surnames" || s == "patronymics" || s == "given names" {
			return
		}
		if !seen[s] {
			seen[s] = true
			f.origins = append(f.origins, s)
		}
	}

	for _, m := range reSurname.FindAllStringSubmatch(text, -1) {
		f.isSurname = true
		if from := reFrom.FindStringSubmatch(m[2]); from != nil {
			addOrigin(from[1])
		} else if name, ok := langNames[m[1]]; ok && m[1] != "en" {
			// A surname documented under Spanish is a Spanish surname.
			addOrigin(name)
		}
	}
	for _, m := range reGivenName.FindAllStringSubmatch(text, -1) {
		f.isGiven = true
		if m[2] == "male" || m[2] == "female" {
			// The English reading wins; others are noted only if nothing else.
			if f.givenGen == "" || m[1] == "en" {
				f.givenGen = m[2]
			}
		}
		if from := reFrom.FindStringSubmatch(m[3]); from != nil {
			addOrigin(from[1])
		}
	}
	if !f.isSurname && !f.isGiven {
		return nil
	}
	sort.Strings(f.origins)
	return f
}

func joinWords(s []string) string {
	switch len(s) {
	case 1:
		return s[0]
	case 2:
		return s[0] + " or " + s[1]
	default:
		return strings.Join(s[:len(s)-1], ", ") + " or " + s[len(s)-1]
	}
}

// backoff grows the wait between retries, starting at a second.
func backoff(attempt int) time.Duration {
	d := time.Second << uint(attempt)
	if d > 60*time.Second {
		d = 60 * time.Second
	}
	return d
}

// checkBatchFailures stops the run when too many batches were lost. Without
// this a throttled run silently writes a thinner file than the previous one,
// which is worse than not writing at all.
func checkBatchFailures(failed, total int) {
	if total == 0 {
		return
	}
	if frac := float64(failed) / float64(total); frac > maxFailedFrac {
		check(fmt.Errorf("%d of %d batches failed (%.0f%%); refusing to write a degraded file",
			failed, total, frac*100))
	}
}

// doWithRetry performs a request, waiting out throttling and transient server
// errors. A public API will push back on a run this size, and the answer to
// being told to slow down is to slow down -- not to carry on and quietly write
// a smaller file than the last run produced.
func doWithRetry(client *http.Client, req *http.Request) ([]byte, error) {
	for attempt := 0; ; attempt++ {
		resp, err := client.Do(req)
		if err != nil {
			if attempt >= maxRetries {
				return nil, err
			}
			time.Sleep(backoff(attempt))
			continue
		}
		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			wait := backoff(attempt)
			if ra := resp.Header.Get("Retry-After"); ra != "" {
				if secs, e := strconv.Atoi(ra); e == nil && secs > 0 {
					wait = time.Duration(secs) * time.Second
				}
			}
			resp.Body.Close()
			if attempt >= maxRetries {
				return nil, fmt.Errorf("http %d after %d attempts", resp.StatusCode, attempt+1)
			}
			fmt.Fprintf(os.Stderr, "  http %d, waiting %s\n", resp.StatusCode, wait)
			time.Sleep(wait)
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("http %d", resp.StatusCode)
		}
		return io.ReadAll(resp.Body)
	}
}
