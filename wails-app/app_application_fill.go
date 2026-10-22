// wails-app/app_application_fill.go
package main

import "errors"

// fillGoal is the header text of an application-fill task: what was asked,
// and that the person submits.
const fillGoal = "Fill in the application form in your browser (you submit it)"

// StartApplicationFill is `application fill start <id>`: the coder opens the
// posting's form and fills it, then stops before the submit. It returns the
// task (TaskID, ConversationID, Status). With coder mode off it returns
// {Fallback:"oneshot", Reason} and no error, and the regular Apply remains.
func (a *App) StartApplicationFill(applicationID string) (AITask, error) {
	if err := a.signedOut(); err != nil {
		return AITask{}, err
	}
	full, err := a.scopedArgs("application", "fill", "start", applicationID)
	if err != nil {
		return AITask{}, err
	}
	var t AITask
	if err := a.jsonCLIInto(&t, full); err != nil {
		var ae *aitaskError
		if errors.As(err, &ae) && ae.code == "coder_disabled" {
			return AITask{Fallback: "oneshot", Reason: ae.msg}, nil
		}
		return AITask{}, err
	}
	t.Kind, t.Goal = "application-fill", fillGoal
	if t.ApplicationID == "" {
		t.ApplicationID = applicationID
	}
	return t, nil
}
