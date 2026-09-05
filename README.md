# dict

Spell-check and dictionary lookup in the terminal, with Webster's 1913, WordNet, Wiktionary and Wikipedia built in.

**[Live demo](https://0magnet.github.io/dict/)** — the whole thing as a wasm terminal, interactive picker included, answering the same words as the installed binary.

![dict in the browser](docs/dict-demo.png "the interactive picker in a browser tab, showing Webster's 1913 on a word fetched a chunk at a time")

It replaces the shell function

```sh
dict() { fzf -q "$1" < /usr/share/dict/words; }
```

with a matcher that also handles the errors `fzf` cannot. `fzf` matches a
subsequence, so a transposed pair such as `recieve` finds nothing at all —
above, it is the first result.

## Matching

Candidates are ranked in tiers, so an exact hit never loses to a fuzzy one:

- exact, then case- and accent-folded exact
- prefix, then subsequence (what `fzf` does)
- edit distance, which is what catches transpositions and doubled letters
- phonetic, for a word heard rather than read

`-t` labels each match with the tier that found it, which is the quickest way
to see why something ranked where it did.

## Usage

```
dict [query] [flags]
```

| flag | meaning |
| --- | --- |
| `-w`, `--words` | word list to search (default: the built-in list) |
| `-l`, `--lang` | language list under `/usr/share/dict`, e.g. `french` |
| `-f`, `--filter` | print matches and exit, without the interactive view |
| `-n`, `--num` | maximum matches to print (0 for all) |
| `-t`, `--tier` | label each match with why it matched |
| `-d`, `--define` | print the definition of the best match and exit |
| `-D`, `--definition-only` | print only the definition text, no headword or source |
| `-r`, `--random` | print a random word and exit |
| `-R`, `--random-define` | print a random word with its definition |

Three subcommands report on the data rather than search it: `:languages` lists
other-language word lists installed on the system, `:sources` shows which word
list and dictionaries are in use, and `:licenses` prints the licenses of the
built-in dictionaries.

## Install

```sh
go install github.com/0magnet/dict@latest
```

## The demo

`make demo` builds what is committed under `docs/` and takes about two seconds:

```sh
make demo     # standard Go, ~2s
make serve    # build, then serve it at http://127.0.0.1:8791
```

`make demo-tinygo` builds the same page a third the size, but takes minutes
rather than seconds — TinyGo's compile-time interpreter folds the embedded
corpus on every build. A demo that is expensive to rebuild is a demo that goes
stale, so the fast one is the committed one.

The page is built with `-tags dictlite`, which embeds the full word list plus
FOLDOC, the Jargon File and Elements. Webster's 1913 and WordNet are the other
24.7 MB and are not embedded — they are served beside the page and read over
HTTP, one dictzip chunk at a time.

That works because a `.dict.dz` is deflated in independent chunks with a table
of where each one starts, so a definition is a ranged `GET` of about 58 KB
rather than the whole file. Nothing is fetched until something is looked up;
the first definition from a given dictionary also pulls its index (1.6 MB for
GCIDE, 1.4 MB for WordNet), and after that a lookup is one chunk. `dict
:sources` says which dictionaries were fetched and which are built in.

The upshot is that the page answers the same words, from the same
dictionaries, in the same order as the installed binary, having downloaded
almost none of the corpus. GitHub Pages serves the repository root so that
`data/dictd/` is reachable from the page; that is why `index.html` lives at
the top level.

## Licenses

The code is MIT. The bundled dictionaries are not — each keeps its own license,
and `dict :licenses` prints them. See `NOTICE` and `data/licenses/`.

## Dependency Graph

Made with [goda](https://github.com/loov/goda):

```
# GOOS=js: the import edges of a wasm program live in js/wasm-tagged
# files and are invisible to a host-context run
GOOS=js GOARCH=wasm go run github.com/loov/goda@latest graph github.com/0magnet/dict/... | dot -Tsvg -o docs/dict-goda-graph.svg
```

![Dependency Graph](docs/dict-goda-graph.svg "github.com/0magnet/dict Dependency Graph")

## Lines of Code

Made with [gocloc](https://github.com/hhatto/gocloc) (excludes `vendor/`, `node_modules/`, `.git/`):

```
gocloc --not-match-d='(vendor|node_modules|\.git)' .
```

```
-------------------------------------------------------------------------------
Language                     files          blank        comment           code
-------------------------------------------------------------------------------
Go                              41            555            949           5042
JavaScript                       1             61             36            478
YAML                             1              0              7             98
Markdown                         1             23              0             58
HTML                             1              0              7             28
Bourne Shell                     1              9             30             28
Makefile                         1              8              8             16
-------------------------------------------------------------------------------
TOTAL                           47            656           1037           5748
-------------------------------------------------------------------------------
```
