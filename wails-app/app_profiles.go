// wails-app/app_profiles.go
//
// Profiles. The profile rows, their folders and moving a folder all belong
// to `monoagentcli profile …`; this file forwards to it and keeps only what
// is the app's own: the selected profile (profile_selection.go), its file
// watchers, the native folder picker and revealing a folder in the file
// manager.
package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	goruntime "runtime"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// profileCLITimeout bounds the profile reads the Sidebar and Settings make.
const profileCLITimeout = 20 * time.Second

type ProfileInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsActive  bool   `json:"is_active"`
	CreatedAt string `json:"created_at"`
	// RootDir is the real, resolved folder this profile's data lives in —
	// always populated (even for profiles using the default location), so
	// the frontend never needs to know the fallback rule itself.
	RootDir string `json:"root_dir"`
	// Icon is an id into the shared agent-avatars.json manifest (the same
	// one Org Designer role icons use), or "" if none was chosen — profiles
	// created before this field existed are always "".
	Icon string `json:"icon"`
}

// cliProfile is one profile as `profile list/get/create --json` print it.
type cliProfile struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	CreatedAt   string `json:"created_at"`
	RootDir     string `json:"root_dir"`
	Icon        string `json:"icon"`
	LayoutError string `json:"layout_error"`
}

func (p cliProfile) info(active bool) ProfileInfo {
	return ProfileInfo{ID: p.ID, Name: p.Name, IsActive: active, CreatedAt: p.CreatedAt, RootDir: p.RootDir, Icon: p.Icon}
}

// listProfilesInto reads `profile list` (machine-wide: it names no profile).
func (a *App) listProfilesInto(rows *[]cliProfile) error {
	return a.globalJSON(profileCLITimeout, rows, "profile", "list")
}

// GetProfiles lists every profile (`profile list`). is_active marks the
// profile this window has selected, which only the app knows.
func (a *App) GetProfiles() ([]ProfileInfo, error) {
	if err := a.signedOut(); err != nil {
		return nil, err
	}
	var rows []cliProfile
	if err := a.listProfilesInto(&rows); err != nil {
		return nil, err
	}
	active := a.selectedProfileOrEmpty()
	profiles := make([]ProfileInfo, 0, len(rows))
	for _, p := range rows {
		profiles = append(profiles, p.info(p.ID == active))
	}
	return profiles, nil
}

// GetActiveProfile returns the profile this window has selected
// (`profile get`), or errNoProfile before the first profile exists.
func (a *App) GetActiveProfile() (*ProfileInfo, error) {
	if err := a.signedOut(); err != nil {
		return nil, err
	}
	id, err := a.profileID()
	if err != nil {
		return nil, err
	}
	var p cliProfile
	if err := a.globalJSON(profileCLITimeout, &p, "profile", "get", id); err != nil {
		return nil, fmt.Errorf("active profile not found: %w", err)
	}
	info := p.info(true)
	return &info, nil
}

// CreateProfile creates a new profile (`profile create`). rootDir, if
// non-empty, is the folder the user picked (via ChooseProfileFolder, or a
// suggested monomind project via ListMonomindProjects) for this profile's
// data instead of the default ~/.monoagent/profiles/<id>/; the CLI checks
// it. icon, if non-empty, is an id into the shared agent-avatars.json
// manifest; leaving it empty is fine, the frontend falls back to a
// placeholder.
func (a *App) CreateProfile(name, rootDir, icon string) (*ProfileInfo, error) {
	if err := a.signedOut(); err != nil {
		return nil, err
	}
	p, err := a.createProfile(name, rootDir, icon)
	if err != nil {
		return nil, err
	}
	if p.LayoutError != "" {
		// Non-fatal: the profile row exists and is usable; its dedicated
		// folder just won't be there until the next startup migration pass
		// retries it.
		a.emitLog("SYSTEM", "WARN", fmt.Sprintf("profile %s: creating folder layout: %s", p.ID, p.LayoutError))
	} else {
		a.bootstrapProfileMonograph(p.ID)
	}
	// The first profile is selected at once; later ones wait to be switched to.
	if _, err := a.profileID(); err != nil {
		if err := a.selectProfile(p.ID); err != nil {
			return nil, err
		}
		a.restartProfileWatchers()
	}
	info := p.info(p.ID == a.selectedProfileOrEmpty())
	return &info, nil
}

func (a *App) createProfile(name, rootDir, icon string) (*cliProfile, error) {
	args := []string{"profile", "create"}
	if dir := strings.TrimSpace(rootDir); dir != "" {
		args = append(args, "--root-dir", dir)
	}
	if icon = strings.TrimSpace(icon); icon != "" {
		args = append(args, "--icon", icon)
	}
	// "--" so a name starting with a dash is a name, not a flag.
	args = append(args, "--", name)
	var p cliProfile
	if err := a.runGlobalCLI("", &p, args...); err != nil {
		return nil, err
	}
	return &p, nil
}

// SwitchProfile makes id the profile this window shows: the choice is saved
// in the app's own state file (never in the CLI's settings, and no CLI
// command changes), then the watchers scoped to the profile move over.
// Running workflow subprocesses are not touched.
func (a *App) SwitchProfile(id string) error {
	if err := a.signedOut(); err != nil {
		return err
	}
	if err := a.selectProfile(id); err != nil {
		return err
	}
	a.restartProfileWatchers()
	a.emitLog("SYSTEM", "INFO", "Switched to profile: "+id)
	return nil
}

// selectProfile checks that id is a profile that exists and saves it as the
// selection.
func (a *App) selectProfile(id string) error {
	var rows []cliProfile
	if err := a.listProfilesInto(&rows); err != nil {
		return err
	}
	found := false
	for _, p := range rows {
		found = found || p.ID == id
	}
	if !found {
		return fmt.Errorf("no profile %q", id)
	}
	return a.setSelectedProfile(id)
}

