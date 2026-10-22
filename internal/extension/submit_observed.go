package extension

import (
	"context"
	"fmt"
)

// form.submit_observed: "the person submitted the form in this tab".
//
// The extension (chrome-extension/submit_observe.js) sends it only for a
// submit event the browser itself marked trusted (event.isTrusted: a person's
// click or Enter, not page script), followed by a same-tab navigation or an
// in-page change of the form. A page that merely shows "Application
// submitted" sends nothing. The host matches tab id and origin to a parked
// application and records a hint on its fill; it never changes the
// application's status (applied is only ever `application send`, the
// person's), and the app asks the person to confirm.
//
//	extension -> Go  {"kind":"request","id":"req-...","method":"form.submit_observed",
//	                  "params":{"tab_id":12,"origin":"https://jobs.example","url":"https://jobs.example/thanks"}}
//	Go -> extension  {"kind":"reply","id":"req-...","ok":true,"data":{"matched":true}}

// MethodFormSubmitObserved reports a trusted submit seen in a tab.
const MethodFormSubmitObserved = "form.submit_observed"

// SubmitObservation is a checked form.submit_observed request.
type SubmitObservation struct {
	TabID  int
	Origin string
	URL    string
}

// SubmitSink records an observed submit on the parked application it belongs
// to. matched is false when no parked application is on that tab and origin.
type SubmitSink interface {
	Observed(ctx context.Context, o SubmitObservation) (matched bool, err error)
}

// SetSubmitSink installs form.submit_observed over sink; nil removes it.
func (s *Server) SetSubmitSink(sink SubmitSink) {
	if sink == nil {
		s.handlerMu.Lock()
		delete(s.handlers, MethodFormSubmitObserved)
		s.handlerMu.Unlock()
		return
	}
	s.HandleRequest(MethodFormSubmitObserved, func(ctx context.Context, req *Request, _ ProgressFunc) (any, error) {
		o := SubmitObservation{TabID: req.Int("tab_id", 0), Origin: req.String("origin"), URL: req.String("url")}
		if o.TabID <= 0 || o.TabID > 1<<31-1 {
			return nil, refusal("form.submit_observed needs a tab_id")
		}
		if o.Origin == "" || len(o.Origin) > 2048 || len(o.URL) > 8192 {
			return nil, refusal(fmt.Sprintf("form.submit_observed needs the page's origin (tab %d)", o.TabID))
		}
		matched, err := sink.Observed(ctx, o)
		if err != nil {
			return nil, err
		}
		return map[string]bool{"matched": matched}, nil
	})
}
