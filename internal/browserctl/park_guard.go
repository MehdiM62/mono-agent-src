package browserctl

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/monoes/mono-agent/internal/applypark"
)

// ParkedTabError is an agent's command to a tab that belongs to an
// application: parked tabs are the person's to submit, and a tab one task is
// working in is that task's alone (internal/applypark).
type ParkedTabError struct {
	TabID         int
	ApplicationID string
	State         string
}

func (e *ParkedTabError) Error() string {
	if e.State == string(applypark.StateParked) {
		return fmt.Sprintf("tab %d holds the filled application %s and waits for the person to submit it: leave it alone; "+
			"the person resumes it with `application fill resume`", e.TabID, e.ApplicationID)
	}
	return fmt.Sprintf("tab %d belongs to the task working on application %s, not to this one: open your own tab with `browser open`",
		e.TabID, e.ApplicationID)
}

// parkGuard refuses an agent a tab that application_tabs binds: parked for
// every agent, active for every task but the one it is bound to. It fails
// closed: with the binding unreadable the tab is not driven.
func (s *Session) parkGuard(id int) error {
	if !s.policy.Agent || s.db == nil {
		return nil
	}
	h, err := applypark.Store{DB: s.db}.Holder(context.Background(), id)
	if err != nil {
		return fmt.Errorf("tab %d: cannot check whether it holds a parked application: %w", id, err)
	}
	if h == nil {
		return nil
	}
	if h.State == applypark.StateActive && h.TaskID != "" && h.TaskID == strings.TrimSpace(os.Getenv(applypark.EnvTaskID)) {
		return nil
	}
	return &ParkedTabError{TabID: id, ApplicationID: h.ApplicationID, State: string(h.State)}
}
