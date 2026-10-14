package main

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/account"
	"github.com/monoes/mono-agent/internal/cli/accountgate"
)

// The CLI gate (spec §6.1, D6, D20), layer 1 of the monoes.me account gate, lives in
// internal/cli/accountgate: the classification table, the resolution of an invocation and the
// verdict. This is its adapter: the gate over this CLI's command tree, refusing with this CLI's
// login_required error. run installs it before anything of a command runs.

const (
	accountAnnotation = accountgate.Annotation
	classOpen         = accountgate.ClassOpen
	classGated        = accountgate.ClassGated
	classServe        = accountgate.ClassServe
)

// accountGate is the gate over this CLI's tree.
func accountGate() accountgate.Gate {
	return accountgate.Gate{NewTree: newRootCmd, LoginRequired: newLoginRequiredError}
}

// gateCommand is the CLI gate: nil lets the command run, the refusal fails it.
func gateCommand(ctx context.Context, root *cobra.Command, args []string, g *account.Guard, stderr io.Writer) error {
	return accountGate().Check(ctx, root, args, g, stderr)
}

// invocationClass is the class of what running args would execute.
func invocationClass(root *cobra.Command, args []string) string {
	return accountGate().InvocationClass(root, args)
}

// newGateRefusal is what a refused command fails with: exit 4, login_required.
func newGateRefusal(st account.Status) error { return accountGate().Refusal(st) }

// applyClassification stamps every command below root with its class.
func applyClassification(root *cobra.Command) { accountgate.Apply(root) }

// commandClass is c's class: its own stamp, else its nearest stamped ancestor's, else gated.
func commandClass(c *cobra.Command) string { return accountgate.CommandClass(c) }

// commandKey is c's path below the root: "doctor fix", "library login".
func commandKey(c *cobra.Command) string { return accountgate.CommandKey(c) }
