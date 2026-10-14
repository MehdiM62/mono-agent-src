package main

import "github.com/monoes/mono-agent/internal/cli/clicore"

// Exit codes (see the exit-code table in AGENTS.md):
//
//	0 ok · 1 general error · 2 not-found · 3 invalid input / validation · 4 auth/connection
//
// The errors and exitCodeFor live in internal/cli/clicore; these are the CLI's
// old names for them.

// cliError is an error carrying the process exit code it should map to.
type cliError = clicore.Error

// Sentinel errors — wrap or construct with the errNotFound / errInvalidInput /
// errAuthConnection helpers below, or test with errors.As(&cliError).
var (
	ErrNotFound       = clicore.ErrNotFound
	ErrInvalidInput   = clicore.ErrInvalidInput
	ErrAuthConnection = clicore.ErrAuthConnection
)

func errNotFound(format string, a ...interface{}) error {
	return clicore.NotFound(format, a...)
}

func errInvalidInput(format string, a ...interface{}) error {
	return clicore.InvalidInput(format, a...)
}

func errAuthConnection(format string, a ...interface{}) error {
	return clicore.AuthConnection(format, a...)
}

// exitCodeFor maps a command error to the process exit code (clicore.ExitCodeFor).
func exitCodeFor(err error) int { return clicore.ExitCodeFor(err) }
