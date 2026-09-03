package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/0magnet/dict/data"
)

// searchPaths are tried in order when no word list is named. The first two
// cover Linux with the words/wordlist packages installed; web2 is the BSD and
// macOS name.
var searchPaths = []string{
	"/usr/share/dict/words",
	"/usr/share/dict/american-english",
	"/usr/share/dict/british-english",
	"/usr/share/dict/web2",
	"/usr/dict/words",
}

const dictDir = "/usr/share/dict"

// findWordlist resolves the word list to read. Precedence: an explicit path,
// then a language name resolved under /usr/share/dict, then $DICT_WORDS, then
// the standard locations.
func findWordlist(explicit, lang string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err != nil {
			return "", fmt.Errorf("word list %s: %w", explicit, err)
		}
		return explicit, nil
	}
	if lang != "" {
		p := filepath.Join(dictDir, lang)
		if _, err := os.Stat(p); err != nil {
			return "", fmt.Errorf("no word list for %q in %s (try: dict -L)", lang, dictDir)
		}
		return p, nil
	}
	if p := os.Getenv("DICT_WORDS"); p != "" {
		if _, err := os.Stat(p); err != nil {
			return "", fmt.Errorf("$DICT_WORDS=%s: %w", p, err)
		}
		return p, nil
	}
	for _, p := range searchPaths {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("no word list found; install a words package or pass -w")
}

// listLanguages reports the word lists available under /usr/share/dict.
func listLanguages() ([]string, error) {
	entries, err := os.ReadDir(dictDir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		info, err := os.Stat(filepath.Join(dictDir, e.Name()))
		if err != nil || info.IsDir() {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

// loadWords reads a newline-separated word list. Blank lines and comments are
// dropped; nothing else is filtered. In particular possessives and
// contractions are kept, because "don't" is a word you may well be checking,
// and ranking pushes the noisy forms down on its own.
func loadWords(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var words []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		words = append(words, line)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return words, nil
}

// loadWordList resolves the word list, falling back to the embedded copy when
// nothing is installed. It reports the name to show in the status line.
func loadWordList(explicit, lang string) ([]string, string, error) {
	path, err := findWordlist(explicit, lang)
	if err != nil {
		// An explicitly named list that cannot be read is an error; having no
		// system list at all is not, because one is built in.
		if explicit != "" || lang != "" {
			return nil, "", err
		}
		words, werr := data.Words()
		if werr != nil {
			return nil, "", werr
		}
		return words, "built in", nil
	}
	words, err := loadWords(path)
	if err != nil {
		return nil, "", err
	}
	return words, path, nil
}
