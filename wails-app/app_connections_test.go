package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlatformNamesComeFromConnectListAll(t *testing.T) {
	dir := t.TempDir()
	argsLog := filepath.Join(dir, "args.log")
	script := `#!/bin/sh
echo "$*" >> '` + argsLog + `'
echo '[{"ID":"generic_api","Name":"Generic API Key","Category":"generic"},{"ID":"gmail","Name":"Gmail"}]'
`
	bin := filepath.Join(dir, "monoagentcli")
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MONOAGENTCLI_BIN", bin)
	a := newTestApp(t)
	a.ctx = context.Background()

	names, err := a.PlatformNames()
	if err != nil || names["generic_api"] != "Generic API Key" || names["gmail"] != "Gmail" {
		t.Fatalf("PlatformNames = %v, %v", names, err)
	}
	log, _ := os.ReadFile(argsLog)
	if !strings.Contains(string(log), "connect list --all") {
		t.Errorf("argv log:\n%s", log)
	}
}
