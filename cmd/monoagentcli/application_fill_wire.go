package main

import "github.com/monoes/mono-agent/internal/cli/application"

// `application fill start` submits its task as the user's own `aitask submit`; this hands it the
// operator check and the --db-path of this process.
func wireApplicationFillStart(cfg *globalConfig) {
	application.FillStartWiring.RequireOperator = requireOperator
	application.FillStartWiring.DBPath = func() string { return cfg.DBPath }
}
