// Command checkkeys is `monoagentcli workflow check-keys` for a source
// checkout and no sign-in: the same check (internal/checkkeys), output and
// exit codes, reading only the files it is given and the built-in catalog,
// and writing nothing under HOME.
//
//	go run ./scripts/checkkeys [--json] <file.json…|->
//	exit 0 clean, 1 an app node sends a key its op doesn't declare, 3 a file that can't be read or isn't JSON
package main
