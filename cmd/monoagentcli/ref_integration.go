package main

// The `integration` entries of `ref commands`: declarative API packages
// (internal/apipkg) that run as app.<id> nodes.
func init() {
	cliDocs = append(cliDocs,
		cmdDoc{
			Name:  "integration list",
			Short: "List the API packages: the built-in catalog and ~/.monoagent/apipkgs/<id>/package.json",
			Usage: "monoagentcli integration list",
			Examples: []string{
				"monoagentcli integration list",
				"monoagentcli --json integration list",
				"monoagentcli node schema app.todoist",
			},
		},
		cmdDoc{
			Name:  "integration show",
			Short: "Show one API package: node type, connection, n8n types and every operation with its fields (* = required)",
			Usage: "monoagentcli integration show <id>",
			Examples: []string{
				"monoagentcli integration show todoist",
				"monoagentcli --json integration show todoist",
			},
		},
		cmdDoc{
			Name:  "integration import-openapi",
			Short: "Convert an OpenAPI 3.x / Swagger 2.0 document (file or URL, JSON or YAML) into an API package; installs it unless --out",
			Usage: "monoagentcli integration import-openapi <file|url> [--id <id>] [--ops <tag,…>] [--out <dir>]",
			Flags: `  --id string   Package id ([a-z0-9_]+; default: from info.title)
  --ops string  Only operations with one of these tags (comma-separated)
  --out string  Write <dir>/<id>.json instead of installing the package`,
			Examples: []string{
				"monoagentcli integration import-openapi ./petstore.yaml --id petstore",
				"monoagentcli integration import-openapi https://example.com/openapi.json --ops pets --out ./pkgs",
			},
		},
		cmdDoc{
			Name:  "integration validate",
			Short: "Validate an API package file (apipkg.v1 schema and semantic checks); exit 3 with every problem listed",
			Usage: "monoagentcli integration validate <file>",
			Examples: []string{
				"monoagentcli integration validate internal/apipkg/catalog/todoist.json",
			},
		},
	)
}
