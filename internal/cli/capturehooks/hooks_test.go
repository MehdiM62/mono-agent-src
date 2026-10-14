package capturehooks

import (
	"context"
	"database/sql"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/zalando/go-keyring"

	"github.com/monoes/mono-agent/internal/capture"
	"github.com/monoes/mono-agent/internal/extension"
	"github.com/monoes/mono-agent/internal/testdb"
	"github.com/monoes/mono-agent/internal/testhome"
)

func TestMain(m *testing.M) {
	keyring.MockInit()
	testhome.Main(m)
}

var errNoDB = errors.New("no database")

func noDB() (*sql.DB, func(), error) { return nil, nil, errNoDB }

func TestInstallReadsTheRuntimeOnceAndBuildsTheClassifier(t *testing.T) {
	t.Setenv("MONOMIND_BIN", "/nonexistent/monomind") // nothing here may run a real monomind
	srv := extension.NewServer("127.0.0.1:0", zerolog.Nop())
	var runtimes, classifiers int
	var gotLogf func(string, ...any)
	logf := func(string, ...any) {}
	h := Install(srv, logf, Deps{
		Runtime: func() string { runtimes++; return "codex" },
		NewClassifier: func(l func(string, ...any)) func(*capture.Result) {
			classifiers++
			gotLogf = l
			return func(*capture.Result) {}
		},
		OpenDB: noDB,
	})
	if runtimes != 1 || classifiers != 1 || gotLogf == nil {
		t.Fatalf("runtime read %d times, classifier built %d times (logf %v); want once each", runtimes, classifiers, gotLogf != nil)
	}
	if h.summaries.Runtime != "codex" || h.index == nil {
		t.Fatalf("hooks = %+v; want the summarizer on codex and an index queue", h)
	}
	h.Close()
}

func TestSweepWaitsForItsDelayAndStopsWithItsContext(t *testing.T) {
	var opened atomic.Int32
	open := func() (*sql.DB, func(), error) { opened.Add(1); return nil, nil, errNoDB }
	h := &Hooks{index: newCaptureIndexQueue(open, func(string, ...any) {}), open: open}

	ctx, cancel := context.WithCancel(context.Background())
	h.SweepIndexAfter(ctx, time.Hour)
	cancel()
	time.Sleep(20 * time.Millisecond)
	if n := opened.Load(); n != 0 {
		t.Fatalf("a sweep whose context ended opened the database %d times", n)
	}

	h.SweepIndexAfter(context.Background(), 0)
	deadline := time.Now().Add(5 * time.Second)
	for opened.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if n := opened.Load(); n != 1 {
		t.Fatalf("the sweep opened the database %d times, want once", n)
	}
}

func TestBrainStatusReadsTheProfilesIndex(t *testing.T) {
	db := testdb.Open(t)
	closed := 0
	open := func() (*sql.DB, func(), error) { return db.DB, func() { closed++ }, nil }
	st, err := captureBrainStatus(context.Background(), open, "default")
	if err != nil || st == nil || st.Captures != 0 {
		t.Fatalf("captureBrainStatus = %+v, %v; want an empty status", st, err)
	}
	if closed != 1 {
		t.Errorf("the database was closed %d times, want once", closed)
	}
	if _, err := captureBrainStatus(context.Background(), noDB, "default"); !errors.Is(err, errNoDB) {
		t.Errorf("a database that does not open: %v", err)
	}
}

func TestIndexHooksIgnoreCapturesWithoutAProfile(t *testing.T) {
	handleIndexCapture(nil, &capture.Result{})                             // no queue
	handleIndexCapture(newCaptureIndexQueue(noDB, nil), nil)               // no capture
	handleIndexCapture(newCaptureIndexQueue(noDB, nil), &capture.Result{}) // the shared inbox
	handleIndexSummary(newCaptureIndexQueue(noDB, nil), t.TempDir())       // no meta.json
}
