// Package fixture runs bounded, customer-supplied SQL transform fixtures without providers.
package fixture

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/Braidplane/cli/pkg/model"
	"github.com/Braidplane/cli/pkg/pipeline/payload"
	transformsql "github.com/Braidplane/cli/pkg/pipeline/transform/sql"
	"github.com/Braidplane/cli/pkg/xerror"
)

const MaxBytes = 1 << 20

type Fixture struct {
	Inputs   map[string]json.RawMessage `json:"inputs"`
	Expected json.RawMessage            `json:"expected,omitempty"`
}

type Diagnostic struct {
	Step    string `json:"step"`
	Field   string `json:"field,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Result struct {
	Passed      bool            `json:"passed"`
	Asserted    bool            `json:"asserted"`
	Step        string          `json:"step"`
	Scope       string          `json:"scope"`
	Output      json.RawMessage `json:"output,omitempty"`
	Columns     []string        `json:"columns,omitempty"`
	Diagnostics []Diagnostic    `json:"diagnostics"`
}

// Run executes the pipeline SQL runtime and its existing contract checks. Output
// is returned only after contract success; failures never echo fixture values.
func Run(ctx context.Context, step string, transform transformsql.Transform, fixture Fixture) Result {
	result := Result{Step: step, Scope: "fixture_explicit_contract_shapes", Diagnostics: []Diagnostic{}}
	fail := func(code, message string) Result {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{Step: step, Code: code, Message: message})
		return result
	}
	if ctx == nil {
		return fail("TRANSFORM_FIXTURE_INVALID", "Supply an execution context.")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	runtime := transformsql.NewRuntime(transformsql.Limits{
		MaxWork: 1000000, MaxIntermediateRows: 1000, MaxIntermediateBytes: MaxBytes,
		MaxOutputRows: 1000, MaxOutputBytes: MaxBytes, MaxDepth: 32, Timeout: 2 * time.Second,
	})
	configuration, err := json.Marshal(transform)
	if err != nil || len(configuration) > MaxBytes {
		return fail("TRANSFORM_FIXTURE_INVALID", "SQL and contract declarations together must be at most 1 MiB.")
	}
	contracts := []*payload.Contract{transform.InputContract, transform.OutputContract}
	for _, contract := range transform.InputContracts {
		contracts = append(contracts, contract)
	}
	fieldCount := 0
	for _, contract := range contracts {
		if contract == nil {
			continue
		}
		fieldCount += len(contract.Fields)
		if fieldCount > 256 || payload.ValidateContractFieldDeclarations(contract) != nil {
			return fail("TRANSFORM_FIXTURE_CONTRACT_INVALID", "Supply valid contract constraints with at most 256 total declared fields.")
		}
	}
	raw, err := json.Marshal(fixture)
	if err != nil || len(raw) > MaxBytes || len(fixture.Inputs) == 0 || len(fixture.Inputs) > 64 || strings.TrimSpace(step) == "" {
		return fail("TRANSFORM_FIXTURE_INVALID", "Supply a step name and 1–64 named inputs; the complete fixture must be at most 1 MiB.")
	}
	inputs := make(map[string]*payload.Payload, len(fixture.Inputs))
	for name, raw := range fixture.Inputs {
		if transformsql.ValidateTransformInputRelationName(name) != nil {
			return fail("TRANSFORM_FIXTURE_INPUT_INVALID", "Fixture input names must be valid transform relation identifiers.")
		}
		decoded, err := decodeJSON(raw)
		if err != nil {
			return fail("TRANSFORM_FIXTURE_INPUT_INVALID", "Each input must be one JSON value within the numeric and nesting limits.")
		}
		contract := transformsql.ContractForNamedInput(transform.InputContract, name, len(fixture.Inputs))
		if transform.InputContracts != nil {
			contract = transform.InputContracts[name]
		}
		representation := model.ContractRepresentationRows
		if contract != nil && contract.Representation != model.ContractRepresentationUnknown {
			representation = contract.Representation
		}
		input := &payload.Payload{Representation: representation}
		var rows []map[string]any
		switch representation {
		case model.ContractRepresentationJSON:
			input.Document, input.RootShape = raw, payload.DetectJSONRootShape(raw)
			rows, _, err = payload.ParseJSONBodyToRowsPreserveNumbers(raw)
		case model.ContractRepresentationCSV:
			var ok bool
			input.CSV, ok = decoded.(string)
			if !ok {
				return fail("TRANSFORM_FIXTURE_INPUT_INVALID", "CSV fixture inputs must be JSON strings containing CSV text.")
			}
			rows, _, err = payload.ParseCSVBodyToRows([]byte(input.CSV))
		case model.ContractRepresentationRows:
			if _, ok := decoded.([]any); !ok {
				return fail("TRANSFORM_FIXTURE_INPUT_INVALID", "Row fixture inputs must be JSON arrays of objects.")
			}
			rows, _, err = payload.ParseJSONBodyToRowsPreserveNumbers(raw)
			input.Rows = rows
		default:
			return fail("TRANSFORM_FIXTURE_INPUT_INVALID", "Fixtures support rows, JSON and CSV input representations.")
		}
		if err != nil || len(rows) > 1000 {
			return fail("TRANSFORM_FIXTURE_INPUT_INVALID", "Each fixture input must parse into at most 1000 row objects.")
		}
		input.ColumnNames, input.RowCount = payload.CollectColumnNames(rows), len(rows)
		if err := runtime.ValidateInputContext(ctx, input, contract); err != nil {
			return fail("TRANSFORM_FIXTURE_INPUT_CONTRACT_FAILED", "Input violates its declared representation, root shape or field constraints; sample values are omitted.")
		}
		inputs[name] = input
	}
	var expected any
	if len(fixture.Expected) > 0 {
		expected, err = decodeJSON(fixture.Expected)
		if err != nil {
			return fail("TRANSFORM_FIXTURE_EXPECTED_INVALID", "Expected output must be one JSON value within the numeric and nesting limits.")
		}
	}
	if err := transformsql.ValidateTransformOutputFacets(transform.OutputKind, transform.OutputContract); err != nil {
		return fail("TRANSFORM_FIXTURE_OUTPUT_CONTRACT_FAILED", "Output representation or root shape differs from its declared contract.")
	}
	output, err := runtime.ExecuteContext(ctx, transform, inputs)
	if err != nil {
		code := "TRANSFORM_FIXTURE_EXECUTION_FAILED"
		var typed *xerror.Error
		if xerror.As(err, &typed) {
			code = typed.Code()
		}
		return fail(code, "Transform execution failed. Check input fields, SQL conversions, input contracts and fixture limits; sample values are omitted.")
	}
	if transform.OutputContract != nil {
		if err := transformsql.ValidateOutputContract(output, transform.OutputContract); err != nil {
			failed := fail("TRANSFORM_FIXTURE_OUTPUT_CONTRACT_FAILED", "Output violates the declared contract. Check required fields, nullability, types and field constraints; sample values are omitted.")
			for _, field := range transform.OutputContract.Fields {
				selected := *transform.OutputContract
				selected.Fields = []payload.ContractField{field}
				if transformsql.ValidateOutputContract(output, &selected) != nil {
					failed.Diagnostics[0].Field = field.Name
					break
				}
			}
			return failed
		}
	}
	var actual json.RawMessage
	switch transform.OutputKind {
	case model.TransformOutputKindRows, model.TransformOutputKindUnknown:
		actual, err = json.Marshal(output.Rows)
	case model.TransformOutputKindJSON:
		actual = output.Document
	case model.TransformOutputKindCSV:
		actual, err = json.Marshal(output.CSV)
	}
	if err != nil || len(actual) == 0 {
		return fail("TRANSFORM_FIXTURE_OUTPUT_INVALID", "Transform output could not be represented as JSON.")
	}
	if len(fixture.Expected) > 0 {
		result.Asserted = true
		value, err := decodeJSON(actual)
		if err != nil || !equalJSON(ctx, value, expected) {
			return fail("TRANSFORM_FIXTURE_ASSERTION_FAILED", "Actual output differs from expected output. Row and array order matter; numeric comparison is exact. Output values are omitted on failure.")
		}
	}
	result.Passed, result.Output, result.Columns = true, actual, output.ColumnNames
	return result
}

func decodeJSON(raw []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, xerror.ValidationFailed()
	}
	if !boundedJSON(value, 0) {
		return nil, xerror.ValidationFailed()
	}
	return value, nil
}

func boundedJSON(value any, depth int) bool {
	if depth > 32 {
		return false
	}
	switch value := value.(type) {
	case json.Number:
		text := value.String()
		if len(text) > 1024 {
			return false
		}
		if i := strings.IndexAny(text, "eE"); i >= 0 {
			exponent, err := strconv.Atoi(text[i+1:])
			if err != nil || exponent < -4096 || exponent > 4096 {
				return false
			}
		}
	case []any:
		for _, item := range value {
			if !boundedJSON(item, depth+1) {
				return false
			}
		}
	case map[string]any:
		for _, item := range value {
			if !boundedJSON(item, depth+1) {
				return false
			}
		}
	}
	return true
}

func equalJSON(ctx context.Context, left, right any) bool {
	if ctx.Err() != nil {
		return false
	}
	switch left := left.(type) {
	case json.Number:
		right, ok := right.(json.Number)
		if !ok {
			return false
		}
		a, aOK := new(big.Rat).SetString(left.String())
		b, bOK := new(big.Rat).SetString(right.String())
		return aOK && bOK && a.Cmp(b) == 0
	case []any:
		right, ok := right.([]any)
		if !ok || len(left) != len(right) {
			return false
		}
		for i := range left {
			if !equalJSON(ctx, left[i], right[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		right, ok := right.(map[string]any)
		if !ok || len(left) != len(right) {
			return false
		}
		for key, value := range left {
			other, exists := right[key]
			if !exists || !equalJSON(ctx, value, other) {
				return false
			}
		}
		return true
	default:
		return left == right
	}
}
