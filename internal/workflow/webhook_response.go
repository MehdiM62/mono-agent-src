package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/monoes/mono-agent/internal/account"
	"github.com/monoes/mono-agent/internal/accountdoor"
)

// Webhook response modes (trigger.webhook response_mode, n8n's names).
const (
	// WebhookRespondOnReceived answers as soon as the run is queued.
	WebhookRespondOnReceived = "onReceived"
	// WebhookRespondLastNode waits for the run and answers with the output
	// of the node that ran last.
	WebhookRespondLastNode = "lastNode"
	// WebhookRespondNode waits for a core.respond_webhook node to give the
	// response.
	WebhookRespondNode = "responseNode"
)

// response_data values for the lastNode mode.
const (
	webhookDataFirstEntry = "firstEntryJson"
	webhookDataAllEntries = "allEntries"
	webhookDataNone       = "noData"
)

// defaultWebhookResponseTimeout bounds how long a waiting mode holds the
// request; maxWebhookResponseTimeout bounds what response_timeout may set.
const (
	defaultWebhookResponseTimeout = 30 * time.Second
	maxWebhookResponseTimeout     = 5 * time.Minute
)

// webhookPollInterval is how often a waiting request reads the run's status.
var webhookPollInterval = 100 * time.Millisecond

// WebhookResponse is what a core.respond_webhook node sends back to the
// request that started its run.
type WebhookResponse struct {
	StatusCode int
	Headers    map[string]string
	Body       []byte
}

// WebhookRunner starts a run for a webhook delivery and returns its execution
// ID, or why no run was started. prepare, when non-nil, is called with the
// execution ID before the run is queued.
type WebhookRunner func(workflowID, nodeID string, items []Item, prepare func(executionID string)) (string, error)

// WebhookExecutions reads back the runs a webhook started.
type WebhookExecutions interface {
	GetExecutionStatus(ctx context.Context, id string) (string, error)
	GetExecution(ctx context.Context, id string) (*WorkflowExecution, error)
	// ExecutionInFlight is WorkflowEngine.ExecutionInFlight: the run is
	// queued, running, or waiting only for its sub-workflows.
	ExecutionInFlight(ctx context.Context, id string) (string, bool, error)
}

// webhookResponders holds the requests waiting for a Respond to Webhook node,
// by execution ID. It is in-process: a run adopted by another monoagent
// process cannot answer, and the request times out with a 202.
var webhookResponders sync.Map // executionID -> chan WebhookResponse

func addWebhookResponder(executionID string) chan WebhookResponse {
	ch := make(chan WebhookResponse, 1)
	webhookResponders.Store(executionID, ch)
	return ch
}

func removeWebhookResponder(executionID string) { webhookResponders.Delete(executionID) }

// DeliverWebhookResponse hands resp to the request waiting on executionID's
// run. It reports false when no request is waiting (the webhook does not use
// the responseNode mode, the run was not started by a webhook, or the request
// already got its response: only the first response counts).
func DeliverWebhookResponse(executionID string, resp WebhookResponse) bool {
	v, ok := webhookResponders.Load(executionID)
	if !ok {
		return false
	}
	select {
	case v.(chan WebhookResponse) <- resp:
		return true
	default:
		return false
	}
}

// webhookStartStatus is the status for a delivery that started no run.
func webhookStartStatus(err error) (int, string) {
	switch {
	case account.IsLoginRequired(err):
		return http.StatusServiceUnavailable, accountdoor.Code
	case errors.Is(err, ErrWebhookQueueFull):
		return http.StatusServiceUnavailable, "queue full; try again later"
	case errors.Is(err, ErrWebhookWorkflowNotFound):
		return http.StatusNotFound, "workflow not found"
	case errors.Is(err, ErrWebhookWorkflowInactive):
		return http.StatusConflict, "workflow is not active"
	default:
		return http.StatusInternalServerError, "could not start the workflow"
	}
}

