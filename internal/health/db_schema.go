package health

import (
	"context"
	"fmt"
)

// CheckDBSchema warns when the database was migrated by a newer monoagent
// than the running one (an older GUI-bundled CLI, a backup binary): it keeps
// working, but may not understand newer data.
const CheckDBSchema = "core.db.schema"

func dbSchemaChecks() []Check {
	return []Check{{ID: CheckDBSchema, Group: GroupCore, Title: "Database schema version",
		DependsOn: []string{CheckDB}, Run: checkDBSchema}}
}

func checkDBSchema(ctx context.Context, env *Env) Result {
	if env.SchemaAhead == nil {
		return Result{Status: StatusSkip, Summary: "not available"}
	}
	dbV, binV, ahead, err := env.SchemaAhead(ctx)
	if err != nil {
		return Result{Status: StatusWarn, Summary: "cannot read schema version", Detail: err.Error()}
	}
	if ahead {
		return Result{Status: StatusWarn,
			Summary: fmt.Sprintf("database was migrated by a newer monoagent (v%03d > v%03d)", dbV, binV),
			Detail:  "this monoagentcli is older than the database; update it with: monoagentcli update"}
	}
	return Result{Status: StatusOK, Summary: fmt.Sprintf("v%03d", dbV)}
}
