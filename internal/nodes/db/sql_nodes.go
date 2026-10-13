package dbnodes

import (
	"context"
	"fmt"

	_ "github.com/microsoft/go-mssqldb" // registers the "sqlserver" driver
	_ "github.com/sijms/go-ora/v2"      // registers the "oracle" driver

	"github.com/monoes/mono-agent/internal/workflow"
)

// sqlNode is a database/sql-backed node that only speaks the n8n-compatible
// operations (executeQuery, select, insert, update, upsert, delete,
// deleteTable). db.postgres and db.mysql additionally keep their legacy mode.
type sqlNode struct {
	nodeType string
	driver   string
	dialect  *sqlDialect
	// defaultOp is used when 'operation' is blank.
	defaultOp string
}

func (n *sqlNode) Type() string { return n.nodeType }

// PerItemConfigFields declares sqlPerItemFields (see its doc comment).
func (n *sqlNode) PerItemConfigFields() []string { return sqlPerItemFields }

func (n *sqlNode) Execute(ctx context.Context, input workflow.NodeInput, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	dsn := cfgString(config, "connection_string")
	if dsn == "" {
		return nil, fmt.Errorf("%s: 'connection_string' is required", n.nodeType)
	}
	if cfgString(config, "operation") == "" {
		cfg := make(map[string]interface{}, len(config)+1)
		for k, v := range config {
			cfg[k] = v
		}
		cfg["operation"] = n.defaultOp
		config = cfg
	}
	db, err := openPingedSQL(ctx, n.nodeType, n.driver, dsn)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return runSQLOps(ctx, n.nodeType, n.dialect, dbRunner{db}, input, config)
}

// NewMSSQLNode returns db.mssql (Microsoft SQL Server / Azure SQL).
// connection_string: sqlserver://user:pass@host:1433?database=db (ADO
// "server=...;user id=..." strings work too).
func NewMSSQLNode() workflow.NodeExecutor {
	return &sqlNode{nodeType: "db.mssql", driver: "sqlserver", dialect: mssqlDialect, defaultOp: "executeQuery"}
}

// NewOracleNode returns db.oracle (pure-Go go-ora driver, no Instant Client).
// connection_string: oracle://user:pass@host:1521/service_name
func NewOracleNode() workflow.NodeExecutor {
	return &sqlNode{nodeType: "db.oracle", driver: "oracle", dialect: oracleDialect, defaultOp: "executeQuery"}
}

// pgWireNode runs a Postgres-wire database through the Postgres driver with
// that engine's dialect.
type pgWireNode struct {
	nodeType string
	dialect  *sqlDialect
}

func (n *pgWireNode) Type() string                  { return n.nodeType }
func (n *pgWireNode) PerItemConfigFields() []string { return sqlPerItemFields }

func (n *pgWireNode) Execute(ctx context.Context, input workflow.NodeInput, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	if cfgString(config, "operation") == "" {
		cfg := make(map[string]interface{}, len(config)+1)
		for k, v := range config {
			cfg[k] = v
		}
		cfg["operation"] = "executeQuery"
		config = cfg
	}
	return executePostgresFamily(ctx, n.nodeType, n.dialect, input, config)
}

// NewTimescaleDBNode returns db.timescaledb (Postgres dialect).
func NewTimescaleDBNode() workflow.NodeExecutor {
	return &pgWireNode{nodeType: "db.timescaledb", dialect: postgresDialect}
}

// NewCrateDBNode returns db.cratedb (Postgres wire, CrateDB dialect).
func NewCrateDBNode() workflow.NodeExecutor {
	return &pgWireNode{nodeType: "db.cratedb", dialect: crateDialect}
}

// NewQuestDBNode returns db.questdb (Postgres wire, no RETURNING/upsert).
func NewQuestDBNode() workflow.NodeExecutor {
	return &pgWireNode{nodeType: "db.questdb", dialect: questDialect}
}
