package sql

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Braidplane/cli/pkg/model"
	pipepayload "github.com/Braidplane/cli/pkg/pipeline/payload"
	"github.com/stretchr/testify/require"
)

// These are observations of sql_v1, including defects, rather than a target SQL
// standard. Change the expectations with the linked qualification and follow-up.
func TestSQLQualification(t *testing.T) {
	t.Parallel()
	schema := map[string]model.ContractFieldType{
		"input.id":  model.ContractFieldTypeValueInteger,
		"input.n":   model.ContractFieldTypeValueNumber,
		"input.s":   model.ContractFieldTypeValueString,
		"input.doc": model.ContractFieldTypeValueObject,
	}
	inputs := map[string]*pipepayload.Payload{"input": {
		Representation: model.ContractRepresentationJSON,
		Document:       json.RawMessage(`[{"id":1,"n":null,"s":"2.675","doc":{"x":3}},{"id":2,"n":0.1,"s":"2.675","doc":{"x":3}},{"id":3,"n":0.2,"s":"2.675","doc":{"x":3}}]`),
	}}
	for _, tc := range []struct {
		name, statement              string
		fieldType                    model.ContractFieldType
		structuralReject, typeReject bool
		runtimeError, rows           string
		validationError              error
	}{
		{name: "projection", statement: "SELECT id AS v FROM input", fieldType: model.ContractFieldTypeValueInteger, rows: `[{"v":1},{"v":2},{"v":3}]`},
		{name: "integer widening", statement: "SELECT id AS v FROM input", fieldType: model.ContractFieldTypeValueNumber, rows: `[{"v":1},{"v":2},{"v":3}]`},
		{name: "nullable projection", statement: "SELECT n AS v FROM input", rows: `[{"v":null},{"v":0.1},{"v":0.2}]`},
		{name: "is null", statement: "SELECT id AS v FROM input WHERE n IS NULL", rows: `[{"v":1}]`},
		{name: "null equality", statement: "SELECT id AS v FROM input WHERE n = NULL", rows: `[]`},
		{name: "null inequality", statement: "SELECT id AS v FROM input WHERE n != 0", rows: `[{"v":2},{"v":3}]`},
		{name: "not unknown", statement: "SELECT id AS v FROM input WHERE NOT (n > 0)", rows: `[]`},
		{name: "null arithmetic", statement: "SELECT n + 1 AS v FROM input", rows: `[{"v":null},{"v":1.1},{"v":1.2}]`},
		{name: "decimal literal", statement: "SELECT CAST(0.1 + 0.2 AS DECIMAL(3,2)) AS v FROM input", fieldType: model.ContractFieldTypeValueDecimal, rows: `[{"v":0.30},{"v":0.30},{"v":0.30}]`},
		{name: "decimal sum", statement: "SELECT SUM(n) AS v FROM input", rows: `[{"v":0.30000000000000004}]`},
		{name: "decimal avg", statement: "SELECT AVG(n) AS v FROM input", rows: `[{"v":0.15000000000000002}]`},
		{name: "repeating division", statement: "SELECT id / 3 AS v FROM input LIMIT 1", rows: `[{"v":0.3333333333333333}]`},
		{name: "decimal cast enforces scale", statement: "SELECT CAST(s AS DECIMAL(3,2)) AS v FROM input LIMIT 1", fieldType: model.ContractFieldTypeValueDecimal, rows: `[{"v":2.68}]`},
		{name: "integer cast truncates", statement: "SELECT CAST(s AS SIGNED) AS v FROM input LIMIT 1", fieldType: model.ContractFieldTypeValueInteger, rows: `[{"v":2}]`},
		{name: "numeric string comparison", statement: "SELECT id AS v FROM input WHERE s > 2", rows: `[{"v":1},{"v":2},{"v":3}]`},
		{name: "sum all null", statement: "SELECT SUM(n) AS v FROM input WHERE id = 1", rows: `[{"v":null}]`},
		{name: "empty sum", statement: "SELECT SUM(n) AS v FROM input WHERE id > 3", rows: `[{"v":null}]`},
		{name: "count nulls", statement: "SELECT COUNT(n) AS v FROM input", fieldType: model.ContractFieldTypeValueInteger, rows: `[{"v":2}]`},
		{name: "count star", statement: "SELECT COUNT(*) AS v FROM input", fieldType: model.ContractFieldTypeValueInteger, rows: `[{"v":3}]`},
		{name: "count distinct", statement: "SELECT COUNT(DISTINCT s) AS v FROM input", fieldType: model.ContractFieldTypeValueInteger, rows: `[{"v":1}]`},
		{name: "count distinct literal rejected", statement: "SELECT COUNT(DISTINCT 1) AS v FROM input", structuralReject: true, typeReject: true, validationError: ErrFeatureNotAllowed, runtimeError: "count aggregate supports only column references at runtime"},
		{name: "sum distinct rejected", statement: "SELECT SUM(DISTINCT input.id) AS v FROM input JOIN input AS other ON other.id > 0", structuralReject: true, typeReject: true, validationError: ErrFeatureNotAllowed, runtimeError: "DISTINCT is supported only for COUNT"},
		{name: "avg distinct rejected", statement: "SELECT AVG(DISTINCT id) AS v FROM input", structuralReject: true, typeReject: true, validationError: ErrFeatureNotAllowed, runtimeError: "DISTINCT is supported only for COUNT"},
		{name: "min distinct rejected", statement: "SELECT MIN(DISTINCT id) AS v FROM input", structuralReject: true, typeReject: true, validationError: ErrFeatureNotAllowed, runtimeError: "DISTINCT is supported only for COUNT"},
		{name: "max distinct rejected", statement: "SELECT MAX(DISTINCT id) AS v FROM input", structuralReject: true, typeReject: true, validationError: ErrFeatureNotAllowed, runtimeError: "DISTINCT is supported only for COUNT"},
		{name: "nested unsupported function", statement: "SELECT JSON_ARRAY(IFNULL(n, 0)) AS v FROM input", structuralReject: true, typeReject: true, validationError: ErrFeatureNotAllowed, runtimeError: "function ifnull is not supported"},
		{name: "cast unsupported operator", statement: "SELECT CAST(id = 1 AS SIGNED) AS v FROM input", structuralReject: true, typeReject: true, validationError: ErrFeatureNotAllowed, runtimeError: "projection operator"},
		{name: "cte distinct rejected", statement: "WITH chosen AS (SELECT DISTINCT s AS v FROM input) SELECT v FROM chosen", structuralReject: true, typeReject: true, validationError: ErrFeatureNotAllowed, runtimeError: "SELECT DISTINCT is not supported"},
		{name: "min", statement: "SELECT MIN(n) AS v FROM input", rows: `[{"v":0.1}]`},
		{name: "max", statement: "SELECT MAX(n) AS v FROM input", rows: `[{"v":0.2}]`},
		{name: "coalesce", statement: "SELECT COALESCE(n, 0) AS v FROM input", rows: `[{"v":0},{"v":0.1},{"v":0.2}]`},
		{name: "ifnull", statement: "SELECT IFNULL(n, 0) AS v FROM input", structuralReject: true, typeReject: true, validationError: ErrFeatureNotAllowed, runtimeError: "function ifnull is not supported"},
		{name: "nullif", statement: "SELECT NULLIF(id, 1) AS v FROM input", fieldType: model.ContractFieldTypeValueInteger, rows: `[{"v":null},{"v":2},{"v":3}]`},
		{name: "modulo", statement: "SELECT id % 2 AS v FROM input", structuralReject: true, typeReject: true, validationError: ErrFeatureNotAllowed, runtimeError: "projection operator"},
		{name: "projection comparison", statement: "SELECT id = 1 AS v FROM input", structuralReject: true, typeReject: true, validationError: ErrFeatureNotAllowed, runtimeError: "projection operator"},
		{name: "parentheses", statement: "SELECT (id + 1) AS v FROM input", rows: `[{"v":2},{"v":3},{"v":4}]`},
		{name: "unary minus", statement: "SELECT -id AS v FROM input", typeReject: true, runtimeError: "unsupported transform projection expression"},
		{name: "json extract", statement: "SELECT JSON_EXTRACT(doc, '$.x') AS v FROM input LIMIT 1", typeReject: true, rows: `[{"v":3}]`},
		{name: "json object rows", statement: "SELECT JSON_OBJECT('x', id) AS v FROM input LIMIT 1", fieldType: model.ContractFieldTypeValueString, rows: `[{"v":"{\"x\":1}"}]`},
		{name: "json array rows", statement: "SELECT JSON_ARRAY(id) AS v FROM input LIMIT 1", fieldType: model.ContractFieldTypeValueString, rows: `[{"v":"[1]"}]`},
		{name: "cte", statement: "WITH chosen AS (SELECT id FROM input WHERE id > 1) SELECT id AS v FROM chosen ORDER BY v DESC LIMIT 1", rows: `[{"v":3}]`},
		{name: "order by input field", statement: "SELECT id AS v FROM input ORDER BY id", structuralReject: true, typeReject: true, validationError: ErrTypeInput, runtimeError: "ORDER BY can reference only unqualified projected fields"},
		{name: "order by projection alias", statement: "SELECT id AS v FROM input ORDER BY v DESC", fieldType: model.ContractFieldTypeValueInteger, rows: `[{"v":3},{"v":2},{"v":1}]`},
		{name: "order by alias expression", statement: "SELECT id AS v FROM input ORDER BY v * 2 DESC", fieldType: model.ContractFieldTypeValueInteger, rows: `[{"v":3},{"v":2},{"v":1}]`},
		{name: "order by qualified input", statement: "SELECT id AS v FROM input ORDER BY input.id", structuralReject: true, typeReject: true, validationError: ErrTypeInput, runtimeError: "ORDER BY can reference only unqualified projected fields"},
		{name: "order by hidden input", statement: "SELECT id AS v FROM input ORDER BY n + 1", structuralReject: true, typeReject: true, validationError: ErrTypeInput, runtimeError: "ORDER BY can reference only unqualified projected fields"},
		{name: "order by unsupported operator", statement: "SELECT id AS v FROM input ORDER BY v % 2", structuralReject: true, typeReject: true, validationError: ErrFeatureNotAllowed, runtimeError: "projection operator"},
		{name: "grouped order by alias", statement: "SELECT s AS v, COUNT(DISTINCT id) AS total FROM input GROUP BY s ORDER BY total DESC", fieldType: model.ContractFieldTypeValueString, rows: `[{"total":3,"v":"2.675"}]`},
		{name: "where in", statement: "SELECT id AS v FROM input WHERE id IN (1,2)", runtimeError: "unsupported transform projection expression"},
		{name: "where like", statement: "SELECT id AS v FROM input WHERE s LIKE '2%'", runtimeError: "unsupported transform projection expression"},
		{name: "distinct projection rejected", statement: "SELECT DISTINCT s AS v FROM input", fieldType: model.ContractFieldTypeValueString, structuralReject: true, typeReject: true, validationError: ErrFeatureNotAllowed, runtimeError: "SELECT DISTINCT is not supported"},
		{name: "left join", statement: "SELECT input.id AS v FROM input LEFT JOIN input AS other ON input.id = other.id", fieldType: model.ContractFieldTypeValueInteger, rows: `[{"v":1},{"v":2},{"v":3}]`},
		{name: "recursive cte", statement: "WITH RECURSIVE chosen AS (SELECT id FROM input) SELECT id AS v FROM chosen", structuralReject: true, typeReject: true, runtimeError: "recursive CTEs"},
		{name: "subquery", statement: "SELECT id AS v FROM (SELECT id FROM input) AS chosen", structuralReject: true, typeReject: true, runtimeError: "subqueries"},
		{name: "window", statement: "SELECT ROW_NUMBER() OVER () AS v FROM input", structuralReject: true, typeReject: true, runtimeError: "window"},
		{name: "having", statement: "SELECT SUM(id) AS v FROM input HAVING SUM(id) > 1", rows: `[{"v":6}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.fieldType == model.ContractFieldTypeUnknown {
				tc.fieldType = model.ContractFieldTypeValueNumber
			}
			contract := &pipepayload.Contract{Fields: []pipepayload.ContractField{{Name: "v", Type: tc.fieldType, Required: true, Nullable: true}}}
			if tc.fieldType == model.ContractFieldTypeValueDecimal {
				precision, scale := 3, 2
				contract.Fields[0].Precision, contract.Fields[0].Scale = &precision, &scale
			}
			_, err := ValidateTransformSQL(tc.statement)
			require.Equal(t, tc.structuralReject, err != nil, "structural: %v", err)
			if tc.validationError != nil {
				require.ErrorIs(t, err, tc.validationError)
			}
			err = ValidateOutputContractTypesWithInputSchema(tc.statement, contract, model.DataSourceTypePostgres, schema)
			require.Equal(t, tc.typeReject, err != nil, "types: %v", err)
			if tc.validationError != nil {
				require.ErrorIs(t, err, tc.validationError)
			}
			inferred, err := InferOutputContractWithInputSchema(tc.statement, model.TransformOutputKindRows, model.DataSourceTypePostgres, schema)
			require.Equal(t, tc.typeReject, err != nil, "inference: %v", err)
			if tc.typeReject {
				require.Nil(t, inferred)
			}
			output, err := NewRuntime(qualificationLimits()).Execute(Transform{SQL: tc.statement}, inputs)
			if tc.runtimeError != "" {
				require.ErrorContains(t, err, tc.runtimeError)
				if tc.validationError != nil {
					require.ErrorIs(t, err, tc.validationError)
				}
				return
			}
			require.NoError(t, err)
			require.NoError(t, ValidateOutputContract(output, contract))
			encoded, err := json.Marshal(output.Rows)
			require.NoError(t, err)
			require.Equal(t, tc.rows, string(encoded))
		})
	}
}

func TestSQLQualificationOperatorInference(t *testing.T) {
	t.Parallel()
	for _, operator := range []string{"+", "-", "*", "/", "%", "=", "<", "&", "AND", "OR"} {
		t.Run(operator, func(t *testing.T) {
			// Predicate syntax lets us probe inference independently of projection rejection.
			stmt, _, err := parseTransformSelect("SELECT id FROM input WHERE id " + operator + " 1")
			require.NoError(t, err)
			want := model.ContractFieldTypeUnknown
			switch operator {
			case "+", "-", "*", "/":
				want = model.ContractFieldTypeValueNumber
			}
			require.Equal(t, want, inferTransformRowFieldType(stmt.Where, transformInputFieldTypes{"id": model.ContractFieldTypeValueInteger}, nil))
		})
	}
}

func TestSQLQualificationJSONExtractionTypeProof(t *testing.T) {
	t.Parallel()
	schema := map[string]model.ContractFieldType{"input.doc": model.ContractFieldTypeValueObject}
	inputs := map[string]*pipepayload.Payload{"input": {Representation: model.ContractRepresentationJSON, Document: json.RawMessage(`[{"doc":{"x":3}}]`)}}
	for _, expression := range []string{"JSON_EXTRACT(doc, '$.x')", "doc -> '$.x'", "CAST(JSON_EXTRACT(doc, '$.x') AS SIGNED)"} {
		statement := "SELECT " + expression + " AS v FROM input"
		_, err := ValidateTransformSQL(statement)
		require.NoError(t, err)
		for _, kind := range []model.TransformOutputKind{model.TransformOutputKindRows, model.TransformOutputKindJSON, model.TransformOutputKindCSV} {
			contract := &pipepayload.Contract{Fields: []pipepayload.ContractField{{Name: "v", Type: model.ContractFieldTypeValueInteger, Required: true}}}
			if kind == model.TransformOutputKindCSV {
				contract.Fields[0].Type = model.ContractFieldTypeValueString
			}
			err := ValidateOutputContractTypesWithInputSchemaForOutputKind(statement, kind, contract, model.DataSourceTypePostgres, schema)
			inferred, inferenceErr := InferOutputContractWithInputSchema(statement, kind, model.DataSourceTypePostgres, schema)
			if kind == model.TransformOutputKindCSV {
				require.NoError(t, err)
				require.NoError(t, inferenceErr)
				require.Equal(t, model.ContractFieldTypeValueString, inferred.Fields[0].Type)
			} else {
				require.ErrorIs(t, err, ErrContractType)
				require.ErrorContains(t, err, "uncertain projection type")
				require.ErrorIs(t, inferenceErr, ErrContractType)
				require.Nil(t, inferred)
			}
			output, err := NewRuntime().Execute(Transform{SQL: statement, OutputKind: kind}, inputs)
			require.NoError(t, err)
			require.Equal(t, 1, output.RowCount)
			require.NoError(t, ValidateOutputContract(output, contract))
		}
	}
}

func TestSQLQualificationNumericBindings(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, expression, literal, want, wantError string }{
		{"decimal addition", "n + m", "0.1", "0.30000000000000004", ""},
		{"scientific integer", "n + 1", "1e3", "1001", ""},
		{"large integer", "n + 1", "9007199254740993", "9007199254740994", ""},
		{"mixed large integer fraction", "n + m", "9007199254740993", "", "integer precision"},
		{"overflow", "n + 1", "18446744073709551615", "", "64-bit range"},
		{"hostile exponent", "n + 1", "1e1000000000", "", "exponent exceeds supported range"},
		{"hostile precision", "n + 1", strings.Repeat("9", 129), "", "conversion exceeds supported range"},
	} {
		for _, representation := range []model.ContractRepresentation{model.ContractRepresentationJSON, model.ContractRepresentationCSV} {
			t.Run(tc.name+"/"+representation.String(), func(t *testing.T) {
				input := &pipepayload.Payload{Representation: representation, Document: json.RawMessage(`[{"n":` + tc.literal + `,"m":0.2}]`), CSV: "n,m\n" + tc.literal + ",0.2\n"}
				statement := "SELECT " + tc.expression + " AS v FROM input"
				contract := &pipepayload.Contract{Fields: []pipepayload.ContractField{{Name: "v", Type: model.ContractFieldTypeValueNumber, Required: true}}}
				require.NoError(t, ValidateOutputContractTypesWithInputSchema(statement, contract, model.DataSourceTypePostgres, map[string]model.ContractFieldType{"input.n": model.ContractFieldTypeValueNumber, "input.m": model.ContractFieldTypeValueNumber}))
				for _, kind := range []model.TransformOutputKind{model.TransformOutputKindRows, model.TransformOutputKindJSON, model.TransformOutputKindCSV} {
					output, err := NewRuntime(qualificationLimits()).Execute(Transform{SQL: statement, OutputKind: kind}, map[string]*pipepayload.Payload{"input": input})
					if tc.wantError != "" {
						require.ErrorContains(t, err, tc.wantError)
						continue
					}
					require.NoError(t, err)
					require.NoError(t, ValidateOutputContract(output, contract))
					switch kind {
					case model.TransformOutputKindUnknown, model.TransformOutputKindRows:
						encoded, err := json.Marshal(output.Rows)
						require.NoError(t, err)
						require.Equal(t, `[{"v":`+tc.want+`}]`, string(encoded))
					case model.TransformOutputKindJSON:
						require.Equal(t, `[{"v":`+tc.want+`}]`, string(output.Document))
					case model.TransformOutputKindCSV:
						require.Equal(t, "v\n"+tc.want+"\n", output.CSV)
					}
				}
			})
		}
	}
}

// Snapshot of CONFIG.md defaults, deliberately independent of server packages.
func qualificationLimits() Limits {
	return Limits{MaxWork: 1_000_000, MaxIntermediateRows: 10_000, MaxIntermediateBytes: 32 << 20, MaxOutputRows: 1_000, MaxOutputBytes: 10 << 20, MaxDepth: 16, Timeout: 10 * time.Second}
}
