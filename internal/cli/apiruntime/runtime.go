// Package apiruntime is the OpenAI-compatible API (/v1) as the `httpapi` and
// `daemon` commands run it: the flags they share, the settings read from the
// flags, the environment and what `api config` saved, the gateway (built only
// when something serves it), its listeners, and how they stop.
package apiruntime

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/apiconfig"
	"github.com/monoes/mono-agent/internal/daemonhb"
	"github.com/monoes/mono-agent/internal/httpapi"
	"github.com/monoes/mono-agent/internal/monomind"
	"github.com/monoes/mono-agent/internal/openaiapi"
	"github.com/monoes/mono-agent/internal/tlsserve"
)

// Environment variables of the dedicated /v1 listener's TLS certificate.
const (
	TLSCertEnv = "MONOAGENT_API_TLS_CERT"
	TLSKeyEnv  = "MONOAGENT_API_TLS_KEY"
)

// Deps are the CLI's pieces the runtime uses.
type Deps struct {
	// Version is the CLI's version, for the gateway and the heartbeat.
	Version func() string
	// InvalidInput builds the CLI's invalid-input error (exit code 3).
	InvalidInput func(format string, a ...interface{}) error
	// AsCLIError gives the errors of internal/apiconfig the CLI's exit codes.
	AsCLIError func(error) error
}

// Flags are the options `httpapi` and `daemon` share for the
// OpenAI-compatible API (/v1).
type Flags struct {
	v1Addr             string
	confinement        string
	contextConfinement string
	autoConfinement    string
	maxConcurrent      int
	maxConcurrentSet   bool // --max-concurrent was given, so an explicit 0 is an error and not "the default"
}

// optInt is an int flag that remembers whether it was given.
type optInt struct {
	v   *int
	set *bool
}

func (o optInt) String() string {
	if o.v == nil {
		return "0"
	}
	return strconv.Itoa(*o.v)
}

func (o optInt) Set(s string) error {
	n, err := strconv.Atoi(s)
	if err != nil {
		return err
	}
	*o.v, *o.set = n, true
	return nil
}

func (optInt) Type() string { return "int" }

// Bind adds the flags to cmd.
func (f *Flags) Bind(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.v1Addr, "v1-addr", "",
		"Dedicated listener for the OpenAI-compatible API (serves only /v1 and /health). Off-loopback it requires TLS: "+
			TLSCertEnv+"/"+TLSKeyEnv+", else a self-signed certificate. Also MONOAGENT_API_V1_ADDR")
	cmd.Flags().StringVar(&f.confinement, "confinement", "",
		"Strongest runtime class the API serves: chat-only, sandboxed or any (default: any on loopback, chat-only off-loopback). Also MONOAGENT_API_CONFINEMENT")
	cmd.Flags().StringVar(&f.contextConfinement, "context-confinement", "",
		"Strongest runtime class a key created with --context may use: chat-only, sandboxed or any (default chat-only, never above --confinement). "+
			"Its requests carry excerpts of the profile's knowledge, which includes captured web pages nobody vetted. Also MONOAGENT_API_CONTEXT_CONFINEMENT")
	cmd.Flags().StringVar(&f.autoConfinement, "auto-confinement", "",
		"Strongest runtime class the auto model may pick: chat-only, sandboxed or any (default chat-only, never above --confinement). "+
			"A prompt can steer which model Jev picks, and its author need not hold the key. Also MONOAGENT_API_AUTO_CONFINEMENT")
	cmd.Flags().Var(optInt{&f.maxConcurrent, &f.maxConcurrentSet}, "max-concurrent",
		fmt.Sprintf("Maximum number of API turns running at once, 1 to %d (default 4). Also MONOAGENT_API_MAX_CONCURRENT", openaiapi.MaxConcurrentLimit))
}

