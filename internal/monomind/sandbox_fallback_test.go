package monomind

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// writeFallbackFake is a monomind advertising caps. Its agent exec records
// argv; with refuse it fails the way monomind does for --sandbox-fallback
// fail (fatal unsupported, exit 2), else it streams start+startExtra.
func writeFallbackFake(t *testing.T, caps []string, startExtra string, refuse bool) (bin, record string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake monomind is a shell script")
	}
	dir := t.TempDir()
	bin, record = filepath.Join(dir, "monomind"), filepath.Join(dir, "argv.txt")
	body := `  echo '{"v":1,"type":"start","runtime":"antigravity"` + startExtra + `}'` + "\n" +
		`  echo '{"v":1,"type":"result","subtype":"success","stop_reason":"end_turn","text":"ok"}'` + "\n" +
		`  echo '{"v":1,"type":"done","exit_code":0}'` + "\n  exit 0\n"
	if refuse {
		body = `  echo '{"v":1,"type":"error","code":"unsupported","fatal":true,"message":"--sandbox workspace-write is not supported by runtime \"antigravity\""}'` + "\n" +
			`  echo '{"v":1,"type":"done","exit_code":2}'` + "\n  exit 2\n"
	}
	script := "#!/bin/sh\n" +
		`if [ "$1" = "--version" ]; then echo '{"v":1,"version":"2.24.5","min_caller":"1.0.0","capabilities":["` + strings.Join(caps, `","`) + `"]}'; exit 0; fi` + "\n" +
		`if [ "$1" = "agent" ] && [ "$2" = "exec" ]; then` + "\n" +
		`  printf '%s\n' "$@" > '` + record + "'\n" + body + "fi\nexit 2\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, record
}

var fallbackCaps = []string{"agent-exec", "agent-scan", "org-json-v1", CapAgentExecSandbox, CapAgentExecSandboxFallback}

func TestSandboxFallbackSetting(t *testing.T) {
	prev := SandboxFallbackProvider
	t.Cleanup(func() { SandboxFallbackProvider = prev })
	saved := ""
	SandboxFallbackProvider = func() string { return saved }

	t.Setenv(SandboxFallbackEnv, "")
	for s, want := range map[string]string{"": "run", "bogus": "run", "strictest": "strictest", " FAIL ": "fail"} {
		saved = s
		if got := SandboxFallbackSetting(); got != want {
			t.Errorf("saved %q: setting = %q, want %q", s, got, want)
		}
	}
	saved = "fail"
	t.Setenv(SandboxFallbackEnv, "strictest")
	if got := SandboxFallbackSetting(); got != "strictest" {
		t.Errorf("env override: %q", got)
	}
	t.Setenv(SandboxFallbackEnv, "nonsense")
	if got := SandboxFallbackSetting(); got != "fail" {
		t.Errorf("an invalid env value falls through to the saved one, got %q", got)
	}
	SandboxFallbackProvider = nil
	t.Setenv(SandboxFallbackEnv, "")
	if got := SandboxFallbackSetting(); got != "run" {
		t.Errorf("no provider: %q", got)
	}
}

// With "run" the plan IS SandboxArgs, for every case TestSandboxArgs knows:
// the setting's default changes nothing.
func TestSandboxFallbackPlanRunIsSandboxArgs(t *testing.T) {
	caps := []*CapabilitySet{nil, NewCapabilitySet("2.10.0"), NewCapabilitySet("2.18.5"), NewCapabilitySet("9.0.0", CapAgentExecSandbox), NewCapabilitySet("9.0.0", fallbackCaps...)}
	for _, c := range caps {
		for _, rt := range []string{"codex", "claude", "antigravity", "copilot", "qwen"} {
			for _, modes := range [][]string{nil, {SandboxFull}, scanModes2190["codex"], {"restricted", SandboxFull}} {
				for _, mode := range []string{"", SandboxReadOnly, SandboxWorkspaceWrite, SandboxFull} {
					wantArgs, wantEff := SandboxArgs(c, modes, rt, mode)
					args, eff, active, note := SandboxFallbackPlan(c, modes, rt, mode, SandboxFallbackRun)
					if !slices.Equal(args, wantArgs) || eff != wantEff || active || note != "" {
						t.Fatalf("run: caps %v modes %v %s %q: got %v %q %v %q, want %v %q", c, modes, rt, mode, args, eff, active, note, wantArgs, wantEff)
					}
				}
			}
		}
	}
}

