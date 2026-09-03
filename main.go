// Command dict looks up how a word is spelled, and what it means.
//
// It replaces the shell function
//
//	dict() { fzf -q "$1" < /usr/share/dict/words; }
//
// with a matcher that also handles the errors fzf cannot -- fzf matches a
// subsequence, so a transposed pair such as "recieve" finds nothing at all --
// and shows the definition of whatever is highlighted.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/0magnet/dict/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, strings.TrimSpace(err.Error()))
		os.Exit(1)
	}
}