// Runtime is the gateway's settings and how its listeners are set up. The
// gateway itself is built when something is going to serve it: building it
// takes the lock on the working folders and empties them, which a process that
// serves no /v1 (a daemon started with --api=false, a second server over the
// same home) must not do to one that does.
type Runtime struct {
	cli      Deps
	deps     openaiapi.Deps
	conf     openaiapi.Config
	override string // an explicit confinement; "" means each listener's default
	// contextMax is the strongest class a key created with --context may use.
	contextMax openaiapi.Class
	// autoMax is the strongest class the auto model may pick.
	autoMax openaiapi.Class
	v1Addr  string
	// tlsCertFile and tlsKeyFile are the saved pair of the dedicated listener's certificate:
	// tlsserve uses them when the environment names none.
	tlsCertFile, tlsKeyFile string
	// resolved is the effective value of each setting and where it came from, for the
	// heartbeat.
	resolved []apiconfig.Resolved
	logf     func(format string, args ...any)
	// disabled is why this runtime serves nothing (the daemon found the settings saved with
	// `api config` unusable, see NewForDaemon), nil for a runtime that does.
	disabled error

	mu       sync.Mutex
	gw       *openaiapi.Gateway // nil until built, and when it could not be
	gwErr    error
	v1Done   chan struct{}      // closed when the dedicated listener has stopped; nil without one
	cancelV1 context.CancelFunc // stops the dedicated listener

	// published is gw, readable without mu. Building the gateway holds mu while it
	// empties the working folders, which takes as long as a process that keeps
	// changing one makes it take: ending the turns (a second interrupt) must not
	// wait for that, and while the gateway is being built it has no turns.
	published atomic.Pointer[openaiapi.Gateway]
}

// New reads the flags, falling back to the environment and then to the settings
// saved with `api config` (an empty variable is an unset one). Bad values are invalid input
// (exit 3), saved ones included: a server does not start on settings that fail their rules.
func New(db *sql.DB, f Flags, logf func(format string, args ...any), d Deps) (*Runtime, error) {
	saved, err := apiconfig.LoadValid(context.Background(), db)
	err = d.AsCLIError(err)
	if err != nil {
		return nil, &savedSettingsError{err}
	}
	getenv := apiconfig.Overlay(saved, os.Getenv) // the environment, with the saved settings under it
	conf, err := openaiapi.ConfigFromEnv(getenv)
	if err != nil {
		return nil, d.InvalidInput("%v", err)
	}
	if f.maxConcurrent < 0 || (f.maxConcurrentSet && f.maxConcurrent == 0) || f.maxConcurrent > openaiapi.MaxConcurrentLimit {
		return nil, d.InvalidInput("--max-concurrent must be a number from 1 to %d, got %d", openaiapi.MaxConcurrentLimit, f.maxConcurrent)
	}
	if f.maxConcurrent > 0 {
		conf.MaxConcurrent = f.maxConcurrent
	}

	override := f.confinement
	if override == "" {
		override = getenv("MONOAGENT_API_CONFINEMENT")
	}
	if override != "" {
		if _, err := openaiapi.ParsePolicy(override); err != nil {
			return nil, d.InvalidInput("%v", err)
		}
	}
	contextMax, err := apiconfig.EffectiveContextMax(f.contextConfinement, getenv)
	err = d.AsCLIError(err)
	if err != nil {
		return nil, err
	}
	autoMax, err := apiconfig.EffectiveAutoMax(f.autoConfinement, getenv)
	err = d.AsCLIError(err)
	if err != nil {
		return nil, err
	}
	v1 := f.v1Addr
	if v1 == "" {
		v1 = getenv("MONOAGENT_API_V1_ADDR")
	}
	if v1 != "" {
		if err := apiconfig.ValidListenAddr(v1); err != nil {
			return nil, d.InvalidInput("--v1-addr (MONOAGENT_API_V1_ADDR) must be host:port, such as 127.0.0.1:9443 or :9443: %v", err)
		}
	}

	deps := openaiapi.DefaultDeps(db, d.Version())
	deps.Logf = logf
	resolved := apiconfig.ResolveAll(apiconfig.Flags{
		V1Addr: f.v1Addr, Confinement: f.confinement, ContextConfinement: f.contextConfinement,
		AutoConfinement: f.autoConfinement, MaxConcurrent: f.maxConcurrent,
	}, os.Getenv, saved)
	return &Runtime{
		cli: d, deps: deps, conf: conf, override: override, contextMax: contextMax, autoMax: autoMax, v1Addr: v1,
		tlsCertFile: saved.TLSCertFile, tlsKeyFile: saved.TLSKeyFile, resolved: resolved, logf: logf,
	}, nil
}

