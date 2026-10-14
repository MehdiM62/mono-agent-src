package workflow

import (
	"time"
)

// replyWakeWindow bounds how long an answer waits for its run to finish
// pausing (the node is still sending its message) before leaving the run
// to the resume loop's poll.
const replyWakeWindow = 2 * time.Minute

// wireWaitReplies lets the webhook server serve reply links and resume
// URLs, waking the run an answer is for.
func (e *WorkflowEngine) wireWaitReplies() {
	if e.store == nil || e.store.RawDB() == nil {
		return
	}
	e.webhookServer.SetReplyHandler(&replyHandler{db: e.store.RawDB(), wake: e.wakeForReply, logger: e.logger})
}

// replyUnread reports whether executionID has an answer its node has not
// read yet.
func (e *WorkflowEngine) replyUnread(executionID string) bool {
	dctx, cancel := dbCtx()
	defer cancel()
	var n int
	err := e.store.RawDB().QueryRowContext(dctx,
		`SELECT COUNT(*) FROM wait_replies WHERE execution_id = ? AND status = 'answered' AND consumed = 0`,
		executionID).Scan(&n)
	return err == nil && n > 0
}

// wakeForReply resumes executionID once it is WAITING. A run still RUNNING
// is between storing its reply and pausing; it is retried for a while
// rather than left to the poll window it is about to record.
func (e *WorkflowEngine) wakeForReply(executionID string) {
	deadline := time.Now().Add(replyWakeWindow)
	for {
		sctx, cancel := dbCtx()
		status, err := e.store.GetExecutionStatus(sctx, executionID)
		cancel()
		if err != nil {
			return
		}
		switch status {
		case "WAITING":
			if !e.replyUnread(executionID) {
				return // its node already read the answer
			}
			if s, ok := e.store.(resumeAfterSetter); ok {
				uctx, ucancel := dbCtx()
				_ = s.SetExecutionResumeAfter(uctx, executionID, nil)
				ucancel()
			}
			if err := e.ResumeExecution(executionID); err != nil {
				e.logger.Warn().Err(err).Str("execution_id", executionID).Msg("engine: resume after reply")
			}
			return
		case "RUNNING", "QUEUED":
			if time.Now().After(deadline) {
				return
			}
			time.Sleep(100 * time.Millisecond)
		default:
			return
		}
	}
}
