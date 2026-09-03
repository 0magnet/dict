package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// What Wiktionary has no page for is largely brands, companies and people:
// Accenture, Altoids, Banneker, Zyrtec. Wikipedia has all of them, and its
// opening sentences are exactly the gloss wanted -- "a brand of mints, sold
// primarily in distinctive metal tins".
//
// Like Wiktionary this is CC BY-SA 4.0 text, used with attribution.
const (
	wikiAPI = "https://en.wikipedia.org/w/api.php"
	// Extracts are capped at 20 pages per query, lower than the 50 allowed
	// for raw content.
	extractsPerQuery = 20
)

type wikiResponse struct {
	Query struct {
		Normalized []struct {
			From string `json:"from"`
			To   string `json:"to"`
		} `json:"normalized"`
		Redirects []struct {
			From string `json:"from"`
			To   string `json:"to"`
		} `json:"redirects"`
		Pages []struct {
			Title   string `json:"title"`
			Missing bool   `json:"missing"`
			Extract string `json:"extract"`
		} `json:"pages"`
	} `json:"query"`
}

// fetchWikipedia returns a one or two sentence summary for each word that has
// an article, keyed by the lower-cased word as asked for.
func fetchWikipedia(words []string) map[string]string {
	out := map[string]string{}
	client := newClient()
	var totalBatches, failedBatches int

	for i := 0; i < len(words); i += extractsPerQuery {
		end := i + extractsPerQuery
		if end > len(words) {
			end = len(words)
		}
		totalBatches++
		got, err := queryExtracts(client, words[i:end])
		if err != nil {
			failedBatches++
			fmt.Fprintf(os.Stderr, "  wiki batch %d: %v\n", i/extractsPerQuery, err)
			continue
		}
		for k, v := range got {
			out[k] = v
		}
		if (i/extractsPerQuery)%20 == 0 {
			fmt.Fprintf(os.Stderr, "  wikipedia %d/%d words, %d summarised so far\n", end, len(words), len(out))
		}
		sleepPolite()
	}
	checkBatchFailures(failedBatches, totalBatches)
	return out
}

func queryExtracts(client *http.Client, titles []string) (map[string]string, error) {
	v := url.Values{}
	v.Set("action", "query")
	v.Set("prop", "extracts")
	v.Set("exintro", "1")
	v.Set("explaintext", "1")
	v.Set("exsentences", "2")
	v.Set("exlimit", "20")
	v.Set("redirects", "1")
	v.Set("format", "json")
	v.Set("formatversion", "2")
	v.Set("titles", strings.Join(titles, "|"))

	body, err := getWithRetry(client, wikiAPI+"?"+v.Encode())
	if err != nil {
		return nil, err
	}
	var r wikiResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}

	// A query for "Zyrtec" comes back as "Cetirizine", so the redirect and
	// normalisation chains have to be walked to match a result to its word.
	hop := map[string]string{}
	for _, n := range r.Query.Normalized {
		hop[n.From] = n.To
	}
	for _, rd := range r.Query.Redirects {
		hop[rd.From] = rd.To
	}
	byTitle := map[string]string{}
	for _, p := range r.Query.Pages {
		if p.Missing || strings.TrimSpace(p.Extract) == "" {
			continue
		}
		byTitle[p.Title] = p.Extract
	}

	out := map[string]string{}
	for _, want := range titles {
		title := want
		for i := 0; i < 4; i++ {
			next, ok := hop[title]
			if !ok {
				break
			}
			title = next
		}
		text := cleanExtract(byTitle[title])
		if text != "" {
			out[strings.ToLower(want)] = text
		}
	}
	return out, nil
}

// cleanExtract tidies a Wikipedia summary and rejects the ones that say
// nothing: a disambiguation list, or a stub with no sentence in it.
func cleanExtract(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Pronunciation parentheticals are long and unhelpful here.
	s = strings.Join(strings.Fields(s), " ")

	low := strings.ToLower(s)
	if strings.Contains(low, "may refer to") || strings.Contains(low, "may also refer to") ||
		strings.Contains(low, "commonly refers to") {
		return ""
	}
	if len(s) < 20 {
		return ""
	}
	if !strings.HasSuffix(s, ".") && !strings.HasSuffix(s, "!") && !strings.HasSuffix(s, "?") {
		s += "."
	}
	return s
}

// getWithRetry performs the request with the same backoff the Wiktionary
// passes use, so a throttled Wikipedia is waited out rather than skipped.
func getWithRetry(client *http.Client, u string) ([]byte, error) {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	return doWithRetry(client, req)
}

var _ = io.Discard