// savedSettingsError is the error of New that comes from the settings saved with
// `api config` (a row that cannot be read, a value that fails its rule), as against a flag or a
// variable of this start. It is that error to everything else (the exit code, the message), and
// the daemon tells it apart: see NewForDaemon.
type savedSettingsError struct{ err error }

func (e *savedSettingsError) Error() string { return e.err.Error() }
func (e *savedSettingsError) Unwrap() error { return e.err }

// NewForDaemon is New for the daemon, which does more than serve the API. When the
// settings saved with `api config` cannot be used the daemon must not stop with them: a login
// service would start it again and again, and nothing it runs (workflows, schedules, org runs)
// would ever run. It serves no OpenAI-compatible API instead and says why where it logs; `api
// config show` and the repair it names are how the person finds out and fixes it. A saved setting
// is never ignored to make the API start: ignoring a row would serve, with the defaults, a server
// that someone had limited. A bad flag or variable of this start is still the error it was.
func NewForDaemon(db *sql.DB, f Flags, logf func(format string, args ...any), d Deps) (*Runtime, error) {
	rt, err := New(db, f, logf, d)
	var unusable *savedSettingsError
	if !errors.As(err, &unusable) {
		return rt, err
	}
	why := fmt.Errorf("the settings saved with `monoagentcli api config` cannot be used, so this daemon does not serve the OpenAI-compatible API: %w", unusable.err)
	logf("%v", why)
	return &Runtime{cli: d, logf: logf, gwErr: errors.New("the saved settings cannot be used"), disabled: why}, nil
}

// gateway builds the gateway on first use. A process that cannot (another one
// owns the working folders) gets the error every time.
func (a *Runtime) gateway() (*openaiapi.Gateway, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.gw == nil && a.gwErr == nil {
		a.gw, a.gwErr = openaiapi.New(a.deps, a.conf)
		if a.gwErr != nil && !errors.Is(a.gwErr, openaiapi.ErrScratchBusy) {
			a.gwErr = fmt.Errorf("starting the OpenAI-compatible API: %w", a.gwErr)
		}
		a.published.Store(a.gw)
	}
	return a.gw, a.gwErr
}

// built is the gateway if it has been built, nil otherwise. It never waits for a
// build in progress.
func (a *Runtime) built() *openaiapi.Gateway {
	return a.published.Load()
}

// Disabled is why this runtime serves nothing (the settings saved with `api
// config` cannot be used, see NewForDaemon), nil for a runtime that does.
func (a *Runtime) Disabled() error { return a.disabled }

// ReleaseUnused gives the working folders back when no listener serves the
// gateway. The HTTP API server takes its routes when it is made, so the gateway
// exists before the listener it is mounted on is bound: a bind that then fails
// (a port in use) must not leave the home locked by a process that serves
// nothing. A gateway the dedicated listener serves stays.
func (a *Runtime) ReleaseUnused() {
	a.mu.Lock()
	if a.v1Done != nil {
		a.mu.Unlock()
		return
	}
	gw := a.gw
	a.gw, a.gwErr = nil, nil
	a.published.Store(nil)
	a.mu.Unlock()
	if gw != nil {
		gw.Shutdown(time.Second)
	}
}

