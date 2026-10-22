package aitask

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ContextHash is a hash of task taskID's context folder under cwd: every
// name and the bytes of every file (a link or device counts by name and
// kind, never followed). A folder that is not there hashes as empty. Anything
// added, removed or changed since submit changes it.
func ContextHash(cwd, taskID string) (string, error) {
	root := filepath.Join(cwd, filepath.FromSlash(ContextDir), taskID)
	var lines []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == root && os.IsNotExist(err) {
				return filepath.SkipAll
			}
			return err
		}
		if p == root {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		switch {
		case d.IsDir():
			lines = append(lines, "d "+rel)
		case d.Type().IsRegular():
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(b)
			lines = append(lines, "f "+rel+" "+hex.EncodeToString(sum[:]))
		default:
			lines = append(lines, "x "+rel)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:]), nil
}

// CapsHash is a hash of what a task is allowed to be: its access level,
// runtime, unattended flag, the kind's definition (prompt, schema, defaults)
// and the launcher's policy fingerprint (the coder settings in force).
func CapsHash(t Task, k Kind, policy string) string {
	schema, _ := json.Marshal(k.Schema)
	doc, _ := json.Marshal([]any{t.Kind, t.Access, t.Runtime, t.Unattended, FullAccess(t.Access), k.UnattendedOK,
		k.DefaultAccess, k.PromptTemplate, string(schema), policy})
	sum := sha256.Sum256(doc)
	return hex.EncodeToString(sum[:])
}