func TestSandboxFallbackPlan(t *testing.T) {
	has := NewCapabilitySet("2.24.5", fallbackCaps...)
	noFlag := NewCapabilitySet("2.24.5", CapAgentExecSandbox)
	restricted := []string{"restricted", SandboxFull}
	cases := []struct {
		name         string
		caps         *CapabilitySet
		modes        []string
		runtime      string
		fallback     string
		wantArgs     []string
		wantEff      string
		wantActive   bool
		wantNoteSome bool
	}{
		{"strictest asks monomind", has, restricted, "antigravity", "strictest",
			[]string{SandboxFlag, SandboxWorkspaceWrite, SandboxFallbackFlag, "strictest"}, SandboxStatusRestricted, true, false},
		{"fail asks monomind to refuse", has, restricted, "antigravity", "fail",
			[]string{SandboxFlag, SandboxWorkspaceWrite, SandboxFallbackFlag, "fail"}, SandboxStatusUnsupported, true, false},
		{"mode listed: no flag added", has, scanModes2190["codex"], "codex", "strictest",
			[]string{SandboxFlag, SandboxWorkspaceWrite}, SandboxStatusSandboxed, false, false},
		{"claude unchanged", has, restricted, "claude", "strictest", nil, SandboxStatusScoped, false, false},
		{"scan failed: no flag ever", has, nil, "antigravity", "strictest", nil, SandboxStatusUnsupported, false, false},
		{"capability missing: ignored, and says so", noFlag, restricted, "antigravity", "strictest", nil, SandboxStatusUnsupported, false, true},
		{"capability missing, fail: ignored too", noFlag, restricted, "antigravity", "fail", nil, SandboxStatusUnsupported, false, true},
	}
	for _, tc := range cases {
		args, eff, active, note := SandboxFallbackPlan(tc.caps, tc.modes, tc.runtime, SandboxWorkspaceWrite, tc.fallback)
		if !slices.Equal(args, tc.wantArgs) || eff != tc.wantEff || active != tc.wantActive || (note != "") != tc.wantNoteSome {
			t.Errorf("%s: got %v %q %v %q", tc.name, args, eff, active, note)
		}
	}
}

func TestAppliedSandboxStatus(t *testing.T) {
	for applied, want := range map[string]string{
		SandboxWorkspaceWrite: SandboxStatusSandboxed,
		"restricted":          SandboxStatusRestricted,
		SandboxReadOnly:       SandboxStatusRestricted,
		SandboxFull:           SandboxStatusOff,
	} {
		if got := AppliedSandboxStatus(SandboxWorkspaceWrite, applied); got != want {
			t.Errorf("applied %s = %s, want %s", applied, got, want)
		}
	}
}

func execWithFallback(t *testing.T, setting string, bin string, required bool) (*TurnResult, error, Event) {
	t.Helper()
	t.Setenv(SandboxFallbackEnv, setting)
	var start Event
	res, err := Exec(context.Background(), ExecOptions{Bin: bin, Runtime: "antigravity", Prompt: "hi", Sandbox: SandboxWorkspaceWrite, RequireSandbox: required},
		func(ev Event) {
			if ev.Type == EventStart {
				start = ev
			}
		})
	return res, err, start
}

// The argv and statuses for every setting, against a monomind that has the
// capability and an antigravity that lists only restricted and full.
func TestExecSandboxFallbackSettings(t *testing.T) {
	stubSandboxScan(t, map[string][]string{"antigravity": {"restricted", SandboxFull}})
	cases := []struct {
		setting    string
		startExtra string
		wantPairs  []string
		wantStatus string
	}{
		// "run" and unset: byte-identical to before the setting.
		{"run", `,"sandbox_applied":"full"`, nil, SandboxStatusUnsupported},
		{"", "", nil, SandboxStatusUnsupported},
		{"strictest", `,"sandbox_requested":"workspace-write","sandbox_applied":"restricted"`,
			[]string{"--sandbox workspace-write"}, SandboxStatusRestricted},
	}
	for _, tc := range cases {
		bin, record := writeFallbackFake(t, fallbackCaps, tc.startExtra, false)
		res, err, start := execWithFallback(t, tc.setting, bin, false)
		if err != nil || res.Err != nil {
			t.Fatalf("%q: %v %+v", tc.setting, err, res.Err)
		}
		argv := readArgv(t, record)
		if got := sandboxPairs(argv); !slices.Equal(got, tc.wantPairs) {
			t.Errorf("%q: sandbox args = %v, want %v", tc.setting, got, tc.wantPairs)
		}
		if tc.setting == "strictest" {
			if v, ok := flagValue(argv, SandboxFallbackFlag); !ok || v != "strictest" {
				t.Errorf("--sandbox-fallback = %q (%v)", v, ok)
			}
		} else if slices.Contains(argv, SandboxFallbackFlag) {
			t.Errorf("%q: argv must not carry --sandbox-fallback: %v", tc.setting, argv)
		}
		if res.SandboxStatus != tc.wantStatus || start.SandboxStatus != tc.wantStatus {
			t.Errorf("%q: status = %q / start %q, want %q", tc.setting, res.SandboxStatus, start.SandboxStatus, tc.wantStatus)
		}
	}
}

