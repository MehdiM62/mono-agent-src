// Package orgsign checks monomind 2.21's operator signatures on org
// definitions (monomind#502, orgrt/org-signature.ts) so mono-agent can keep
// them intact without defeating them: a write mono-agent makes to an org
// file is re-signed only when the file verified before the write and the
// document came from exactly those bytes (guard.go); anything else is left
// for the operator to review and sign.
//
// Verification is done here, in Go, because monomind has no
// machine-readable verify command: `org status --format json` carries no
// signature state and `org sign` without --yes only prints a human review.
// This package reimplements the signed projection, its hash and the HMAC
// check against the sidecar with the operator key. It only ever reads the
// operator dir, except that Withdraw deletes a signature sidecar (and its
// projection) that turned out to cover bytes mono-agent did not write.
// Signing is always monomind's (`monomind org sign --yes`).
// Where this code cannot reproduce monomind's hash with certainty (an
// instructions_file it would refuse to read), the answer is StateUnknown,
// which never leads to a signature.
package orgsign

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Signature states: monomind's OrgSignatureReason, plus "signed" and
// "unknown" (this package could not decide; treated as not verified).
const (
	StateSigned       = "signed"
	StateUnsigned     = "unsigned"
	StateChanged      = "changed"
	StateInvalid      = "invalid-signature"
	StateForbiddenKey = "forbidden-key"
	StateUnknown      = "unknown"
	// StateInvalidDefinition is monomind's --check "invalid": the JSON is
	// unreadable or not a valid org. Not a signature problem.
	StateInvalidDefinition = "invalid-definition"
)

// Status is one org definition's signature state.
type Status struct {
	State  string `json:"state"`
	Detail string `json:"detail,omitempty"`
}

// OK reports whether the definition verified.
func (s Status) OK() bool { return s.State == StateSigned }

// Refused reports whether monomind 2.21 would refuse to start or reload
// the org for this state. StateUnknown is not a refusal: monomind decides.
func (s Status) Refused() bool {
	switch s.State {
	case StateUnsigned, StateChanged, StateInvalid, StateForbiddenKey:
		return true
	}
	return false
}

var (
	unsignedOrgFields  = map[string]bool{"goal": true, "status": true}
	unsignedRoleFields = map[string]bool{"title": true, "responsibilities": true, "ui": true}
	forbiddenKeys      = map[string]bool{"__proto__": true, "constructor": true, "prototype": true}
	// monomind's assertSafeOrgName.
	safeOrgName = regexp.MustCompile(`(?i)^[a-z0-9][a-z0-9_-]*$`)
	// file-roots.ts DASHBOARD_CREDENTIAL.
	dashboardCredential = regexp.MustCompile(`^dashboard-token(-\d+)?$`)
)

// errUnknown marks a hash this package can't reproduce with certainty.
var errUnknown = errors.New("cannot reproduce monomind's hash for this definition")

// OperatorDir is monomind's defaultOperatorDir() for a monomind run in
// project root: where the operator key and the signature sidecars live. A
// relative MONOMIND_ORGRT_OPERATOR_DIR is taken against root, as monomind
// takes it against its cwd, which mono-agent sets to root.
func OperatorDir(root string) string {
	if d := os.Getenv("MONOMIND_ORGRT_OPERATOR_DIR"); d != "" {
		if filepath.IsAbs(d) {
			return d
		}
		if abs, err := filepath.Abs(filepath.Join(root, d)); err == nil {
			return abs
		}
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".monomind", "orgrt-operator")
}

// realRoot is monomind's projectRoot(): the real path, else the absolute one.
func realRoot(root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	if r, err := filepath.EvalSymlinks(abs); err == nil {
		return r
	}
	return abs
}

// SignaturePath is where monomind keeps org's signature for project root.
func SignaturePath(root, org string) (string, error) {
	if !safeOrgName.MatchString(org) {
		return "", fmt.Errorf("invalid org name: %s", org)
	}
	sum := sha256.Sum256([]byte(realRoot(root)))
	return filepath.Join(OperatorDir(root), "org-signatures", hex.EncodeToString(sum[:])[:24], org+".json"), nil
}