// Policy is the confinement policy of a listener bound to addr: what it
// serves, what a key created with --context may use of that, and what the auto
// model may pick of it.
func (a *Runtime) Policy(addr string) openaiapi.Policy {
	p := openaiapi.DefaultPolicy(addr)
	if a.override != "" {
		p, _ = openaiapi.ParsePolicy(a.override) // validated in New
	}
	p.ContextMax = a.contextMax
	p.AutoMax = a.autoMax
	return p
}

// contextReport is the context maximum to record in the daemon's heartbeat.
func (a *Runtime) contextReport() string {
	return openaiapi.Policy{Max: a.contextMax}.String()
}

// autoReport is the auto maximum to record in the daemon's heartbeat.
func (a *Runtime) autoReport() string {
	return openaiapi.Policy{Max: a.autoMax}.String()
}

// settingsReport is the effective value of every setting and where it came from, for the
// daemon's heartbeat.
func (a *Runtime) settingsReport() map[string]daemonhb.APISetting {
	if len(a.resolved) == 0 {
		return nil // a runtime that serves nothing resolved nothing: say nothing, as a daemon that predates the report does
	}
	out := make(map[string]daemonhb.APISetting, len(a.resolved))
	for _, r := range a.resolved {
		out[r.Key] = daemonhb.APISetting{Value: r.Text, Source: r.Source}
	}
	return out
}

// Heartbeat is the daemon's heartbeat before its schedules: what it serves, the policies it
// applies and the setting each of them came from. apiAddr, bridgeAddr and v1Addr are the
// addresses it is serving, "" for what it does not.
func (a *Runtime) Heartbeat(apiAddr, bridgeAddr, v1Addr string) daemonhb.Heartbeat {
	return daemonhb.Heartbeat{
		APIAddr: apiAddr, BridgeAddr: bridgeAddr, V1Addr: v1Addr, Version: a.cli.Version(),
		APIConfinement: a.confinementReport(apiAddr, false), V1Confinement: a.confinementReport(v1Addr, true),
		ContextConfinement: a.contextReport(), AutoConfinement: a.autoReport(),
		APISettings: a.settingsReport(),
	}
}

// MainMount returns the route registrar that serves /v1 on the main HTTP API
// listener, or nil when that listener is not loopback: /v1 is never served
// in plaintext off-loopback. Expose it with --v1-addr instead.
func (a *Runtime) MainMount(mainAddr string) func(*http.ServeMux) {
	if !tlsserve.IsLoopbackAddr(mainAddr) {
		a.logf("the HTTP API listener %s is not loopback, so it does not serve /v1: give the OpenAI-compatible API its own listener with --v1-addr", mainAddr)
		return nil
	}
	gw, err := a.gateway()
	if err != nil {
		a.logf("this process does not serve /v1: %v", err)
		return nil
	}
	p := a.Policy(mainAddr)
	return func(mux *http.ServeMux) { gw.Mount(mux, p) }
}

// StartV1 binds the dedicated /v1 listener, when one is configured, and
// serves it until ctx ends. It returns the bound address, "" when there is
// none. Off-loopback the listener is TLS only.
func (a *Runtime) StartV1(ctx context.Context) (string, error) {
	if a.v1Addr == "" {
		return "", nil
	}
	tlsCfg, err := tlsserve.Resolve(tlsserve.Config{
		Addr: a.v1Addr, CertEnv: TLSCertEnv, KeyEnv: TLSKeyEnv, CertFile: a.tlsCertFile, KeyFile: a.tlsKeyFile,
		CacheDir: "api-tls", CommonName: "monoagentcli API server (self-signed)", Label: "API server",
		Warn: func(msg string) { a.logf("%s", msg) },
	})
	if err != nil {
		return "", err
	}
	ln, err := net.Listen("tcp", a.v1Addr)
	if err != nil {
		return "", fmt.Errorf("listen on %s: %w", a.v1Addr, err)
	}
	// Only now, with the address in hand: building the gateway takes the home's
	// working folders, and a listener that cannot bind must not keep them.
	gw, err := a.gateway()
	if err != nil {
		ln.Close()
		return "", err
	}
	p := a.Policy(a.v1Addr)
	// The listener gets a context of its own: a command whose other listener
	// failed to bind never ends the one it was started with, and drain must
	// still be able to stop this one.
	lctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	a.mu.Lock()
	a.v1Done, a.cancelV1 = done, cancel
	a.mu.Unlock()
	go func() {
		defer close(done)
		if err := gw.Serve(lctx, ln, p, tlsCfg); err != nil && lctx.Err() == nil {
			a.logf("the /v1 listener stopped: %v", err)
		}
	}()
	return ln.Addr().String(), nil
}

