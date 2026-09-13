package kongcmd

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type parseTestCLI struct {
	Name string `help:"Name to parse."`
}

// TestParse verifies generic parser behavior for normal and Kong-managed exits.
func TestParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                   string
		args                   []string
		expectedExited         bool
		expectedExitCode       int
		expectedName           string
		expectedStdoutContains string
		expectedContextNil     bool
	}{
		{
			name:             "ReturnsParsedTarget",
			args:             []string{"--name", "local"},
			expectedName:     "local",
			expectedExitCode: 0,
		},
		{
			name:                   "ReportsHelpExit",
			args:                   []string{"--help"},
			expectedExited:         true,
			expectedExitCode:       0,
			expectedStdoutContains: "Usage: parse-test",
			expectedContextNil:     true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			result, err := Parse(
				test.args,
				parseTestCLI{},
				"parse-test",
				"Test parser.",
				&stdout,
				&stderr,
				func(code int) error {
					return fmt.Errorf("exit %d", code)
				},
			)

			require.NoError(t, err)
			assert.Equal(t, test.expectedExited, result.Exited)
			assert.Equal(t, test.expectedExitCode, result.ExitCode)
			if test.expectedName != "" {
				assert.Equal(t, test.expectedName, result.CLI.Name)
			}
			if test.expectedStdoutContains != "" {
				assert.Contains(t, stdout.String(), test.expectedStdoutContains)
			}
			if test.expectedContextNil {
				assert.Nil(t, result.Context)
			} else {
				require.NotNil(t, result.Context)
			}
		})
	}
}

// TestRecoverExitConvertsNonZeroExit verifies callers control the command error
// contract for Kong-initiated process exits.
func TestRecoverExitConvertsNonZeroExit(t *testing.T) {
	t.Parallel()

	var err error
	func() {
		defer RecoverExit(&err, func(code int) error {
			return fmt.Errorf("exit %d", code)
		})

		panic(exit{code: 2})
	}()

	require.Error(t, err)
	assert.Equal(t, "exit 2", err.Error())
}

// TestRecoverExitClearsZeroExit treats help/version exits as successful command
// completion, matching Kong's normal process semantics.
func TestRecoverExitClearsZeroExit(t *testing.T) {
	t.Parallel()

	err := assert.AnError
	func() {
		defer RecoverExit(&err, func(code int) error {
			return fmt.Errorf("exit %d", code)
		})

		panic(exit{code: 0})
	}()

	require.NoError(t, err)
}

// TestRecoverExitPreservesUnrelatedPanic ensures the helper only handles the
// panic value created by this package's Kong exit hook.
func TestRecoverExitPreservesUnrelatedPanic(t *testing.T) {
	t.Parallel()

	assert.PanicsWithValue(t, "boom", func() {
		var err error
		func() {
			defer RecoverExit(&err, func(code int) error {
				return fmt.Errorf("exit %d", code)
			})

			panic("boom")
		}()
	})
}
