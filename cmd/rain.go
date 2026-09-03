// Package cmd rain.go
//
// The word list, falling.
//
// It is the matrix code rain with the alphabet replaced by the words dict
// already carries, so each stream spells one and a column reads downward.
// The rain is termanim's; what dict supplies is the vocabulary.
package cmd

import (
	"os"
	"time"

	"github.com/gdamore/tcell/v3"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/0magnet/termanim/canvas"
	"github.com/0magnet/termanim/matrix"
	"github.com/0magnet/termanim/matrix/backdrop"

	"github.com/0magnet/dict/data"
)

// rainSeed is the seed to use for one run.
//
// Zero means "pick one", not "use zero": matrix.New(0) is documented as a
// FIXED sequence for repeatable tests, and rand.NewSource(0) samples the
// same words every time, so passing zero through gave the same rain on
// every run.
//
// A --seed reproduces a STILL exactly. It does not reproduce an animation
// frame for frame: the simulation is stepped from elapsed wall-clock time,
// so the same seed lays down the same streams but a slower machine lands
// its frames between different steps. Same rain, not the same recording.
func rainSeed() int64 {
	if rainSeedFlag != 0 {
		return rainSeedFlag
	}
	// Nanoseconds, so two runs in the same second still differ.
	return time.Now().UnixNano()
}

// rainVocabulary returns the words to rain: all of them.
//
// It used to sample a few hundred and filter them by length and shape, on
// the grounds that a short word repeats within a trail and reads as a
// stutter. FreshWords removed that reason — a stream now takes a NEW word
// each time it finishes one, so a three-letter word is followed by a
// different word rather than by itself — and with the reason gone the
// limit was just a limit. The whole list falls.
//
// seed is unused now and kept so the signature still says what the frame
// depends on.
func rainVocabulary(_ int64) []string {
	all, err := data.Words()
	if err != nil {
		return nil
	}
	return all
}

// RainStill returns one frame of the word rain, as text with escapes.
//
// Nothing is written over it. The still exists to be looked at, and as
// the thing the help backdrop composites text onto.
func RainStill(width, height, gap int, seed int64, force bool) string {
	if height <= 0 {
		height = terminalHeight()
	}
	return backdrop.Render("", backdrop.Options{
		Width:   width,
		Height:  height,
		Seed:    seed,
		Force:   force,
		Words:   rainVocabulary(seed),
		WordGap: gap,
		// A stream that says one word down the whole column reads as a
		// pattern; the dictionary has 123,985 of them and can spare more.
		FreshWords: true,
	})
}

// terminalHeight is the rows to fill, less one so the shell prompt that
// follows does not scroll the top row away.
func terminalHeight() int {
	if _, h, err := term.GetSize(int(os.Stdout.Fd())); err == nil && h > 4 {
		return h - 1
	}
	return 24
}

// rainCmd is the still, as a command rather than a flag.
//
// The leading colon is what makes it safe. Every other subcommand name
// here is an ordinary English word — languages, licenses, sources are all
// in the list — and cobra matches a subcommand before it reaches the
// query, so naming one `rain` would make the word "rain" the one word
// this dictionary could not look up. Nothing in a word list starts with a
// colon, so the sigil is a namespace that cannot collide with the thing
// the program is for.
var rainCmd = &cobra.Command{
	Use:   ":rain",
	Short: "a still frame of the word list, falling",
	Long: "A still frame of the matrix code rain with the word list as its\n" +
		"alphabet: each stream spells a word, so a column reads downward.\n\n" +
		"It renders even when stdout is not a terminal. A backdrop behind\n" +
		"help text withholds itself when piped, because `--help | less` wants\n" +
		"plain text; here the rain is the output.",
	Args: cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		seed := rainSeed()
		if rainTUI {
			return runRainTUI(rainGap, rainSpeed, rainGlow_, seed)
		}
		printf("%s", RainStill(host.Width, host.Height, rainGap, seed, true))
		return nil
	},
}

// rainGap is the blank cells between repeats of a word in one stream.
//
// Zero, which is the default here and in termanim, runs the repeats
// together the way the glyph rain does. A gap makes each repeat legible
// as a separate word, which is easier to read and less like the film.
var (
	rainGap      int
	rainTUI      bool
	rainSpeed    float64
	rainGlow_    bool
	rainSeedFlag int64
)

func init() {
	rainCmd.Flags().IntVar(&rainGap, "gap", 0, "blank cells between one word in a stream and the next")
	rainCmd.Flags().BoolVar(&rainTUI, "tui", false, "animate it full-screen instead of printing one frame (Esc or Ctrl-C to leave)")
	rainCmd.Flags().Float64Var(&rainSpeed, "speed", 1, "how fast the rain falls, as a multiple of its usual rate (--tui only)")
	rainCmd.Flags().Int64Var(&rainSeedFlag, "seed", 0, "reproduce a still exactly; 0 picks a new one each time")
	rainCmd.Flags().BoolVar(&rainGlow_, "glow", false, "hold lit any word that appears reading ACROSS the rain (--tui only)")
}

// runRainTUI animates the rain full-screen until Esc or Ctrl-C.
//
// The still and the animation are the same simulation: the still is one
// frame of it, taken after enough steps that the screen is full. What
// differs is only who drives the clock — Advance for a still, the canvas
// frame loop here.
func runRainTUI(gap int, speed float64, glow bool, seed int64) error {
	screen, err := tcell.NewScreen()
	if err != nil {
		return err
	}
	if err := screen.Init(); err != nil {
		return err
	}
	// Fini restores the terminal. Without it a quit leaves the alternate
	// screen up and the cursor hidden.
	defer screen.Fini()

	m := matrix.New(seed)
	m.Words = rainVocabulary(seed)
	m.WordGap = gap
	m.FreshWords = true
	// StepRate is simulation steps per second and defaults to 30, which is
	// the speed the rain was tuned at. --speed scales it.
	if speed > 0 {
		m.StepRate *= speed
	}
	if glow {
		all, err := data.Words()
		if err != nil {
			return err
		}
		return canvas.RunCells(screen, newRainGlow(m, all), canvas.Options{})
	}
	return canvas.RunCells(screen, m, canvas.Options{})
}
