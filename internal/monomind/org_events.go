package monomind

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"strings"
)

// OrgEventsOptions configures OrgEvents.
type OrgEventsOptions struct {
	Run    string // --run <id>, empty = current run
	Follow bool   // --follow: keep streaming (like tail -f)
	Since  string // --since <eventId|iso>
}

// OrgEvents streams the org's bus.jsonl as NDJSON (`org events <name>`).
// NDJSON is the command's only output mode — no --format flag applies here.
// onLine is invoked once per raw JSON line, in arrival order. OrgEvents
// blocks until the subprocess exits or ctx is cancelled (in which case the
// process group is killed so no `monomind` or agent-CLI grandchild survives
// the caller, matching Exec's cancellation contract).
func OrgEvents(ctx context.Context, projectRoot, name string, opts OrgEventsOptions, onLine func(line []byte)) error {
	bin, err := EnsureIn(ctx, projectRoot)
	if err != nil {
		return err
	}
	args := []string{"org", "events", name}
	if opts.Run != "" {
		args = append(args, "--run", opts.Run)
	}
	if opts.Follow {
		args = append(args, "--follow")
	}
	if opts.Since != "" {
		args = append(args, "--since", opts.Since)
	}

	cmd := Command(bin, args...)
	inRoot(cmd, projectRoot)
	setProcessGroup(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	release, err := startProcessGroup(cmd)
	if err != nil {
		return fmt.Errorf("start monomind org events: %w", err)
	}
	defer release()

	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for sc.Scan() {
			line := sc.Bytes()
			if len(line) == 0 || line[0] != '{' {
				continue
			}
			lineCopy := make([]byte, len(line))
			copy(lineCopy, line)
			onLine(lineCopy)
		}
	}()

	waitCh := make(chan error, 1)
	go func() {
		// Wait closes the stdout pipe as soon as the process exits, and the
		// os/exec docs are explicit that calling it before every read has
		// completed is incorrect. Running it concurrently with the scanner
		// meant a fast-exiting `org events` could have its last lines closed
		// out from under the reader: the caller silently saw fewer events, or
		// none. It showed up as a CI-only test failure ("delivered 0 lines,
		// want 2") because losing the race needs the machine to be busy.
		<-readerDone
		waitCh <- cmd.Wait()
	}()

	select {
	case <-ctx.Done():
		killProcessGroup(cmd, cmd.Process.Pid)
		<-waitCh // the reader has already finished; waitCh implies readerDone
		return ctx.Err()
	case err := <-waitCh:
		if err != nil {
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = err.Error()
			}
			return fmt.Errorf("monomind org events %s: %s", name, msg)
		}
		return nil
	}
}
