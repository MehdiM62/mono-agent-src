package workflow

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// replyMaxBody caps a reply request's body (a form or a resume call).
const replyMaxBody = 1 << 20

// replyHandler serves /reply/<token>[/approve|/decline] on the webhook
// server: the pages behind a node's reply links, and resume calls.
//
// A link opened with GET only shows a page; the answer is recorded by the
// page's POST, so a mail scanner that fetches every link answers nothing.
// A webhook reply (core.wait resume "webhook") takes the call itself.
type replyHandler struct {
	db     *sql.DB
	wake   func(executionID string)
	logger zerolog.Logger
}

// replyRow is a stored reply as the handler sees it.
type replyRow struct {
	executionID, nodeID string
	run                 int
	kind, status        string
	spec                WaitReplySpec
	answer              map[string]interface{}
	expired             bool // pending past its limit
}

// serveReply hands /reply/ requests to the reply handler; it reports
// whether it did.
func (s *WebhookServer) serveReply(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/reply/") {
		return false
	}
	s.mu.RLock()
	h := s.replies
	s.mu.RUnlock()
	if h == nil {
		writeJSONError(w, http.StatusNotFound, "not found")
		return true
	}
	h.ServeHTTP(w, r)
	return true
}

// SetReplyHandler installs the handler of /reply/ requests.
func (s *WebhookServer) SetReplyHandler(h http.Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replies = h
}

