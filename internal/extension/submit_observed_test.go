package extension

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
)

type fakeSubmitSink struct {
	got     []SubmitObservation
	matched bool
}

func (f *fakeSubmitSink) Observed(_ context.Context, o SubmitObservation) (bool, error) {
	f.got = append(f.got, o)
	return f.matched, nil
}

func callSubmitObserved(t *testing.T, srv *Server, params map[string]any) (any, error) {
	t.Helper()
	h, ok := srv.handlerFor(MethodFormSubmitObserved)
	if !ok {
		t.Fatalf("%s is not registered", MethodFormSubmitObserved)
	}
	return h(context.Background(), &Request{Method: MethodFormSubmitObserved, Params: params}, func(string, string) {})
}

func TestFormSubmitObserved_AdvertisedOnlyWithASink(t *testing.T) {
	srv := NewServer("127.0.0.1:0", zerolog.Nop())
	if hasMethod(srv.RequestMethods(), MethodFormSubmitObserved) {
		t.Fatal("advertised with no sink")
	}
	srv.SetSubmitSink(&fakeSubmitSink{})
	if !hasMethod(srv.RequestMethods(), MethodFormSubmitObserved) {
		t.Fatal("not advertised after SetSubmitSink")
	}
	srv.SetSubmitSink(nil)
	if hasMethod(srv.RequestMethods(), MethodFormSubmitObserved) {
		t.Fatal("still advertised after the sink was removed")
	}
}

func TestFormSubmitObserved_PassesACheckedObservationAndRefusesTheRest(t *testing.T) {
	sink := &fakeSubmitSink{matched: true}
	srv := NewServer("127.0.0.1:0", zerolog.Nop())
	srv.SetSubmitSink(sink)
	got, err := callSubmitObserved(t, srv, map[string]any{"tab_id": float64(12), "origin": "https://jobs.example", "url": "https://jobs.example/thanks"})
	if err != nil {
		t.Fatal(err)
	}
	if m, _ := got.(map[string]bool); !m["matched"] {
		t.Fatalf("reply = %v", got)
	}
	if len(sink.got) != 1 || sink.got[0].TabID != 12 || sink.got[0].Origin != "https://jobs.example" {
		t.Fatalf("sink got %+v", sink.got)
	}
	for name, p := range map[string]map[string]any{
		"no tab":      {"origin": "https://jobs.example"},
		"zero tab":    {"tab_id": float64(0), "origin": "https://jobs.example"},
		"huge tab":    {"tab_id": float64(1 << 40), "origin": "https://jobs.example"},
		"no origin":   {"tab_id": float64(3)},
		"origin long": {"tab_id": float64(3), "origin": string(make([]byte, 3000))},
	} {
		if _, err := callSubmitObserved(t, srv, p); err == nil || codeOf(t, err) != CodeInvalidInput {
			t.Errorf("%s: %v", name, err)
		}
	}
	if len(sink.got) != 1 {
		t.Fatalf("a refused request reached the sink: %+v", sink.got)
	}
}
