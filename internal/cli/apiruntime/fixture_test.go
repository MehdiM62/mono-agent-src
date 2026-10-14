package apiruntime

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/monoes/mono-agent/internal/apiconfig"
	"github.com/monoes/mono-agent/internal/testhome"
)

func TestMain(m *testing.M) {
	keyring.MockInit()
	testhome.Main(m)
}

// testVersion is the CLI version the tests' runtimes report.
const testVersion = "test-version"

// codeError is the CLI's error with an exit code, as main's cliError.
type codeError struct {
	code int
	msg  string
}

func (e *codeError) Error() string { return e.msg }

func invalid(format string, a ...interface{}) error {
	return &codeError{code: 3, msg: fmt.Sprintf(format, a...)}
}

// exitCode is the exit code the CLI gives err.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ce *codeError
	if errors.As(err, &ce) {
		return ce.code
	}
	return 1
}

// testDeps are the CLI's pieces as main gives them.
func testDeps() Deps {
	return Deps{
		Version:      func() string { return testVersion },
		InvalidInput: invalid,
		// As the CLI's asCLIError: a value that fails its check, and a damaged saved row, are invalid input.
		AsCLIError: func(err error) error {
			var input *apiconfig.InputError
			var bad *apiconfig.ValidationError
			if errors.As(err, &input) || errors.As(err, &bad) || errors.Is(err, apiconfig.ErrDamaged) {
				return invalid("%v", err)
			}
			return err
		},
	}
}

// newAPIRuntime and daemonAPIRuntime are main's wrappers of New and NewForDaemon.
func newAPIRuntime(db *sql.DB, f Flags, logf func(format string, args ...any)) (*Runtime, error) {
	return New(db, f, logf, testDeps())
}

func daemonAPIRuntime(db *sql.DB, f Flags, logf func(format string, args ...any)) (*Runtime, error) {
	return NewForDaemon(db, f, logf, testDeps())
}