// restartProfileWatchers moves the watchers scoped to the selected profile.
func (a *App) restartProfileWatchers() {
	a.restartOrgWatcher()
	a.restartDocumentWatcher()
	a.restartImageWatcher()
	a.restartTaskWatcher()
}

// ChooseProfileFolder opens a native folder picker and returns the chosen
// absolute path, or "" if the user cancelled — same shape as ExportData's
// existing use of the same dialog.
func (a *App) ChooseProfileFolder() (string, error) {
	if err := a.signedOut(); err != nil {
		return "", err
	}
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Choose a folder for this profile",
	})
}

// RevealProfileFolder opens a file manager window at a profile's current
// folder (`profile folder` resolves it and makes sure it exists) —
// distinct from ChooseProfileFolder/MoveProfileFolder, which change where
// the folder is; this just shows where it already is.
func (a *App) RevealProfileFolder(profileID string) error {
	if err := a.signedOut(); err != nil {
		return err
	}
	dir, err := a.profileFolder(profileID)
	if err != nil {
		return err
	}
	return revealFolder(dir)
}

func (a *App) profileFolder(profileID string) (string, error) {
	var res struct {
		RootDir string `json:"root_dir"`
	}
	if err := a.cliJSON(profileCLITimeout, &res, "profile", "folder", profileID); err != nil {
		return "", fmt.Errorf("preparing profile folder: %w", err)
	}
	if res.RootDir == "" {
		return "", fmt.Errorf("profile %q has no folder", profileID)
	}
	return res.RootDir, nil
}

// revealFolder shells out to the platform's file-manager "reveal" command
// for dir. Split out from RevealProfileFolder so tests can exercise the
// per-GOOS command selection without spawning a real GUI file manager.
func revealFolder(dir string) error {
	name, args := revealFolderCommand(goruntime.GOOS, dir)
	opener := exec.Command(name, args...)
	suppressConsole(opener)
	err := opener.Run()
	if err == nil {
		return nil
	}
	if goruntime.GOOS == "windows" {
		// explorer.exe is documented to return a non-zero exit status even
		// when it successfully opened the folder (a known quirk unrelated
		// to whether the folder was revealed). Only swallow that case —
		// the process ran but reported a non-zero exit — not a failure to
		// launch it at all (e.g. explorer missing from PATH), which should
		// still surface to the caller.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil
		}
	}
	return err
}

// revealFolderCommand returns the OS-appropriate command name and arguments
// to reveal dir in the platform's file manager: Finder ("open") on darwin,
// "explorer" on windows, and "xdg-open" everywhere else (linux and other
// unix-likes) — the three platforms the release pipeline ships CLI/GUI
// builds for.
func revealFolderCommand(goos, dir string) (string, []string) {
	switch goos {
	case "darwin":
		return "open", []string{dir}
	case "windows":
		return "explorer", []string{dir}
	default:
		return "xdg-open", []string{dir}
	}
}

// MoveProfileFolder moves an existing profile's data (vault files and its
// .monomind knowledge-graph directory) to newRootDir: `profile move`, which
// only repoints the profile once both moves succeeded, so a failure leaves
// the old location authoritative and the caller can retry.
//
// Orgs run out of the folder, so before anything moves the folder's running
// orgs and its `org serve` daemon are stopped (the move is refused if that
// fails), and afterwards the org files are reconciled and the daemon is
// restarted wherever the folder ended up (C-24, app_org_profile.go). The
// destination is checked first (`profile move --check`), so a folder the
// move would refuse never stops anything.
func (a *App) MoveProfileFolder(profileID, newRootDir string) error {
	if err := a.signedOut(); err != nil {
		return err
	}
	if err := a.moveProfileFolder(profileID, newRootDir); err != nil {
		return err
	}
	if profileID == a.selectedProfileOrEmpty() {
		a.restartOrgWatcher()
		a.restartDocumentWatcher()
		a.restartImageWatcher()
	}
	a.emitLog("SYSTEM", "INFO", fmt.Sprintf("profile %s: moved to %s", profileID, strings.TrimSpace(newRootDir)))
	return nil
}

func (a *App) moveProfileFolder(profileID, newRootDir string) error {
	newRootDir = strings.TrimSpace(newRootDir)
	if newRootDir == "" {
		return fmt.Errorf("no folder chosen")
	}
	if err := a.runProfileMove("--check", profileID, newRootDir); err != nil {
		return err
	}
	wasServing, err := a.stopProfileOrgs(profileID)
	if err != nil {
		return fmt.Errorf("stopping this profile's orgs before the move: %w", err)
	}
	// Whether or not the move lands, the orgs resume at whatever folder the
	// profile points at once this returns.
	defer a.resumeProfileOrgs(profileID, wasServing)
	return a.runProfileMove(profileID, newRootDir)
}

// runProfileMove runs `profile move <args…>`. Unlike runMonoCLI it has no
// deadline of its own (moving .monomind across filesystems copies it, which
// can take a while) and runs without the app's lifecycle context too, like
// the org steps around it (runUnifiedCLI).
func (a *App) runProfileMove(args ...string) error {
	cliBin, err := findMonoAgentCLI()
	if err != nil {
		return err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	full, err := a.scopedArgs(append([]string{"profile", "move"}, args...)...)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, cliBin, full...)
	suppressConsole(cmd)
	if _, err := cmd.Output(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && cliStderr(ee.Stderr) != "" {
			return fmt.Errorf("%s", cliStderr(ee.Stderr))
		}
		return err
	}
	return nil
}
