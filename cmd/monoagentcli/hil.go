package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"

	hilcli "github.com/monoes/mono-agent/internal/cli/hil"
	"github.com/monoes/mono-agent/internal/hilsend"
	"github.com/monoes/mono-agent/internal/operator"
	"github.com/monoes/mono-agent/internal/publication"
	"github.com/monoes/mono-agent/internal/storage"
)

// `hil` lives in internal/cli/hil; this is its adapter: the CLI's globals and helpers handed
// over as hilcli.Deps. Who may decide a send (requireOperator), how an approved send runs
// (hilSendRunner) and the publication gate (requirePublicationApproval) stay here: coder and
// publication use them too.

func newHILCmd(cfg *globalConfig) *cobra.Command {
	return hilcli.NewCommand(hilcli.Deps{
		JSONOutput:      func() bool { return cfg.JSONOutput },
		ProfileID:       func() string { return cfg.ProfileID },
		OpenDB:          func() (*storage.Database, error) { return initDB(cfg) },
		NotFound:        errNotFound,
		InvalidInput:    errInvalidInput,
		WriteJSON:       writeJSONTo,
		NewTable:        func(w io.Writer, headers []string) *tablewriter.Table { return newPlainTable(w, headers, nil) },
		RequireOperator: requireOperator,
		SendRunner:      func() hilsend.Runner { return hilSendRunner(cfg) },
		WorkspaceRoot: func(db *sql.DB) (string, error) {
			s, err := loadCoderSettings(db)
			return s.WorkspaceRoot, err
		},
	})
}

// hilSendRunner builds the Runner that performs an approved send. A variable
// so tests run a fake instead of starting a process.
var hilSendRunner = func(cfg *globalConfig) hilsend.Runner {
	return func(ctx context.Context, r hilsend.Run) (json.RawMessage, error) {
		exe, err := os.Executable()
		if err != nil {
			return nil, err
		}
		items := make([]map[string]interface{}, 0, len(r.Input))
		for _, in := range r.Input {
			items = append(items, map[string]interface{}{"json": in})
		}
		stdin, err := json.Marshal(map[string]interface{}{"config": r.Config, "input": items})
		if err != nil {
			return nil, err
		}
		args := []string{}
		if cfg.DBPath != "" {
			args = append(args, "--db-path", cfg.DBPath)
		}
		if cfg.ProfileID != "" {
			args = append(args, "--profile", cfg.ProfileID)
		}
		args = append(args, "node", "run", r.NodeType, "--stdin", "--output", "json")
		cmd := exec.CommandContext(ctx, exe, args...)
		cmd.Env = append(os.Environ(), hilsend.EnvApprovalID+"="+r.ApprovalID, hilsend.EnvApprovalProof+"="+r.Proof)
		cmd.Stdin = bytes.NewReader(stdin)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil {
			msg := strings.TrimSpace(stderr.String())
			if len(msg) > 800 {
				msg = msg[len(msg)-800:]
			}
			if msg == "" {
				msg = err.Error()
			}
			return nil, errors.New(msg)
		}
		out := bytes.TrimSpace(stdout.Bytes())
		if !json.Valid(out) {
			return json.RawMessage(strconv.Quote(string(out))), nil
		}
		return json.RawMessage(out), nil
	}
}

// requireOperator refuses whatever is not the operator's own terminal: it
// may prepare a send, never decide one, nor approve what agents asked for.
func requireOperator(what string) error {
	if _, err := callerFor("").operator(what); err != nil {
		return err
	}
	if why := notOperator(); why != "" {
		return operatorOnlyError("%s: only the operator can %s; run it in your own terminal", why, what)
	}
	return nil
}

// notOperator says why this process is not the operator's own terminal, or
// "" (internal/operator): an agent, a process of any kind of agent, a chat
// tool call's run, or a process a workflow node started.
func notOperator() string { return operator.Refusal() }

// requirePublicationApproval gates `publication register` from whatever is not
// the operator's own terminal (notOperator: an agent, the chat's runs, what a
// workflow node starts): a publication it claims must be the one a send the
// operator approved made, and that approval registers it alone
// (hilsend.ClaimPublicationApproval). approvalID names the send item (as returned
// when it was queued); an approved send registers its own publication, so this is
// only for a publication made outside that path. Operators, and profiles that
// turned sendApproval off, are not asked. release gives the claim back when the
// registration fails.
func requirePublicationApproval(cfg *globalConfig, approvalID string, e publication.Entry) (release func(), err error) {
	release = func() {}
	if notOperator() == "" {
		return release, nil
	}
	var claim *hilsend.PublicationClaim
	err = withImageDB(cfg, func(db *sql.DB) error {
		var cerr error
		claim, cerr = hilsend.ClaimPublicationApproval(context.Background(), db, cfg.ProfileID, approvalID, e.Platform, e.RemoteID)
		if errors.Is(cerr, hilsend.ErrPublicationNeedsApproval) {
			return operatorOnlyError("registering a publication from an agent needs an approved send: prepare the send (it is queued for approval, and registers itself once approved), or pass --approval <hil id> of a send that was approved and sent. Asked because %s", notOperator())
		}
		if cerr != nil {
			return operatorOnlyError("%v (asked because %s)", cerr, notOperator())
		}
		return nil
	})
	if err != nil || claim == nil {
		return release, err
	}
	return func() {
		_ = withImageDB(cfg, func(db *sql.DB) error { claim.Release(context.Background(), db); return nil })
	}, nil
}
