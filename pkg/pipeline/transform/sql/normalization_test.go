package sql

import (
	"encoding/csv"
	"strings"
	"testing"

	"github.com/Braidplane/cli/pkg/model"
	pipepayload "github.com/Braidplane/cli/pkg/pipeline/payload"
	"github.com/stretchr/testify/require"
)

func TestPricingNormalizationAndExport(t *testing.T) {
	statement := `SELECT TRIM(supplier_sku) AS sku, title, product_url,
 CAST(TRIM(list_price) AS DECIMAL(18,2)) AS price,
 STR_TO_DATE(valid_from, '%d.%m.%Y') AS effective_from,
 DATE_FORMAT(STR_TO_DATE(valid_to, '%d.%m.%Y'), '%Y-%m-%d') AS effective_to,
 CASE WHEN NULLIF(TRIM(tag), '') IS NULL THEN 'standard' ELSE LOWER(TRIM(tag)) END AS category
 FROM input ORDER BY sku`
	input := &pipepayload.Payload{Representation: model.ContractRepresentationRows, Rows: []map[string]any{
		{"supplier_sku": " 0002 ", "title": "Żółć, \"duża\"\nlinia", "product_url": "https://example.test/p?id=0002&variant=a%2Bb", "list_price": " 2.675 ", "valid_from": "10.10.2026", "valid_to": "01.11.2026", "tag": " PROMO "},
		{"supplier_sku": "0001", "title": "zero", "product_url": "https://example.test/p?id=0001", "list_price": "0", "valid_from": "29.02.2024", "valid_to": "01.03.2024", "tag": " "},
	}}
	schema := map[string]model.ContractFieldType{}
	for name := range input.Rows[0] {
		schema[name] = model.ContractFieldTypeValueString
	}
	for _, kind := range []model.TransformOutputKind{model.TransformOutputKindRows, model.TransformOutputKindJSON, model.TransformOutputKindCSV} {
		t.Run(kind.String(), func(t *testing.T) {
			_, err := ValidateTransformSQL(statement)
			require.NoError(t, err)
			inferred, err := InferOutputContractWithInputSchema(statement, kind, model.DataSourceTypeHTTP, schema)
			require.NoError(t, err)
			require.NoError(t, ValidateOutputContractTypesWithInputSchemaForOutputKind(statement, kind, inferred, model.DataSourceTypeHTTP, schema))
			output, err := NewRuntime(qualificationLimits()).Execute(Transform{SQL: statement, OutputKind: kind, OutputContract: inferred}, map[string]*pipepayload.Payload{"input": input})
			require.NoError(t, err)
			require.NoError(t, ValidateOutputContract(output, inferred))
			if kind == model.TransformOutputKindCSV {
				want := "sku,title,product_url,price,effective_from,effective_to,category\n0001,zero,https://example.test/p?id=0001,0.00,2024-02-29,2024-03-01,standard\n0002,\"Żółć, \"\"duża\"\"\nlinia\",https://example.test/p?id=0002&variant=a%2Bb,2.68,2026-10-10,2026-11-01,promo\n"
				require.Equal(t, want, output.CSV)
				records, err := csv.NewReader(strings.NewReader(output.CSV)).ReadAll()
				require.NoError(t, err)
				require.Len(t, records, 3)
				require.Equal(t, input.Rows[0]["title"], records[2][1])
				require.Equal(t, input.Rows[0]["product_url"], records[2][2])
				empty, err := NewRuntime().Execute(Transform{SQL: statement, OutputKind: kind}, map[string]*pipepayload.Payload{"input": {Representation: model.ContractRepresentationRows}})
				require.NoError(t, err)
				require.Equal(t, strings.SplitN(want, "\n", 2)[0]+"\n", empty.CSV)
			}
		})
	}
}

