package match

// Distance returns the optimal string alignment distance between a and b:
// Levenshtein extended with adjacent transposition, so "recieve" is distance 1
// from "receive" rather than 2.
//
// The transposition case is the whole reason this package exists. A fuzzy
// subsequence matcher cannot find "receive" from "recieve" at any score,
// because the letters are not in order; an edit metric finds it immediately.
//
// It returns max+1 as soon as it can prove the true distance exceeds max, so
// callers can filter a large word list cheaply. Computation is banded to the
// diagonal ±max, which is what makes a full pass over ~124k words affordable
// on every keystroke.
func Distance(a, b []rune, max int) int {
	// Work with a as the shorter string; OSA distance is symmetric.
	if len(a) > len(b) {
		a, b = b, a
	}
	la, lb := len(a), len(b)

	// A length gap alone already costs that many edits.
	if lb-la > max {
		return max + 1
	}
	if la == 0 {
		if lb <= max {
			return lb
		}
		return max + 1
	}

	prev2 := make([]int, la+1) // row j-2, for the transposition case
	prev := make([]int, la+1)  // row j-1
	cur := make([]int, la+1)   // row j
	for i := 0; i <= la; i++ {
		prev[i] = i
	}

	for j := 1; j <= lb; j++ {
		cur[0] = j

		// Only cells within max of the diagonal can hold a value <= max.
		lo, hi := j-max, j+max
		if lo < 1 {
			lo = 1
		}
		if hi > la {
			hi = la
		}
		for i := 1; i < lo; i++ {
			cur[i] = max + 1
		}

		rowMin := max + 1
		for i := lo; i <= hi; i++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			d := prev[i] + 1 // deletion
			if v := cur[i-1] + 1; v < d {
				d = v // insertion
			}
			if v := prev[i-1] + cost; v < d {
				d = v // substitution
			}
			// Adjacent transposition: a[i-2:i] equals b[j-1], b[j-2] reversed.
			if i > 1 && j > 1 && a[i-1] == b[j-2] && a[i-2] == b[j-1] {
				if v := prev2[i-2] + 1; v < d {
					d = v
				}
			}
			cur[i] = d
			if d < rowMin {
				rowMin = d
			}
		}
		for i := hi + 1; i <= la; i++ {
			cur[i] = max + 1
		}

		// Every reachable cell in this row already exceeds the budget, and
		// distances never decrease going down, so no later row can recover.
		if rowMin > max {
			return max + 1
		}

		prev2, prev, cur = prev, cur, prev2
	}

	if prev[la] > max {
		return max + 1
	}
	return prev[la]
}

// MaxDistance is the edit budget allowed for a query of the given rune length.
// Short queries get a tight budget because almost everything is within two
// edits of a three-letter word; long queries can afford more slack.
func MaxDistance(queryLen int) int {
	switch {
	case queryLen <= 3:
		return 1
	case queryLen <= 7:
		return 2
	default:
		return 3
	}
}
