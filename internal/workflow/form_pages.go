package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Form pages (n8n's Form node after a Form Trigger): one browser session
// walks through the run. The trigger's submission starts the run and is
// held until the run shows its next step; a core.form page opens a
// WaitReplyFormPage reply on the execution's resume URL and pauses, and the
// browser is redirected there; its answer resumes the run and is held the
// same way; a core.form ending records a WaitReplyFormEnd step the held
// request shows (a completion page, a redirect, or the given text). A run
// that ends without an ending shows the default confirmation.

// formStepWait bounds how long a submission is held for the run's next
// step. A page answered past it redirects to the resume URL, which keeps
// refreshing until the next page is ready.
var formStepWait = 5 * time.Minute

// formWaitRefresh is how often the "next page is being prepared" page
// reloads.
const formWaitRefresh = "2"

// EndFormFlow records a form ending (core.form completion) as the run's next
// browser step and returns at once: the request held for the step shows it.
// spec.RespondWith is "text" (Title and Message), "redirect" (RedirectURL)
// or "showText" (ResponseText as the page).
func EndFormFlow(ctx context.Context, input NodeInput, spec WaitReplySpec) error {
	switch spec.RespondWith {
	case "", "text":
		spec.RespondWith = "text"
	case "redirect":
		u, err := url.Parse(spec.RedirectURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("%w: form ending: respondWith redirect needs an http(s) redirectUrl", ErrInvalidConfig)
		}
	case "showText":
	default:
		return fmt.Errorf("%w: form ending: respondWith must be text, redirect or showText, got %q", ErrInvalidConfig, spec.RespondWith)
	}
	if input.ExecutionID == "" || input.NodeID == "" {
		return errors.New("form ending: needs a running execution")
	}
	db, err := replyDB(ctx)
	if err != nil {
		return err
	}
	token, err := signReply("execution", input.ExecutionID)
	if err != nil {
		return fmt.Errorf("form ending: %w", err)
	}
	spec.Kind, spec.ExecutionURL = WaitReplyFormEnd, true
	specJSON, _ := json.Marshal(spec)
	// Stored answered and read: it is a step to show, not a reply to wait for.
	_, err = db.ExecContext(ctx,
		`INSERT INTO wait_replies (execution_id, node_id, run_index, workflow_id, token, kind, spec, status, answer, sent, consumed)
		 SELECT ?, ?, COALESCE(MAX(run_index)+1, 0), ?, ?, ?, ?, 'answered', '{}', 1, 1
		 FROM wait_replies WHERE execution_id=? AND node_id=?`,
		input.ExecutionID, input.NodeID, input.WorkflowID, token, WaitReplyFormEnd, string(specJSON),
		input.ExecutionID, input.NodeID)
	if err != nil {
		return fmt.Errorf("form ending: store: %w", err)
	}
	return nil
}

// workflowHasFormSteps reports whether w has nodes that show a later step
// of a form's browser session: core.form, or a core.wait form.
func workflowHasFormSteps(w *Workflow) bool {
	for i := range w.Nodes {
		n := &w.Nodes[i]
		switch n.Type {
		case "core.form":
			return true
		case "core.wait":
			if n.Config == nil {
				_ = n.ParseConfig()
			}
			if r, _ := n.Config["resume"].(string); r == "form" {
				return true
			}
		}
	}
	return false
}

// continueFormFlow holds a trigger.form submission that started execID
// until the run shows its first step and shows it. It reports false when
// the run ended or the wait ran out first: the trigger answers as usual.
func (s *WebhookServer) continueFormFlow(w http.ResponseWriter, r *http.Request, execID string) bool {
	s.mu.RLock()
	h, _ := s.replies.(*replyHandler)
	s.mu.RUnlock()
	if h == nil {
		return false
	}
	step, _ := h.awaitFormStep(w, r, execID, 0)
	if step == nil {
		return false
	}
	h.showFormStep(w, r, step)
	return true
}

// awaitFormStep waits for executionID's next browser step after the reply
// row after: a pending page (core.form, or a core.wait form) on the
// execution's resume URL, or a form ending. It reports ended when the run
// finished without one; neither means the wait ran out or the caller left.
func (h *replyHandler) awaitFormStep(w http.ResponseWriter, r *http.Request, executionID string, after int64) (*replyRow, bool) {
	token, err := signReply("execution", executionID)
	if err != nil {
		return nil, false
	}
	ctx := r.Context()
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(formStepWait + 10*time.Second))
	next := func() (*replyRow, error) {
		return scanReplyRow(h.db.QueryRowContext(ctx,
			`SELECT `+replyColumns+` FROM wait_replies
			 WHERE execution_id = ? AND token = ? AND rowid > ?
			   AND (kind = ? OR (kind IN (?, ?) AND status = 'pending' AND consumed = 0))
			 ORDER BY rowid LIMIT 1`,
			executionID, token, after, WaitReplyFormEnd, WaitReplyFormPage, WaitReplyCustomForm))
	}
	deadline := time.Now().Add(formStepWait)
	tick := time.NewTicker(webhookPollInterval)
	defer tick.Stop()
	for {
		row, err := next()
		if err != nil {
			h.logger.Warn().Err(err).Msg("form: next step")
			return nil, false
		}
		if row != nil {
			return row, false
		}
		if !h.executionWaits(ctx, executionID) {
			// A step stored just before the run ended still counts.
			if row, err := next(); err == nil && row != nil {
				return row, false
			}
			return nil, true
		}
		if time.Now().After(deadline) {
			return nil, false
		}
		select {
		case <-ctx.Done():
			return nil, false
		case <-tick.C:
		}
	}
}

