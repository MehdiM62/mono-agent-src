package extension

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/capture"
	"github.com/monoes/mono-agent/internal/profiledir"
)

// A capture queued in the extension names the profile chosen when it was
// made. If that profile is deleted before the queue flushes, the capture
// must neither land in nor recreate the deleted profile's folder.

func liveProfiles(ids ...string) ProfileSource {
	return func(context.Context) ([]profiledir.Profile, error) {
		out := make([]profiledir.Profile, 0, len(ids))
		for i, id := range ids {
			out = append(out, profiledir.Profile{ID: id, Name: id, Default: i == 0})
		}
		return out, nil
	}
}

// startProfiledCaptureServer is startCaptureServer routing by profile, as
// the real bridge does: no configured inbox, and a profile source.
func startProfiledCaptureServer(t *testing.T, src ProfileSource) (*Server, *fakeExtension) {
	t.Helper()
	t.Setenv(capture.InboxEnv, "")
	t.Setenv(capture.HomeEnv, "")
	srv, ext, _ := startCaptureServerWith(t, func(s *Server) {
		s.SetCaptureInbox("")
		s.SetProfileSource(src)
	})
	return srv, ext
}

// flushProfiled sends one capture nothing asked for, naming profile, and
// waits for it to land.
func flushProfiled(t *testing.T, srv *Server, ext *fakeExtension, profile string) *capture.Result {
	t.Helper()
	landed := make(chan *capture.Result, 1)
	failed := make(chan error, 1)
	srv.OnCapture(func(res *capture.Result, err error) {
		if err != nil {
			failed <- err
			return
		}
		landed <- res
	})
	meta := sampleMeta()
	meta["profile"] = profile
	ext.sendFinal("ext-queued-"+profile, meta, b64Artifact(capture.ArtifactReadable, "# A Post"))
	select {
	case err := <-failed:
		t.Fatalf("flushed capture failed: %v", err)
	case res := <-landed:
		return res
	case <-time.After(5 * time.Second):
		t.Fatal("flushed capture never landed")
	}
	return nil
}

func wantDefaultInbox(t *testing.T, res *capture.Result, gone string) {
	t.Helper()
	if got, want := filepath.Dir(res.Path), capture.DefaultInbox(); got != want {
		t.Fatalf("capture landed in %s, want the default inbox %s", got, want)
	}
	if res.Meta.Profile != "" {
		t.Fatalf("meta.profile = %q, want it cleared", res.Meta.Profile)
	}
	if !strings.Contains(strings.Join(res.Warnings, "\n"), `"`+gone+`" no longer exists`) {
		t.Fatalf("warnings = %q, want one naming the deleted profile", res.Warnings)
	}
	if _, err := os.Stat(profiledir.Root(nil, gone)); !os.IsNotExist(err) {
		t.Fatalf("a folder was made for the deleted profile %q (stat: %v)", gone, err)
	}
}

func TestQueuedCaptureForDeletedProfileLandsInDefaultInbox(t *testing.T) {
	srv, ext := startProfiledCaptureServer(t, liveProfiles("p-work"))
	wantDefaultInbox(t, flushProfiled(t, srv, ext, "p-gone"), "p-gone")
}

func TestQueuedCaptureForLiveProfileLandsInItsInbox(t *testing.T) {
	srv, ext := startProfiledCaptureServer(t, liveProfiles("p-work", "p-home"))
	res := flushProfiled(t, srv, ext, "p-home")
	want, err := capture.ProfileInbox("p-home")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(res.Path) != want || res.Meta.Profile != "p-home" || len(res.Warnings) != 0 {
		t.Fatalf("landed in %s as %q (warnings %q), want %s as p-home", res.Path, res.Meta.Profile, res.Warnings, want)
	}
}

// A source that cannot answer right now (a locked database) is no reason to
// move a capture out of what may well be a live profile.
func TestQueuedCaptureKeepsItsProfileWhenProfilesCannotBeListed(t *testing.T) {
	srv, ext := startProfiledCaptureServer(t, func(context.Context) ([]profiledir.Profile, error) {
		return nil, errors.New("database is locked")
	})
	res := flushProfiled(t, srv, ext, "p-work")
	want, err := capture.ProfileInbox("p-work")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(res.Path) != want || res.Meta.Profile != "p-work" {
		t.Fatalf("landed in %s as %q, want %s as p-work", res.Path, res.Meta.Profile, want)
	}
}

func TestCapturePageForDeletedProfileLandsInDefaultInbox(t *testing.T) {
	srv, ext := startProfiledCaptureServer(t, liveProfiles("p-work"))
	type outcome struct {
		res *capture.Result
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		res, err := srv.CapturePage(CaptureRequest{TabID: 7, Timeout: 5 * time.Second})
		done <- outcome{res, err}
	}()
	cmd := ext.nextCommand()
	meta := sampleMeta()
	meta["profile"] = "p-gone"
	ext.sendFinal(cmd.ID, meta, b64Artifact(capture.ArtifactReadable, "# A Post"))
	got := <-done
	if got.err != nil {
		t.Fatalf("CapturePage: %v", got.err)
	}
	wantDefaultInbox(t, got.res, "p-gone")
}
