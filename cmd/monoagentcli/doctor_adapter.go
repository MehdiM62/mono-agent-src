package main

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/cli/clicore"
	doctorcli "github.com/monoes/mono-agent/internal/cli/doctor"
	"github.com/monoes/mono-agent/internal/health"
)

// `doctor` and `setup` live in internal/cli/doctor; this is their adapter: the real machine
// (newHealthEnv, doctor_env*.go) and the CLI's helpers handed over as doctorcli.Deps, and the
// old names of the streamed-progress helpers the rest of the CLI uses.

func doctorDeps(cfg *globalConfig) doctorcli.Deps {
	return doctorcli.Deps{
		JSONOutput: func() bool { return cfg.JSONOutput },
		NewEnv: func(projects []string) (*health.Env, func()) {
			c := *cfg
			c.projectFilter = projects
			return newHealthEnv(&c)
		},
		FinishStreamed: finishStreamed,
	}
}

func newDoctorCmd(cfg *globalConfig) *cobra.Command {
	return doctorcli.NewDoctorCommand(doctorDeps(cfg))
}

func newSetupCmd(cfg *globalConfig) *cobra.Command { return doctorcli.NewSetupCommand(doctorDeps(cfg)) }

// fixEvent is one NDJSON progress event of a streamed fix.
type fixEvent = doctorcli.FixEvent

func writeFixEvent(w io.Writer, ev fixEvent) { doctorcli.WriteFixEvent(w, ev) }

// progressWriter streams fix output: NDJSON line events in JSON mode, indented text otherwise.
func progressWriter(w io.Writer, asJSON bool) func(string) {
	return doctorcli.ProgressWriter(w, asJSON)
}

// captureStdLog sends the standard logger's output to progress until restore is called.
func captureStdLog(progress func(string)) (restore func()) { return doctorcli.CaptureStdLog(progress) }

// stdinIsTerminal reports whether a person can answer on stdin.
func stdinIsTerminal() bool { return clicore.StdinIsTerminal() }
