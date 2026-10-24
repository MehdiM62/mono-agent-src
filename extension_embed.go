// Package monoagent holds the files that ship inside the monoagentcli binary
// from outside the Go packages.
package monoagent

import "embed"

// ChromeExtension is the MonoAgent Bridge browser extension, the same folder
// the release zips. `monoagentcli extension install` unpacks it to
// ~/.monoagent/extension, where the browser loads it unpacked.
//
//go:embed chrome-extension
var ChromeExtension embed.FS

// ExtensionNotice is the NOTICE file the release zip carries beside the
// extension; `extension install` writes it into the unpacked folder too.
//
//go:embed NOTICE
var ExtensionNotice []byte
