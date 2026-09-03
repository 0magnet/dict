package match_test

import (
	"testing"

	"github.com/0magnet/dict/match"
)

func BenchmarkPerQuery(b *testing.B) {
	ix := match.NewIndex(loadSystemWords(b))
	for _, q := range []string{"a", "re", "rec", "recieve", "definately", "antidisestab"} {
		b.Run(q, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				ix.Search(q, 500)
			}
		})
	}
}
