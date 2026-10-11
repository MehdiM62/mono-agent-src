package main

// The sandbox fallback setting (Settings -> "Agent sandbox"): what an agent turn does when
// its runtime has no sandbox for it. Like app_coder.go this only shells out to
// `monoagentcli sandbox fallback --json` and returns its stdout; which values exist, what is
// saved and whether monomind can apply it are the CLI's to say.

// SandboxFallbackGet returns `sandbox fallback`: the effective value, where it comes from
// (env | saved | default), the accepted values and whether monomind supports the setting.
func (a *App) SandboxFallbackGet() string { return a.jsonResult("sandbox", "fallback") }

// SandboxFallbackSet saves value and returns the same document. The CLI validates it; the
// flag is attached to its value so that no value can be read as another flag.
func (a *App) SandboxFallbackSet(value string) string {
	return a.jsonResult("sandbox", "fallback", "--set="+value)
}
