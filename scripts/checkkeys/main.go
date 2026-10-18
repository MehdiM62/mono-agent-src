package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/monoes/mono-agent/internal/checkkeys"
)

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

// run is the command line without the process. Its streams are the
// command's: the report on stdout, and on stderr why it doesn't exit clean,
// except for the keys a text report has already shown.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("checkkeys", flag.ContinueOnError)
	flags.SetOutput(stderr)
	asJSON := flags.Bool("json", false, "print the report as JSON")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return checkkeys.ExitClean
		}
		return checkkeys.ExitBadInput
	}
	rep, err := checkkeys.Check(stdin, flags.Args())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	rep.Write(stdout, *asJSON)
	code := rep.ExitCode()
	if code != checkkeys.ExitClean && (*asJSON || code != checkkeys.ExitUndeclared) {
		fmt.Fprintln(stderr, rep.Reason())
	}
	return code
}
