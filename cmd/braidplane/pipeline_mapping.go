package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/Braidplane/cli/pkg/model"
	transformfixture "github.com/Braidplane/cli/pkg/pipeline/fixture"
	"github.com/Braidplane/cli/pkg/pipeline/payload"
	transformsql "github.com/Braidplane/cli/pkg/pipeline/transform/sql"
	"github.com/Braidplane/cli/pkg/xerror"
)

type pipelineMappingCommand struct {
	Inspect inspectPipelineMappingCommand `cmd:"" help:"Read a SQL projection as editable fields; advanced SQL remains unchanged."`
	Render  renderPipelineMappingCommand  `cmd:"" help:"Render an editing JSON file as ordinary transform SQL on stdout."`
	Suggest suggestPipelineMappingCommand `cmd:"" help:"Propose compatible exact-name fields from explicit source and destination shapes."`
	Check   checkPipelineMappingCommand   `cmd:"" help:"Check mapped fields against explicit cached or resolved contract shapes."`
	Test    testPipelineMappingCommand    `cmd:"" help:"Execute a bounded local SQL fixture and optional expected-output assertion without providers."`
}

type testPipelineMappingCommand struct {
	mappingContractFlags
	File       string `required:"" help:"SQL transform file." type:"path"`
	Fixture    string `required:"" help:"JSON fixture with named inputs matching their contract representations and optional expected output." type:"path"`
	Step       string `required:"" help:"Authored step name for fixture diagnostics."`
	OutputKind string `default:"rows" enum:"rows,json,csv" help:"Transform output serialization." name:"output-kind"`
}

func (c *testPipelineMappingCommand) Run(output *cliOutput) error {
	source, destination, err := c.load()
	if err != nil {
		return mappingCommandFailure(output, err)
	}
	raw, err := readMappingFile(c.File)
	if err != nil {
		return mappingCommandFailure(output, err)
	}
	var fixture transformfixture.Fixture
	if err := decodeMappingFile(c.Fixture, &fixture); err != nil {
		return mappingCommandFailure(output, err)
	}
	kind, err := model.TransformOutputKindString(c.OutputKind)
	if err != nil {
		return mappingCommandFailure(output, err)
	}
	result := transformfixture.Run(context.Background(), c.Step, transformsql.Transform{
		SQL: string(raw), OutputKind: kind, InputContract: source, OutputContract: destination,
	}, fixture)
	if err := writeCLIJSON(output.stdout, result); err != nil {
		return err
	}
	if !result.Passed {
		return xerror.ValidationFailed().Detail("transform fixture failed; inspect the structured step diagnostics")
	}
	return nil
}

type inspectPipelineMappingCommand struct {
	File string `required:"" help:"SQL file to inspect." type:"path"`
}

func (c *inspectPipelineMappingCommand) Run(output *cliOutput) error {
	raw, err := readMappingFile(c.File)
	if err != nil {
		return mappingCommandFailure(output, err)
	}
	mapping, err := transformsql.ReadFieldMapping(string(raw))
	if err != nil {
		return mappingCommandFailure(output, err)
	}
	return writeCLIJSON(output.stdout, mapping)
}

type renderPipelineMappingCommand struct {
	File string `required:"" help:"Temporary field-editing JSON file; the emitted SQL is the pipeline artifact." type:"path"`
}

func (c *renderPipelineMappingCommand) Run(output *cliOutput) error {
	var mapping transformsql.FieldMapping
	if err := decodeMappingFile(c.File, &mapping); err != nil {
		return mappingCommandFailure(output, err)
	}
	query, err := transformsql.WriteFieldMapping(mapping)
	if err != nil {
		return mappingCommandFailure(output, err)
	}
	if _, err := fmt.Fprintln(output.stdout, query); err != nil {
		return xerror.Unexpected().With(err)
	}
	return nil
}

