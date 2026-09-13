package kongcmd

import (
	"io"

	"github.com/alecthomas/kong"

	"github.com/Braidplane/cli/pkg/xerror"
)

type exit struct {
	code int
}

// ParseResult captures a parsed Kong target plus any Kong-managed exit.
type ParseResult[T any] struct {
	CLI      T
	Context  *kong.Context
	ExitCode int
	Exited   bool
}

// New builds a Kong parser with the standard Braidplane command metadata, writers,
// and exit handling used by command entrypoints.
func New(target any, name, description string, stdout, stderr io.Writer) (*kong.Kong, error) {
	parser, err := kong.New(
		target,
		kong.Name(name),
		kong.Description(description),
		kong.Writers(stdout, stderr),
		kong.Exit(func(code int) {
			panic(exit{code: code})
		}),
	)
	if err != nil {
		return nil, xerror.Unexpected().With(err)
	}
	return parser, nil
}

// Parse builds the standard parser, parses args into cli, and reports help or
// other Kong-managed exits without terminating the process.
func Parse[T any](
	args []string,
	cli T,
	name string,
	description string,
	stdout io.Writer,
	stderr io.Writer,
	exitError func(code int) error,
) (result ParseResult[T], err error) {
	result.CLI = cli
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}

		exit, ok := recovered.(exit)
		if !ok {
			panic(recovered)
		}
		result.Exited = true
		result.ExitCode = exit.code
		if exit.code == 0 {
			err = nil
			return
		}
		err = exitError(exit.code)
	}()

	parser, err := New(&result.CLI, name, description, stdout, stderr)
	if err != nil {
		return result, err
	}

	ctx, err := parser.Parse(args)
	if err != nil {
		return result, xerror.ValidationFailed().Detailf("parse command arguments: %w", err)
	}
	result.Context = ctx
	return result, nil
}

// RecoverExit converts Kong's configured exit panic into the caller's error
// contract while preserving unrelated panics.
func RecoverExit(err *error, exitError func(code int) error) {
	recovered := recover()
	if recovered == nil {
		return
	}

	exit, ok := recovered.(exit)
	if !ok {
		panic(recovered)
	}
	if exit.code == 0 {
		*err = nil
		return
	}
	*err = exitError(exit.code)
}
