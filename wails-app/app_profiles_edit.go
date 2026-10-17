// wails-app/app_profiles_edit.go
//
// Renaming, re-iconing and deleting profiles. The work is
// `monoagentcli profile update|delete`; this file forwards to it and keeps
// the app's own selection and watchers in step.
package main

import (
	"fmt"
)

// ProfileTableCount is how many rows a profile has in one table.
type ProfileTableCount struct {
	Table string `json:"table"`
	Rows  int    `json:"rows"`
}

// ProfileDeleteSummary is what deleting a profile would remove.
type ProfileDeleteSummary struct {
	Tables []ProfileTableCount `json:"tables"`
	// Folder is the profile's folder on disk, which a delete leaves alone.
	Folder string `json:"folder"`
}

// UpdateProfile renames a profile and sets its icon (`profile update`). An
// empty icon clears it.
func (a *App) UpdateProfile(id, name, icon string) (*ProfileInfo, error) {
	if err := a.signedOut(); err != nil {
		return nil, err
	}
	var p cliProfile
	if err := a.runGlobalCLI("", &p, "profile", "update", "--name", name, "--icon", icon, "--", id); err != nil {
		return nil, err
	}
	info := p.info(p.ID == a.selectedProfileOrEmpty())
	return &info, nil
}

// GetProfileDeleteSummary says what deleting the profile would remove
// (`profile delete --summary`).
func (a *App) GetProfileDeleteSummary(id string) (*ProfileDeleteSummary, error) {
	if err := a.signedOut(); err != nil {
		return nil, err
	}
	var res struct {
		Profile cliProfile          `json:"profile"`
		Tables  []ProfileTableCount `json:"tables"`
	}
	if err := a.globalJSON(profileCLITimeout, &res, "profile", "delete", "--summary", "--", id); err != nil {
		return nil, err
	}
	return &ProfileDeleteSummary{Tables: res.Tables, Folder: res.Profile.RootDir}, nil
}

// DeleteProfile permanently deletes a profile (`profile delete`, after a
// database snapshot whose path is returned). Its orgs are stopped first. When
// it is the profile this window shows, the window moves to another one; the
// only profile cannot be deleted. The profile's folder on disk is not touched.
func (a *App) DeleteProfile(id string) (string, error) {
	if err := a.signedOut(); err != nil {
		return "", err
	}
	var rows []cliProfile
	if err := a.listProfilesInto(&rows); err != nil {
		return "", err
	}
	var target *cliProfile
	next := ""
	for i, p := range rows {
		if p.ID == id {
			target = &rows[i]
		} else if next == "" {
			next = p.ID
		}
	}
	if target == nil {
		return "", fmt.Errorf("no profile %q", id)
	}
	if next == "" {
		return "", fmt.Errorf("cannot delete the only profile: create another one first")
	}
	wasServing, err := a.stopProfileOrgs(id)
	if err != nil {
		return "", fmt.Errorf("stopping this profile's orgs before deleting it: %w", err)
	}
	var res struct {
		Backup   string   `json:"backup"`
		Warnings []string `json:"warnings"`
	}
	if err := a.runGlobalCLI("", &res, "profile", "delete", "--yes-delete-profile", target.Name, "--", id); err != nil {
		// Nothing was deleted: the orgs it stopped run again.
		a.resumeProfileOrgs(id, wasServing)
		return "", err
	}
	for _, w := range res.Warnings {
		a.emitLog("SYSTEM", "WARN", "profile delete: "+w)
	}
	a.emitLog("SYSTEM", "INFO", "Deleted profile: "+target.Name)
	if id == a.selectedProfileOrEmpty() {
		if err := a.selectProfile(next); err != nil {
			return res.Backup, err
		}
		a.restartProfileWatchers()
	}
	return res.Backup, nil
}
