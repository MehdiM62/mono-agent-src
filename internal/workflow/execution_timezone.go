package workflow

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// TimezoneSetting is the Workflow.Settings key naming the IANA zone (e.g.
// "Europe/Berlin") that $now and $today use. Without it the
// MONOAGENT_TIMEZONE environment variable applies, and without that UTC,
// as before.
const TimezoneSetting = "timezone"

// TimezoneEnv names the machine-wide default zone for $now and $today.
const TimezoneEnv = "MONOAGENT_TIMEZONE"

var locationCache sync.Map // zone name → *time.Location

// workflowLocation returns the zone for wf's expressions: settings.timezone,
// else $MONOAGENT_TIMEZONE, else nil (UTC). An unknown zone name is UTC too;
// ValidateWorkflow warns about it.
func workflowLocation(wf *Workflow) *time.Location {
	var name string
	if wf != nil {
		name, _ = wf.Settings[TimezoneSetting].(string)
	}
	if strings.TrimSpace(name) == "" {
		name = os.Getenv(TimezoneEnv)
	}
	loc, _ := loadLocation(name)
	return loc
}

// loadLocation loads and caches an IANA zone; "" is nil without error.
func loadLocation(name string) (*time.Location, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil
	}
	if v, ok := locationCache.Load(name); ok {
		return v.(*time.Location), nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, err
	}
	locationCache.Store(name, loc)
	return loc, nil
}

// settingsIssues warns about a settings.timezone that is not a known zone.
func settingsIssues(w *Workflow) []ValidationIssue {
	name, _ := w.Settings[TimezoneSetting].(string)
	if _, err := loadLocation(name); err == nil {
		return nil
	}
	return []ValidationIssue{{Code: IssueInvalidOption, Severity: "warning",
		Message: fmt.Sprintf("settings.timezone %q is not a known IANA zone; $now and $today use UTC", name)}}
}
