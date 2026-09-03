package match

import "unicode"

// Scoring constants and the bonus model are taken from fzf's algo.go, so that
// for the queries fzf handles well the ordering here stays recognisable.
const (
	scoreMatch        = 16
	scoreGapStart     = -3
	scoreGapExtension = -1

	bonusBoundary    = scoreMatch / 2
	bonusNonWord     = scoreMatch / 2
	bonusCamel123    = bonusBoundary + scoreGapExtension
	bonusConsecutive = -(scoreGapStart + scoreGapExtension)

	// fzf weights the very first character of a match more heavily, which is
	// what makes prefix matches sort to the top.
	bonusFirstCharMultiplier = 2
)

type charClass int

const (
	classWhite charClass = iota
	classNonWord
	classDelimiter
	classLower
	classUpper
	classDigit
)

func classOf(r rune) charClass {
	switch {
	case unicode.IsLower(r):
		return classLower
	case unicode.IsUpper(r):
		return classUpper
	case unicode.IsDigit(r):
		return classDigit
	case unicode.IsSpace(r):
		return classWhite
	case r == '-' || r == '_' || r == '\'' || r == '/' || r == '.':
		return classDelimiter
	default:
		return classNonWord
	}
}

func bonusFor(prev, cur charClass) int {
	if cur > classNonWord {
		switch prev {
		case classWhite, classNonWord, classDelimiter:
			return bonusBoundary
		}
	}
	if prev == classLower && cur == classUpper {
		return bonusCamel123
	}
	if prev != classDigit && cur == classDigit {
		return bonusCamel123
	}
	switch cur {
	case classNonWord, classDelimiter:
		return bonusNonWord
	case classWhite:
		return bonusBoundary
	}
	return 0
}

// Fuzzy reports whether query is a subsequence of text and, if so, scores the
// match. positions, when non-nil, is filled with the index in text of each
// matched query rune, for highlighting.
//
// fold selects case-insensitive comparison. This is fzf's FuzzyMatchV1: a
// forward pass to find a match, then a backward pass to tighten it to the last
// starting point that still matches, then scoring over that window.
func Fuzzy(text, query []rune, fold bool, positions *[]int) (score int, ok bool) {
	if len(query) == 0 {
		return 0, true
	}
	if len(text) < len(query) {
		return 0, false
	}

	eq := func(a, b rune) bool {
		if a == b {
			return true
		}
		if fold {
			return unicode.ToLower(a) == unicode.ToLower(b)
		}
		return false
	}

	// Forward: find the earliest end index that completes the query.
	qi, start, end := 0, -1, -1
	for i, r := range text {
		if eq(r, query[qi]) {
			if start < 0 {
				start = i
			}
			qi++
			if qi == len(query) {
				end = i + 1
				break
			}
		}
	}
	if end < 0 {
		return 0, false
	}

	// Backward: pull start forward as far as it will go, which is what turns
	// a sprawling match into a tight one and lets the score reflect it.
	qi = len(query) - 1
	for i := end - 1; i >= start; i-- {
		if eq(text[i], query[qi]) {
			qi--
			if qi < 0 {
				start = i
				break
			}
		}
	}

	return score1(text, query, start, end, eq, positions), true
}

func score1(text, query []rune, start, end int, eq func(a, b rune) bool, positions *[]int) int {
	qi := 0
	score := 0
	inGap := false
	consecutive := 0
	firstBonus := 0

	prevClass := classWhite
	if start > 0 {
		prevClass = classOf(text[start-1])
	}

	for i := start; i < end; i++ {
		cur := text[i]
		curClass := classOf(cur)

		if qi < len(query) && eq(cur, query[qi]) {
			if positions != nil {
				*positions = append(*positions, i)
			}
			score += scoreMatch
			bonus := bonusFor(prevClass, curClass)

			if consecutive == 0 {
				firstBonus = bonus
			} else {
				// A run of consecutive matches keeps the strongest bonus seen
				// at its head, so "rec" in "receive" is not penalised for the
				// weak bonuses on 'e' and 'c'.
				if bonus >= bonusBoundary && bonus > firstBonus {
					firstBonus = bonus
				}
				if bonus < firstBonus {
					bonus = firstBonus
				}
				if bonus < bonusConsecutive {
					bonus = bonusConsecutive
				}
			}

			if qi == 0 {
				score += bonus * bonusFirstCharMultiplier
			} else {
				score += bonus
			}

			inGap = false
			consecutive++
			qi++
		} else {
			if inGap {
				score += scoreGapExtension
			} else {
				score += scoreGapStart
			}
			inGap = true
			consecutive = 0
			firstBonus = 0
		}
		prevClass = curClass
	}
	return score
}

// HasUpper reports whether s contains an uppercase rune. It drives smart case:
// an all-lowercase query matches case-insensitively, a query with any capital
// is taken literally.
func HasUpper(s []rune) bool {
	for _, r := range s {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}
