package interactive

import (
	"os"

	"golang.org/x/term"
)

// HasInteractiveStdin reports whether stdin can drive a huh prompt. Callers use
// it before entering a picker so they can degrade gracefully when there is no
// TTY (e.g. CI) — huh's accessible mode (TERM=dumb) reads answers from stdin,
// so a real TTY isn't required there; everything else needs one.
func HasInteractiveStdin() bool {
	if os.Getenv("TERM") == "dumb" {
		return true
	}

	return term.IsTerminal(int(os.Stdin.Fd()))
}
