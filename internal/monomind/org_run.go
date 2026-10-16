package monomind

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/monoes/mono-agent/internal/logging"
	"github.com/monoes/mono-agent/internal/orgdesign"
)

// OrgRun starts `monomind org run <name> --yes [--task ...] [--dry-run]`,
// blocking until it returns. Deliberately does NOT go through runOrgJSON:
// that helper caps every call at orgTimeout (60s), but a real run blocks
// for as long as the org takes to finish (minutes to hours) unless a `org
// serve` daemon is already live for the project, in which case it hands
// off and returns almost immediately — either way, the caller's own ctx is
// the only deadline that should apply here. Callers that need a bounded
// wait should poll OrgStatus instead of waiting on this call to return.
func OrgRun(ctx context.Context, projectRoot, name, task string, dryRun bool) (json.RawMessage, error) {
	bin, err := EnsureIn(ctx, projectRoot)
	if err != nil {
		return nil, err
	}
	if err := checkOrgSigned(ctx, projectRoot, name); err != nil {
		return nil, err
	}
	args := []string{"run", name, "--yes"}
	if task != "" {
		args = append(args, "--task", task)
	}
	if dryRun {
		args = append(args, "--dry-run")
	}
	full := append(append([]string{"org"}, args...), "--format", "json")

	// Deliberately NOT exec.CommandContext: its default Cancel behavior
	// only kills the direct child on ctx cancellation, not its process
	// group — leaving any agent-CLI grandchild `monomind org run` spawned
	// as an orphan (this can block "minutes to hours" per the doc comment
	// above, so cancellation is the only way most callers ever stop it).
	// setProcessGroup + a manual ctx.Done()/killProcessGroup select mirrors
	// OrgEvents below, the most similar long-running case.
	cmd := Command(bin, full...)
	cmd.Env = logging.ChildEnv(ctx, cmd.Env)
	_, kindDone := withOrgKind(cmd) // its roles are agents of kind org
	defer kindDone()
	inRoot(cmd, projectRoot)
	setProcessGroup(cmd)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	release, err := startProcessGroup(cmd)
	if err != nil {
		return nil, fmt.Errorf("start monomind org run %s: %w", name, err)
	}
	defer release()

	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()

	select {
	case <-ctx.Done():
		killProcessGroup(cmd, cmd.Process.Pid)
		<-waitCh
		return nil, ctx.Err()
	case err := <-waitCh:
		if err != nil {
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = err.Error()
			}
			if signatureRefusalReason(msg) == "" && signatureRefusalReason(stdout.String()) != "" {
				// org run prints a signature refusal on stdout.
				msg = strings.TrimSpace(msg + "\n" + stdout.String())
			}
			// ...and a host (R6) or daemon-lock (R1) refusal too, leaving only
			// "[ERROR] org start failed" on stderr.
			if r := asStartRefusal(name, stdout.String()+"\n"+msg); r != nil {
				return nil, r
			}
			return nil, asSignatureRefusal(name, fmt.Errorf("monomind org %s: %s", strings.Join(args, " "), msg))
		}
		trimmed := bytes.TrimSpace(stdout.Bytes())
		if len(trimmed) == 0 {
			return nil, fmt.Errorf("monomind org run %s: empty output", name)
		}
		return json.RawMessage(trimmed), nil
	}
}

