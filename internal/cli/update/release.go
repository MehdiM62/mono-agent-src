package update

import (
	"context"
	"errors"
	"fmt"

	"github.com/monoes/mono-agent/internal/release"
)

// LatestInfo is the latest release as the updaters need it: from the verified
// signed manifest, or, only while release.AllowLegacyGitHubUpdates, from the
// GitHub API.
type LatestInfo struct {
	Tag      string
	URL      string
	Manifest *release.Manifest // set on the signed path
	Legacy   *LatestRelease    // set on the legacy path
}

// FetchLatest prefers the signed manifest (rc). Only "unavailable" (no pinned key,
// manifest not reachable) falls back to GitHub's latest-release endpoint
// (legacyURL); a manifest that fails a check is an error, never a reason to
// try the weaker path.
func FetchLatest(ctx context.Context, rc *release.Client, legacyURL string, force bool) (*LatestInfo, error) {
	rc.Force = rc.Force || force
	m, err := rc.Latest(ctx)
	if err == nil {
		return &LatestInfo{Tag: m.Version, URL: m.NotesURL, Manifest: m}, nil
	}
	if !errors.Is(err, release.ErrUnavailable) {
		return nil, fmt.Errorf("the signed release manifest was rejected: %w; nothing was installed", err)
	}
	if !release.AllowLegacyGitHubUpdates {
		return nil, fmt.Errorf("cannot verify an update: %w", err)
	}
	rel, lerr := FetchLatestRelease(ctx, legacyURL)
	if lerr != nil {
		return nil, lerr
	}
	return &LatestInfo{Tag: rel.TagName, URL: rel.HTMLURL, Legacy: rel}, nil
}