// showFormStep answers with step: a page redirects to its URL (the browser
// loads it with GET), an ending shows itself.
func (h *replyHandler) showFormStep(w http.ResponseWriter, r *http.Request, step *replyRow) {
	if step.kind != WaitReplyFormEnd {
		http.Redirect(w, r, "/reply/"+replyTokenOf(step), http.StatusSeeOther)
		return
	}
	switch step.spec.RespondWith {
	case "redirect":
		http.Redirect(w, r, step.spec.RedirectURL, http.StatusSeeOther)
	case "showText":
		// The node's own HTML, as n8n shows it, sandboxed: no scripts, no
		// forms, nothing loaded but images.
		hd := w.Header()
		hd.Set("Content-Type", "text/html; charset=utf-8")
		hd.Set("Content-Security-Policy", "sandbox; default-src 'none'; style-src 'unsafe-inline'; img-src https: data:; frame-ancestors 'none'")
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(step.spec.ResponseText))
	default:
		writeReplyPage(w, http.StatusOK, replyPageData{Title: orDefault(step.spec.Title, "Form submitted"), Done: step.spec.Message})
	}
}

// serveFormStep serves a form page or ending on the execution's resume URL.
// GET shows the page (or, once answered, waits for the next one); POST
// records the answers and continues the session to the next step.
func (h *replyHandler) serveFormStep(w http.ResponseWriter, r *http.Request, row *replyRow) {
	ctx := r.Context()
	if row.kind == WaitReplyFormEnd {
		h.showFormStep(w, r, row)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if row.status == WaitReplyAnswered {
		// Between pages: the run is preparing the next one, or is done.
		if h.executionWaits(ctx, row.executionID) {
			w.Header().Set("Refresh", formWaitRefresh)
			writeReplyPage(w, http.StatusOK, replyPageData{Title: row.spec.Title, Done: "Your answers were recorded. The next page is being prepared…"})
			return
		}
		renderReplyDone(w, http.StatusOK, row.spec, defaultFormResponse)
		return
	}
	switch {
	case row.status != WaitReplyPending || row.expired:
		renderReplyDone(w, http.StatusGone, row.spec, "This form has expired.")
		return
	case !h.executionWaits(ctx, row.executionID):
		renderReplyDone(w, http.StatusGone, row.spec, "This workflow is no longer waiting for an answer.")
		return
	}
	fields := row.spec.FormFields
	if r.Method == http.MethodGet {
		renderFormPage(w, http.StatusOK, row.spec, fields, nil, "")
		return
	}
	if err := r.ParseForm(); err != nil {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "request body too large or malformed")
		return
	}
	values, problem := readReplyForm(fields, r.PostForm)
	if problem != "" {
		renderFormPage(w, http.StatusBadRequest, row.spec, fields, r.PostForm, problem)
		return
	}
	values["submittedAt"] = time.Now().UTC().Format(time.RFC3339)
	values["formMode"] = "production"
	recorded, err := h.record(ctx, row, values)
	if err != nil {
		h.logger.Warn().Err(err).Msg("form page: record")
		renderReplyDone(w, http.StatusInternalServerError, row.spec, "Your answers could not be recorded. Please try again.")
		return
	}
	if !recorded { // answered elsewhere first (another tab)
		http.Redirect(w, r, r.URL.Path, http.StatusSeeOther)
		return
	}
	step, ended := h.awaitFormStep(w, r, row.executionID, row.rowid)
	switch {
	case step != nil:
		h.showFormStep(w, r, step)
	case ended:
		renderReplyDone(w, http.StatusOK, row.spec, defaultFormResponse)
	default:
		http.Redirect(w, r, r.URL.Path, http.StatusSeeOther)
	}
}

// replyTokenOf is a stored step's token: every form step uses the
// execution's.
func replyTokenOf(row *replyRow) string {
	tok, _ := signReply("execution", row.executionID)
	return tok
}
