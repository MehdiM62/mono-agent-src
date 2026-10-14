package service

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// schemaDriftDirs are the node packages whose Execute config reads must be
// visible in the node's schema (relative to this package's directory).
var schemaDriftDirs = []string{"../service", "../comm", "../db", "../data", "../system"}

// credentialConfigKeys are filled in from the saved connection by the
// credential layer, not typed into the node form, so a schema need not
// list them.
var credentialConfigKeys = map[string]bool{
	"access_token": true, "api_key": true, "token": true, "secret_key": true,
	"bot_token": true, "instance_url": true, "username": true, "password": true,
	"app_password": true, "identifier": true, "domain": true, "email": true,
	"base_url": true, "api_token": true, "credential": true,
	"credential_id": true, "refresh_token": true,
}

// schemaDriftAllowed lists, per node type, config keys Execute reads that
// are intentionally absent from the schema. Each entry says why. Add to it
// only with a reason; a missing form field is otherwise a bug.
var schemaDriftAllowed = map[string]map[string]string{
	// Undocumented aliases of a field the form already shows.
	"data.spreadsheet":       {"sheet": "alias of sheet_name"},
	"data.crypto":            {"key": "alias of secret"},
	"service.google_sheets":  {"sheet": "alias of sheet_name"},
	"system.execute_command": {"timeout_seconds": "alias of timeout"},
	"comm.telegram":          {"text": "legacy alias of message"},
	"comm.email_send":        {"html": "legacy boolean; body_type replaces it"},
	// Connection fields the engine merges into config from credential_id.
	"db.snowflake": {"account": "connection field", "user": "connection field", "database": "connection field",
		"warehouse": "connection field", "role": "connection field", "private_key": "connection field",
		"token_type": "connection field", "timeout_seconds": "connection option"},
	// n8n's RabbitMQ node has one operation; an imported deleteMessage
	// config is refused with a clear error instead of silently sending.
	"queue.rabbitmq": {"operation": "n8n import guard (only sendMessage)"},
}

// TestExecuteConfigKeysAreInSchema fails when a service/comm/db/data/system node
// reads a config key (config["k"], or helper(config, "k")) that its schema
// does not expose and no allowlist names — the form would hide a setting
// the node supports.
func TestExecuteConfigKeysAreInSchema(t *testing.T) {
	reads := map[string]map[string]bool{}
	for _, dir := range schemaDriftDirs {
		for typ, keys := range configKeysReadIn(t, dir) {
			if reads[typ] == nil {
				reads[typ] = map[string]bool{}
			}
			for k := range keys {
				reads[typ][k] = true
			}
		}
	}
	if len(reads) < 20 {
		t.Fatalf("found config reads for only %d node types — the scan is broken", len(reads))
	}
	types := make([]string, 0, len(reads))
	for typ := range reads {
		types = append(types, typ)
	}
	sort.Strings(types)
	for _, typ := range types {
		if _, deprecated := workflow.IsDeprecatedNodeType(typ); deprecated {
			continue
		}
		schema, err := workflow.LoadDefaultSchema(typ)
		if err != nil {
			t.Errorf("%s: load schema: %v", typ, err)
			continue
		}
		inSchema := map[string]bool{}
		for _, f := range schema.Fields {
			inSchema[f.Key] = true
		}
		var missing []string
		for k := range reads[typ] {
			if inSchema[k] || credentialConfigKeys[k] || schemaDriftAllowed[typ][k] != "" {
				continue
			}
			missing = append(missing, k)
		}
		sort.Strings(missing)
		for _, k := range missing {
			t.Errorf("%s: Execute reads config key %q but the schema has no such field — add it to the schema (or to schemaDriftAllowed with a reason)", typ, k)
		}
	}
}

// configKeysReadIn parses the package in dir and returns, per node type,
// the literal config keys its code reads. A method is attributed to its
// receiver's node type (from the receiver's Type() method); a plain
// function to the file's node type when the file defines exactly one.
func configKeysReadIn(t *testing.T, dir string) map[string]map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var files []*ast.File
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		files = append(files, f)
	}

	typeOfRecv := map[string]string{}
	for _, f := range files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Name.Name != "Type" || fd.Recv == nil || fd.Body == nil || len(fd.Body.List) != 1 {
				continue
			}
			ret, ok := fd.Body.List[0].(*ast.ReturnStmt)
			if !ok || len(ret.Results) != 1 {
				continue
			}
			if lit, ok := ret.Results[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
				typ, _ := strconv.Unquote(lit.Value)
				typeOfRecv[recvName(fd)] = typ
			}
		}
	}

	out := map[string]map[string]bool{}
	for _, f := range files {
		fileTypes := map[string]bool{}
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv != nil && typeOfRecv[recvName(fd)] != "" {
				fileTypes[typeOfRecv[recvName(fd)]] = true
			}
		}
		fileType := ""
		if len(fileTypes) == 1 {
			for typ := range fileTypes {
				fileType = typ
			}
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			owner := fileType
			if fd.Recv != nil && typeOfRecv[recvName(fd)] != "" {
				owner = typeOfRecv[recvName(fd)]
			}
			if owner == "" {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if k, ok := literalConfigKey(n); ok {
					if out[owner] == nil {
						out[owner] = map[string]bool{}
					}
					out[owner][k] = true
				}
				return true
			})
		}
	}
	return out
}

// literalConfigKey matches config["k"] and fn(config, "k").
func literalConfigKey(n ast.Node) (string, bool) {
	var x, key ast.Expr
	switch e := n.(type) {
	case *ast.IndexExpr:
		x, key = e.X, e.Index
	case *ast.CallExpr:
		if len(e.Args) < 2 {
			return "", false
		}
		// credentialScopeCheck(config, "gmail") names a scope group, not a key.
		if fn, ok := e.Fun.(*ast.Ident); ok && notKeyReaders[fn.Name] {
			return "", false
		}
		x, key = e.Args[0], e.Args[1]
	default:
		return "", false
	}
	if id, ok := x.(*ast.Ident); !ok || id.Name != "config" {
		return "", false
	}
	lit, ok := key.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	k, err := strconv.Unquote(lit.Value)
	return k, err == nil
}

func recvName(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return ""
	}
	expr := fd.Recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if id, ok := expr.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// notKeyReaders are functions called as f(config, "x") whose string is not
// a config key.
var notKeyReaders = map[string]bool{"credentialScopeCheck": true}
