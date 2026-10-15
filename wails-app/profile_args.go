package main

// scopedCLIArgs is the argv prefix for a monoagentcli command that acts on a
// profile: `--profile <id> --json` followed by args. The CLI's profile gate
// refuses a scoped command that does not name its profile, so every scoped
// call the app makes builds its argv here. An empty profile is errNoProfile:
// there is no default to fall back to. Machine-wide commands (profile
// list/create, account, agent, config, db, node, update, ...) use
// unscopedCLIArgs.
func scopedCLIArgs(profileID string, args ...string) ([]string, error) {
	if profileID == "" {
		return nil, errNoProfile
	}
	return append([]string{"--profile", profileID, "--json"}, args...), nil
}

// unscopedCLIArgs is the argv of a machine-wide command: `--json` and args.
func unscopedCLIArgs(args ...string) []string {
	return append([]string{"--json"}, args...)
}

// orgCLIRunArgs is the argv for `org <sub...>` run on a profile, with the
// org project root when the app has one. No --json: the org subcommands
// print JSON on their own.
func orgCLIRunArgs(profileID, projectRoot string, sub ...string) ([]string, error) {
	if profileID == "" {
		return nil, errNoProfile
	}
	full := []string{"--profile", profileID, "org"}
	if projectRoot != "" {
		full = append(full, "--project", projectRoot)
	}
	return append(full, sub...), nil
}
