package main

// scopedCLIArgs is the argv prefix for a monoagentcli command that acts on a
// profile: `--profile <id> --json` followed by args. The CLI's profile gate
// refuses (and, in warn mode, complains about) a scoped command that does not
// name its profile, so every scoped call the app makes builds its argv here.
// Global commands (account, agent, config, db, node, update, ...) do not need
// it.
func scopedCLIArgs(profileID string, args ...string) []string {
	return append([]string{"--profile", profileID, "--json"}, args...)
}

// orgCLIRunArgs is the argv for `org <sub...>` run on a profile, with the
// org project root when the app has one. No --json: the org subcommands
// print JSON on their own.
func orgCLIRunArgs(profileID, projectRoot string, sub ...string) []string {
	full := []string{"--profile", profileID, "org"}
	if projectRoot != "" {
		full = append(full, "--project", projectRoot)
	}
	return append(full, sub...)
}