// apiDrainWait is how long a stopping command waits for the API's turns to be
// killed. The listeners' own graceful shutdown has already had its share.
const apiDrainWait = 30 * time.Second

// Drain stops the dedicated listener, which gives its running requests their
// grace and then ends the turns it still has, and waits for it; then it ends
// every turn that is still running and waits for them to be gone. A command
// calls it after its servers were told to stop, so that no agent CLI outlives
// the process that was supposed to supervise it.
func (a *Runtime) Drain() {
	// The dedicated listener first: it gives its requests their grace, then
	// ends the turns it still has.
	a.mu.Lock()
	done, cancel := a.v1Done, a.cancelV1
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(apiDrainWait):
			a.logf("the /v1 listener did not stop within %s", apiDrainWait)
		}
	}
	if gw := a.built(); gw != nil && !gw.Shutdown(apiDrainWait) {
		a.logf("turns of the OpenAI-compatible API were still running %s after the server stopped", apiDrainWait)
	}
}

// killReapMargin is how long killTurns waits beyond monomind.KillGrace. A turn
// that ignores its cancel is ended by a group kill that Exec arms KillGrace
// after the cancel reaches it, so a wait of just the grace returns a moment
// before the kill.
const killReapMargin = 3 * time.Second

// KillTurns ends the turns in flight and waits for their processes to be
// killed. A command about to exit at once (a second Ctrl+C) calls it first, so
// the agent CLIs do not outlive it: where a child gets no signal when its
// parent dies (macOS), one left behind runs on.
func (a *Runtime) KillTurns() {
	if gw := a.built(); gw != nil {
		gw.Shutdown(monomind.KillGrace + killReapMargin)
	}
}

// confinementReport is the policy to record in the daemon's heartbeat for a
// listener at addr: "" when there is no such listener, or when it does not
// serve /v1 (the main listener serves it only on loopback, and nothing serves
// it in a process that could not build the gateway).
func (a *Runtime) confinementReport(addr string, dedicated bool) string {
	a.mu.Lock()
	failed := a.gwErr != nil
	a.mu.Unlock()
	if failed || addr == "" || (!dedicated && !tlsserve.IsLoopbackAddr(addr)) {
		return ""
	}
	return a.Policy(addr).String()
}

// DaemonRoutes is what the daemon's HTTP API server mounts through its single
// ExtraRoutes slot: the org endpoint receiver and, on a loopback bind, the
// OpenAI-compatible API. Mounting only one of them would silently lose the
// other.
func DaemonRoutes(orgReceiver func(*http.ServeMux), api *Runtime, addr string) func(*http.ServeMux) {
	return composeRoutes(orgReceiver, api.MainMount(httpapi.ResolveAddr(addr)))
}

// composeRoutes mounts several route registrars, in order, on one mux. nil
// registrars are skipped. httpapi.Options has a single ExtraRoutes slot.
func composeRoutes(fns ...func(*http.ServeMux)) func(*http.ServeMux) {
	return func(mux *http.ServeMux) {
		for _, f := range fns {
			if f != nil {
				f(mux)
			}
		}
	}
}