func TestNormalizationNullBlankAndLazyBranches(t *testing.T) {
	for _, test := range []struct {
		expr string
		want any
	}{
		{"COALESCE(NULL, NULLIF(TRIM(s), ''), 'fallback')", "fallback"},
		{"COALESCE('', 'fallback')", ""},
		{"COALESCE(0, 1)", int64(0)},
		{"COALESCE('ok', STR_TO_DATE('bad', '%Y-%m-%d'))", "ok"},
		{"CASE WHEN s IS NULL THEN 'null' WHEN TRIM(s) = '' THEN 'blank' ELSE STR_TO_DATE(s, '%Y-%m-%d') END", "blank"},
		{"CASE WHEN NULL = 1 THEN 'yes' ELSE 'unknown' END", "unknown"},
		{"CASE WHEN s IS NULL THEN 'null' END", nil},
		{"UPPER('żółć')", "ŻÓŁĆ"},
		{"LOWER(NULL)", nil},
		{"DATE_FORMAT('2024-02-29', '%d.%m.%Y')", "29.02.2024"},
	} {
		t.Run(test.expr, func(t *testing.T) {
			statement := "SELECT " + test.expr + " AS v FROM input"
			contract, err := InferOutputContractWithInputSchema(statement, model.TransformOutputKindRows, model.DataSourceTypeS3, map[string]model.ContractFieldType{"s": model.ContractFieldTypeValueString})
			require.NoError(t, err)
			require.True(t, contract.Fields[0].Nullable)
			output, err := NewRuntime().Execute(Transform{SQL: statement}, map[string]*pipepayload.Payload{"input": {Representation: model.ContractRepresentationRows, Rows: []map[string]any{{"s": " "}}}})
			require.NoError(t, err)
			require.Equal(t, test.want, output.Rows[0]["v"])
			require.NoError(t, ValidateOutputContract(output, contract))
			if test.want == nil {
				contract.Fields[0].Nullable = false
				require.ErrorIs(t, ValidateOutputContract(output, contract), ErrOutputContractViolation)
			}
		})
	}
}

func TestNormalizationRejectsUnsupportedBeforeExecution(t *testing.T) {
	for _, expression := range []string{
		"STR_TO_DATE(s, '%m/%d/%Y')", "STR_TO_DATE(s, s)", "DATE_FORMAT(s, '%Y-%m-%d %H:%i:%s')", "LOWER(s,s)", "COALESCE(s)", "NULLIF(s)", "TRIM(LEADING 'x' FROM s)",
		"COALESCE(SUM(1), 0)", "CASE WHEN s IS NULL THEN COUNT(*) ELSE 0 END", "NULLIF(MAX(s), s)",
		"CASE s WHEN 'x' THEN 'yes' ELSE 'no' END", "CASE WHEN s LIKE 'x%' THEN 'yes' END", "CASE WHEN s IN ('x') THEN 'yes' END", "ABS(1)",
	} {
		t.Run(expression, func(t *testing.T) {
			statement := "SELECT " + expression + " AS v FROM input"
			_, err := ValidateTransformSQL(statement)
			require.Error(t, err)
			_, err = NewRuntime().Execute(Transform{SQL: statement}, map[string]*pipepayload.Payload{"input": {Representation: model.ContractRepresentationRows, Rows: []map[string]any{{"s": "x"}}}})
			require.Error(t, err)
		})
	}
	for _, expression := range []string{"LOWER(1)", "COALESCE(s,1)", "CASE WHEN s IS NULL THEN 1 ELSE s END"} {
		_, err := InferOutputContractWithInputSchema("SELECT "+expression+" AS v FROM input", model.TransformOutputKindRows, model.DataSourceTypeS3, map[string]model.ContractFieldType{"s": model.ContractFieldTypeValueString})
		require.ErrorIs(t, err, ErrContractType)
	}
}

func TestCalendarDateFailuresAreSafe(t *testing.T) {
	for _, value := range []string{"2026-02-29", "2026-04-31", "2026-1-01", "0000-01-01", "01/02/2026", "2026-10-10T00:00:00Z", "secret.invalid.date"} {
		_, err := NewRuntime().Execute(Transform{SQL: "SELECT STR_TO_DATE(s, '%Y-%m-%d') AS v FROM input"}, map[string]*pipepayload.Payload{"input": {Representation: model.ContractRepresentationRows, Rows: []map[string]any{{"s": value}}}})
		require.ErrorIs(t, err, ErrRuntimeProjectionExpressionUnsupported)
		require.NotContains(t, err.Error(), value)
	}
}

func TestNormalizationBudgets(t *testing.T) {
	input := map[string]*pipepayload.Payload{"input": {Representation: model.ContractRepresentationRows, Rows: []map[string]any{{"s": "value"}}}}
	for _, expression := range []string{"UPPER(s)", "COALESCE(NULL,NULL,NULL,s)", "CASE WHEN s IS NULL THEN 'a' WHEN s IS NULL THEN 'b' ELSE s END"} {
		_, err := NewRuntime(Limits{MaxWork: 1}).Execute(Transform{SQL: "SELECT " + expression + " AS v FROM input"}, input)
		require.ErrorIs(t, err, ErrWorkBudgetExceeded)
	}
	_, err := NewRuntime(Limits{MaxDepth: 2}).Execute(Transform{SQL: "SELECT LOWER(TRIM(UPPER(s))) AS v FROM input"}, input)
	require.ErrorIs(t, err, ErrDepthBudgetExceeded)
}
