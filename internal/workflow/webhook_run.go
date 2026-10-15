package workflow

import (
	"context"
	"net/http"

	"github.com/monoes/mono-agent/internal/tracesig"
)

// serveRun turns an accepted, authenticated request into a run and answers
// it. The answer depends on the registration's response mode:
//
//	onReceived    {"success":true,"execution_id":...} with ResponseCode
//	lastNode      the output of the node that ran last, once the run ends
//	responseNode  whatever the run's Respond to Webhook node sends
//
// A delivery that starts no run (login required, queue full, workflow gone
// or inactive) is answered with a non-2xx status and a JSON error, and a
// delivery ID seen before (DedupeHeader) with the first delivery's run.
func (s *WebhookServer) serveRun(w http.ResponseWriter, r *http.Request, reg *WebhookRegistration, pathKey string, body []byte) {
	if reg.Verify != nil {
		if err := reg.Verify(r.Header, body); err != nil {
			writeJSONError(w, http.StatusUnauthorized, err.Error())
			return
		}
	}
	item, err := buildWebhookItem(r, body, pathKey, reg.AuthHeader)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.mu.RLock()
	run, execs, deliveries := s.runner, s.execs, s.deliveries
	s.mu.RUnlock()

	// Dedupe comes before the trace admission, so a redelivery is not
	// counted as another crossing of its chain.
	deliveryID := ""
	if reg.DedupeHeader != "" && deliveries != nil {
		deliveryID = r.Header.Get(reg.DedupeHeader)
	}
	if len(deliveryID) > maxDeliveryIDLen {
		writeJSONError(w, http.StatusBadRequest, reg.DedupeHeader+" is too long")
		return
	}
	started := false
	if deliveryID != "" {
		claimed, prevExec, err := deliveries.Claim(r.Context(), reg.WorkflowID, deliveryID)
		if err != nil {
			s.logger.Error().Err(err).Str("workflow_id", reg.WorkflowID).Msg("webhook: recording delivery id")
			writeJSONError(w, http.StatusServiceUnavailable, "could not record the delivery; try again")
			return
		}
		if !claimed {
			writeWebhookJSON(w, http.StatusOK, map[string]interface{}{
				"success": true, "duplicate": true, "execution_id": prevExec,
			})
			return
		}
		defer func() {
			if !started {
				// Let the sender's retry through.
				if err := deliveries.Release(context.WithoutCancel(r.Context()), reg.WorkflowID, deliveryID); err != nil {
					s.logger.Warn().Err(err).Str("workflow_id", reg.WorkflowID).Msg("webhook: releasing delivery id")
				}
			}
		}()
	}

	trace, status, msg := s.admitTrace(r.Context(), reg.WorkflowID, r.Header.Get(tracesig.Header))
	if status != 0 {
		writeJSONError(w, status, msg)
		return
	}
	if trace != nil {
		item.JSON[WebhookTraceKey] = trace
	}

	s.logger.Debug().
		Str("path", pathKey).
		Str("workflow_id", reg.WorkflowID).
		Str("method", r.Method).
		Msg("webhook triggered")

	if run == nil {
		// No runner (a server without an engine): fire and answer at once.
		reg.TriggerFn([]Item{item})
		started = true
		writeWebhookJSON(w, s.onReceivedCode(reg), map[string]interface{}{"success": true})
		return
	}

	var responder chan WebhookResponse
	var prepare func(string)
	if reg.ResponseMode == WebhookRespondNode {
		prepare = func(execID string) { responder = addWebhookResponder(execID) }
	}
	execID, err := run(reg.WorkflowID, reg.NodeID, []Item{item}, prepare)
	if responder != nil {
		defer removeWebhookResponder(execID)
	}
	if err != nil {
		code, msg := webhookStartStatus(err)
		if code >= 500 && code != http.StatusServiceUnavailable {
			s.logger.Error().Err(err).Str("workflow_id", reg.WorkflowID).Msg("webhook: starting the run")
		}
		if code == http.StatusServiceUnavailable {
			w.Header().Set("Retry-After", webhookRetryAfter)
		}
		writeJSONError(w, code, msg)
		return
	}
	started = true
	if deliveryID != "" {
		if err := deliveries.Attach(context.WithoutCancel(r.Context()), reg.WorkflowID, deliveryID, execID); err != nil {
			s.logger.Warn().Err(err).Str("workflow_id", reg.WorkflowID).Msg("webhook: recording delivery's run")
		}
	}

	waits := reg.ResponseMode == WebhookRespondLastNode || reg.ResponseMode == WebhookRespondNode
	switch {
	case waits && execs != nil:
		s.awaitWebhookResult(w, r, reg, execs, execID, responder)
	default:
		writeWebhookJSON(w, s.onReceivedCode(reg), map[string]interface{}{"success": true, "execution_id": execID})
	}
}

// onReceivedCode is the status of an onReceived answer.
func (s *WebhookServer) onReceivedCode(reg *WebhookRegistration) int {
	if reg.ResponseCode >= 200 && reg.ResponseCode <= 299 {
		return reg.ResponseCode
	}
	return http.StatusOK
}
