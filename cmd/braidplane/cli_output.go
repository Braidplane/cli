package main

import (
	"encoding/json"
	"io"
	"strings"

	"github.com/Braidplane/cli/pkg/xerror"
)

const (
	cliOutputFormatJSON = "json"
	cliOutputFormatText = "text"
)

// cliOutputValue is the common denominator for command output that supports
// both structured JSON and command-specific human text. Generated API records
// stay as JSON DTOs; commands opt in by wrapping them with a small CLI adapter.
type cliOutputValue interface {
	JSONValue() any
	WriteCLIText(io.Writer) error
}

func writeCLIJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return xerror.Unexpected().With(err)
	}

	return nil
}

func writeCLIOutput(
	writer io.Writer,
	value cliOutputValue,
	outputFormat string,
) error {
	switch strings.TrimSpace(outputFormat) {
	case "", cliOutputFormatJSON:
		return writeCLIJSON(writer, value.JSONValue())
	case cliOutputFormatText:
		return value.WriteCLIText(writer)
	default:
		return xerror.InvalidInput(xerror.JSON).Detail("output must be json or text")
	}
}