// awaitWebhookResult holds the request until the run gives its answer: a
// Respond to Webhook node's response (responder non-nil), or the run's end.
// A run still going at the timeout, or paused, is answered 202 with its
// execution ID. A caller that hangs up stops the wait, not the run.
func (s *WebhookServer) awaitWebhookResult(w http.ResponseWriter, r *http.Request, reg *WebhookRegistration, execs WebhookExecutions, execID string, responder <-chan WebhookResponse) {
	timeout := reg.ResponseTimeout
	if timeout <= 0 {
		timeout = defaultWebhookResponseTimeout
	}
	// The server's WriteTimeout is shorter than a long wait.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(timeout + 10*time.Second))

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(webhookPollInterval)
	defer tick.Stop()
	for {
		select {
		case resp := <-responder:
			writeWebhookResponse(w, resp)
			return
		case <-r.Context().Done():
			return
		case <-deadline.C:
			writeWebhookJSON(w, http.StatusAccepted, map[string]interface{}{
				"success": true, "execution_id": execID, "status": "running",
			})
			return
		case <-tick.C:
		}
		st, err := execs.GetExecutionStatus(r.Context(), execID)
		if err != nil {
			s.logger.Warn().Err(err).Str("execution_id", execID).Msg("webhook: reading run status")
			continue
		}
		switch st {
		case "QUEUED", "RUNNING", "WAITING", "":
			continue
		}
		// The run ended. A response the run gave just before it ended wins.
		select {
		case resp := <-responder:
			writeWebhookResponse(w, resp)
			return
		default:
		}
		s.answerFinishedRun(w, r, reg, execs, execID, st, reg.ResponseMode == WebhookRespondNode)
		return
	}
}

// answerFinishedRun answers for a run that ended with status st.
func (s *WebhookServer) answerFinishedRun(w http.ResponseWriter, r *http.Request, reg *WebhookRegistration, execs WebhookExecutions, execID, st string, viaNode bool) {
	if st != "SUCCESS" && st != "SUCCESS_WITH_ERRORS" {
		// The run's error stays in its history; the caller may be anyone.
		writeWebhookJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false, "execution_id": execID, "status": st, "error": "workflow run did not succeed",
		})
		return
	}
	if viaNode {
		// The run finished without reaching a Respond to Webhook node.
		writeWebhookJSON(w, http.StatusOK, map[string]interface{}{"success": true, "execution_id": execID})
		return
	}
	if reg.ResponseData == webhookDataNone {
		w.WriteHeader(http.StatusOK)
		return
	}
	exec, err := execs.GetExecution(r.Context(), execID)
	if err != nil || exec == nil {
		s.logger.Warn().Err(err).Str("execution_id", execID).Msg("webhook: loading finished run")
		writeJSONError(w, http.StatusInternalServerError, "could not read the run's result")
		return
	}
	items := lastNodeItems(exec, reg.NodeID)
	var out interface{} = map[string]interface{}{}
	if reg.ResponseData == webhookDataAllEntries {
		all := make([]interface{}, 0, len(items))
		for _, it := range items {
			all = append(all, it.JSON)
		}
		out = all
	} else if len(items) > 0 && items[0].JSON != nil {
		out = items[0].JSON
	}
	writeWebhookJSON(w, http.StatusOK, out)
}

// lastNodeItems is the output of the node that finished last in exec. The
// trigger node's record holds its items as input (it passes them through).
func lastNodeItems(exec *WorkflowExecution, triggerNodeID string) []Item {
	var last *WorkflowExecutionNode
	for i := range exec.Nodes {
		n := &exec.Nodes[i]
		if n.Status != "SUCCESS" {
			continue
		}
		if last == nil || n.FinishedAt == nil || last.FinishedAt == nil || !n.FinishedAt.Before(*last.FinishedAt) {
			last = n
		}
	}
	if last == nil {
		return nil
	}
	if len(last.OutputItems) == 0 && last.NodeID == triggerNodeID {
		return last.InputItems
	}
	return last.OutputItems
}

// writeWebhookResponse writes a Respond to Webhook node's response. Header
// lines that would split the response are dropped.
func writeWebhookResponse(w http.ResponseWriter, resp WebhookResponse) {
	w.Header().Del("Content-Type")
	for k, v := range resp.Headers {
		if k == "" || strings.ContainsAny(k, "\r\n: ") || strings.ContainsAny(v, "\r\n") {
			continue
		}
		w.Header().Set(k, v)
	}
	code := resp.StatusCode
	if code < 100 || code > 599 {
		code = http.StatusOK
	}
	w.WriteHeader(code)
	_, _ = w.Write(resp.Body)
}

// writeWebhookJSON writes v as a JSON response with the given status.
func writeWebhookJSON(w http.ResponseWriter, code int, v interface{}) {
	b, err := json.Marshal(v)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not encode the response")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(b)
}