func (h *replyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/reply/"), "/")
	token, action, _ := strings.Cut(rest, "/")
	if !validReplyToken(token) || (action != "" && action != "approve" && action != "decline") {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	ctx := r.Context()
	row, err := h.load(ctx, token)
	if err != nil {
		h.logger.Warn().Err(err).Msg("reply: load")
		writeJSONError(w, http.StatusInternalServerError, "could not read this reply")
		return
	}
	if row == nil {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, replyMaxBody)

	if row.kind == WaitReplyWebhook {
		h.serveWebhook(w, r, token, row)
		return
	}
	if action == "decline" && (row.kind != WaitReplyApproval || row.spec.ApprovalType != "double") ||
		action == "approve" && row.kind != WaitReplyApproval {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	switch {
	case row.status == WaitReplyAnswered:
		renderReplyDone(w, http.StatusOK, row.spec, answerSummary(row))
		return
	case row.status != WaitReplyPending || row.expired:
		renderReplyDone(w, http.StatusGone, row.spec, "This request has expired.")
		return
	case !h.executionWaits(ctx, row.executionID):
		renderReplyDone(w, http.StatusGone, row.spec, "This workflow is no longer waiting for an answer.")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if row.kind == WaitReplyApproval {
		if action == "" || r.Method == http.MethodGet {
			renderApprovalPage(w, row.spec, token, action)
			return
		}
		h.answer(w, r, row, map[string]interface{}{"approved": action == "approve"})
		return
	}
	fields := replyFormFields(row)
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
	h.answer(w, r, row, values)
}

// serveWebhook takes a call to a core.wait resume URL: the request is the
// answer. A second call finds nothing waiting.
func (h *replyHandler) serveWebhook(w http.ResponseWriter, r *http.Request, token string, row *replyRow) {
	w.Header().Set("Content-Type", "application/json")
	if row.status != WaitReplyPending || row.expired || !h.executionWaits(r.Context(), row.executionID) {
		writeJSONError(w, http.StatusConflict, "nothing is waiting on this URL (already resumed, expired or finished)")
		return
	}
	if m := row.spec.HTTPMethod; m != "" && !strings.EqualFold(m, r.Method) {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed; expected "+strings.ToUpper(m))
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "request body too large or read error")
		return
	}
	item, err := buildWebhookItem(r, body, token, "")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	recorded, err := h.record(r.Context(), row, item.JSON)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not record the call")
		return
	}
	if !recorded {
		writeJSONError(w, http.StatusConflict, "nothing is waiting on this URL (already resumed)")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Workflow was resumed"})
}

// answer records values (stamped respondedAt) and shows the thank-you page,
// or the earlier answer when someone was first.
func (h *replyHandler) answer(w http.ResponseWriter, r *http.Request, row *replyRow, values map[string]interface{}) {
	values["respondedAt"] = time.Now().UTC().Format(time.RFC3339)
	recorded, err := h.record(r.Context(), row, values)
	if err != nil {
		h.logger.Warn().Err(err).Msg("reply: record")
		renderReplyDone(w, http.StatusInternalServerError, row.spec, "Your answer could not be recorded. Please try again.")
		return
	}
	if !recorded {
		if again, err := h.loadRun(r.Context(), row); err == nil && again != nil {
			row = again
		}
		renderReplyDone(w, http.StatusOK, row.spec, answerSummary(row))
		return
	}
	row.answer = values
	renderReplyDone(w, http.StatusOK, row.spec, "Got it, thanks. "+answerSummary(row))
}

// record stores the answer if the reply is still pending and wakes the
// run. It reports false when another answer was recorded first.
func (h *replyHandler) record(ctx context.Context, row *replyRow, answer map[string]interface{}) (bool, error) {
	b, err := json.Marshal(answer)
	if err != nil {
		return false, err
	}
	res, err := h.db.ExecContext(ctx,
		`UPDATE wait_replies SET status='answered', answer=?, answered_at=CURRENT_TIMESTAMP
		 WHERE execution_id=? AND node_id=? AND run_index=? AND status='pending'`,
		string(b), row.executionID, row.nodeID, row.run)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, nil
	}
	// Due at once for any engine's resume tick; the wake resumes it here
	// without waiting for the tick, and covers a run still pausing.
	_, _ = h.db.ExecContext(ctx,
		`UPDATE workflow_executions SET resume_after = NULL WHERE id = ? AND status = 'WAITING'`, row.executionID)
	if h.wake != nil {
		go h.wake(row.executionID)
	}
	return true, nil
}

// executionWaits reports whether the reply's execution can still use it.
func (h *replyHandler) executionWaits(ctx context.Context, executionID string) bool {
	var status string
	if err := h.db.QueryRowContext(ctx, `SELECT status FROM workflow_executions WHERE id = ?`, executionID).Scan(&status); err != nil {
		return false
	}
	return status == "WAITING" || status == "RUNNING" || status == "QUEUED"
}

const replyColumns = `execution_id, node_id, run_index, kind, status, spec, answer, COALESCE(expires_at, '')`

// load finds token's reply: the pending one if any (an execution URL is
// reused by every core.wait in the run), else the latest.
func (h *replyHandler) load(ctx context.Context, token string) (*replyRow, error) {
	return scanReplyRow(h.db.QueryRowContext(ctx,
		`SELECT `+replyColumns+` FROM wait_replies WHERE token = ?
		 ORDER BY (status = 'pending' AND consumed = 0) DESC, rowid DESC LIMIT 1`, token))
}

// loadRun re-reads row's reply.
func (h *replyHandler) loadRun(ctx context.Context, row *replyRow) (*replyRow, error) {
	return scanReplyRow(h.db.QueryRowContext(ctx,
		`SELECT `+replyColumns+` FROM wait_replies WHERE execution_id = ? AND node_id = ? AND run_index = ?`,
		row.executionID, row.nodeID, row.run))
}

func scanReplyRow(sc *sql.Row) (*replyRow, error) {
	var row replyRow
	var spec, answer, expires string
	err := sc.Scan(&row.executionID, &row.nodeID, &row.run, &row.kind, &row.status, &spec, &answer, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(spec), &row.spec)
	if answer != "" {
		_ = json.Unmarshal([]byte(answer), &row.answer)
	}
	row.expired = row.status == WaitReplyPending && expires != "" && expires <= formatResumeAfter(time.Now())
	return &row, nil
}

// validReplyToken accepts the shape signReply makes (43 base64url chars).
func validReplyToken(t string) bool {
	if len(t) != 43 {
		return false
	}
	for _, c := range t {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