func TestExecSandboxFallbackApplied(t *testing.T) {
	stubSandboxScan(t, map[string][]string{"antigravity": {"restricted", SandboxFull}})
	for applied, want := range map[string]string{"restricted": SandboxStatusRestricted, "workspace-write": SandboxStatusSandboxed, "full": SandboxStatusOff} {
		bin, _ := writeFallbackFake(t, fallbackCaps, `,"sandbox_applied":"`+applied+`"`, false)
		res, err, start := execWithFallback(t, "strictest", bin, false)
		if err != nil || res.Err != nil {
			t.Fatal(err, res.Err)
		}
		if res.SandboxStatus != want || start.SandboxApplied != applied {
			t.Errorf("applied %s: status %q (applied field %q), want %q", applied, res.SandboxStatus, start.SandboxApplied, want)
		}
	}
}

func TestExecSandboxFallbackFailIsAClearError(t *testing.T) {
	stubSandboxScan(t, map[string][]string{"antigravity": {"restricted", SandboxFull}})
	bin, record := writeFallbackFake(t, fallbackCaps, "", true)
	res, err, _ := execWithFallback(t, "fail", bin, false)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := flagValue(readArgv(t, record), SandboxFallbackFlag); v != "fail" {
		t.Errorf("--sandbox-fallback = %q, want fail", v)
	}
	if res.Err == nil || res.Err.Code != ErrUnsupported || !strings.Contains(res.Err.Message, "sandbox fallback setting is 'fail'") {
		t.Fatalf("err = %+v, want a clear unsupported refusal", res.Err)
	}
}

// restricted never satisfies RequireSandbox, and neither does fail: the
// turn does not start (no argv recorded).
func TestExecSandboxFallbackNeverSatisfiesRequireSandbox(t *testing.T) {
	stubSandboxScan(t, map[string][]string{"antigravity": {"restricted", SandboxFull}})
	for _, setting := range []string{"run", "strictest", "fail"} {
		bin, record := writeFallbackFake(t, fallbackCaps, `,"sandbox_applied":"restricted"`, false)
		_, err, _ := execWithFallback(t, setting, bin, true)
		if !errors.Is(err, ErrSandboxRequired) {
			t.Errorf("%s: err = %v, want ErrSandboxRequired", setting, err)
		}
		if _, statErr := os.Stat(record); !os.IsNotExist(statErr) {
			t.Errorf("%s: monomind was started for a turn that requires a sandbox", setting)
		}
	}
}

// Without the capability the setting is ignored (the turn behaves as run)
// and the start event says so.
func TestExecSandboxFallbackWithoutCapability(t *testing.T) {
	stubSandboxScan(t, map[string][]string{"antigravity": {"restricted", SandboxFull}})
	bin, record := writeFallbackFake(t, []string{"agent-exec", "agent-scan", "org-json-v1", CapAgentExecSandbox}, "", false)
	res, err, start := execWithFallback(t, "strictest", bin, false)
	if err != nil || res.Err != nil {
		t.Fatal(err, res.Err)
	}
	if argv := readArgv(t, record); slices.Contains(argv, SandboxFlag) || slices.Contains(argv, SandboxFallbackFlag) {
		t.Errorf("argv = %v, want no sandbox flags", argv)
	}
	if res.SandboxStatus != SandboxStatusUnsupported || start.SandboxNote != SandboxFallbackIgnoredNote || res.Sandbox.SandboxNote == "" {
		t.Errorf("status %q note %q / result note %q", res.SandboxStatus, start.SandboxNote, res.Sandbox.SandboxNote)
	}
}
