package main

// coderDefaultAccessLevels are the levels `coder set --default-access` takes.
var coderDefaultAccessLevels = map[string]bool{"scoped": true, "coding": true, "workspace": true, "full": true}

// CoderSetDefaultAccess is `coder set --default-access <level>`: the access a
// new chat starts at when coder mode is on and a runtime is ready. The CLI
// refuses anyone but the user; this window is the operator. Returns coder
// status, whose defaultAccess CoderStatus also reads back.
func (a *App) CoderSetDefaultAccess(level string) string {
	if err := a.signedOut(); err != nil {
		return loginRequiredJSON(err)
	}
	if !coderDefaultAccessLevels[level] {
		return `{"error":"unknown access level","code":"invalid_access"}`
	}
	return a.jsonResult("coder", "set", "--default-access", level)
}