type mappingContractFlags struct {
	InputContract  string `required:"" help:"JSON source contract shape, obtained from authorized metadata or declared explicitly." type:"path" name:"input-contract"`
	OutputContract string `required:"" help:"JSON destination contract shape. Catalog references require an exact resolved revision." type:"path" name:"output-contract"`
}

func (f mappingContractFlags) load() (*payload.Contract, *payload.Contract, error) {
	var source, destination payload.Contract
	if err := decodeMappingFile(f.InputContract, &source); err != nil {
		return nil, nil, err
	}
	if err := decodeMappingFile(f.OutputContract, &destination); err != nil {
		return nil, nil, err
	}
	for _, contract := range []*payload.Contract{&source, &destination} {
		if err := transformsql.ValidateFieldMappingSchema(contract); err != nil {
			return nil, nil, err
		}
	}
	return &source, &destination, nil
}

type suggestPipelineMappingCommand struct {
	mappingContractFlags
	Input string `default:"input" help:"Transform relation name."`
}

func (c *suggestPipelineMappingCommand) Run(output *cliOutput) error {
	source, destination, err := c.load()
	if err != nil {
		return mappingCommandFailure(output, err)
	}
	if err := transformsql.ValidateTransformInputRelationName(c.Input); err != nil {
		return mappingCommandFailure(output, err)
	}
	return writeCLIJSON(output.stdout, transformsql.SuggestFieldMapping(c.Input, source, destination))
}

type checkPipelineMappingCommand struct {
	mappingContractFlags
	File       string `required:"" help:"SQL file to check. This does not resolve ODCS references or contact providers." type:"path"`
	OutputKind string `default:"rows" enum:"rows,json,csv" help:"Transform output serialization." name:"output-kind"`
}

func (c *checkPipelineMappingCommand) Run(output *cliOutput) error {
	source, destination, err := c.load()
	if err != nil {
		return mappingCommandFailure(output, err)
	}
	raw, err := readMappingFile(c.File)
	if err != nil {
		return mappingCommandFailure(output, err)
	}
	kind, err := model.TransformOutputKindString(c.OutputKind)
	if err != nil {
		return mappingCommandFailure(output, err)
	}
	if err := transformsql.ValidateFieldMappingContracts(string(raw), kind, source, destination); err != nil {
		return mappingCommandFailure(output, err)
	}
	return writeCLIJSON(output.stdout, map[string]any{
		"valid": true, "scope": "explicit_contract_shapes",
		"notice": "Static shape check only. Runtime nullability, fixture results, exact ODCS resolution and destination delivery require their own checks.",
	})
}

const maxMappingFileBytes = 1 << 20

func readMappingFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, xerror.InvalidInput(xerror.Pipeline).Detail("cannot open mapping input file")
	}
	defer func() { _ = file.Close() }()
	raw, err := io.ReadAll(io.LimitReader(file, maxMappingFileBytes+1))
	if err != nil || len(raw) > maxMappingFileBytes {
		return nil, xerror.InvalidInput(xerror.Pipeline).Detail("mapping input must be readable and at most 1 MiB")
	}
	return raw, nil
}

func decodeMappingFile(path string, value any) error {
	raw, err := readMappingFile(path)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return xerror.InvalidInput(xerror.Pipeline).Detail("mapping input must be valid JSON with only documented fields")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return xerror.InvalidInput(xerror.Pipeline).Detail("mapping input must contain exactly one JSON value")
	}
	return nil
}

func mappingCommandFailure(output *cliOutput, err error) error {
	code := "PIPELINE_MAPPING_INVALID"
	var typed *xerror.Error
	if xerror.As(err, &typed) {
		code = typed.Code()
	}
	if writeErr := writeCLIJSON(output.stdout, map[string]any{
		"valid": false, "diagnostics": []map[string]string{{"code": code, "message": err.Error()}},
	}); writeErr != nil {
		return writeErr
	}
	return xerror.ValidationFailed().Detail("field mapping check failed; inspect the structured diagnostics")
}
