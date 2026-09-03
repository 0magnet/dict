package cmd

import (
	"testing"

	"github.com/0magnet/dict/match"
)

// newTestUI builds a ui without a terminal. move() and up() only touch the
// result list and the row count, so they can be exercised directly.
func newTestUI(n int, reverse bool) *ui {
	results := make([]match.Result, n)
	for i := range results {
		results[i] = match.Result{Word: string(rune('a' + i))}
	}
	return &ui{results: results, rows: 12, reverse: reverse}
}

// TestArrowsFollowTheScreen pins the direction the arrow keys move in each
// layout. The arrows follow the screen, not the list index: in the default
// layout the list is drawn upward from the prompt, so pressing Up moves later
// into the results. Getting this backwards makes Up appear to do nothing,
// since the selection starts on the first result.
func TestArrowsFollowTheScreen(t *testing.T) {
	t.Run("default layout: up moves later into the list", func(t *testing.T) {
		u := newTestUI(10, false)
		if u.up() != 1 {
			t.Fatalf("up() = %d, want 1", u.up())
		}
		u.move(u.up())
		if u.sel != 1 {
			t.Errorf("after Up, sel = %d, want 1", u.sel)
		}
		u.move(u.up())
		u.move(u.up())
		if u.sel != 3 {
			t.Errorf("after three Ups, sel = %d, want 3", u.sel)
		}
		u.move(-u.up())
		if u.sel != 2 {
			t.Errorf("after a Down, sel = %d, want 2", u.sel)
		}
		// Down at the first result must not wrap or go negative.
		u.sel = 0
		u.move(-u.up())
		if u.sel != 0 {
			t.Errorf("Down at the best match moved to %d, want 0", u.sel)
		}
	})

	t.Run("reverse layout: up moves earlier, as usual", func(t *testing.T) {
		u := newTestUI(10, true)
		if u.up() != -1 {
			t.Fatalf("up() = %d, want -1", u.up())
		}
		u.move(u.up())
		if u.sel != 0 {
			t.Errorf("Up at the top moved to %d, want 0", u.sel)
		}
		u.move(-u.up())
		u.move(-u.up())
		if u.sel != 2 {
			t.Errorf("after two Downs, sel = %d, want 2", u.sel)
		}
	})
}

func TestMoveClampsAndScrolls(t *testing.T) {
	u := newTestUI(100, false)
	u.move(1000)
	if u.sel != 99 {
		t.Errorf("moving past the end left sel = %d, want 99", u.sel)
	}
	// The window must have followed the selection.
	if u.sel < u.off || u.sel >= u.off+u.listRows() {
		t.Errorf("selection %d outside the visible window [%d,%d)", u.sel, u.off, u.off+u.listRows())
	}
	u.move(-1000)
	if u.sel != 0 || u.off != 0 {
		t.Errorf("moving back gave sel=%d off=%d, want 0 and 0", u.sel, u.off)
	}
}

func TestMoveOnEmptyResults(t *testing.T) {
	u := newTestUI(0, false)
	u.move(u.up()) // must not panic or leave sel out of range
	if u.sel != 0 {
		t.Errorf("sel = %d on an empty list, want 0", u.sel)
	}
}
