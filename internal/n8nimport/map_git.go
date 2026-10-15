package n8nimport

func init() {
	Register("n8n-nodes-base.git", mapGit)
}

// gitTextOps return git's text output in stdout where n8n returned parsed
// JSON (commits, status, config entries).
var gitTextOps = map[string]bool{"log": true, "reflog": true, "status": true, "listConfig": true}

// mapGit runs the git CLI through system.execute_command. Every value is
// its own argv entry (no shell), so paths, messages and branch names need
// no quoting.
func mapGit(c *MapContext) Mapping {
	op := c.Literal("operation", "log")
	repo := c.Str("repositoryPath", "")
	args := []interface{}{}
	add := func(a ...interface{}) { args = append(args, a...) }
	paths := func(param string) {
		for _, p := range commaList(c.Literal(param, "")) {
			add(p)
		}
	}
	limit := func() {
		if !c.Bool("returnAll", false) {
			add("-n", c.Str("limit", "100"))
		}
	}
	switch op {
	case "add":
		add("add", "--")
		paths("pathsToAdd")
	case "addConfig":
		add("config")
		if c.Literal("options.mode", "set") == "append" {
			add("--add")
		}
		add(c.Str("key", ""), c.Str("value", ""))
	case "clone":
		add("clone", "--", c.Str("sourceRepository", ""), repo)
		repo = ""
	case "commit":
		if c.Has("options.branch") {
			c.Warn("options.branch", c.Raw("options.branch"), "the branch is not switched before the commit; add a Switch Branch step")
		}
		add("commit", "-m", c.Str("message", ""))
		if c.Has("options.pathsToAdd") {
			add("--")
			paths("options.pathsToAdd")
		}
	case "fetch", "pull":
		add(op)
	case "listConfig":
		add("config", "--list")
	case "log":
		add("log")
		limit()
		if f := c.Str("options.file", ""); f != "" {
			add("--", f)
		}
	case "push":
		add("push")
		if t := c.Str("options.targetRepository", ""); t != "" {
			add(t)
			if b := c.Str("options.branch", ""); b != "" {
				add(b)
			}
		} else {
			c.Ignore("options.branch")
		}
	case "pushTags":
		add("push", "--tags")
	case "reflog":
		add("reflog")
		limit()
		if r := c.Str("options.reference", ""); r != "" {
			add(r)
		}
	case "status":
		add("status")
	case "switchBranch":
		return gitSwitchBranch(c, repo)
	case "tag":
		add("tag", c.Str("name", ""))
	default:
		return c.OperationUnsupported("", op)
	}
	if c.Literal("authentication", "none") == "gitPassword" {
		c.Warn("authentication", "gitPassword", "credentials are not added to the repository URL: use an SSH key or a git credential helper on this machine")
	}
	if gitTextOps[op] {
		c.Warn("operation", op, "the output is git's text (stdout), not n8n's parsed JSON")
	}
	cfg := map[string]interface{}{"command": "git", "args": args}
	if repo != "" {
		cfg["working_dir"] = repo
	}
	return Mapping{Type: "system.execute_command", Config: cfg, EntrySuffix: " (git)", Chain: []ChainStep{exitCodeStep}}
}

// gitSwitchBranch checks the branch out and, as n8n does, creates it when
// the checkout fails. A shell runs the fallback; the values are passed as
// positional arguments, never spliced into the script.
func gitSwitchBranch(c *MapContext, repo string) Mapping {
	force := ""
	if c.Bool("options.force", false) {
		force = "-f "
	}
	script := "case \"$1\" in -*) echo \"branch name must not start with -\" >&2; exit 2;; esac\n" +
		"if ! git checkout " + force + "\"$1\" 2>/dev/null; then\n"
	if c.Bool("options.createBranch", true) {
		script += "  if [ -n \"$2\" ]; then git checkout -b \"$1\" \"$2\"; else git checkout -b \"$1\"; fi || exit $?\n"
	} else {
		script += "  git checkout " + force + "\"$1\" || exit $?\n"
	}
	script += "fi\n"
	if c.Bool("options.setUpstream", false) {
		// n8n treats a failed upstream setup as non-fatal.
		script += "git config \"branch.$1.remote\" \"$3\" && git config \"branch.$1.merge\" \"refs/heads/$1\" || true\n"
	}
	cfg := map[string]interface{}{"command": "sh", "args": []interface{}{"-c", script, "git-switch",
		c.Str("branchName", ""), c.Str("options.startPoint", ""), c.Str("options.remoteName", "origin")}}
	if repo != "" {
		cfg["working_dir"] = repo
	}
	return Mapping{Type: "system.execute_command", Config: cfg, EntrySuffix: " (git)", Chain: []ChainStep{exitCodeStep}}
}
