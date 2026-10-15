package clicore

import (
	"os"

	"golang.org/x/term"
)

// StdinIsTerminal reports whether a person can answer on stdin. A character
// device is not enough: /dev/null is one, and a prompt read from it gets
// end-of-file, which setup took as "no" while looking like a normal run.
func StdinIsTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}