// readRecord reads a sidecar: (nil, "") when there is none, or a problem
// when it can't be trusted (untrustedFileReason).
func readRecord(path string) (map[string]interface{}, string) {
	st, err := os.Lstat(path)
	if err != nil {
		return nil, ""
	}
	if bad := untrusted(st, "signature "+path, false); bad != "" {
		return nil, bad
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Sprintf("signature %s is unreadable (%v)", path, err)
	}
	v, err := parseJSON(b)
	if err != nil {
		return nil, fmt.Sprintf("signature %s is unreadable (%v)", path, err)
	}
	rec, ok := v.(map[string]interface{})
	if !ok {
		return nil, fmt.Sprintf("signature %s is not an object", path)
	}
	return rec, ""
}

// untrusted is access-grant-key.ts's untrustedFileReason: a symlink, the
// wrong kind of file, another user's, or open to group/other.
func untrusted(st fs.FileInfo, what string, dir bool) string {
	if st.Mode()&fs.ModeSymlink != 0 {
		return what + " is a symlink"
	}
	if dir && !st.IsDir() {
		return what + " is not a directory"
	}
	if !dir && !st.Mode().IsRegular() {
		return what + " is not a regular file"
	}
	if msg := foreignOwner(st, what); msg != "" {
		return msg
	}
	if !dir && looseMode(st) {
		return fmt.Sprintf("%s has mode %o (must be 600)", what, st.Mode().Perm())
	}
	return ""
}

// errKeyUnreadable: the key is there and trusted, but this process can't
// read it (monomind can), so this package has no verdict.
type errKeyUnreadable struct{ msg string }

func (e *errKeyUnreadable) Error() string { return e.msg }

// loadKey reads the operator key the way loadOperatorKey does (never
// creating it). The operator dir's own mode is not checked: monomind
// tightens a dir of ours to 0700 before trusting it, and the key and every
// sidecar must be ours and owner-only regardless.
func loadKey(dir string) ([]byte, error) {
	path := filepath.Join(dir, "full-access-grant.key")
	st, err := os.Lstat(path)
	if err != nil {
		return nil, errors.New("no operator key at " + path)
	}
	if dst, err := os.Lstat(dir); err == nil {
		if bad := untrusted(dst, "operator dir "+dir, true); bad != "" {
			return nil, errors.New(bad)
		}
	}
	if bad := untrusted(st, "operator key "+path, false); bad != "" {
		return nil, errors.New(bad)
	}
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, &errKeyUnreadable{fmt.Sprintf("operator key %s is unreadable (%v)", path, err)}
	}
	if len(key) == 0 {
		return nil, fmt.Errorf("operator key %s is empty", path)
	}
	return key, nil
}

// Describe says what the state means for the definition, as monomind's
// orgSignatureMessage does ("the definition has no operator signature").
func (s Status) Describe() string {
	switch s.State {
	case StateSigned:
		return "the definition is signed"
	case StateUnsigned:
		return "the definition has no operator signature"
	case StateChanged:
		return "the definition changed since the operator signed it"
	case StateForbiddenKey:
		return fmt.Sprintf("the definition holds a forbidden key (%s) — remove it", s.Detail)
	case StateInvalid:
		return fmt.Sprintf("the definition has an operator signature that does not verify (%s)", s.Detail)
	case StateInvalidDefinition:
		return "the definition is unreadable or not a valid org — see monoagentcli org validate"
	}
	return "the definition's signature could not be checked here (" + s.Detail + ")"
}

// Message is monomind's orgSignatureMessage, with mono-agent's sign command.
func Message(org string, st Status) string {
	if st.OK() {
		return fmt.Sprintf("org %s: %s", org, st.Describe())
	}
	return fmt.Sprintf("org %s: %s — review it, then sign it: monoagentcli org sign %s", org, st.Describe(), org)
}

