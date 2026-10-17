// wails-app/profile_selection.go
//
// The profile the window is looking at. It is the app's own UI state: a small
// JSON file next to the database, written and read only by this app. The CLI,
// the MCP server and the extension never learn it, and no setting in the
// database holds it; every CLI call the app makes names the profile with
// --profile (profile_args.go).
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/monoes/mono-agent/internal/profileorder"
)

// errNoProfile is what every binding that needs a profile returns while none
// is selected, which is only the case before the first profile is created.
var errNoProfile = errors.New("no profile selected: create a profile first")

// guiStateFile is the app's UI state file, kept beside the database.
const guiStateFile = "gui-state.json"

type guiState struct {
	SelectedProfile string `json:"selected_profile"`
}

func guiStatePath(dbPath string) string {
	return filepath.Join(filepath.Dir(dbPath), guiStateFile)
}

// loadSelectedProfile returns the saved selection, or "" when nothing is
// saved or the file cannot be read (a damaged file is the same as no choice).
func loadSelectedProfile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var st guiState
	if json.Unmarshal(b, &st) != nil {
		return ""
	}
	return st.SelectedProfile
}

// saveSelectedProfile writes the selection through a temp file and a rename,
// so a crash never leaves half a file.
func saveSelectedProfile(path, id string) error {
	b, err := json.Marshal(guiState{SelectedProfile: id})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), guiStateFile+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// chooseProfile picks the selection from the profiles that exist: the saved
// one while it still exists, otherwise the oldest profile (by created_at in either stored format, ties broken by id,
// so the choice is the same on every start). No profiles is errNoProfile.
func chooseProfile(saved string, profiles []cliProfile) (string, error) {
	if len(profiles) == 0 {
		return "", errNoProfile
	}
	for _, p := range profiles {
		if saved != "" && p.ID == saved {
			return saved, nil
		}
	}
	sorted := append([]cliProfile(nil), profiles...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return profileorder.Older(sorted[i].CreatedAt, sorted[i].ID, sorted[j].CreatedAt, sorted[j].ID)
	})
	return sorted[0].ID, nil
}

// resolveSelectedProfile applies chooseProfile to the saved state and saves
// the result when it differs, returning the selected id.
func resolveSelectedProfile(path string, profiles []cliProfile) (string, error) {
	saved := loadSelectedProfile(path)
	id, err := chooseProfile(saved, profiles)
	if err != nil {
		return "", err
	}
	if id != saved {
		if err := saveSelectedProfile(path, id); err != nil {
			return id, fmt.Errorf("saving the selected profile: %w", err)
		}
	}
	return id, nil
}

// profileID is the selected profile, or errNoProfile. Every binding that acts
// on a profile asks for it here; there is no fallback.
func (a *App) profileID() (string, error) {
	if p := a.selectedProfilePtr.Load(); p != nil && *p != "" {
		return *p, nil
	}
	return "", errNoProfile
}

// selectedProfileOrEmpty is the selected profile for an event payload that
// only tells open pages which profile changed; it never builds a CLI call.
func (a *App) selectedProfileOrEmpty() string {
	id, _ := a.profileID()
	return id
}

// scopedArgs is scopedCLIArgs for the selected profile.
func (a *App) scopedArgs(args ...string) ([]string, error) {
	id, err := a.profileID()
	if err != nil {
		return nil, err
	}
	return scopedCLIArgs(id, args...)
}

// chatCLI is chatSupervisor.cli for the selected profile.
func (a *App) chatCLI(result any, args ...string) error {
	id, err := a.profileID()
	if err != nil {
		return err
	}
	return a.chatSup.cli(id, result, args...)
}

// setSelectedProfile sets the selection in memory and on disk.
func (a *App) setSelectedProfile(id string) error {
	a.selectedProfilePtr.Store(&id)
	return saveSelectedProfile(guiStatePath(a.dbPath), id)
}

// selectProfileAtStartup reads the saved selection and the profile list, and
// selects the saved profile, else the oldest. With no profiles nothing is
// selected and the create-first-profile screen shows.
func (a *App) selectProfileAtStartup() error {
	var rows []cliProfile
	if err := a.listProfilesInto(&rows); err != nil {
		return err
	}
	id, err := resolveSelectedProfile(guiStatePath(a.dbPath), rows)
	if err != nil {
		if errors.Is(err, errNoProfile) {
			a.selectedProfilePtr.Store(nil)
		}
		return err
	}
	a.selectedProfilePtr.Store(&id)
	return nil
}