// OrgRunStart starts `monomind org run <name> --yes [--task ...]` as a
// detached background process and returns as soon as it's spawned, without
// waiting for it to finish — for callers (the org.run workflow node,
// RunOrg's Wails binding) that poll OrgStatus separately instead of
// blocking on OrgRun's return. The process is reaped in a background
// goroutine so it never becomes a zombie; its exit is otherwise
// unobserved by this function — callers that need to know when it exits
// should poll OrgStatus for closed_by, not rely on this call. Nothing
// here kills it: OrgStop (`monomind org stop`) ends it cooperatively, the
// same on every platform, so startDetached keeps it out of our jobs.
func OrgRunStart(ctx context.Context, projectRoot, name, task string) error {
	if !orgdesign.ValidOrgName(name) {
		return fmt.Errorf("invalid org name %q", name)
	}
	bin, err := EnsureIn(ctx, projectRoot)
	if err != nil {
		return err
	}
	if err := checkOrgSigned(ctx, projectRoot, name); err != nil {
		return err
	}
	args := []string{"org", "run", name, "--yes"}
	if task != "" {
		args = append(args, "--task", task)
	}
	cmd := Command(bin, args...)
	cmd.Env = logging.ChildEnv(ctx, cmd.Env)
	kindBind, _ := withOrgKind(cmd) // detached: bound to the run below, never done
	inRoot(cmd, projectRoot)
	// A start monomind refuses (R6/R1) exits at once and says why on its
	// output; keep that (boundedly) to report it instead of a start nobody
	// sees fail.
	capture, err := newStartCapture()
	if err != nil {
		return fmt.Errorf("start monomind org run %s: %w", name, err)
	}
	if err := capture.startDrain(ctx, bin, projectRoot); err != nil {
		capture.close()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("start org output capture: %w", err)
	}
	cmd.Stdout, cmd.Stderr = capture.stream, capture.stream
	began := time.Now()
	cmd, err = startDetached(cmd)
	// Only monomind keeps the write end, so its exit ends the drainer.
	capture.stream.Close()
	if err != nil {
		capture.finishDrain()
		capture.close()
		return fmt.Errorf("start monomind org run %s: %w", name, err)
	}
	kindBind(cmd.Process.Pid) // the kind counts while the run does, not this launcher
	return watchStart(ctx, cmd, name, capture, func() bool { return runStarted(projectRoot, name, began) })
}

// OrgStatus returns one org's status, or every org's status when name=="".
func OrgStatus(ctx context.Context, projectRoot, name string) (json.RawMessage, error) {
	args := []string{"status"}
	if name != "" {
		args = append(args, name)
	}
	raw, err := runOrgJSON(ctx, projectRoot, args...)
	if err != nil {
		return nil, err
	}
	// `status` with no name is a list too, and carries the same phantoms.
	if name == "" {
		return deadRunsStopped(projectRoot, dropUnnamableOrgs(raw)), nil
	}
	return deadRunsStopped(projectRoot, raw), nil
}

// deadRunsStopped reports a "running" org whose run is dead (OrgRunDead)
// as "stopped", in a one-org status or in the list's items, so the GUI and
// every caller agree with `org summary` (#294, monoes/monomind#573).
func deadRunsStopped(projectRoot string, raw json.RawMessage) json.RawMessage {
	fix := func(item json.RawMessage) (json.RawMessage, bool) {
		var obj map[string]json.RawMessage
		if json.Unmarshal(item, &obj) != nil {
			return item, false
		}
		var name, status string
		_ = json.Unmarshal(obj["name"], &name)
		_ = json.Unmarshal(obj["status"], &status)
		if status != "running" || !orgdesign.ValidOrgName(name) || !OrgRunDead(projectRoot, name) {
			return item, false
		}
		obj["status"] = json.RawMessage(`"stopped"`)
		out, err := json.Marshal(obj)
		if err != nil {
			return item, false
		}
		return out, true
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(raw, &envelope) != nil {
		return raw
	}
	itemsRaw, isList := envelope["items"]
	if !isList {
		out, _ := fix(raw)
		return out
	}
	var items []json.RawMessage
	if json.Unmarshal(itemsRaw, &items) != nil {
		return raw
	}
	changed := false
	for i, item := range items {
		if out, ok := fix(item); ok {
			items[i], changed = out, true
		}
	}
	if !changed {
		return raw
	}
	b, err := json.Marshal(items)
	if err != nil {
		return raw
	}
	envelope["items"] = b
	out, err := json.Marshal(envelope)
	if err != nil {
		return raw
	}
	return out
}