// Verify checks raw (the bytes of <root>/.monomind/orgs/<org>.json)
// against the operator's signature, as monomind's verifyOrgDef does.
func Verify(root, org string, raw []byte) Status {
	v, err := parseOrgJSON(raw)
	if err != nil {
		return Status{State: StateUnknown, Detail: "unreadable JSON: " + err.Error()}
	}
	if p := forbiddenKeyPath(v, ""); p != "" {
		return Status{State: StateForbiddenKey, Detail: p}
	}
	if m := RoleContextMarker(); m != "" {
		return Status{State: StateUnknown, Detail: roleOperatorDirNote(m)}
	}
	path, err := SignaturePath(root, org)
	if err != nil {
		return Status{State: StateUnknown, Detail: err.Error()}
	}
	rec, problem := readRecord(path)
	if problem != "" {
		return Status{State: StateInvalid, Detail: problem}
	}
	if rec == nil {
		return Status{State: StateUnsigned}
	}
	dir := OperatorDir(root)
	key, err := loadKey(dir)
	var unreadable *errKeyUnreadable
	if errors.As(err, &unreadable) {
		return Status{State: StateUnknown, Detail: unreadable.msg}
	}
	if err != nil {
		return Status{State: StateInvalid, Detail: err.Error()}
	}
	sig, sigOK := rec["sig"].(string)
	hash, hashOK := rec["hash"].(string)
	if !sigOK || !hashOK {
		return Status{State: StateInvalid, Detail: "the signature file is malformed"}
	}
	at, atOK := jsString(rec["at"])
	if !atOK {
		return Status{State: StateUnknown, Detail: "the signature's timestamp is not a string"}
	}
	v0, hasV := rec["v"]
	input := hmacInput(v0, hasV, org, realRoot(root), hash, at)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(input))
	want := mac.Sum(nil)
	got, err := hex.DecodeString(evenHexPrefix(sig))
	if err != nil || !hmac.Equal(want, got) {
		return Status{State: StateInvalid, Detail: "the HMAC does not match the operator key"}
	}
	h, err := hashValue(v, readDigests(root))
	if err != nil {
		return Status{State: StateUnknown, Detail: err.Error()}
	}
	if h != hash {
		return Status{State: StateChanged}
	}
	return Status{State: StateSigned}
}

// SignedHash is the hash recorded in org's signature sidecar, without
// checking the HMAC — used right after monomind signs, to confirm what it
// signed is what mono-agent wrote.
func SignedHash(root, org string) (string, bool) {
	if RoleContextMarker() != "" {
		return "", false
	}
	path, err := SignaturePath(root, org)
	if err != nil {
		return "", false
	}
	rec, problem := readRecord(path)
	if rec == nil || problem != "" {
		return "", false
	}
	h, ok := rec["hash"].(string)
	return h, ok
}

// Withdraw removes org's signature (and the projection kept beside it), so
// the org is unsigned: used only when monomind signed bytes other than the
// ones mono-agent wrote.
func Withdraw(root, org string) error {
	if m := RoleContextMarker(); m != "" {
		return errors.New(roleOperatorDirNote(m))
	}
	path, err := SignaturePath(root, org)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Remove(strings.TrimSuffix(path, ".json") + ".projection.json"); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ReadFile reads org's definition under root: its bytes and sha256.
func ReadFile(root, org string) ([]byte, string, error) {
	if !safeOrgName.MatchString(org) {
		return nil, "", fmt.Errorf("invalid org name: %s", org)
	}
	b, err := os.ReadFile(filepath.Join(root, ".monomind", "orgs", org+".json"))
	if err != nil {
		return nil, "", err
	}
	return b, SHA256(b), nil
}

// SHA256 is the hex sha256 of b (orgdesign.Save's return value).
func SHA256(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// VerifyFile verifies org's definition as it is on disk now.
func VerifyFile(root, org string) (Status, string, error) {
	b, sha, err := ReadFile(root, org)
	if err != nil {
		return Status{}, "", err
	}
	return Verify(root, org, b), sha, nil
}
