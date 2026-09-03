//go:build dictlite

package data

import "embed"

// A corpus small enough to download. GCIDE and WordNet are 24.7 MB of
// the 28 MB, and a browser demo that spends thirty megabytes before it
// can say anything is not a demo. What is left still answers the two
// questions dict exists for: the full word list (316 KB) tells you how a
// word is spelled, and FOLDOC, Jargon and Elements tell you what a good
// many of them mean.
//
//go:embed dictd/foldoc.dict.dz dictd/foldoc.index.gz dictd/jargon.dict.dz dictd/jargon.index.gz dictd/elements.dict.dz dictd/elements.index.gz words.gz names.tsv.gz wikt.tsv.gz wiki.tsv.gz licenses
var FS embed.FS

// Order omits the two dictionaries this build does not carry.
var Order = []string{"foldoc", "jargon", "elements"}
