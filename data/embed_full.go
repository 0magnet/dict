//go:build !dictlite

package data

import "embed"

// The whole corpus. GCIDE and WordNet are 24.7 MB of the 28 MB, which is
// the right trade for a binary you install once and a dictionary that
// then works with nothing else present.
//
//go:embed dictd/*.dict.dz dictd/*.index.gz words.gz names.tsv.gz wikt.tsv.gz wiki.tsv.gz licenses
var FS embed.FS

// Order is the order dictionaries are consulted. GCIDE leads because its
// 1913 prose is the reason to reach for a dictionary at all; WordNet
// covers the modern vocabulary GCIDE predates; the rest fill in acronyms
// and computing terms that neither general dictionary carries.
var Order = []string{"gcide", "wn", "foldoc", "jargon", "elements"}
