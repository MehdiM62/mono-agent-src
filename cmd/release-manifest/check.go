package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

var urlPrefixRe = regexp.MustCompile(`^https://github\.com/([A-Za-z0-9._-]+/[A-Za-z0-9._-]+)/releases/download/(v\d+\.\d+\.\d+)/$`)

// validateNotesURL requires an https URL under github.com/<repo>/ or on monoes.me.
func validateNotesURL(raw, repo string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil {
		return fmt.Errorf("notes_url %q must be a plain https URL", raw)
	}
	for _, seg := range strings.Split(u.Path, "/") {
		if seg == ".." {
			return fmt.Errorf("notes_url %q must not contain .. segments", raw)
		}
	}
	cleaned := path.Clean("/" + u.Path)
	if strings.HasSuffix(u.Path, "/") && cleaned != "/" {
		cleaned += "/"
	}
	switch u.Host {
	case "monoes.me":
		return nil
	case "github.com":
		if strings.HasPrefix(cleaned, "/"+repo+"/") {
			return nil
		}
	}
	return fmt.Errorf("notes_url %q must be under https://github.com/%s/ or https://monoes.me/", raw, repo)
}

// decodeManifestStrict parses the manifest, rejecting unknown fields and trailing data.
func decodeManifestStrict(data []byte) (*Manifest, error) {
	var m Manifest
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("manifest is not valid: %w", err)
	}
	// A second Decode must hit a clean EOF; anything else (an extra } or ], a second object) is junk.
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("manifest has trailing data after the JSON object")
	}
	// encoding/json matches keys case-insensitively, so {"version":..,"Version":..} would parse
	// with the last one winning while other parsers may take the first. Reject such duplicates.
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, fmt.Errorf("manifest is not valid: %w", err)
	}
	seen := map[string]bool{}
	for k := range top {
		lk := strings.ToLower(k)
		if seen[lk] {
			return nil, fmt.Errorf("manifest has duplicate key %q (keys are compared case-insensitively)", k)
		}
		seen[lk] = true
	}
	return &m, nil
}

// checkManifestAgainstAssets is the pre-sign gate: the manifest must describe exactly the
// files in dir (sha256 and size recomputed), carry the version being released, and not be
// older than minVersion (the latest published release; empty skips that check). With a non-empty
// urlPrefix (https://github.com/OWNER/REPO/releases/download/TAG/) every asset url must be that
// prefix plus the asset name, and notes_url must sit under that repo or on monoes.me. With strict,
// dir may hold nothing but the listed assets and the manifest/signature/checksum files.
func checkManifestAgainstAssets(data []byte, dir, expectVersion, minVersion, urlPrefix string, strict bool, now time.Time) error {
	m, err := decodeManifestStrict(data)
	if err != nil {
		return err
	}
	if m.Schema != 1 {
		return fmt.Errorf("manifest schema %d, want 1", m.Schema)
	}
	have, err := parseSemver(m.Version)
	if err != nil {
		return err
	}
	if m.Version != expectVersion {
		return fmt.Errorf("manifest version %s is not the version being released (%s)", m.Version, expectVersion)
	}
	if m.ExpiresAt != "" {
		exp, err := time.Parse(time.RFC3339, m.ExpiresAt)
		if err != nil {
			return fmt.Errorf("expires_at %q is not RFC3339: %w", m.ExpiresAt, err)
		}
		if !exp.After(now) {
			return fmt.Errorf("expires_at %s is not in the future", m.ExpiresAt)
		}
	}
	if urlPrefix != "" {
		pm := urlPrefixRe.FindStringSubmatch(urlPrefix)
		if pm == nil {
			return fmt.Errorf("url prefix %q must be https://github.com/OWNER/REPO/releases/download/vX.Y.Z/", urlPrefix)
		}
		if pm[2] != expectVersion {
			return fmt.Errorf("url prefix tag %s is not the version being released (%s)", pm[2], expectVersion)
		}
		if err := validateNotesURL(m.NotesURL, pm[1]); err != nil {
			return err
		}
	}
	if minVersion != "" {
		min, err := parseSemver(minVersion)
		if err != nil {
			return err
		}
		for i := range min {
			if have[i] != min[i] {
				if have[i] < min[i] {
					return fmt.Errorf("manifest version %s is lower than the latest published release %s", m.Version, minVersion)
				}
				break
			}
		}
	}
	if len(m.Assets) == 0 {
		return errors.New("manifest lists no assets")
	}
	listed := map[string]bool{}
	for _, a := range m.Assets {
		if listed[a.Name] {
			return fmt.Errorf("asset %s is listed twice", a.Name)
		}
		listed[a.Name] = true
		if a.Name != filepath.Base(a.Name) || a.Name == "" {
			return fmt.Errorf("asset name %q is not a plain file name", a.Name)
		}
		if urlPrefix != "" {
			if a.URL != urlPrefix+a.Name {
				return fmt.Errorf("asset %s: url %q is not %s%s", a.Name, a.URL, urlPrefix, a.Name)
			}
		} else if !strings.HasSuffix(a.URL, "/releases/download/"+m.Version+"/"+a.Name) {
			return fmt.Errorf("asset %s: url does not point at release %s", a.Name, m.Version)
		}
		o, ar, k, ok := classify(a.Name)
		if !ok || o != a.OS || ar != a.Arch || k != a.Kind {
			return fmt.Errorf("asset %s: os/arch/kind do not match its name", a.Name)
		}
		b, err := os.ReadFile(filepath.Join(dir, a.Name))
		if err != nil {
			return fmt.Errorf("asset %s: %w", a.Name, err)
		}
		sum := sha256.Sum256(b)
		if hex.EncodeToString(sum[:]) != a.SHA256 {
			return fmt.Errorf("asset %s: sha256 does not match the built file", a.Name)
		}
		if int64(len(b)) != a.Size {
			return fmt.Errorf("asset %s: size %d does not match the built file (%d)", a.Name, a.Size, len(b))
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			if strict {
				return fmt.Errorf("%s is not a regular file (%s); the upload dir may hold only regular files", e.Name(), e.Type())
			}
			continue
		}
		if listed[e.Name()] {
			continue
		}
		if _, _, _, ok := classify(e.Name()); ok {
			return fmt.Errorf("built file %s is missing from the manifest", e.Name())
		}
		if strict && e.Name() != "SHA256SUMS" && e.Name() != "NOTICE" && !strings.HasPrefix(e.Name(), "manifest.json") {
			return fmt.Errorf("file %s is not listed in the manifest", e.Name())
		}
	}
	return nil
}

// checkKeyFileMode refuses a key file that other users could read. Stat follows symlinks, so a
// link to a looser file is judged by its target.
func checkKeyFileMode(path string) error {
	if runtime.GOOS == "windows" {
		return nil // Windows has no POSIX mode bits to judge; the file's ACL is the owner's job.
	}
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("key file %s is not a regular file", path)
	}
	if st.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("key file %s has mode %04o: it must be 0600 or tighter (chmod 600)", path, st.Mode().Perm())
	}
	return nil
}
